package main

import (
	"fmt"
	"net"
	"time"

	"github.com/CCDD2022/seckill-system/internal/dao"
	"github.com/CCDD2022/seckill-system/internal/dao/mysql"
	"github.com/CCDD2022/seckill-system/internal/dao/redis"
	"github.com/CCDD2022/seckill-system/internal/service"
	"github.com/CCDD2022/seckill-system/pkg/app"
	"github.com/CCDD2022/seckill-system/pkg/logger"
	"github.com/CCDD2022/seckill-system/proto_output/seckill"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	cfg := app.BootstrapApp()

	// 连接数据库
	db, err := mysql.InitDB(&cfg.Database.Mysql)
	if err != nil {
		logger.Fatal("连接Mysql数据库失败: ", "err", err)
	}

	// 连接Redis
	redisDB, err := redis.InitRedis(&cfg.Database.Redis)
	if err != nil {
		logger.Fatal("连接Redis数据库失败: ", "err", err)
	}
	logger.Info("顺利连接数据库")

	// ✅ 预热Redis连接池（关键！）
	if err := redis.WarmupRedis(redisDB, 100); err != nil {
		logger.Warn("Redis预热失败", "err", err)
	} else {
		logger.Info("Redis连接池预热完成", "minIdleConns", 100)
	}

	// 创建ProductDao
	productDao := dao.NewProductDao(db, redisDB)

	// Redis 原子预占并记录待发事件；独立 relay 负责 RabbitMQ 交付。
	seckillService := service.NewSeckillService(productDao)

	// 创建 gRPC 服务器
	grpcServer := grpc.NewServer(
		grpc.MaxConcurrentStreams(10000),
		grpc.NumStreamWorkers(100),
		grpc.InitialWindowSize(1<<24), // 16MB
		grpc.InitialConnWindowSize(1<<24),
		grpc.ConnectionTimeout(10*time.Second),
	)
	reflection.Register(grpcServer)
	seckill.RegisterSeckillServiceServer(grpcServer, seckillService)

	// 启动 gRPC 服务器
	lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", cfg.Services.SeckillService.Host, cfg.Services.SeckillService.Port))
	if err != nil {
		logger.Fatal("Failed to listen: ", "err", err)
	}

	logger.Info("Seckill gRPC service started on :", cfg.Services.SeckillService.Port)
	if err := grpcServer.Serve(lis); err != nil {
		logger.Error("Failed to serve: ", "err", err)
	}
}
