// ProductConsumer RabbitMQ 订单取消事件消费入口
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/CCDD2022/seckill-system/internal/dao"
	"github.com/CCDD2022/seckill-system/internal/dao/mysql"
	rds "github.com/CCDD2022/seckill-system/internal/dao/redis"
	"github.com/CCDD2022/seckill-system/pkg/app"
	"github.com/CCDD2022/seckill-system/pkg/logger"
	"github.com/streadway/amqp"
)

type OrderCanceledEvent struct {
	EventID    string `json:"event_id"`
	OccurredAt int64  `json:"occurred_at"`
	OrderID    int64  `json:"order_id"`
	UserID     int64  `json:"user_id"`
	ProductID  int64  `json:"product_id"`
	Quantity   int32  `json:"quantity"`
}

const (
	queueOrderCanceled = "order.canceled"
	eventDedupKeyFmt   = "event:order.canceled:%s"
)

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
	productDao := dao.NewProductDao(db, rdb)

	mqURL := fmt.Sprintf("amqp://%s:%s@%s:%d/", cfg.MQ.User, cfg.MQ.Password, cfg.MQ.Host, cfg.MQ.Port)
	mqConn, err := amqp.Dial(mqURL)
	if err != nil {
		logger.Fatal("连接RabbitMQ失败", "err", err)
	}
	defer mqConn.Close()
	ch, err := mqConn.Channel()
	if err != nil {
		logger.Fatal("打开RabbitMQ通道失败", "err", err)
	}
	defer ch.Close()
	q, err := ch.QueueDeclare(queueOrderCanceled, true, false, false, false, nil)
	if err != nil {
		logger.Fatal("声明队列失败", "err", err)
	}
	if err := ch.Qos(1, 0, false); err != nil {
		logger.Fatal("设置QoS失败", "err", err)
	}
	msgs, err := ch.Consume(q.Name, "", false, false, false, false, nil)
	if err != nil {
		logger.Fatal("注册消费者失败", "err", err)
	}

	logger.Info("Product Consumer started, waiting for order.canceled events...")
	forever := make(chan bool)

	go func() {
		for d := range msgs {
			var evt OrderCanceledEvent
			if err := json.Unmarshal(d.Body, &evt); err != nil {
				logger.Error("取消事件解析失败", "err", err)
				// 拒绝消费某条消息
				// multiple 是否拒绝所有未确认的消息
				// requeue 是否重新入队
				d.Nack(false, false)
				continue
			}
			// 幂等去重（Redis SETNX）
			dedupKey := fmt.Sprintf(eventDedupKeyFmt, evt.EventID)
			ok, derr := rdb.SetNX(context.Background(), dedupKey, 1, 24*time.Hour).Result()
			if derr != nil {
				logger.Error("去重键写入失败", "err", derr)
				d.Nack(false, true)
				continue
			}
			if !ok {
				// 这个代表已经处理过该事件
				d.Ack(false)
				continue
			}
			// 归还库存
			if evt.Quantity > 0 && evt.ProductID > 0 {
				if err := productDao.ReturnStock(context.Background(), evt.ProductID, evt.Quantity); err != nil {
					logger.Error("归还库存失败", "product_id", evt.ProductID, "qty", evt.Quantity, "err", err)
					d.Nack(false, true)
					_ = rdb.Del(context.Background(), dedupKey).Err()
					continue
				}
				logger.Info("归还库存成功", "product_id", evt.ProductID, "qty", evt.Quantity, "order_id", evt.OrderID)
			}
			d.Ack(false)
		}
	}()
	<-forever
}
