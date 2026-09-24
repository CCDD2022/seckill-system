package main

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/CCDD2022/seckill-system/internal/model"
	"github.com/redis/go-redis/v9"
)

func TestValidateArchivedCreate(t *testing.T) {
	valid := model.DeadLetter{
		ID: 10, RoutingKey: "order.create", MessageID: "create:7:9",
		Body: []byte(`{"user_id":7,"product_id":9,"quantity":1,"total_price":12.5}`),
	}
	if _, err := validateArchivedCreate(valid); err != nil {
		t.Fatalf("valid archived reservation refused: %v", err)
	}
	tests := []struct {
		name string
		item model.DeadLetter
	}{
		{"wrong route", model.DeadLetter{ID: 11, RoutingKey: "order.canceled", MessageID: valid.MessageID, Body: valid.Body}},
		{"mismatched message ID", model.DeadLetter{ID: 11, RoutingKey: "order.create", MessageID: "create:7:10", Body: valid.Body}},
		{"invalid quantity", model.DeadLetter{ID: 11, RoutingKey: "order.create", MessageID: valid.MessageID, Body: []byte(`{"user_id":7,"product_id":9,"quantity":0,"total_price":12.5}`)}},
		{"invalid JSON", model.DeadLetter{ID: 11, RoutingKey: "order.create", MessageID: valid.MessageID, Body: []byte(`{`)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := validateArchivedCreate(tc.item); err == nil {
				t.Fatal("unsafe compensation input was accepted")
			}
		})
	}
}

func TestVerifyNoStreamEventAcrossPages(t *testing.T) {
	if os.Getenv("REDIS_INTEGRATION") != "1" {
		t.Skip("set REDIS_INTEGRATION=1 against disposable Redis")
	}
	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{Addr: "redis:6379"})
	defer rdb.Close()
	stream := fmt.Sprintf("test:compensation:stream:%d", time.Now().UnixNano())
	defer rdb.Del(ctx, stream)
	pipe := rdb.Pipeline()
	for i := 0; i < 1000; i++ {
		pipe.XAdd(ctx, &redis.XAddArgs{Stream: stream, Values: map[string]any{"message_id": fmt.Sprintf("other-%d", i)}})
	}
	pipe.XAdd(ctx, &redis.XAddArgs{Stream: stream, Values: map[string]any{"message_id": "target"}})
	if _, err := pipe.Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if err := verifyNoStreamEventIn(ctx, rdb, stream, "target"); err == nil {
		t.Fatal("matching reservation beyond first page was missed")
	}
	if err := verifyNoStreamEventIn(ctx, rdb, stream, "absent"); err != nil {
		t.Fatalf("absent reservation was rejected: %v", err)
	}
}
