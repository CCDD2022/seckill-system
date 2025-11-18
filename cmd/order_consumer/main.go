package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/CCDD2022/seckill-system/internal/dao"
	"github.com/CCDD2022/seckill-system/internal/dao/mysql"
	redisinit "github.com/CCDD2022/seckill-system/internal/dao/redis"
	"github.com/CCDD2022/seckill-system/internal/model"
	"github.com/CCDD2022/seckill-system/pkg/app"
	"github.com/CCDD2022/seckill-system/pkg/logger"
	"github.com/streadway/amqp"
)

// SeckillMessage 秒杀订单消息
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
	logger.Info("顺利连接数据库")

	// 创建OrderDao
	orderDao := dao.NewOrderDao(db)

	// Redis for idempotency
	rdb, err := redisinit.InitRedis(&cfg.Database.Redis)
	if err != nil {
		logger.Fatal("连接Redis失败", "err", err)
	}

	// 连接RabbitMQ
	mqConn, err := amqp.Dial(fmt.Sprintf("amqp://%s:%s@%s:%d/",
		cfg.MQ.User,
		cfg.MQ.Password,
		cfg.MQ.Host,
		cfg.MQ.Port))
	if err != nil {
		logger.Fatal("Failed to connect to RabbitMQ", "err", err)
	}
	defer mqConn.Close()

	mqChannel, err := mqConn.Channel()
	if err != nil {
		logger.Fatal("Failed to open a channel", "err", err)
	}
	defer mqChannel.Close()

	const exchangeName = "seckill.exchange"
	if err := mqChannel.ExchangeDeclare(exchangeName, "topic", true, false, false, false, nil); err != nil {
		logger.Fatal("Failed to declare exchange", "err", err)
	}
	q, err := mqChannel.QueueDeclare(
		"orders",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		logger.Fatal("Failed to declare a queue", "err", err)
	}
	if err := mqChannel.QueueBind(q.Name, "order.#", exchangeName, false, nil); err != nil {
		logger.Fatal("Failed to bind queue", "err", err)
	}

	// 设置消费者的QoS，允许批量预取
	err = mqChannel.Qos(
		cfg.MQ.ConsumerPrefetch, // prefetch count
		0,                       // prefetch size
		false,                   // global
	)
	if err != nil {
		logger.Fatal("Failed to set QoS", "err", err)
	}

	// 开始消费消息
	msgs, err := mqChannel.Consume(
		q.Name,
		"",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		logger.Fatal("Failed to register a consumer", "err", err)
	}

	logger.Info("Order Consumer started, waiting for messages...")

	forever := make(chan bool)

	// 批处理缓冲
	type item struct {
		d   amqp.Delivery
		msg SeckillMessage
	}
	batch := make([]item, 0, cfg.MQ.OrderBatchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		// 反序列化为订单
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
			// 逐条Nack重新入队（避免丢失）
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

	// 定时刷新
	ticker := time.NewTicker(time.Duration(cfg.MQ.OrderBatchIntervalMs) * time.Millisecond)
	defer ticker.Stop()

	go func() {
		for {
			select {
			case d, ok := <-msgs:
				if !ok {
					return
				}
				// 幂等：基于MessageId去重
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
					logger.Error("Failed to unmarshal message: ", "err", err)
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
	}()

	logger.Info("Waiting for messages. To exit press CTRL+C")
	<-forever
}
