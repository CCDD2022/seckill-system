package main

import (
	"fmt"
	"log"
	"net"

	"github.com/CCDD2022/seckill-system/config"
	"github.com/CCDD2022/seckill-system/internal/dao"
	"github.com/CCDD2022/seckill-system/internal/dao/mysql"
	"github.com/CCDD2022/seckill-system/internal/service"
	"github.com/CCDD2022/seckill-system/proto_output/auth"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatal("配置加载失败", err)
	}

	db, err := mysql.InitDB(&cfg.Database.Mysql)
	if err != nil {
		log.Fatalf("连接Mysql数据库失败: %v", err)
	}
	log.Println("顺利连接数据库")

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
		log.Fatalf("Failed to listen: %v", err)
	}

	log.Println("Auth gRPC service started on :", cfg.Services.AuthService.Port)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
