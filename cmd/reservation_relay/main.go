package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/CCDD2022/seckill-system/internal/dao"
	redisinit "github.com/CCDD2022/seckill-system/internal/dao/redis"
	"github.com/CCDD2022/seckill-system/internal/mq"
	"github.com/CCDD2022/seckill-system/pkg/app"
	"github.com/CCDD2022/seckill-system/pkg/logger"
	"github.com/redis/go-redis/v9"
)

const (
	orderExchange = "seckill.exchange"
	orderRoute    = "order.create"
	invalidStream = "seckill:orders:invalid"
)

func main() {
	cfg := app.BootstrapApp()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	rdb, err := redisinit.InitRedis(&cfg.Database.Redis)
	if err != nil {
		logger.Fatal("reservation relay Redis connection failed", "err", err)
	}
	defer rdb.Close()
	if err := rdb.XGroupCreateMkStream(ctx, dao.ReservationStream, dao.ReservationGroup, "0").Err(); err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		logger.Fatal("reservation relay group creation failed", "err", err)
	}

	consumer := os.Getenv("RESERVATION_RELAY_CONSUMER")
	if consumer == "" {
		consumer, _ = os.Hostname()
	}
	if consumer == "" {
		consumer = "reservation-relay"
	}
	for ctx.Err() == nil {
		pool, err := mq.Init(&cfg.MQ)
		if err != nil {
			logger.Error("reservation relay RabbitMQ connection failed", "err", err)
			sleep(ctx, time.Second)
			continue
		}
		if err := pool.EnsureBaseTopology(); err != nil {
			pool.Close()
			logger.Error("reservation relay topology failed", "err", err)
			sleep(ctx, time.Second)
			continue
		}
		err = relay(ctx, rdb, pool, consumer)
		pool.Close()
		if ctx.Err() == nil {
			logger.Error("reservation relay restarting", "err", err)
			sleep(ctx, time.Second)
		}
	}
}

func relay(ctx context.Context, rdb redis.UniversalClient, pool *mq.Pool, consumer string) error {
	for ctx.Err() == nil {
		// Read this consumer's pending deliveries first. A crash after broker
		// confirmation but before XACK safely replays the same business ID.
		streams, err := rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group: dao.ReservationGroup, Consumer: consumer,
			Streams: []string{dao.ReservationStream, "0"}, Count: 64, Block: -1,
		}).Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			return err
		}
		if len(streams) > 0 && len(streams[0].Messages) > 0 {
			if err := deliver(ctx, rdb, pool, streams[0].Messages); err != nil {
				return err
			}
			continue
		}

		// Reclaim messages left by a previous container instance. Redis 7+
		// supports XAUTOCLAIM; the deployment pins a compatible Redis image.
		claimed, _, err := rdb.XAutoClaim(ctx, &redis.XAutoClaimArgs{
			Stream: dao.ReservationStream, Group: dao.ReservationGroup,
			Consumer: consumer, MinIdle: 10 * time.Second, Start: "0-0", Count: 64,
		}).Result()
		if err != nil {
			return err
		}
		if len(claimed) > 0 {
			if err := deliver(ctx, rdb, pool, claimed); err != nil {
				return err
			}
			continue
		}

		streams, err = rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group: dao.ReservationGroup, Consumer: consumer,
			Streams: []string{dao.ReservationStream, ">"}, Count: 64, Block: 2 * time.Second,
		}).Result()
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil {
			return err
		}
		for _, stream := range streams {
			if err := deliver(ctx, rdb, pool, stream.Messages); err != nil {
				return err
			}
		}
	}
	return ctx.Err()
}

func deliver(ctx context.Context, rdb redis.UniversalClient, pool *mq.Pool, messages []redis.XMessage) error {
	for _, item := range messages {
		messageID := fmt.Sprint(item.Values["message_id"])
		payload := fmt.Sprint(item.Values["payload"])
		if messageID == "<nil>" || payload == "<nil>" || messageID == "" || payload == "" {
			// Preserve malformed entries in a separate durable stream before ACK.
			if err := rdb.XAdd(ctx, &redis.XAddArgs{Stream: invalidStream, Values: map[string]any{
				"source_id": item.ID, "message_id": messageID, "payload": payload,
			}}).Err(); err != nil {
				return err
			}
		} else {
			publishCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := pool.PublishConfirmedWithID(publishCtx, orderExchange, orderRoute, []byte(payload), messageID)
			cancel()
			if err != nil {
				return fmt.Errorf("publish reservation %s: %w", item.ID, err)
			}
		}
		if err := rdb.XAck(ctx, dao.ReservationStream, dao.ReservationGroup, item.ID).Err(); err != nil {
			return err
		}
		if err := rdb.XDel(ctx, dao.ReservationStream, item.ID).Err(); err != nil {
			logger.Warn("reservation relay cleanup failed", "stream_id", item.ID, "err", err)
		}
	}
	return nil
}

func sleep(ctx context.Context, delay time.Duration) {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
