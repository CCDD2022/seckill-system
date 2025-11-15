package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/CCDD2022/seckill-system/internal/dao"
	"github.com/CCDD2022/seckill-system/internal/dao/mysql"
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
		logger.Error("连接Mysql数据库失败: ", "err", err)
		log.Fatal(err)
	}
	logger.Info("顺利连接数据库")

	// 创建OrderDao
	orderDao := dao.NewOrderDao(db)

	// 连接RabbitMQ
	mqConn, err := amqp.Dial(fmt.Sprintf("amqp://%s:%s@%s:%d/",
		cfg.MQ.User,
		cfg.MQ.Password,
		cfg.MQ.Host,
		cfg.MQ.Port))
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer mqConn.Close()

	mqChannel, err := mqConn.Channel()
	if err != nil {
		log.Fatalf("Failed to open a channel: %v", err)
	}
	defer mqChannel.Close()

	// 声明队列
	q, err := mqChannel.QueueDeclare(
		"order.create", // name
		true,           // durable
		false,          // delete when unused
		false,          // exclusive
		false,          // no-wait
		nil,            // arguments
	)
	if err != nil {
		log.Fatalf("Failed to declare a queue: %v", err)
	}

	// 设置消费者的QoS，一次只处理一条消息
	err = mqChannel.Qos(
		1,     // prefetch count
		0,     // prefetch size
		false, // global
	)
	if err != nil {
		log.Fatalf("Failed to set QoS: %v", err)
	}

	// 开始消费消息
	msgs, err := mqChannel.Consume(
		q.Name, // queue
		"",     // consumer
		false,  // auto-ack
		false,  // exclusive
		false,  // no-local
		false,  // no-wait
		nil,    // args
	)
	if err != nil {
		log.Fatalf("Failed to register a consumer: %v", err)
	}

	logger.Info("Order Consumer started, waiting for messages...")

	forever := make(chan bool)

	go func() {
		for d := range msgs {
			logger.Info("Received a message")

			var msg SeckillMessage
			err := json.Unmarshal(d.Body, &msg)
			if err != nil {
				logger.Error("Failed to unmarshal message: ", "err", err)
				d.Nack(false, false) // 拒绝消息，不重新入队
				continue
			}

			// 创建订单
			order := &model.Order{
				UserID:     msg.UserID,
				ProductID:  msg.ProductID,
				Quantity:   msg.Quantity,
				TotalPrice: msg.TotalPrice,
				Status:     model.OrderStatusPending,
			}

			ctx := context.Background()
			err = orderDao.CreateOrder(ctx, order)
			if err != nil {
				logger.Error("Failed to create order: ", "err", err)
				d.Nack(false, true) // 拒绝消息，重新入队
				continue
			}

			logger.Info(fmt.Sprintf("Order created successfully: order_id=%d, user_id=%d, product_id=%d",
				order.ID, msg.UserID, msg.ProductID))

			// 确认消息
			d.Ack(false)
		}
	}()

	logger.Info("Waiting for messages. To exit press CTRL+C")
	<-forever
}
