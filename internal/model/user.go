package model

import (
	"time"
)

type User struct {
	ID               int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Username         string    `gorm:"size:50;not null;uniqueIndex" json:"username"`
	PasswordHash     string    `gorm:"size:255;not null" json:"-"`
	Email            string    `gorm:"size:100" json:"email"`
	Phone            string    `gorm:"size:20" json:"phone"`
	CreatedAt        time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt        time.Time `gorm:"autoUpdateTime" json:"updated_at"`
	Status           int       `gorm:"default:0;not null" json:"status"`
	IsSeckillAllowed bool      `gorm:"default:true;not null;" json:"is_seckill_allowed"`
}

func (*User) TableName() string {
	return "users"
}
