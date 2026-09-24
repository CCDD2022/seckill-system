package main

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/CCDD2022/seckill-system/internal/dao"
	"github.com/CCDD2022/seckill-system/internal/dao/mysql"
	rds "github.com/CCDD2022/seckill-system/internal/dao/redis"
	"github.com/CCDD2022/seckill-system/internal/model"
	"github.com/CCDD2022/seckill-system/internal/mq"
	"github.com/CCDD2022/seckill-system/pkg/app"
	"github.com/CCDD2022/seckill-system/pkg/logger"
	"github.com/streadway/amqp"
	"gorm.io/gorm"
)

type OrderCanceledEvent struct {
	EventID    string `json:"event_id"`
	OccurredAt int64  `json:"occurred_at"`
	OrderID    int64  `json:"order_id"`
	UserID     int64  `json:"user_id"`
	ProductID  int64  `json:"product_id"`
	Quantity   int32  `json:"quantity"`
}

func main() {
	cfg := app.BootstrapApp()
	db, err := mysql.InitDB(&cfg.Database.Mysql)
	if err != nil {
		logger.Fatal("连接Mysql数据库失败", "err", err)
	}
	rdb, err := rds.InitRedis(&cfg.Database.Redis)
	if err != nil {
		logger.Fatal("连接Redis失败", "err", err)
	}
	orderDao := dao.NewOrderDao(db)
	productDao := dao.NewProductDao(db, rdb)

	if err := mq.EnsureOrderDeadLetters(&cfg.MQ); err != nil {
		logger.Fatal("setup dead letter queue failed", "err", err)
	}
	args := amqp.Table{"x-dead-letter-exchange": "seckill.dlx"}
	conn, ch, msgs, err := mq.NewConsumerChannel(&cfg.MQ, "order.canceled", "order.canceled", "seckill.exchange", true, cfg.MQ.ConsumerPrefetch, args)
	if err != nil {
		logger.Fatal("init cancellation consumer failed", "err", err)
	}
	defer mq.CloseConsumer(conn, ch)
	logger.Info("Order cancellation consumer started")

	for d := range msgs {
		var evt OrderCanceledEvent
		if err := json.Unmarshal(d.Body, &evt); err != nil || evt.EventID == "" || evt.OrderID <= 0 || evt.UserID <= 0 || evt.ProductID <= 0 || evt.Quantity <= 0 || d.MessageId != evt.EventID {
			logger.Error("invalid cancellation event", "message_id", d.MessageId, "err", err)
			_ = d.Nack(false, false)
			continue
		}
		// Only a committed cancellation may restore stock. This also prevents
		// a malformed/forged event from incrementing an unrelated product.
		ord, err := orderDao.GetOrderByID(context.Background(), evt.OrderID)
		if err != nil {
			logger.Error("lookup canceled order failed", "order_id", evt.OrderID, "err", err)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				_ = d.Nack(false, false)
			} else {
				time.Sleep(time.Second)
				_ = d.Nack(false, true)
			}
			continue
		}
		if ord.Status != model.OrderStatusCancelled || ord.UserID != evt.UserID || ord.ProductID != evt.ProductID || ord.Quantity != evt.Quantity {
			logger.Error("cancellation event does not match canceled order", "event_id", evt.EventID)
			_ = d.Nack(false, false)
			continue
		}
		// ReturnStockOnce updates stock, dirty tracking, and the event marker
		// in one Redis Lua operation. ACK follows only after that operation.
		if err := productDao.ReturnStockOnce(context.Background(), evt.ProductID, evt.Quantity, evt.EventID); err != nil {
			logger.Error("restore stock failed", "event_id", evt.EventID, "err", err)
			if errors.Is(err, dao.ErrStockNotInitialized) || errors.Is(err, dao.ErrStockStateInvalid) {
				_ = d.Nack(false, false)
			} else {
				time.Sleep(time.Second)
				_ = d.Nack(false, true)
			}
			continue
		}
		if err := d.Ack(false); err != nil {
			logger.Error("cancellation ACK failed; broker will redeliver", "event_id", evt.EventID, "err", err)
		}
	}
}
