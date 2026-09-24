package main

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/CCDD2022/seckill-system/internal/dao"
	"github.com/CCDD2022/seckill-system/internal/dao/mysql"
	redisinit "github.com/CCDD2022/seckill-system/internal/dao/redis"
	"github.com/CCDD2022/seckill-system/internal/model"
	"github.com/CCDD2022/seckill-system/internal/mq"
	"github.com/CCDD2022/seckill-system/pkg/app"
	"github.com/CCDD2022/seckill-system/pkg/logger"
	"github.com/streadway/amqp"
)

type SeckillMessage struct {
	UserID     int64   `json:"user_id"`
	ProductID  int64   `json:"product_id"`
	Quantity   int32   `json:"quantity"`
	TotalPrice float64 `json:"total_price"`
}

const (
	// 只处理创建订单的消息，避免与取消事件混淆
	orderCreateQueue = "order.create"
	orderCreateKey   = "order.create"
	// 死信交换机与队列配置
	dlxName = "seckill.dlx"
)

func main() {
	cfg := app.BootstrapApp()

	db, err := mysql.InitDB(&cfg.Database.Mysql)
	if err != nil {
		logger.Fatal("连接Mysql数据库失败", "err", err)
	}

	orderDao := dao.NewOrderDao(db)
	rdb, err := redisinit.InitRedis(&cfg.Database.Redis)
	if err != nil {
		logger.Fatal("连接Redis失败", "err", err)
	}
	defer rdb.Close()
	productDao := dao.NewProductDao(db, rdb)

	// 1. 初始化死信队列基础设施 (DLX + DLQ)
	if err := mq.EnsureOrderDeadLetters(&cfg.MQ); err != nil {
		logger.Fatal("setup dlq failed", "err", err)
	}

	// 2. 配置主队列参数，指定死信交换机
	args := amqp.Table{
		"x-dead-letter-exchange": dlxName,
	}

	// 3. 启动消费者，绑定 order.create，避免误消费 order.canceled
	conn, consumerCh, msgs, err := mq.NewConsumerChannel(&cfg.MQ, orderCreateQueue, orderCreateKey, "seckill.exchange", true, cfg.MQ.ConsumerPrefetch, args)
	if err != nil {
		logger.Fatal("init consumer channel failed", "err", err)
	}
	defer mq.CloseConsumer(conn, consumerCh)

	logger.Info("Order Create Consumer started with DLQ support")

	for d := range msgs {
		var m SeckillMessage
		if err := json.Unmarshal(d.Body, &m); err != nil {
			logger.Error("订单创建消息解析失败", "err", err)
			// 解析失败属于不可恢复错误，直接丢入死信队列，不重试
			_ = d.Nack(false, false)
			continue
		}
		if d.MessageId == "" || m.UserID <= 0 || m.ProductID <= 0 || m.Quantity <= 0 || m.TotalPrice < 0 || math.IsNaN(m.TotalPrice) || math.IsInf(m.TotalPrice, 0) {
			logger.Error("invalid order creation delivery", "message_id", d.MessageId)
			_ = d.Nack(false, false)
			continue
		}
		compensated, err := productDao.IsReservationCompensated(context.Background(), d.MessageId)
		if err != nil {
			// Without this marker check, a manually compensated reservation
			// could be materialized into an order on a delayed redelivery.
			logger.Error("compensation marker unavailable; delivery retained", "message_id", d.MessageId, "err", err)
			_ = d.Nack(false, true)
			time.Sleep(time.Second)
			continue
		}
		if compensated {
			logger.Warn("compensated reservation delivery refused", "message_id", d.MessageId)
			_ = d.Nack(false, false)
			continue
		}
		messageID := d.MessageId
		order := &model.Order{
			UserID:          m.UserID,
			ProductID:       m.ProductID,
			Quantity:        m.Quantity,
			TotalPrice:      m.TotalPrice,
			Status:          model.OrderStatusPending,
			SourceMessageID: &messageID,
		}
		created, err := orderDao.CreateOrderOnce(context.Background(), order)
		if err != nil {
			if errors.Is(err, dao.ErrOrderConflict) {
				logger.Error("permanent order delivery conflict; dead lettering", "message_id", d.MessageId, "err", err)
				_ = d.Nack(false, false)
			} else {
				// MySQL outages are transient. Keep the persistent broker message
				// available for recovery instead of archiving every failed attempt.
				logger.Error("order database unavailable; will retry", "message_id", d.MessageId, "err", err)
				time.Sleep(time.Second)
				_ = d.Nack(false, true)
			}
			continue
		}
		if !created {
			logger.Info("duplicate order delivery", "message_id", d.MessageId)
		}
		if err := d.Ack(false); err != nil {
			logger.Error("order delivery ACK failed; broker will redeliver", "message_id", d.MessageId, "err", err)
		}
	}
}
