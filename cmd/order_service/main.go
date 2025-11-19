// OrderService gRPC 启动入口
package main

import (
	"fmt"
	"net"

	"github.com/CCDD2022/seckill-system/internal/dao"
	"github.com/CCDD2022/seckill-system/internal/dao/mysql"
	"github.com/CCDD2022/seckill-system/internal/mq"
	"github.com/CCDD2022/seckill-system/internal/service"
	"github.com/CCDD2022/seckill-system/pkg/app"
	"github.com/CCDD2022/seckill-system/pkg/logger"
	"github.com/CCDD2022/seckill-system/proto_output/order"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	cfg := app.BootstrapApp()

	db, err := mysql.InitDB(&cfg.Database.Mysql)
	if err != nil {
		logger.Error("连接Mysql数据库失败", "err", err)
		return
	}
	logger.Info("数据库连接成功")
	orderDao := dao.NewOrderDao(db)

	mqPool, err := mq.Init(&cfg.MQ)
	if err != nil {
		logger.Warn("init mq failed", "err", err)
	}

	orderService := service.NewOrderServiceWithMQ(orderDao, mqPool)
	grpcServer := grpc.NewServer()
	reflection.Register(grpcServer)
	order.RegisterOrderServiceServer(grpcServer, orderService)

	lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", cfg.Services.OrderService.Host, cfg.Services.OrderService.Port))
	if err != nil {
		logger.Error("监听端口失败", "err", err)
		return
	}
	logger.Info("Order gRPC service started", "port", cfg.Services.OrderService.Port)
	if err := grpcServer.Serve(lis); err != nil {
		logger.Error("gRPC服务启动失败", "err", err)
	}
}
