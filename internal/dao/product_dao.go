package dao

import (
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type productDao struct {
	db    *gorm.DB
	redis *redis.Client
}

func NewProductDao(db *gorm.DB, redis *redis.Client) *productDao {
	return &productDao{
		db:    db,
		redis: redis,
	}
}
