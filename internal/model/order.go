package model

import "time"

// Order 订单模型
type Order struct {
	ID        int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	UserID    int64 `gorm:"column:user_id;not null;index;uniqueIndex:idx_order_user_product" json:"user_id"`
	ProductID int64 `gorm:"column:product_id;not null;index;uniqueIndex:idx_order_user_product" json:"product_id"`
	// NULL for legacy/direct orders; a message ID is unique whenever present.
	SourceMessageID *string   `gorm:"column:source_message_id;size:191;uniqueIndex" json:"-"`
	Quantity        int32     `gorm:"column:quantity;not null" json:"quantity"`
	TotalPrice      float64   `gorm:"column:total_price;not null" json:"total_price"`
	Status          int32     `gorm:"column:status;default:0" json:"status"` // 0:待支付 1:已支付 2:已取消 3:已完成
	CreatedAt       time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt       time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

// TableName 指定表名
func (Order) TableName() string {
	return "orders"
}

// Order status constants
const (
	OrderStatusPending   = 0 // 待支付
	OrderStatusPaid      = 1 // 已支付
	OrderStatusCancelled = 2 // 已取消
	OrderStatusCompleted = 3 // 已完成
)
