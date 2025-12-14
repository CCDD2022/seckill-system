package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/CCDD2022/seckill-system/config"
	"github.com/CCDD2022/seckill-system/internal/dao/mysql"
	redisinit "github.com/CCDD2022/seckill-system/internal/dao/redis"
	"github.com/CCDD2022/seckill-system/internal/model"
	"github.com/CCDD2022/seckill-system/internal/mq"
	"github.com/CCDD2022/seckill-system/pkg/app"
	"github.com/CCDD2022/seckill-system/pkg/logger"
	"github.com/streadway/amqp"
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
	// 死信交换机与队列配置
	dlxName = "seckill.dlx"
	dlqName = "order.create.dlq"
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

	// 1. 初始化死信队列基础设施 (DLX + DLQ)
	if err := setupDLQ(&cfg.MQ); err != nil {
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
			// 解析失败属于不可恢复错误，直接丢入死信队列，不重试
			_ = d.Nack(false, false)
			continue
		}
		// 事务：仅创建订单（库存扣减已由Redis+Reconciler保障）
		err = db.Transaction(func(tx *gorm.DB) error {
			// 1. 激进派策略：不再扣减MySQL库存，直接信任Redis的扣减结果
			// 优势：数据库写入性能翻倍（少了一次行锁竞争和Update操作）
			// 风险：如果Redis挂了且数据丢失，MySQL库存会偏多（少卖），但绝不会超卖（因为Redis挡住了）

			// 2. 创建订单
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
			// 关键修改：requeue=false，将失败消息投递到死信队列，防止无限循环
			_ = d.Nack(false, false)
			rdb.Del(context.Background(), key) // 消费失败，删除幂等key，允许重试（如果后续有人处理死信队列并重发）
			continue
		}
		_ = d.Ack(false)
	}
}

// setupDLQ 声明死信交换机和死信队列
func setupDLQ(cfg *config.MQConfig) error {
	url := fmt.Sprintf("amqp://%s:%s@%s:%d/", cfg.User, cfg.Password, cfg.Host, cfg.Port)
	conn, err := amqp.Dial(url)
	if err != nil {
		return fmt.Errorf("dial rabbitmq failed: %w", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("open channel failed: %w", err)
	}
	defer ch.Close()

	// 1. 声明死信交换机 (修改为 Topic 类型，以便接收所有死信)
	if err := ch.ExchangeDeclare(dlxName, "topic", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare dlx failed: %w", err)
	}

	// 2. 声明死信队列
	if _, err := ch.QueueDeclare(dlqName, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare dlq failed: %w", err)
	}

	// 3. 绑定死信队列到死信交换机
	// 使用 "#" 接收所有死信消息（包括 order.create 和 order.canceled）
	if err := ch.QueueBind(dlqName, "#", dlxName, false, nil); err != nil {
		return fmt.Errorf("bind dlq failed: %w", err)
	}

	return nil
}
