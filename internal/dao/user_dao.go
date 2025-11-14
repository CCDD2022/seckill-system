package dao

import (
	"context"
	"time"

	"github.com/CCDD2022/seckill-system/internal/model"

	"gorm.io/gorm"
)

type UserDao struct {
	db *gorm.DB
}

// NewUserDao 构造函数（依赖注入）
func NewUserDao(db *gorm.DB) *UserDao {
	return &UserDao{db: db}
}

// GetUserByID 根据用户ID获取用户
func (dao *UserDao) GetUserByID(ctx context.Context, id int64) (*model.User, error) {
	var user model.User
	err := dao.db.WithContext(ctx).First(&user, id).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// UpdateUserPassword 更新用户密码
func (dao *UserDao) UpdateUserPassword(ctx context.Context, userID int64, newPasswordHash string) error {
	return dao.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", userID).Updates(map[string]interface{}{
		"password_hash": newPasswordHash,
		"updated_at":    time.Now(),
	}).Error
}

// UpdateUser 更新用户信息（不包括密码）
func (dao *UserDao) UpdateUser(ctx context.Context, userID int64, updates map[string]interface{}) error {
	return dao.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", userID).Updates(updates).Error
}
