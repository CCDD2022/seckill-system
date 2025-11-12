package dao

import (
	"errors"
	"seckill-system/internal/model"

	"gorm.io/gorm"
)

type AuthDao struct {
	db *gorm.DB
}

func NewAuthDao(db *gorm.DB) *AuthDao {
	return &AuthDao{db: db}
}

// CreateUser 创建用户
func (dao *AuthDao) CreateUser(user *model.User) error {
	return dao.db.Create(user).Error
}

// GetUserByUsername 根据用户名查询用户
func (dao *AuthDao) GetUserByUsername(username string) (*model.User, error) {
	var user model.User
	err := dao.db.Where("username = ?", username).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

// UserExists 检查用户名是否存在
func (dao *AuthDao) UserExists(username string) (bool, error) {
	var count int64
	err := dao.db.Model(&model.User{}).Where("username = ?", username).Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
