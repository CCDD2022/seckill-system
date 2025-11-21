package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/CCDD2022/seckill-system/internal/dao/mysql"
	redisinit "github.com/CCDD2022/seckill-system/internal/dao/redis"
	"github.com/CCDD2022/seckill-system/internal/model"
	"github.com/CCDD2022/seckill-system/internal/mq"
	"github.com/CCDD2022/seckill-system/pkg/app"
	"github.com/CCDD2022/seckill-system/pkg/logger"
	"gorm.io/gorm"
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
)

func main() {
	cfg := app.BootstrapApp()

	db, err := mysql.InitDB(&cfg.Database.Mysql)
	if err != nil {
		logger.Fatal("连接Mysql数据库失败", "err", err)
	}

	rdb, err := redisinit.InitRedis(&cfg.Database.Redis)
	if err != nil {
		logger.Fatal("连接Redis失败", "err", err)
	}

	// 仅绑定 order.create，避免误消费 order.canceled
	conn, consumerCh, msgs, err := mq.NewConsumerChannel(&cfg.MQ, orderCreateQueue, orderCreateKey, "seckill.exchange", true, cfg.MQ.ConsumerPrefetch)
	if err != nil {
		logger.Fatal("init consumer channel failed", "err", err)
	}
	defer mq.CloseConsumer(conn, consumerCh)

	for d := range msgs {
		key := "seckill:msg:done:" + d.MessageId
		// 幂等：如果MessageId存在则用Redis去重
		if d.MessageId != "" {
			added, _ := rdb.SetNX(context.Background(), key, 1, 30*time.Minute).Result()
			if !added {
				// 如果已经存在，说明已经处理过，直接ACK
				logger.Error("Duplicate message detected, skipping", "message_id", d.MessageId)
				_ = d.Ack(false)
				continue
			}
		}
		var m SeckillMessage
		if err := json.Unmarshal(d.Body, &m); err != nil {
			logger.Error("订单创建消息解析失败", "err", err)
			_ = d.Nack(false, false)
			continue
		}
		// 事务：扣减真实库存 + 创建订单
		err = db.Transaction(func(tx *gorm.DB) error {
			res := tx.Model(&model.Product{}).
				Where("id = ? AND stock >= ?", m.ProductID, m.Quantity).
				Update("stock", gorm.Expr("stock - ?", m.Quantity))
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return fmt.Errorf("库存不足或商品不存在: product_id=%d", m.ProductID)
			}
			// 创建订单
			order := &model.Order{
				UserID:     m.UserID,
				ProductID:  m.ProductID,
				Quantity:   m.Quantity,
				TotalPrice: m.TotalPrice,
				Status:     model.OrderStatusPending,
			}
			return tx.Create(order).Error
		})
		if err != nil {
			logger.Error("处理消息失败", "err", err)
			_ = d.Nack(false, true)
			rdb.Del(context.Background(), key)
			continue
		}
		_ = d.Ack(false)
	}
}
