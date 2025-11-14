package service

import (
	"context"
	"errors"
	"time"

	"github.com/CCDD2022/seckill-system/internal/dao"
	"github.com/CCDD2022/seckill-system/pkg/e"
	"github.com/CCDD2022/seckill-system/pkg/utils"
	"github.com/CCDD2022/seckill-system/proto_output/user"

	"gorm.io/gorm"
)

// todo 要先验证token  应该是使用拦截器的方式

// UserService 这个类指定了 所有的依赖字段 和对应的方法
type UserService struct {
	userDao *dao.UserDao
	user.UnimplementedUserServiceServer
}

func NewUserService(userDao *dao.UserDao) *UserService {
	return &UserService{
		userDao: userDao,
	}
}

func (s *UserService) GetUser(ctx context.Context, req *user.GetUserRequest) (*user.GetUserResponse, error) {
	// 根据id获取用户
	userInfo, err := s.userDao.GetUserByID(ctx, req.GetUserId())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &user.GetUserResponse{
				Code:    e.ERROR_USER_NOT_EXISTS,
				Message: e.GetMsg(e.ERROR_USER_NOT_EXISTS),
			}, nil
		}

		return &user.GetUserResponse{
			Code:    e.ERROR,
			Message: e.GetMsg(e.ERROR),
		}, err
	}

	return &user.GetUserResponse{
		Code:    e.SUCCESS,
		Message: e.GetMsg(e.SUCCESS),
		User: &user.User{
			Id:        userInfo.ID,
			Username:  userInfo.Username,
			Email:     userInfo.Email,
			Phone:     userInfo.Phone,
			CreatedAt: userInfo.CreatedAt.Format(time.RFC3339),
			UpdatedAt: userInfo.UpdatedAt.Format(time.RFC3339),
		},
	}, nil
}

func (s *UserService) UpdateUser(ctx context.Context, req *user.UpdateUserRequest) (*user.UpdateUserResponse, error) {
	// 1. 检查用户是否存在
	_, err := s.userDao.GetUserByID(ctx, req.GetUserId())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &user.UpdateUserResponse{
				Code:    e.ERROR_USER_NOT_EXISTS,
				Message: e.GetMsg(e.ERROR_USER_NOT_EXISTS),
			}, nil
		}
		return &user.UpdateUserResponse{
			Code:    e.ERROR,
			Message: e.GetMsg(e.ERROR),
		}, err
	}

	// 2. 构建更新字段（只有 email 和 phone）
	updates := map[string]interface{}{}
	if req.Email != "" {
		updates["email"] = req.Email
	}
	if req.Phone != "" {
		updates["phone"] = req.Phone
	}

	// 3. 没有更新字段则直接返回
	if len(updates) == 0 {
		return &user.UpdateUserResponse{
			Code:    e.SUCCESS,
			Message: "暂无需要更新内容",
		}, nil
	}

	// 4. 执行更新
	if err := s.userDao.UpdateUser(ctx, req.GetUserId(), updates); err != nil {
		return &user.UpdateUserResponse{
			Code:    e.ERROR,
			Message: e.GetMsg(e.ERROR),
		}, err
	}

	// 5. 获取最新信息返回
	updatedUser, _ := s.userDao.GetUserByID(ctx, req.GetUserId())
	return &user.UpdateUserResponse{
		Code:    e.SUCCESS,
		Message: e.GetMsg(e.SUCCESS),
		User: &user.User{
			Id:        updatedUser.ID,
			Username:  updatedUser.Username, // 保持不变
			Email:     updatedUser.Email,
			Phone:     updatedUser.Phone,
			CreatedAt: updatedUser.CreatedAt.Format(time.RFC3339),
			UpdatedAt: updatedUser.UpdatedAt.Format(time.RFC3339),
		},
	}, nil
}

func (s *UserService) ChangePassword(ctx context.Context, req *user.ChangePasswordRequest) (*user.ChangePasswordResponse, error) {
	// 1. 检查用户并验证旧密码
	userInfo, err := s.userDao.GetUserByID(ctx, req.GetUserId())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &user.ChangePasswordResponse{
				Code:    e.ERROR_USER_NOT_EXISTS,
				Message: e.GetMsg(e.ERROR_USER_NOT_EXISTS),
			}, nil
		}
		return &user.ChangePasswordResponse{
			Code:    e.ERROR,
			Message: e.GetMsg(e.ERROR),
		}, err
	}

	if !utils.CheckPassword(req.OldPassword, userInfo.PasswordHash) {
		return &user.ChangePasswordResponse{
			Code:    e.ERROR_PASSWORD,
			Message: e.GetMsg(e.ERROR_PASSWORD),
		}, nil
	}

	// 2. 校验新密码长度
	if len(req.NewPassword) < 8 {
		return &user.ChangePasswordResponse{
			Code:    e.ERROR,
			Message: "新密码长度至少8位",
		}, nil
	}

	// 3. 加密并更新密码
	newHash, err := utils.HashPassword(req.NewPassword)
	if err != nil {
		return &user.ChangePasswordResponse{Code: e.ERROR, Message: e.GetMsg(e.ERROR)}, err
	}

	if err := s.userDao.UpdateUserPassword(ctx, req.GetUserId(), newHash); err != nil {
		return &user.ChangePasswordResponse{Code: e.ERROR, Message: e.GetMsg(e.ERROR)}, err
	}

	return &user.ChangePasswordResponse{
		Code:    e.SUCCESS,
		Message: e.GetMsg(e.SUCCESS),
	}, nil
}
