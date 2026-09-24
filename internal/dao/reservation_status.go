package dao

import (
	"context"
	"errors"
	"fmt"

	"github.com/CCDD2022/seckill-system/internal/model"
)

var ErrReservationStatusNotFound = errors.New("reservation not found")

type ReservationState string

const (
	ReservationProcessing      ReservationState = "processing"
	ReservationOrderCreated    ReservationState = "order_created"
	ReservationRecoveryPending ReservationState = "recovery_pending"
	ReservationFailed          ReservationState = "failed"
)

type ReservationSnapshot struct {
	State       ReservationState
	OrderID     int64
	OrderStatus int32
}

// GetReservationSnapshot resolves a single authenticated user's operation.
// MySQL is checked first because an order may already exist even if a Redis
// response was lost, a message was redelivered, or the Redis key is missing.
func (dao *ProductDao) GetReservationSnapshot(ctx context.Context, userID, productID int64) (ReservationSnapshot, error) {
	if userID <= 0 || productID <= 0 {
		return ReservationSnapshot{}, ErrReservationStatusNotFound
	}
	var order model.Order
	result := dao.db.WithContext(ctx).
		Where("user_id = ? AND product_id = ?", userID, productID).
		Limit(1).Find(&order)
	if result.Error != nil {
		return ReservationSnapshot{}, fmt.Errorf("query order: %w", result.Error)
	}
	if result.RowsAffected > 0 {
		return ReservationSnapshot{
			State: ReservationOrderCreated, OrderID: order.ID, OrderStatus: order.Status,
		}, nil
	}

	messageID := fmt.Sprintf("create:%d:%d", userID, productID)
	// A compensated reservation is final even if a malformed late duplicate
	// with the same message ID is archived as a newer dead-letter row.
	var compensated model.DeadLetter
	result = dao.db.WithContext(ctx).
		Where("message_id = ? AND routing_key = ? AND compensated_at IS NOT NULL", messageID, "order.create").
		Order("id DESC").Limit(1).Find(&compensated)
	if result.Error != nil {
		return ReservationSnapshot{}, fmt.Errorf("query compensation: %w", result.Error)
	}
	if result.RowsAffected > 0 {
		return ReservationSnapshot{State: ReservationFailed}, nil
	}
	marked, err := dao.IsReservationCompensated(ctx, messageID)
	if err != nil {
		return ReservationSnapshot{}, fmt.Errorf("query compensation marker: %w", err)
	}
	if marked {
		return ReservationSnapshot{State: ReservationFailed}, nil
	}
	var archived model.DeadLetter
	result = dao.db.WithContext(ctx).
		Where("message_id = ? AND routing_key = ?", messageID, "order.create").
		Order("id DESC").Limit(1).Find(&archived)
	if result.Error != nil {
		return ReservationSnapshot{}, fmt.Errorf("query dead letter: %w", result.Error)
	}
	if result.RowsAffected > 0 {
		if archived.CompensatedAt != nil {
			return ReservationSnapshot{State: ReservationFailed}, nil
		}
		return ReservationSnapshot{State: ReservationRecoveryPending}, nil
	}

	joined, err := dao.redis.SIsMember(ctx, fmt.Sprintf("seckill:joined:product:%d", productID), userID).Result()
	if err != nil {
		return ReservationSnapshot{}, fmt.Errorf("query participation: %w", err)
	}
	if joined {
		return ReservationSnapshot{State: ReservationProcessing}, nil
	}
	return ReservationSnapshot{}, ErrReservationStatusNotFound
}
