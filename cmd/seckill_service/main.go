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

	// 创建通道池
	poolSize := cfg.MQ.ChannelPoolSize
	channels := make([]*amqp.Channel, 0, poolSize)
	for i := 0; i < poolSize; i++ {
		ch, err := mqConn.Channel()
		if err != nil {
			log.Fatalf("Failed to open a channel: %v", err)
		}
		channels = append(channels, ch)
	}
	defer func() {
		for _, ch := range channels {
			_ = ch.Close()
		}
	}()

	// 声明 Topic Exchange 和相关队列/绑定（幂等）
	const exchangeName = "seckill.exchange"
	if err := channels[0].ExchangeDeclare(
		exchangeName,
		"topic",
		true,  // durable
		false, // auto-deleted
		false, // internal
		false, // no-wait
		nil,
	); err != nil {
		log.Fatalf("Failed to declare exchange: %v", err)
	}

	ordersQ, err := channels[0].QueueDeclare(
		"orders",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("Failed to declare orders queue: %v", err)
	}
	if err := channels[0].QueueBind(ordersQ.Name, "order.#", exchangeName, false, nil); err != nil {
		log.Fatalf("Failed to bind orders queue: %v", err)
	}

	stockLogQ, err := channels[0].QueueDeclare(
		"stock_log",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("Failed to declare stock_log queue: %v", err)
	}
	if err := channels[0].QueueBind(stockLogQ.Name, "stock.#", exchangeName, false, nil); err != nil {
		log.Fatalf("Failed to bind stock_log queue: %v", err)
	}

	logger.Info("RabbitMQ connected")

	// 创建ProductDao
	productDao := dao.NewProductDao(db, redisDB)

	// 创建 Seckill Service（传入通道池）
	seckillService := service.NewSeckillService(productDao, redisDB, channels)

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
