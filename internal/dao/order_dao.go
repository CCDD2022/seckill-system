package dao

import (
	"context"
	"errors"

	"github.com/CCDD2022/seckill-system/internal/model"
	"gorm.io/gorm"
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

// CreateOrder 创建订单
func (d *OrderDao) CreateOrder(ctx context.Context, order *model.Order) error {

	return d.db.WithContext(ctx).Create(order).Error
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

// CancelOrder 取消订单
func (d *OrderDao) CancelOrder(ctx context.Context, orderID int64) error {
	return d.UpdateOrderStatus(ctx, orderID, model.OrderStatusPending, model.OrderStatusCancelled)
}

// PayOrder 支付订单（仅允许待支付 -> 已支付）
func (d *OrderDao) PayOrder(ctx context.Context, orderID int64) error {
	return d.UpdateOrderStatus(ctx, orderID, model.OrderStatusPending, model.OrderStatusPaid)
}
