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
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestReservationStatusTransitionsAgainstMySQLAndRedis(t *testing.T) {
	if os.Getenv("MYSQL_INTEGRATION") != "1" {
		t.Skip("set MYSQL_INTEGRATION=1 against the local Compose MySQL and Redis")
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
	rdb := redis.NewClient(&redis.Options{Addr: "redis:6379"})
	defer rdb.Close()
	ctx := context.Background()
	userID := time.Now().UnixNano()
	productID := userID + 1
	joinKey := fmt.Sprintf("seckill:joined:product:%d", productID)
	defer rdb.Del(ctx, joinKey)
	productDao := NewProductDao(tx, rdb)
	if _, err := productDao.GetReservationSnapshot(ctx, userID, productID); !errors.Is(err, ErrReservationStatusNotFound) {
		t.Fatalf("unaccepted request = %v; want not found", err)
	}
	if err := rdb.SAdd(ctx, joinKey, userID).Err(); err != nil {
		t.Fatal(err)
	}
	snapshot, err := productDao.GetReservationSnapshot(ctx, userID, productID)
	if err != nil || snapshot.State != ReservationProcessing {
		t.Fatalf("processing = %+v err=%v", snapshot, err)
	}
	messageID := fmt.Sprintf("create:%d:%d", userID, productID)
	dead := &model.DeadLetter{
		DeliveryKey: fmt.Sprintf("%064x", userID), MessageID: messageID,
		RoutingKey: "order.create", Body: []byte(`{}`),
	}
	if err := tx.Create(dead).Error; err != nil {
		t.Fatal(err)
	}
	snapshot, err = productDao.GetReservationSnapshot(ctx, userID, productID)
	if err != nil || snapshot.State != ReservationRecoveryPending {
		t.Fatalf("recovery pending = %+v err=%v", snapshot, err)
	}
	if err := tx.Model(dead).Update("compensated_at", time.Now()).Error; err != nil {
		t.Fatal(err)
	}
	snapshot, err = productDao.GetReservationSnapshot(ctx, userID, productID)
	if err != nil || snapshot.State != ReservationFailed {
		t.Fatalf("compensated = %+v err=%v", snapshot, err)
	}
	lateDuplicate := &model.DeadLetter{
		DeliveryKey: fmt.Sprintf("%064x", userID+1), MessageID: messageID,
		RoutingKey: "order.create", Body: []byte(`{"different_payload":true}`),
	}
	if err := tx.Create(lateDuplicate).Error; err != nil {
		t.Fatal(err)
	}
	snapshot, err = productDao.GetReservationSnapshot(ctx, userID, productID)
	if err != nil || snapshot.State != ReservationFailed {
		t.Fatalf("late dead letter regressed compensated state: %+v err=%v", snapshot, err)
	}
	order := &model.Order{UserID: userID, ProductID: productID, Quantity: 1, TotalPrice: 9.99, SourceMessageID: &messageID}
	if err := tx.Create(order).Error; err != nil {
		t.Fatal(err)
	}
	snapshot, err = productDao.GetReservationSnapshot(ctx, userID, productID)
	if err != nil || snapshot.State != ReservationOrderCreated || snapshot.OrderID != order.ID {
		t.Fatalf("persisted order = %+v err=%v", snapshot, err)
	}
}
