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
	"github.com/CCDD2022/seckill-system/internal/model"
	"github.com/CCDD2022/seckill-system/internal/mq"
	"github.com/CCDD2022/seckill-system/pkg/app"
	"github.com/CCDD2022/seckill-system/pkg/logger"
	"github.com/streadway/amqp"
	"gorm.io/gorm"
)

type createPayload struct {
	UserID     int64   `json:"user_id"`
	ProductID  int64   `json:"product_id"`
	Quantity   int32   `json:"quantity"`
	TotalPrice float64 `json:"total_price"`
}

type cancelPayload struct {
	EventID   string `json:"event_id"`
	OrderID   int64  `json:"order_id"`
	UserID    int64  `json:"user_id"`
	ProductID int64  `json:"product_id"`
	Quantity  int32  `json:"quantity"`
}

func main() {
	id := flag.Int64("id", 0, "archived dead letter ID to replay")
	flag.Parse()
	if *id <= 0 || len(flag.Args()) != 0 {
		fmt.Fprintln(os.Stderr, "usage: replay_dead_letter --id <positive archive ID>")
		os.Exit(2)
	}
	if err := replay(*id); err != nil {
		logger.Error("dead letter replay failed", "archive_id", *id, "err", err)
		os.Exit(1)
	}
}

func replay(id int64) error {
	cfg := app.BootstrapApp()
	db, err := mysql.InitDB(&cfg.Database.Mysql)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var archived model.DeadLetter
	if err := db.WithContext(ctx).First(&archived, "id = ?", id).Error; err != nil {
		return fmt.Errorf("load archived delivery: %w", err)
	}
	if archived.CompensatedAt != nil {
		return fmt.Errorf("archive %d was already compensated and must not be replayed", id)
	}
	if err := validateFormat(archived); err != nil {
		return err
	}
	if err := validateBusiness(ctx, db, archived); err != nil {
		return err
	}
	if err := checkTargetQueue(&cfg.MQ, archived.RoutingKey); err != nil {
		return err
	}
	producer, err := mq.Init(&cfg.MQ)
	if err != nil {
		return err
	}
	defer producer.Close()
	if err := producer.EnsureBaseTopology(); err != nil {
		return err
	}
	if err := producer.PublishConfirmedWithID(ctx, "seckill.exchange", archived.RoutingKey, archived.Body, archived.MessageID); err != nil {
		return fmt.Errorf("replay not confirmed: %w", err)
	}
	now := time.Now()
	if err := db.WithContext(ctx).Model(&model.DeadLetter{}).Where("id = ?", id).
		Updates(map[string]any{"replay_count": gorm.Expr("replay_count + 1"), "last_replayed_at": now}).Error; err != nil {
		return fmt.Errorf("broker confirmed replay, but archive status update failed (retry is safe): %w", err)
	}
	logger.Info("dead letter replay confirmed", "archive_id", id, "message_id", archived.MessageID, "routing_key", archived.RoutingKey)
	return nil
}

func validateFormat(item model.DeadLetter) error {
	if item.MessageID == "" || len(item.MessageID) > 255 {
		return errors.New("archived delivery has invalid message ID")
	}
	switch item.RoutingKey {
	case "order.create":
		var payload createPayload
		if err := json.Unmarshal(item.Body, &payload); err != nil {
			return fmt.Errorf("invalid order creation JSON: %w", err)
		}
		if payload.UserID <= 0 || payload.ProductID <= 0 || payload.Quantity <= 0 || payload.TotalPrice < 0 || math.IsNaN(payload.TotalPrice) || math.IsInf(payload.TotalPrice, 0) {
			return errors.New("invalid order creation fields")
		}
	case "order.canceled":
		var payload cancelPayload
		if err := json.Unmarshal(item.Body, &payload); err != nil {
			return fmt.Errorf("invalid cancellation JSON: %w", err)
		}
		if payload.EventID != item.MessageID || payload.OrderID <= 0 || payload.UserID <= 0 || payload.ProductID <= 0 || payload.Quantity <= 0 {
			return errors.New("invalid cancellation fields or event ID")
		}
	default:
		return fmt.Errorf("unsupported replay routing key %q", item.RoutingKey)
	}
	return nil
}

func validateBusiness(ctx context.Context, db *gorm.DB, item model.DeadLetter) error {
	if item.RoutingKey == "order.create" {
		var payload createPayload
		_ = json.Unmarshal(item.Body, &payload) // validateFormat already succeeded
		var existing model.Order
		err := db.WithContext(ctx).Where("user_id = ? AND product_id = ?", payload.UserID, payload.ProductID).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if existing.SourceMessageID == nil || *existing.SourceMessageID != item.MessageID || existing.Quantity != payload.Quantity || existing.TotalPrice != payload.TotalPrice {
			return fmt.Errorf("archived create conflicts with existing order %d", existing.ID)
		}
		return nil
	}
	var payload cancelPayload
	_ = json.Unmarshal(item.Body, &payload)
	existing, err := dao.NewOrderDao(db).GetOrderByID(ctx, payload.OrderID)
	if err != nil {
		return err
	}
	if existing.Status != model.OrderStatusCancelled || existing.UserID != payload.UserID || existing.ProductID != payload.ProductID || existing.Quantity != payload.Quantity {
		return fmt.Errorf("archived cancellation does not match canceled order %d", payload.OrderID)
	}
	return nil
}

func checkTargetQueue(cfg *config.MQConfig, queue string) error {
	url := fmt.Sprintf("amqp://%s:%s@%s:%d/", cfg.User, cfg.Password, cfg.Host, cfg.Port)
	conn, err := amqp.Dial(url)
	if err != nil {
		return err
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()
	if _, err := ch.QueueInspect(queue); err != nil {
		return fmt.Errorf("target queue %q is unavailable: %w", queue, err)
	}
	return nil
}
