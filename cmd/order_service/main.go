// OrderService gRPC 启动入口
package main

import (
	"fmt"
	"net"

	"github.com/CCDD2022/seckill-system/internal/dao"
	"github.com/CCDD2022/seckill-system/internal/dao/mysql"
	"github.com/CCDD2022/seckill-system/internal/service"
	"github.com/CCDD2022/seckill-system/pkg/app"
	"github.com/CCDD2022/seckill-system/pkg/logger"
	"github.com/CCDD2022/seckill-system/proto_output/order"
	"github.com/streadway/amqp"
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

	var mqChan *amqp.Channel
	mqURL := fmt.Sprintf("amqp://%s:%s@%s:%d/", cfg.MQ.User, cfg.MQ.Password, cfg.MQ.Host, cfg.MQ.Port)
	mqConn, err := amqp.Dial(mqURL)
	if err != nil {
		logger.Error("连接RabbitMQ失败", "err", err)
	} else {
		ch, chErr := mqConn.Channel()
		if chErr != nil {
			logger.Error("打开RabbitMQ通道失败", "err", chErr)
		} else {
			_, _ = ch.QueueDeclare("order.canceled", true, false, false, false, nil)
			mqChan = ch
		}
	}

	orderService := service.NewOrderServiceWithMQ(orderDao, mqChan)
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
