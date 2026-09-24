package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"github.com/CCDD2022/seckill-system/internal/dao/mysql"
	"github.com/CCDD2022/seckill-system/internal/model"
	"github.com/CCDD2022/seckill-system/internal/mq"
	"github.com/CCDD2022/seckill-system/pkg/app"
	"github.com/CCDD2022/seckill-system/pkg/logger"
	"github.com/streadway/amqp"
	"gorm.io/gorm/clause"
)

func main() {
	cfg := app.BootstrapApp()
	db, err := mysql.InitDB(&cfg.Database.Mysql)
	if err != nil {
		logger.Fatal("DLQ archive database init failed", "err", err)
	}
	if err := mq.EnsureOrderDeadLetters(&cfg.MQ); err != nil {
		logger.Fatal("DLQ topology init failed", "err", err)
	}
	conn, ch, msgs, err := mq.NewConsumerChannel(&cfg.MQ, "order.create.dlq", "", "", true, 10, nil)
	if err != nil {
		logger.Fatal("DLQ consumer init failed", "err", err)
	}
	defer mq.CloseConsumer(conn, ch)
	logger.Info("DLQ archive consumer started")

	for d := range msgs {
		record := archiveRecord(d)
		if err := db.WithContext(context.Background()).Clauses(clause.OnConflict{DoNothing: true}).Create(&record).Error; err != nil {
			// Keep this message in RabbitMQ if the durable archive is unavailable.
			// Closing the channel requeues all unacknowledged deliveries.
			logger.Error("DLQ archive failed; leaving delivery unacknowledged", "message_id", d.MessageId, "err", err)
			return
		}
		logger.Warn("dead letter archived", "message_id", d.MessageId, "archive_key", record.DeliveryKey)
		if err := d.Ack(false); err != nil {
			logger.Error("DLQ ACK failed; broker will redeliver", "message_id", d.MessageId, "err", err)
			return
		}
	}
}

func archiveRecord(d amqp.Delivery) model.DeadLetter {
	h := sha256.New()
	h.Write([]byte(d.MessageId))
	h.Write([]byte{0})
	h.Write([]byte(d.Exchange))
	h.Write([]byte{0})
	h.Write([]byte(d.RoutingKey))
	h.Write([]byte{0})
	h.Write(d.Body)
	return model.DeadLetter{
		DeliveryKey: hex.EncodeToString(h.Sum(nil)),
		MessageID:   d.MessageId,
		Exchange:    d.Exchange,
		RoutingKey:  d.RoutingKey,
		Body:        append([]byte{}, d.Body...),
	}
}
