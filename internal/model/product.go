package model

import (
	"time"
)

// ProductSeckillStatus 定义商品秒杀状态
type ProductSeckillStatus int32

const (
	SeckillStatusNotStarted ProductSeckillStatus = iota // 未开始
	SeckillStatusActive                                 // 进行中
	SeckillStatusEnded                                  // 已结束
)

type Product struct {
	ID                int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	Name              string     `gorm:"size:100;not null" json:"name"`
	Description       string     `gorm:"type:text" json:"description"`
	Price             float64    `gorm:"type:decimal(10,2);not null" json:"price"`
	Stock             int32      `gorm:"not null;default:0" json:"stock"`
	ImageURL          string     `gorm:"size:255" json:"image_url"`
	SeckillStartTime  *time.Time `gorm:"index" json:"seckill_start_time"`
	SeckillEndTime    *time.Time `gorm:"index" json:"seckill_end_time"`
	SecondsUntilStart int64      `gorm:"-" json:"seconds_until_start"`
	SecondsUntilEnd   int64      `gorm:"-" json:"seconds_until_end"`
	CreatedAt         time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt         time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

func (*Product) TableName() string {
	return "products"
}

// IsSeckillProduct 判断是否为秒杀商品
func (p *Product) IsSeckillProduct() bool {
	return p.SeckillStartTime != nil && p.SeckillEndTime != nil
}

// CalculateSeckillStatus 计算秒杀状态（应该在查询后调用）
func (p *Product) CalculateSeckillStatus() {
	// 只计算倒计时，不再保留状态枚举
	if !p.IsSeckillProduct() {
		p.SecondsUntilStart = 0
		p.SecondsUntilEnd = 0
		return
	}

	now := time.Now()
	startTime := *p.SeckillStartTime
	endTime := *p.SeckillEndTime

	if now.Before(startTime) {
		p.SecondsUntilStart = int64(startTime.Sub(now).Seconds())
		p.SecondsUntilEnd = 0
	} else if now.After(endTime) {
		p.SecondsUntilStart = 0
		p.SecondsUntilEnd = 0
	} else {
		p.SecondsUntilStart = 0
		p.SecondsUntilEnd = int64(endTime.Sub(now).Seconds())
	}
}
