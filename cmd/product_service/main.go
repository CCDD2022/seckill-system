package main

import (
	"fmt"
	"log"
	"net"

	"github.com/CCDD2022/seckill-system/config"
	"github.com/CCDD2022/seckill-system/internal/dao"
	"github.com/CCDD2022/seckill-system/internal/dao/mysql"
	"github.com/CCDD2022/seckill-system/internal/dao/redis"
	"github.com/CCDD2022/seckill-system/internal/service"
	"github.com/CCDD2022/seckill-system/proto_output/product"

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

	redisDB, err := redis.InitRedis(&cfg.Database.Redis)
	if err != nil {
		log.Fatalf("连接Redis数据库失败: %v", err)
	}
	log.Println("顺利连接数据库")

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
		log.Fatalf("Failed to listen: %v", err)
	}

	log.Println("Product gRPC service started on :", cfg.Services.ProductService.Port)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
