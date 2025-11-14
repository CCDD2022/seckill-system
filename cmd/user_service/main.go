package main

import (
	"fmt"

	"github.com/CCDD2022/seckill-system/pkg/logger"

	"net"

	"github.com/CCDD2022/seckill-system/internal/dao"
	"github.com/CCDD2022/seckill-system/internal/dao/mysql"
	"github.com/CCDD2022/seckill-system/internal/service"
	"github.com/CCDD2022/seckill-system/pkg/app"
	"github.com/CCDD2022/seckill-system/proto_output/user"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	cfg := app.BootstrapApp()

	// 连接数据库
	db, err := mysql.InitDB(&cfg.Database.Mysql)
	if err != nil {
		logger.Error("连接Mysql数据库失败: ", "err", err)
	}
	logger.Info("顺利连接数据库")

	userDao := dao.NewUserDao(db)
	// 创建 User Service
	userService := service.NewUserService(userDao)

	// 创建 gRPC 服务器
	grpcServer := grpc.NewServer()
	// 测试的时候会依赖反射调用  生产环境要去掉
	reflection.Register(grpcServer)
	// 当收到user.UserService/Register的时候  调用userService.Register方法
	user.RegisterUserServiceServer(grpcServer, userService)

	// 启动 gRPC 服务器
	lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", cfg.Services.UserService.Host, cfg.Services.UserService.Port))
	if err != nil {
		logger.Error("Failed to listen: ", "err", err)
	}

	logger.Info("User gRPC service started on :", "port", cfg.Services.UserService.Port)
	if err := grpcServer.Serve(lis); err != nil {
		logger.Error("Failed to serve: ", "err", err)
	}
}
