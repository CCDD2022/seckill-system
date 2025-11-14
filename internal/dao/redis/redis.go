package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/CCDD2022/seckill-system/config"

	"github.com/redis/go-redis/v9"
)

var redisDB *redis.Client

func InitRedis(cfg *config.RedisConfig) (*redis.Client, error) {
	redisDB = redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Password: cfg.Password,
		DB:       cfg.DB,
	})
	// 设置定时器的任务 用来控制操作的超时时间
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	defer cancel()

	_, err := redisDB.Ping(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("redis连通失败: %w", err)
	}

	return redisDB, nil
}

func GetRedisDB() *redis.Client {
	return redisDB
}
