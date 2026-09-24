package dao

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/CCDD2022/seckill-system/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type OrderDao struct {
	db *gorm.DB
}

func NewOrderDao(db *gorm.DB) *OrderDao {
	return &OrderDao{
		db: db,
	}
}

var ErrOrderStatusChanged = errors.New("订单状态已变更")
var ErrOrderConflict = errors.New("订单消息与已落库订单冲突")

// CreateOrder 创建订单
func (d *OrderDao) CreateOrder(ctx context.Context, order *model.Order) error {

	return d.db.WithContext(ctx).Create(order).Error
}

// CreateOrderOnce commits a single order before the caller acknowledges its
// delivery. Both the message ID and the user/product business key are enforced
// by database unique indexes, so this also handles concurrent/redelivered
// messages after a process restart.
func (d *OrderDao) CreateOrderOnce(ctx context.Context, order *model.Order) (bool, error) {
	if order.SourceMessageID == nil || *order.SourceMessageID == "" {
		return false, errors.New("source message ID is required")
	}
	result := d.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(order)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 1 {
		return true, nil
	}
	var existing model.Order
	if err := d.db.WithContext(ctx).
		Where("source_message_id = ? OR (user_id = ? AND product_id = ?)", *order.SourceMessageID, order.UserID, order.ProductID).
		First(&existing).Error; err != nil {
		return false, fmt.Errorf("order conflict lookup failed: %w", err)
	}
	if existing.SourceMessageID == nil || *existing.SourceMessageID != *order.SourceMessageID ||
		existing.UserID != order.UserID || existing.ProductID != order.ProductID ||
		existing.Quantity != order.Quantity || existing.TotalPrice != order.TotalPrice {
		return false, fmt.Errorf("%w: delivery %q does not match persisted order %d", ErrOrderConflict, *order.SourceMessageID, existing.ID)
	}
	return false, nil
}

// CancelOrderWithOutbox records the cancellation and its durable event in one
// transaction. The relay can safely send the same event more than once.
func (d *OrderDao) CancelOrderWithOutbox(ctx context.Context, orderID int64, event *model.OutboxEvent) error {
	if event == nil || event.EventID == "" || event.RoutingKey == "" || event.Payload == "" {
		return errors.New("invalid cancellation outbox event")
	}
	return d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.Order{}).
			Where("id = ? AND status = ?", orderID, model.OrderStatusPending).
			Update("status", model.OrderStatusCancelled)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrOrderStatusChanged
		}
		return tx.Create(event).Error
	})
}

func (d *OrderDao) ListPendingOutbox(ctx context.Context, limit int) ([]model.OutboxEvent, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	var events []model.OutboxEvent
	err := d.db.WithContext(ctx).
		Where("published_at IS NULL AND (next_attempt_at IS NULL OR next_attempt_at <= ?)", time.Now()).
		Order("id ASC").Limit(limit).Find(&events).Error
	return events, err
}

func (d *OrderDao) MarkOutboxPublished(ctx context.Context, id int64) error {
	now := time.Now()
	return d.db.WithContext(ctx).Model(&model.OutboxEvent{}).
		Where("id = ? AND published_at IS NULL", id).
		Updates(map[string]any{"published_at": now, "last_error": "", "next_attempt_at": nil}).Error
}

func (d *OrderDao) MarkOutboxFailure(ctx context.Context, id int64, previousAttempts int64, publishErr error) error {
	message := publishErr.Error()
	if len(message) > 2048 {
		message = message[:2048]
	}
	if previousAttempts < 0 {
		previousAttempts = 0
	}
	if previousAttempts > 5 {
		previousAttempts = 5
	}
	nextAttempt := time.Now().Add(time.Duration(1<<uint(previousAttempts+1)) * time.Second)
	return d.db.WithContext(ctx).Model(&model.OutboxEvent{}).
		Where("id = ? AND published_at IS NULL", id).
		Updates(map[string]any{"attempts": gorm.Expr("attempts + 1"), "last_error": message, "next_attempt_at": nextAttempt}).Error
}

// CreateOrdersBatch 批量创建订单（单事务）
func (d *OrderDao) CreateOrdersBatch(ctx context.Context, orders []*model.Order) error {
	if len(orders) == 0 {
		return nil
	}
	return d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).CreateInBatches(orders, len(orders)).Error; err != nil {
			return err
		}
		return nil
	})
}

// GetOrderByID 根据ID获取订单
func (d *OrderDao) GetOrderByID(ctx context.Context, orderID int64) (*model.Order, error) {
	var order model.Order
	err := d.db.WithContext(ctx).Where("id = ?", orderID).First(&order).Error
	if err != nil {
		return nil, err
	}
	return &order, nil
}

// GetUserOrders 获取用户订单列表
func (d *OrderDao) GetUserOrders(ctx context.Context, userID int64, page, pageSize int32) ([]*model.Order, int64, error) {
	var orders []*model.Order
	var total int64
	offset := (page - 1) * pageSize

	// 获取总数
	if err := d.db.WithContext(ctx).Model(&model.Order{}).Where("user_id = ?", userID).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 获取分页数据
	err := d.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Limit(int(pageSize)).
		Offset(int(offset)).
		Find(&orders).Error

	return orders, total, err
}

// UpdateOrderStatus 更新订单状态
func (d *OrderDao) UpdateOrderStatus(ctx context.Context, orderID int64, fromStatus, toStatus int32) error {
	result := d.db.WithContext(ctx).Model(&model.Order{}).
		Where("id = ? AND status = ?", orderID, fromStatus).
		Update("status", toStatus)

	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrOrderStatusChanged // 统一错误类型
	}
	return nil
}

// PayOrder 支付订单（仅允许待支付 -> 已支付）
func (d *OrderDao) PayOrder(ctx context.Context, orderID int64) error {
	return d.UpdateOrderStatus(ctx, orderID, model.OrderStatusPending, model.OrderStatusPaid)
}
