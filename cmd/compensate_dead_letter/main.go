package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/CCDD2022/seckill-system/config"
	"github.com/CCDD2022/seckill-system/internal/dao"
	"github.com/CCDD2022/seckill-system/internal/dao/mysql"
	redisinit "github.com/CCDD2022/seckill-system/internal/dao/redis"
	"github.com/CCDD2022/seckill-system/internal/model"
	"github.com/CCDD2022/seckill-system/pkg/app"
	"github.com/CCDD2022/seckill-system/pkg/logger"
	"github.com/redis/go-redis/v9"
	"github.com/streadway/amqp"
	"gorm.io/gorm"
)

type orderCreatePayload struct {
	UserID     int64   `json:"user_id"`
	ProductID  int64   `json:"product_id"`
	Quantity   int32   `json:"quantity"`
	TotalPrice float64 `json:"total_price"`
}

func main() {
	id := flag.Int64("id", 0, "archived order.create dead letter ID")
	stopped := flag.Bool("confirm-stopped", false, "confirm reservation-relay and order-create-consumer are stopped")
	flag.Parse()
	if *id <= 0 || !*stopped || len(flag.Args()) != 0 {
		fmt.Fprintln(os.Stderr, "Stop reservation-relay and order-create-consumer first. Usage: compensate_dead_letter --id <dead_letters.id> --confirm-stopped")
		os.Exit(2)
	}
	if err := compensate(*id); err != nil {
		logger.Error("dead letter compensation refused; keep relay and consumer stopped until resolved", "archive_id", *id, "err", err)
		os.Exit(1)
	}
}

func compensate(id int64) error {
	cfg := app.BootstrapApp()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db, err := mysql.InitDB(&cfg.Database.Mysql)
	if err != nil {
		return err
	}
	rdb, err := redisinit.InitRedis(&cfg.Database.Redis)
	if err != nil {
		return err
	}
	defer rdb.Close()
	var archived model.DeadLetter
	if err := db.WithContext(ctx).First(&archived, "id = ?", id).Error; err != nil {
		return fmt.Errorf("load archived dead letter: %w", err)
	}
	payload, err := validateArchivedCreate(archived)
	if err != nil {
		return err
	}
	if err := verifyQueueStoppedAndEmpty(&cfg.MQ); err != nil {
		return err
	}
	// Observe the queue twice across a short interval. This is an operational
	// safety check, not a replacement for stopping both services.
	time.Sleep(time.Second)
	if err := verifyQueueStoppedAndEmpty(&cfg.MQ); err != nil {
		return err
	}
	if err := verifyNoStreamEvent(ctx, rdb, archived.MessageID); err != nil {
		return err
	}
	if err := verifyNoOrder(ctx, db, payload); err != nil {
		return err
	}
	if err := verifyQueueStoppedAndEmpty(&cfg.MQ); err != nil {
		return err
	}
	productDao := dao.NewProductDao(db, rdb)
	changed, err := productDao.CompensateFailedReservation(ctx, payload.ProductID, payload.UserID, payload.Quantity, archived.MessageID)
	if err != nil {
		return err
	}
	if changed {
		logger.Warn("failed order reservation compensated; user participation marker retained", "archive_id", id, "message_id", archived.MessageID, "product_id", payload.ProductID, "quantity", payload.Quantity)
	} else {
		logger.Info("reservation was already compensated", "archive_id", id, "message_id", archived.MessageID)
	}
	if err := db.WithContext(ctx).Model(&model.DeadLetter{}).Where("id = ?", id).
		Update("compensated_at", time.Now()).Error; err != nil {
		return fmt.Errorf("stock compensation completed but archive status update failed; retry this id safely: %w", err)
	}
	return nil
}

func validateArchivedCreate(item model.DeadLetter) (orderCreatePayload, error) {
	var payload orderCreatePayload
	if item.RoutingKey != "order.create" {
		return payload, fmt.Errorf("archive %d is %q, only order.create may be compensated", item.ID, item.RoutingKey)
	}
	if err := json.Unmarshal(item.Body, &payload); err != nil {
		return payload, fmt.Errorf("invalid archived order payload: %w", err)
	}
	if payload.UserID <= 0 || payload.ProductID <= 0 || payload.Quantity <= 0 ||
		payload.TotalPrice <= 0 || math.IsNaN(payload.TotalPrice) || math.IsInf(payload.TotalPrice, 0) {
		return payload, errors.New("archived order payload has invalid business fields")
	}
	expectedID := fmt.Sprintf("create:%d:%d", payload.UserID, payload.ProductID)
	if item.MessageID != expectedID {
		return payload, fmt.Errorf("archive message ID %q does not match payload (expected %q)", item.MessageID, expectedID)
	}
	return payload, nil
}

func verifyNoOrder(ctx context.Context, db *gorm.DB, payload orderCreatePayload) error {
	var product model.Product
	if err := db.WithContext(ctx).Select("id").First(&product, "id = ?", payload.ProductID).Error; err != nil {
		return fmt.Errorf("verify compensated product exists: %w", err)
	}
	var count int64
	if err := db.WithContext(ctx).Model(&model.Order{}).
		Where("user_id = ? AND product_id = ?", payload.UserID, payload.ProductID).
		Count(&count).Error; err != nil {
		return fmt.Errorf("verify MySQL order absence: %w", err)
	}
	if count != 0 {
		return fmt.Errorf("MySQL already has %d order(s) for user %d/product %d; stock cannot be compensated", count, payload.UserID, payload.ProductID)
	}
	return nil
}

func verifyQueueStoppedAndEmpty(cfg *config.MQConfig) error {
	url := fmt.Sprintf("amqp://%s:%s@%s:%d/", cfg.User, cfg.Password, cfg.Host, cfg.Port)
	conn, err := amqp.Dial(url)
	if err != nil {
		return fmt.Errorf("inspect RabbitMQ order.create queue (stop relay and consumer first): %w", err)
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()
	queue, err := ch.QueueInspect("order.create")
	if err != nil {
		return fmt.Errorf("inspect order.create queue: %w", err)
	}
	if queue.Consumers != 0 || queue.Messages != 0 {
		return fmt.Errorf("order.create has %d consumer(s) and %d queued message(s); stop reservation-relay and order-create-consumer and drain/resolve the queue first", queue.Consumers, queue.Messages)
	}
	return nil
}

func verifyNoStreamEvent(ctx context.Context, rdb redis.UniversalClient, messageID string) error {
	return verifyNoStreamEventIn(ctx, rdb, dao.ReservationStream, messageID)
}

func verifyNoStreamEventIn(ctx context.Context, rdb redis.UniversalClient, stream, messageID string) error {
	start := "-"
	for {
		items, err := rdb.XRangeN(ctx, stream, start, "+", 1000).Result()
		if err != nil {
			return fmt.Errorf("inspect Redis reservation stream: %w", err)
		}
		if len(items) == 0 {
			return nil
		}
		for _, item := range items {
			if fmt.Sprint(item.Values["message_id"]) == messageID {
				return fmt.Errorf("Redis reservation stream still contains %q at %s; stop relay and resolve that event before compensation", messageID, item.ID)
			}
		}
		start = "(" + items[len(items)-1].ID
	}
}
