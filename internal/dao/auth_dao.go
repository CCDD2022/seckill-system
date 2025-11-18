package dao

import (
	"context"

	"github.com/CCDD2022/seckill-system/internal/model"

	"gorm.io/gorm"
)

type AuthDao struct {
	db *gorm.DB
}

func NewAuthDao(db *gorm.DB) *AuthDao {
	return &AuthDao{db: db}
}

// CreateUser 创建用户
func (dao *AuthDao) CreateUser(ctx context.Context, user *model.User) error {
	dao.db.AutoMigrate(&model.User{})
	return dao.db.WithContext(ctx).Create(user).Error
}

// GetUserByUsername 根据用户名查询用户
func (dao *AuthDao) GetUserByUsername(ctx context.Context, username string) (*model.User, error) {
	var user model.User
	err := dao.db.WithContext(ctx).Where("username = ?", username).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// UserExists 检查用户名是否存在
func (dao *AuthDao) UserExists(ctx context.Context, username string) (bool, error) {
	var count int64
	err := dao.db.WithContext(ctx).Model(&model.User{}).Where("username = ?", username).Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
