package dao

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// Run with REDIS_TEST_ADDR against a disposable Redis instance. The test
// writes the shared outbox stream, so it must not run against an application
// Redis that has a live reservation relay.
func TestReservationAndRestorationAgainstRedis(t *testing.T) {
	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("set REDIS_TEST_ADDR to a disposable Redis instance")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer rdb.Close()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	productID := time.Now().UnixNano()
	stockKey := fmt.Sprintf("stock:%d", productID)
	joinKey := fmt.Sprintf("seckill:joined:product:%d", productID)
	stream := fmt.Sprintf("seckill:test:outbox:%d", productID)
	cancelID := fmt.Sprintf("cancel:%d", productID)
	t.Cleanup(func() {
		_ = rdb.Del(context.Background(), stockKey, joinKey, stream, campaignKey(productID)).Err()
		_ = rdb.SRem(context.Background(), "seckill:cancel:processed", cancelID).Err()
		_ = rdb.SRem(context.Background(), productDirtySetKey, productID).Err()
	})
	if err := rdb.Set(ctx, stockKey, 2, 0).Err(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	if err := rdb.HSet(ctx, campaignKey(productID),
		"active", 1, "price_cents", 999, "start_unix", now-60, "end_unix", now+3600,
	).Err(); err != nil {
		t.Fatal(err)
	}
	productDao := &ProductDao{redis: rdb, reservationStream: stream}
	messageID := fmt.Sprintf("create:10:%d", productID)
	if err := productDao.ReserveStockAndEnqueue(ctx, productID, 10, 1, 999, messageID, []byte(`{"order":1}`)); err != nil {
		t.Fatalf("first reservation: %v", err)
	}
	assertStockAndEvents(t, ctx, rdb, stockKey, stream, 1, 1)
	if err := productDao.ReserveStockAndEnqueue(ctx, productID, 10, 1, 999, messageID, []byte(`{"order":1}`)); !errors.Is(err, ErrDuplicateReservation) {
		t.Fatalf("duplicate reservation: %v", err)
	}
	if err := productDao.ReserveStockAndEnqueue(ctx, productID, 11, 2, 999, "other", []byte(`{}`)); !errors.Is(err, ErrSoldOut) {
		t.Fatalf("sold out reservation: %v", err)
	}
	assertStockAndEvents(t, ctx, rdb, stockKey, stream, 1, 1)
	if err := rdb.HSet(ctx, campaignKey(productID), "start_unix", now+3600, "end_unix", now+7200).Err(); err != nil {
		t.Fatal(err)
	}
	if err := productDao.ReserveStockAndEnqueue(ctx, productID, 13, 1, 999, "early", []byte(`{}`)); !errors.Is(err, ErrCampaignNotActive) {
		t.Fatalf("early reservation: %v", err)
	}
	if err := rdb.HSet(ctx, campaignKey(productID), "start_unix", now-60, "end_unix", now+3600).Err(); err != nil {
		t.Fatal(err)
	}
	if err := productDao.ReserveStockAndEnqueue(ctx, productID, 13, 1, 1000, "stale-price", []byte(`{}`)); !errors.Is(err, ErrCampaignNotConfigured) {
		t.Fatalf("stale campaign price: %v", err)
	}
	if err := rdb.HSet(ctx, campaignKey(productID), "active", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := productDao.ReserveStockAndEnqueue(ctx, productID, 13, 1, 999, "uncommitted-product", []byte(`{}`)); !errors.Is(err, ErrCampaignNotConfigured) {
		t.Fatalf("inactive campaign accepted a reservation: %v", err)
	}
	if err := rdb.HSet(ctx, campaignKey(productID), "active", 1).Err(); err != nil {
		t.Fatal(err)
	}
	assertStockAndEvents(t, ctx, rdb, stockKey, stream, 1, 1)

	// A stream write error must not leave a joined user or reduced stock.
	if err := rdb.Del(ctx, stream).Err(); err != nil {
		t.Fatal(err)
	}
	if err := rdb.Set(ctx, stream, "wrong-type", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := productDao.ReserveStockAndEnqueue(ctx, productID, 12, 1, 999, "bad-stream", []byte(`{}`)); err == nil {
		t.Fatal("wrong-type stream unexpectedly accepted a reservation")
	}
	joined, err := rdb.SIsMember(ctx, joinKey, 12).Result()
	if err != nil || joined {
		t.Fatalf("failed stream append left participant marker: joined=%v err=%v", joined, err)
	}
	if stock, err := rdb.Get(ctx, stockKey).Int(); err != nil || stock != 1 {
		t.Fatalf("failed stream append changed stock: stock=%d err=%v", stock, err)
	}
	if err := rdb.Del(ctx, stream).Err(); err != nil {
		t.Fatal(err)
	}

	if err := productDao.ReturnStockOnce(ctx, productID, 1, cancelID); err != nil {
		t.Fatalf("first restoration: %v", err)
	}
	if err := productDao.ReturnStockOnce(ctx, productID, 1, cancelID); err != nil {
		t.Fatalf("repeated restoration: %v", err)
	}
	if stock, err := rdb.Get(ctx, stockKey).Int(); err != nil || stock != 2 {
		t.Fatalf("restoration was not exactly once: stock=%d err=%v", stock, err)
	}
}

func assertStockAndEvents(t *testing.T, ctx context.Context, rdb *redis.Client, stockKey, stream string, wantStock, wantEvents int64) {
	t.Helper()
	stock, err := rdb.Get(ctx, stockKey).Int64()
	if err != nil || stock != wantStock {
		t.Fatalf("stock=%d err=%v; want %d", stock, err, wantStock)
	}
	events, err := rdb.XLen(ctx, stream).Result()
	if err != nil || events != wantEvents {
		t.Fatalf("stream length=%d err=%v; want %d", events, err, wantEvents)
	}
}

func TestConcurrentReservationsNeverOversellAgainstRedis(t *testing.T) {
	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("set REDIS_TEST_ADDR to a disposable Redis instance")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer rdb.Close()
	productID := time.Now().UnixNano()
	stockKey := fmt.Sprintf("stock:%d", productID)
	joinKey := fmt.Sprintf("seckill:joined:product:%d", productID)
	stream := fmt.Sprintf("seckill:test:outbox:%d", productID)
	t.Cleanup(func() {
		_ = rdb.Del(context.Background(), stockKey, joinKey, stream, campaignKey(productID)).Err()
		_ = rdb.SRem(context.Background(), productDirtySetKey, productID).Err()
	})
	now := time.Now().Unix()
	if err := rdb.Set(ctx, stockKey, 50, 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := rdb.HSet(ctx, campaignKey(productID),
		"active", 1, "price_cents", 999, "start_unix", now-60, "end_unix", now+3600,
	).Err(); err != nil {
		t.Fatal(err)
	}
	productDao := &ProductDao{redis: rdb, reservationStream: stream}
	results := make(chan error, 100)
	var workers sync.WaitGroup
	for userID := int64(1); userID <= 100; userID++ {
		workers.Add(1)
		go func(id int64) {
			defer workers.Done()
			results <- productDao.ReserveStockAndEnqueue(ctx, productID, id, 1, 999,
				fmt.Sprintf("create:%d:%d", id, productID), []byte(`{"quantity":1}`))
		}(userID)
	}
	workers.Wait()
	close(results)
	accepted, rejected := 0, 0
	for err := range results {
		switch {
		case err == nil:
			accepted++
		case errors.Is(err, ErrSoldOut):
			rejected++
		default:
			t.Fatalf("unexpected concurrent reservation error: %v", err)
		}
	}
	if accepted != 50 || rejected != 50 {
		t.Fatalf("accepted=%d rejected=%d; want 50 of each", accepted, rejected)
	}
	assertStockAndEvents(t, ctx, rdb, stockKey, stream, 0, 50)
}
