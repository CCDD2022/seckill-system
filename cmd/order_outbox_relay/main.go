package main

import (
	"context"
	"errors"
	"time"

	"github.com/CCDD2022/seckill-system/config"
	"github.com/CCDD2022/seckill-system/internal/dao"
	"github.com/CCDD2022/seckill-system/internal/dao/mysql"
	"github.com/CCDD2022/seckill-system/internal/mq"
	"github.com/CCDD2022/seckill-system/pkg/app"
	"github.com/CCDD2022/seckill-system/pkg/logger"
)

const outboxPollInterval = time.Second

func main() {
	cfg := app.BootstrapApp()
	db, err := mysql.InitDB(&cfg.Database.Mysql)
	if err != nil {
		logger.Fatal("outbox database init failed", "err", err)
	}
	orderDao := dao.NewOrderDao(db)
	var producer *mq.Pool
	defer func() {
		if producer != nil {
			producer.Close()
		}
	}()
	logger.Info("Order outbox relay started")

	for {
		events, err := orderDao.ListPendingOutbox(context.Background(), 100)
		if err != nil {
			logger.Error("load pending outbox failed", "err", err)
			time.Sleep(outboxPollInterval)
			continue
		}
		if len(events) == 0 {
			time.Sleep(outboxPollInterval)
			continue
		}
		if producer == nil {
			producer, err = connectProducer(&cfg.MQ)
			if err != nil {
				logger.Error("outbox RabbitMQ unavailable; events retained", "err", err)
				time.Sleep(3 * outboxPollInterval)
				continue
			}
		}
		for _, event := range events {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			err := producer.PublishConfirmedWithID(ctx, "seckill.exchange", event.RoutingKey, []byte(event.Payload), event.EventID)
			cancel()
			if err != nil {
				if recordErr := orderDao.MarkOutboxFailure(context.Background(), event.ID, event.Attempts, err); recordErr != nil {
					logger.Error("record outbox failure failed", "event_id", event.EventID, "err", recordErr)
				}
				logger.Error("outbox publish failed; retained for retry", "event_id", event.EventID, "attempt", event.Attempts+1, "err", err)
				if errors.Is(err, mq.ErrPublishUncertain) || errors.Is(err, mq.ErrPoolClosed) {
					producer.Close()
					producer = nil
					break
				}
				continue
			}
			if err := orderDao.MarkOutboxPublished(context.Background(), event.ID); err != nil {
				logger.Error("mark outbox published failed; duplicate delivery is safe", "event_id", event.EventID, "err", err)
			}
		}
		time.Sleep(outboxPollInterval)
	}
}

func connectProducer(cfg *config.MQConfig) (*mq.Pool, error) {
	producer, err := mq.Init(cfg)
	if err != nil {
		return nil, err
	}
	if err := producer.EnsureBaseTopology(); err != nil {
		producer.Close()
		return nil, err
	}
	return producer, nil
}
