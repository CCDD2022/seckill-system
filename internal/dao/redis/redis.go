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
		PoolTimeout:     4 * time.Second, // 获取连接的超时时间

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

// WarmupRedis 预热Redis连接池（并发版）
func WarmupRedis(rdb redis.UniversalClient, minIdleConns int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	logger.Info("开始预热Redis连接池", "target", minIdleConns)

	// 使用并发协程强制连接池创建连接
	// 只有并发请求数 > 当前空闲连接数，连接池才会创建新连接
	concurrency := minIdleConns
	done := make(chan error, concurrency)

	for i := 0; i < concurrency; i++ {
		go func() {
			// 简单的 Ping 操作即可，无需写入垃圾数据
			done <- rdb.Ping(ctx).Err()
		}()
	}

	// 等待所有 Ping 完成
	for i := 0; i < concurrency; i++ {
		if err := <-done; err != nil {
			return fmt.Errorf("预热失败: %w", err)
		}
	}

	// 验证结果
	stats := rdb.PoolStats()
	logger.Info("Redis连接池预热完成",
		"totalConns", stats.TotalConns,
		"idleConns", stats.IdleConns,
	)

	return nil
}
