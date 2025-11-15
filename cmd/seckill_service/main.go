package main

import (
	"fmt"
	"log"
	"net"

	"github.com/CCDD2022/seckill-system/internal/dao"
	"github.com/CCDD2022/seckill-system/internal/dao/mysql"
	"github.com/CCDD2022/seckill-system/internal/dao/redis"
	"github.com/CCDD2022/seckill-system/internal/service"
	"github.com/CCDD2022/seckill-system/pkg/app"
	"github.com/CCDD2022/seckill-system/pkg/logger"
	"github.com/CCDD2022/seckill-system/proto_output/seckill"
	"github.com/streadway/amqp"

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

	// 连接Redis
	redisDB, err := redis.InitRedis(&cfg.Database.Redis)
	if err != nil {
		logger.Error("连接Redis数据库失败: ", "err", err)
	}
	logger.Info("顺利连接数据库")

	// 连接RabbitMQ
	mqConn, err := amqp.Dial(fmt.Sprintf("amqp://%s:%s@%s:%d/",
		cfg.MQ.User,
		cfg.MQ.Password,
		cfg.MQ.Host,
		cfg.MQ.Port))
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer mqConn.Close()

	mqChannel, err := mqConn.Channel()
	if err != nil {
		log.Fatalf("Failed to open a channel: %v", err)
	}
	defer mqChannel.Close()

	// 声明队列
	_, err = mqChannel.QueueDeclare(
		"order.create", // name
		true,           // durable
		false,          // delete when unused
		false,          // exclusive
		false,          // no-wait
		nil,            // arguments
	)
	if err != nil {
		log.Fatalf("Failed to declare a queue: %v", err)
	}

	logger.Info("RabbitMQ connected")

	// 创建ProductDao
	productDao := dao.NewProductDao(db, redisDB)

	// 创建 Seckill Service
	seckillService := service.NewSeckillService(productDao, redisDB, mqChannel)

	// 创建 gRPC 服务器
	grpcServer := grpc.NewServer()
	reflection.Register(grpcServer)
	seckill.RegisterSeckillServiceServer(grpcServer, seckillService)

	// 启动 gRPC 服务器
	lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", cfg.Services.SeckillService.Host, cfg.Services.SeckillService.Port))
	if err != nil {
		logger.Error("Failed to listen: ", "err", err)
	}

	logger.Info("Seckill gRPC service started on :", cfg.Services.SeckillService.Port)
	if err := grpcServer.Serve(lis); err != nil {
		logger.Error("Failed to serve: ", "err", err)
	}
}
