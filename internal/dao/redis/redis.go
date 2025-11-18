package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/CCDD2022/seckill-system/config"

	"github.com/redis/go-redis/v9"
)

var redisDB redis.UniversalClient

// InitRedis initializes redis client for standalone or cluster based on config
func InitRedis(cfg *config.RedisConfig) (redis.UniversalClient, error) {
	// Build addresses
	addrs := cfg.Addrs
	if len(addrs) == 0 {
		addrs = []string{fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)}
	}

	// Universal client handles standalone and cluster transparently
	uopts := &redis.UniversalOptions{
		Addrs:    addrs,
		DB:       cfg.DB,
		Password: cfg.Password,
	}

	redisDB = redis.NewUniversalClient(uopts)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := redisDB.Ping(ctx).Result(); err != nil {
		return nil, fmt.Errorf("redis连通失败: %w", err)
	}
	return redisDB, nil
}

func GetRedisDB() redis.UniversalClient {
	return redisDB
}
