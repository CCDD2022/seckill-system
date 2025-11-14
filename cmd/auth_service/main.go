package main

import (
	"fmt"
	"net"

	"github.com/CCDD2022/seckill-system/internal/dao"
	"github.com/CCDD2022/seckill-system/internal/dao/mysql"
	"github.com/CCDD2022/seckill-system/internal/service"
	"github.com/CCDD2022/seckill-system/pkg/app"
	"github.com/CCDD2022/seckill-system/pkg/logger"
	"github.com/CCDD2022/seckill-system/proto_output/auth"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	cfg := app.BootstrapApp()
	db, err := mysql.InitDB(&cfg.Database.Mysql)
	if err != nil {
		logger.Error("连接Mysql数据库失败: ", "err", err)
	}
	logger.Info("顺利连接数据库")

	authDao := dao.NewAuthDao(db)
	// 创建 Auth Service
	authService := service.NewAuthService(authDao, cfg.JWT.Secret, cfg.JWT.ExpireHours)

	// 创建 gRPC 服务器
	grpcServer := grpc.NewServer()
	// 测试的时候会依赖反射调用  生产环境要去掉
	reflection.Register(grpcServer)
	// 当收到auth.authService/Register的时候  调用authService.Register方法
	auth.RegisterAuthServiceServer(grpcServer, authService)

	// 启动 gRPC 服务器
	lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", cfg.Services.AuthService.Host, cfg.Services.AuthService.Port))
	if err != nil {
		logger.Error("Failed to listen: ", "err", err)
	}

	logger.Info("Auth gRPC service started on ", "port", cfg.Services.AuthService.Port)
	if err := grpcServer.Serve(lis); err != nil {
		logger.Error("Failed to serve: ", "err", err)
	}
}
