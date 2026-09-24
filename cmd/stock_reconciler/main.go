package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/CCDD2022/seckill-system/internal/dao/mysql"
	redisinit "github.com/CCDD2022/seckill-system/internal/dao/redis"
	"github.com/CCDD2022/seckill-system/internal/model"
	"github.com/CCDD2022/seckill-system/pkg/app"
	"github.com/CCDD2022/seckill-system/pkg/logger"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

const (
	dirtySetKey   = "product:dirty"
	flushBatch    = 1000
	flushInterval = 200 * time.Millisecond
)

// A dirty product stays in Redis until MySQL holds the exact sampled stock.
// This avoids losing work when the reconciler stops between reading and DB
// commit. A concurrent stock change keeps or re-adds the product to the set.
var clearIfUnchanged = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('SREM', KEYS[2], ARGV[2])
end
return 0
`)

func main() {
	cfg := app.BootstrapApp()
	db, err := mysql.InitDB(&cfg.Database.Mysql)
	if err != nil {
		logger.Fatal("stock reconciler MySQL connection failed", "err", err)
	}
	rdb, err := redisinit.InitRedis(&cfg.Database.Redis)
	if err != nil {
		logger.Fatal("stock reconciler Redis connection failed", "err", err)
	}
	defer rdb.Close()
	logger.Info("Stock Reconciler started")

	ctx := context.Background()
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()
	for range ticker.C {
		ids, err := rdb.SRandMemberN(ctx, dirtySetKey, flushBatch).Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			logger.Error("read dirty products failed", "err", err)
			continue
		}
		for _, member := range ids {
			id, err := strconv.ParseInt(member, 10, 64)
			if err != nil || id <= 0 {
				logger.Error("invalid dirty product ID; retained for inspection", "value", member)
				continue
			}
			if err := reconcileOne(ctx, db, rdb, id); err != nil {
				logger.Error("stock reconciliation failed; will retry", "product_id", id, "err", err)
			}
		}
	}
}

func reconcileOne(ctx context.Context, db *gorm.DB, rdb redis.UniversalClient, productID int64) error {
	stockKey := fmt.Sprintf("stock:%d", productID)
	stockText, err := rdb.Get(ctx, stockKey).Result()
	if err != nil {
		return fmt.Errorf("read Redis stock: %w", err)
	}
	stock, err := strconv.ParseInt(stockText, 10, 32)
	if err != nil || stock < 0 {
		return fmt.Errorf("invalid Redis stock %q", stockText)
	}
	result := db.WithContext(ctx).Model(&model.Product{}).
		Where("id = ?", productID).
		Updates(map[string]any{"stock": int32(stock), "updated_at": time.Now()})
	if result.Error != nil {
		return fmt.Errorf("write MySQL stock: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return errors.New("product missing from MySQL")
	}
	// The Lua compare and set removal is atomic with concurrent reservations.
	return clearIfUnchanged.Run(ctx, rdb, []string{stockKey, dirtySetKey}, stockText, productID).Err()
}
