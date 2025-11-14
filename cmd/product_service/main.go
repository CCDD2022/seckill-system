package main

import (
	"fmt"
	"net"

	"github.com/CCDD2022/seckill-system/internal/dao"
	"github.com/CCDD2022/seckill-system/internal/dao/mysql"
	"github.com/CCDD2022/seckill-system/internal/dao/redis"
	"github.com/CCDD2022/seckill-system/internal/service"
	"github.com/CCDD2022/seckill-system/pkg/app"
	"github.com/CCDD2022/seckill-system/pkg/logger"
	"github.com/CCDD2022/seckill-system/proto_output/product"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	cfg := app.BootstrapApp()

	db, err := mysql.InitDB(&cfg.Database.Mysql)
	if err != nil {
		logger.Error("连接Mysql数据库失败: ", "err", err)
	}

	redisDB, err := redis.InitRedis(&cfg.Database.Redis)
	if err != nil {
		logger.Error("连接Redis数据库失败: ", "err", err)
	}
	logger.Info("顺利连接数据库")

	ProductDao := dao.NewProductDao(db, redisDB)
	// 创建 Product Service
	ProductService := service.NewProductService(ProductDao)

	// 创建 gRPC 服务器
	grpcServer := grpc.NewServer()
	// 测试的时候会依赖反射调用  生产环境要去掉
	reflection.Register(grpcServer)
	// 当收到Product.ProductService/Register的时候  调用ProductService.Register方法
	product.RegisterProductServiceServer(grpcServer, ProductService)

	// 启动 gRPC 服务器
	lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", cfg.Services.ProductService.Host, cfg.Services.ProductService.Port))
	if err != nil {
		logger.Error("Failed to listen: ", "err", err)
	}

	logger.Info("Product gRPC service started on :", cfg.Services.ProductService.Port)
	if err := grpcServer.Serve(lis); err != nil {
		logger.Error("Failed to serve: ", "err", err)
	}
}
