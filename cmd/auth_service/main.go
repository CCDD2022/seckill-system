package main

import (
	"fmt"
	"log"
	"net"
	"seckill-system/config"
	"seckill-system/internal/dao"
	"seckill-system/internal/dao/mysql"
	"seckill-system/internal/service"
	"seckill-system/proto_output/user"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	err := config.InitConfig("./contig/config.yaml")
	if err != nil {
		log.Fatal("配置加载失败"err)
	}
	cfg := config.GetConfig()

	db, err := mysql.InitDB(&cfg.Database.Mysql)
	if err != nil {
		log.Fatalf("连接Mysql数据库失败: %v", err)
	}
	log.Println("顺利连接数据库")

	userDao := dao.NewAuthDao(db)
	// 创建 User Service
	userService := service.NewAuthService(userDao)

	// 创建 gRPC 服务器
	grpcServer := grpc.NewServer()
	// 测试的时候会依赖反射调用  生产环境要去掉
	reflection.Register(grpcServer)
	// 当收到user.UserService/Register的时候  调用userService.Register方法
	user.RegisterUserServiceServer(grpcServer, userService)

	// 启动 gRPC 服务器
	lis, err := net.Listen("tcp", fmt.Sprintf("%s:%s", cfg.Services.UserService.Host, cfg.Services.UserService.Port))
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	log.Println("User gRPC service started on :", cfg.Services.UserService.Port)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
