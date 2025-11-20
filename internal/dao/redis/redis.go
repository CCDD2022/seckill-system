package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/CCDD2022/seckill-system/config"
	"github.com/CCDD2022/seckill-system/pkg/logger"

	"github.com/redis/go-redis/v9"
)

var redisDB redis.UniversalClient

// InitRedis initializes redis client for standalone or cluster based on config
func InitRedis(cfg *config.RedisConfig) (redis.UniversalClient, error) {
	// 单机地址
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	opts := &redis.Options{
		Addr:            addr,
		DB:              cfg.DB,
		Password:        cfg.Password,
		PoolSize:        500,
		MinIdleConns:    100,
		ConnMaxLifetime: 30 * time.Minute,
		MaxRetries:      5,
		MinRetryBackoff: 8 * time.Millisecond,
		MaxRetryBackoff: 512 * time.Millisecond,
		DialTimeout:     5 * time.Second,
		ReadTimeout:     3 * time.Second,
		WriteTimeout:    3 * time.Second,
		PoolTimeout:     4 * time.Second,
	}
	client := redis.NewClient(opts)
	redisDB = client
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := client.Ping(ctx).Result(); err != nil {
		return nil, fmt.Errorf("redis连通失败: %w", err)
	}
	return client, nil
}

func GetRedisDB() redis.UniversalClient {
	return redisDB
}

// WarmupRedis 预热Redis连接池
func WarmupRedis(rdb redis.UniversalClient, minIdleConns int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 方法1：执行多个Ping，强制创建连接
	for i := 0; i < minIdleConns/10; i++ {
		if err := rdb.Ping(ctx).Err(); err != nil {
			return fmt.Errorf("预热ping失败: %w", err)
		}
	}

	// 方法2：批量执行Set命令，更快创建连接
	pipeline := rdb.Pipeline()
	for i := 0; i < minIdleConns/5; i++ {
		pipeline.Set(ctx, fmt.Sprintf("warmup:key:%d", i), i, 1*time.Minute)
	}
	if _, err := pipeline.Exec(ctx); err != nil {
		return fmt.Errorf("预热pipeline失败: %w", err)
	}

	// 等待连接池填充
	time.Sleep(2 * time.Second)

	// 验证预热结果
	stats := rdb.PoolStats()
	logger.Info("Redis连接池状态",
		"totalConns", stats.TotalConns,
		"idleConns", stats.IdleConns,
		"poolSize", minIdleConns,
	)

	if stats.IdleConns < uint32(minIdleConns)/2 {
		return fmt.Errorf("预热不足，空闲连接数=%d，目标=%d", stats.IdleConns, minIdleConns)
	}

	return nil
}
