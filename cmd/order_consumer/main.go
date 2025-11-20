package main

import (
	"context"
	"encoding/json"
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

func main() {
	cfg := app.BootstrapApp()

	// 连接数据库
	db, err := mysql.InitDB(&cfg.Database.Mysql)
	if err != nil {
		logger.Fatal("连接Mysql数据库失败", "err", err)
	}
	orderDao := dao.NewOrderDao(db)

	rdb, err := redisinit.InitRedis(&cfg.Database.Redis)
	if err != nil {
		logger.Fatal("连接Redis失败", "err", err)
	}

	// 初始化消费者
	conn, consumerCh, msgs, err := mq.NewConsumerChannel(&cfg.MQ, "orders", "order.#", "seckill.exchange", true, cfg.MQ.ConsumerPrefetch)
	if err != nil {
		logger.Fatal("init consumer channel failed", "err", err)
	}
	defer mq.CloseConsumer(conn, consumerCh)

	type item struct {
		d   amqp.Delivery
		msg SeckillMessage
	}
	batch := make([]item, 0, cfg.MQ.OrderBatchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		orders := make([]*model.Order, 0, len(batch))
		for _, it := range batch {
			orders = append(orders, &model.Order{
				UserID:     it.msg.UserID,
				ProductID:  it.msg.ProductID,
				Quantity:   it.msg.Quantity,
				TotalPrice: it.msg.TotalPrice,
				Status:     model.OrderStatusPending,
			})
		}
		ctx := context.Background()
		if err := orderDao.CreateOrdersBatch(ctx, orders); err != nil {
			logger.Error("Batch create orders failed", "err", err)
			for _, it := range batch {
				_ = it.d.Nack(false, true)
			}
		} else {
			for _, it := range batch {
				_ = it.d.Ack(false)
			}
		}
		batch = batch[:0]
	}

	ticker := time.NewTicker(time.Duration(cfg.MQ.OrderBatchIntervalMs) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case d, ok := <-msgs:
			if !ok {
				return
			}
			if d.MessageId != "" {
				key := "seckill:msg:done:" + d.MessageId
				added, _ := rdb.SetNX(context.Background(), key, 1, 30*time.Minute).Result()
				if !added {
					_ = d.Ack(false)
					continue
				}
			}
			var m SeckillMessage
			if err := json.Unmarshal(d.Body, &m); err != nil {
				logger.Error("Failed to unmarshal message", "err", err)
				_ = d.Nack(false, false)
				continue
			}
			batch = append(batch, item{d: d, msg: m})
			if len(batch) >= cfg.MQ.OrderBatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}
