package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/CCDD2022/seckill-system/api/middleware"
	v1 "github.com/CCDD2022/seckill-system/api/v1"
	"github.com/CCDD2022/seckill-system/config"
	"github.com/CCDD2022/seckill-system/pkg/utils"
)

func main() {
	// 加载配置
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 设置Gin模式
	switch cfg.Server.Mode {
	case "release":
		gin.SetMode(gin.ReleaseMode)
	case "test":
		gin.SetMode(gin.TestMode)
	default:
		gin.SetMode(gin.DebugMode)
	}

	// 初始化Gin引擎
	r := gin.Default()

	// 健康检查接口
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "ok",
			"message": "API Gateway is running",
		})
	})

	// 初始化gRPC客户端
	// 在这里 每个client
	clients, err := v1.InitClients(cfg)
	if err != nil {
		log.Fatalf("Failed to init gRPC clients: %v", err)
	}

	// JWT 工具
	jwtUtil := utils.NewJWTUtil(cfg.JWT.Secret, cfg.JWT.ExpireHours)

	// 创建处理器实例
	authHandler := v1.NewAuthHandler(clients.AuthService)
	userHandler := v1.NewUserHandler(clients.UserService)
	productHandler := v1.NewProductHandler(clients.ProductService)

	// 定义API路由组
	api := r.Group("/api/v1")
	{
		// 注册认证路由（无需认证）
		authHandler.RegisterRoutes(api)

		// 受保护的路由组（需要JWT认证）
		protected := api.Group("")
		protected.Use(middleware.JWTAuthMiddleware(jwtUtil))
		{
			// 注册用户路由
			userHandler.RegisterRoutes(protected)
			// 注册商品路由
			productHandler.RegisterRoutes(protected)
		}
	}

	// 启动服务器
	serverAddr := fmt.Sprintf("%s:%d", cfg.Services.APIGateway.Host, cfg.Services.APIGateway.Port)
	log.Printf("API Gateway starting on %s", serverAddr)
	if err := r.Run(serverAddr); err != nil {
		log.Fatalf("Failed to start API Gateway: %v", err)
	}
}
