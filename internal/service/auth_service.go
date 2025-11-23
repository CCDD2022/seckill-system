package service

import (
	"context"
	"errors"

	"github.com/CCDD2022/seckill-system/internal/dao"
	"github.com/CCDD2022/seckill-system/internal/model"
	"github.com/CCDD2022/seckill-system/pkg/e"
	"github.com/CCDD2022/seckill-system/pkg/utils"
	"github.com/CCDD2022/seckill-system/proto_output/auth"

	"gorm.io/gorm"
)

// 做到了auth全部不使用token检验拦截器

type AuthService struct {
	authDao *dao.AuthDao
	jwtUtil *utils.JWTUtil
	auth.UnimplementedAuthServiceServer
}

func NewAuthService(authDao *dao.AuthDao, jwtSecret string, jwtExpireHours int) *AuthService {
	return &AuthService{
		authDao: authDao,
		jwtUtil: utils.NewJWTUtil(jwtSecret, jwtExpireHours),
	}
}

func (s *AuthService) Register(ctx context.Context, req *auth.RegisterRequest) (*auth.RegisterResponse, error) {
	// 检查用户是否存在
	exists, err := s.authDao.UserExists(ctx, req.Username)
	if err != nil {
		return &auth.RegisterResponse{
			Code:    e.ERROR,
			Message: e.GetMsg(e.ERROR),
		}, err
	}
	if exists {
		return &auth.RegisterResponse{
			Code:    e.ERROR_USER_EXISTS,
			Message: e.GetMsg(e.ERROR_USER_EXISTS),
		}, nil
	}
	// 加密密码
	passwordHash, err := utils.HashPassword(req.Password)
	if err != nil {
		return &auth.RegisterResponse{
			Code:    e.ERROR,
			Message: e.GetMsg(e.ERROR),
		}, err
	}
	// 创建一个model层的用户给下层dao层存储
	newUser := &model.User{
		Username:     req.Username,
		PasswordHash: passwordHash,
		Email:        req.Email,
		Phone:        req.Phone,
	}

	// 调用dao层  执行数据库操作
	err = s.authDao.CreateUser(ctx, newUser)
	if err != nil {
		return &auth.RegisterResponse{
			Code:    e.ERROR,
			Message: e.GetMsg(e.ERROR),
		}, err
	}

	// 返回用户信息
	userProto := &auth.User{
		Id:        newUser.ID,
		Username:  newUser.Username,
		Email:     newUser.Email,
		Phone:     newUser.Phone,
		CreatedAt: newUser.CreatedAt.Unix(),
		UpdatedAt: newUser.UpdatedAt.Unix(),
	}

	return &auth.RegisterResponse{
		Code:    e.SUCCESS,
		Message: e.GetMsg(e.SUCCESS),
		User:    userProto,
	}, nil
}

func (s *AuthService) Login(ctx context.Context, req *auth.LoginRequest) (*auth.LoginResponse, error) {
	// 获取用户信息
	dbUser, err := s.authDao.GetUserByUsername(ctx, req.Username)
	if err != nil {
		// 未找到记录
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &auth.LoginResponse{
				Code:    e.ERROR_USER_NOT_EXISTS,
				Message: e.GetMsg(e.ERROR_USER_NOT_EXISTS),
			}, nil
		}
		return &auth.LoginResponse{
			Code:    e.ERROR,
			Message: e.GetMsg(e.ERROR),
		}, err
	}

	// 验证密码
	if !utils.CheckPassword(req.Password, dbUser.PasswordHash) {
		// 密码错误
		return &auth.LoginResponse{
			Code:    e.ERROR_PASSWORD,
			Message: e.GetMsg(e.ERROR_PASSWORD),
		}, nil
	}

	// 生成 token
	token, err := s.jwtUtil.GenerateToken(dbUser.ID, dbUser.Username)
	if err != nil {
		return &auth.LoginResponse{
			Code:    e.ERROR_AUTH_TOKEN,
			Message: e.GetMsg(e.ERROR_AUTH_TOKEN),
		}, err
	}

	// 返回用户信息
	userProto := &auth.User{
		Id:        dbUser.ID,
		Username:  dbUser.Username,
		Email:     dbUser.Email,
		Phone:     dbUser.Phone,
		CreatedAt: dbUser.CreatedAt.Unix(),
		UpdatedAt: dbUser.UpdatedAt.Unix(),
	}

	return &auth.LoginResponse{
		Code:    e.SUCCESS,
		Message: e.GetMsg(e.SUCCESS),
		Token:   token,
		User:    userProto,
	}, nil
}
