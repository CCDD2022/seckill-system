package dao

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestCompensateFailedReservationAgainstRedis(t *testing.T) {
	if os.Getenv("REDIS_INTEGRATION") != "1" {
		t.Skip("set REDIS_INTEGRATION=1 against disposable Redis")
	}
	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{Addr: "redis:6379"})
	defer rdb.Close()
	productID := time.Now().UnixNano()
	userID := productID + 1
	stockKey := getProductStockKey(productID)
	joinKey := fmt.Sprintf("seckill:joined:product:%d", productID)
	messageID := fmt.Sprintf("create:%d:%d", userID, productID)
	defer rdb.Del(ctx, stockKey, joinKey, getProductCacheKey(productID))
	defer rdb.SRem(ctx, compensatedReservationsKey, messageID)
	defer rdb.SRem(ctx, productDirtySetKey, productID)
	if err := rdb.Set(ctx, stockKey, 9, 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := rdb.SAdd(ctx, joinKey, userID).Err(); err != nil {
		t.Fatal(err)
	}
	productDao := NewProductDao(nil, rdb)
	changed, err := productDao.CompensateFailedReservation(ctx, productID, userID, 1, messageID)
	if err != nil || !changed {
		t.Fatalf("first compensation = (changed %v, error %v)", changed, err)
	}
	changed, err = productDao.CompensateFailedReservation(ctx, productID, userID, 1, messageID)
	if err != nil || changed {
		t.Fatalf("duplicate compensation = (changed %v, error %v)", changed, err)
	}
	stock, err := rdb.Get(ctx, stockKey).Int64()
	if err != nil || stock != 10 {
		t.Fatalf("stock = %d, error %v; want exactly 10", stock, err)
	}
	joined, err := rdb.SIsMember(ctx, joinKey, userID).Result()
	if err != nil || !joined {
		t.Fatalf("participation marker lost: joined=%v err=%v", joined, err)
	}
	marked, err := productDao.IsReservationCompensated(ctx, messageID)
	if err != nil || !marked {
		t.Fatalf("compensation marker missing: marked=%v err=%v", marked, err)
	}
	if err := rdb.Del(ctx, joinKey).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := productDao.CompensateFailedReservation(ctx, productID, userID, 1, messageID+":other"); !errors.Is(err, ErrReservationNotFound) {
		t.Fatalf("missing reservation marker did not fail closed: %v", err)
	}
	stock, err = rdb.Get(ctx, stockKey).Int64()
	if err != nil || stock != 10 {
		t.Fatalf("stock changed after refused compensation: stock=%d err=%v", stock, err)
	}
}

func TestCompensationScriptDoesNotMarkOnStockOverflow(t *testing.T) {
	if os.Getenv("REDIS_INTEGRATION") != "1" {
		t.Skip("set REDIS_INTEGRATION=1 against disposable Redis")
	}
	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{Addr: "redis:6379"})
	defer rdb.Close()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	keys := []string{"test:stock:" + suffix, "test:compensated:" + suffix, "test:joined:" + suffix, "test:dirty:" + suffix, "test:cache:" + suffix}
	defer rdb.Del(ctx, keys...)
	if err := rdb.Set(ctx, keys[0], "9223372036854775807", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := rdb.SAdd(ctx, keys[2], "user").Err(); err != nil {
		t.Fatal(err)
	}
	result, err := compensateFailedReservation.Run(ctx, rdb, keys, "message", "user", "product", 1).Int64()
	if err != nil || result != -3 {
		t.Fatalf("overflow result = %d, error %v", result, err)
	}
	marked, err := rdb.SIsMember(ctx, keys[1], "message").Result()
	if err != nil || marked {
		t.Fatalf("failed compensation left marker: marked=%v err=%v", marked, err)
	}
	stock, err := rdb.Get(ctx, keys[0]).Result()
	if err != nil || stock != "9223372036854775807" {
		t.Fatalf("failed compensation changed stock: %q, err=%v", stock, err)
	}
}
