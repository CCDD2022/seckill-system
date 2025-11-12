package dao

import (
	"seckill-system/internal/model"
	"time"

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
func (dao *UserDao) GetUserByID(id int64) (*model.User, error) {
	var user model.User
	err := dao.db.First(&user, id).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// UpdateUserPassword 更新用户密码
func (dao *UserDao) UpdateUserPassword(userID int64, newPasswordHash string) error {
	return dao.db.Model(&model.User{}).Where("id = ?", userID).Updates(map[string]interface{}{
		"password_hash": newPasswordHash,
		"updated_at":    time.Now(),
	}).Error
}

// UpdateUser 更新用户信息（不包括密码）
func (dao *UserDao) UpdateUser(userID int64, updates map[string]interface{}) error {
	return dao.db.Model(&model.User{}).Where("id = ?", userID).Updates(updates).Error
}
