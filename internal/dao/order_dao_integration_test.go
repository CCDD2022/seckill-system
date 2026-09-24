package dao

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/CCDD2022/seckill-system/internal/model"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestOrderIdempotencyAndCancellationOutboxAgainstMySQL(t *testing.T) {
	if os.Getenv("MYSQL_INTEGRATION") != "1" {
		t.Skip("set MYSQL_INTEGRATION=1 against the local Compose MySQL")
	}
	password, err := os.ReadFile(os.Getenv("MYSQL_TEST_PASSWORD_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	dsn := fmt.Sprintf("seckill:%s@tcp(mysql:3306)/seckill_shop?charset=utf8mb4&parseTime=True&loc=UTC", strings.TrimSpace(string(password)))
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	orderDao := NewOrderDao(tx)
	ctx := context.Background()
	unique := time.Now().UnixNano()
	messageID := fmt.Sprintf("create:%d:%d", unique, unique)
	order := &model.Order{UserID: unique, ProductID: unique, Quantity: 1, TotalPrice: 12.5, Status: model.OrderStatusPending, SourceMessageID: &messageID}
	created, err := orderDao.CreateOrderOnce(ctx, order)
	if err != nil || !created || order.ID <= 0 {
		t.Fatalf("first delivery = (created %v, id %d, error %v)", created, order.ID, err)
	}
	duplicate := &model.Order{UserID: unique, ProductID: unique, Quantity: 1, TotalPrice: 12.5, Status: model.OrderStatusPending, SourceMessageID: &messageID}
	created, err = orderDao.CreateOrderOnce(ctx, duplicate)
	if err != nil || created {
		t.Fatalf("redelivery = (created %v, error %v), want harmless duplicate", created, err)
	}
	differentID := messageID + ":other"
	conflict := &model.Order{UserID: unique, ProductID: unique, Quantity: 1, TotalPrice: 12.5, Status: model.OrderStatusPending, SourceMessageID: &differentID}
	if _, err := orderDao.CreateOrderOnce(ctx, conflict); !errors.Is(err, ErrOrderConflict) {
		t.Fatalf("a second reservation for the same business key returned %v; want ErrOrderConflict", err)
	}
	outbox := &model.OutboxEvent{EventID: fmt.Sprintf("cancel:%d", order.ID), RoutingKey: "order.canceled", Payload: `{"event_id":"test"}`}
	if err := orderDao.CancelOrderWithOutbox(ctx, order.ID, outbox); err != nil {
		t.Fatalf("cancellation and outbox transaction failed: %v", err)
	}
	var persisted model.Order
	if err := tx.First(&persisted, "id = ?", order.ID).Error; err != nil || persisted.Status != model.OrderStatusCancelled {
		t.Fatalf("cancellation state missing: status %d, error %v", persisted.Status, err)
	}
	var event model.OutboxEvent
	if err := tx.First(&event, "event_id = ?", outbox.EventID).Error; err != nil {
		t.Fatalf("outbox event missing from same transaction: %v", err)
	}
	if err := orderDao.MarkOutboxFailure(ctx, event.ID, 0, errors.New("broker unavailable")); err != nil {
		t.Fatalf("record outbox failure: %v", err)
	}
	if err := tx.First(&event, "id = ?", event.ID).Error; err != nil || event.Attempts != 1 || event.LastError != "broker unavailable" || event.NextAttemptAt == nil {
		t.Fatalf("outbox retry state missing: attempts %d, error %q, next %v, db error %v", event.Attempts, event.LastError, event.NextAttemptAt, err)
	}
	if err := orderDao.MarkOutboxPublished(ctx, event.ID); err != nil {
		t.Fatalf("mark outbox published: %v", err)
	}
	var published model.OutboxEvent
	if err := tx.First(&published, "id = ?", event.ID).Error; err != nil || published.PublishedAt == nil || published.LastError != "" || published.NextAttemptAt != nil {
		t.Fatalf("outbox published state missing: published %v, error %q, next %v, db error %v", published.PublishedAt, published.LastError, published.NextAttemptAt, err)
	}
	if err := orderDao.CancelOrderWithOutbox(ctx, order.ID, outbox); !errors.Is(err, ErrOrderStatusChanged) {
		t.Fatalf("second cancellation = %v, want ErrOrderStatusChanged", err)
	}
}
