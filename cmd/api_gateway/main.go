package main

import (
	"fmt"
	"net/http"

	"github.com/CCDD2022/seckill-system/pkg/app"
	"github.com/CCDD2022/seckill-system/pkg/logger"
	"github.com/gin-gonic/gin"

	"github.com/CCDD2022/seckill-system/api/middleware"
	v1 "github.com/CCDD2022/seckill-system/api/v1"
	"github.com/CCDD2022/seckill-system/pkg/utils"
)

func main() {
	// 加载配置
	cfg := app.BootstrapApp()

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

	// 全局限流中间件 (每秒100个请求，允许200个突发)
	r.Use(middleware.RateLimitMiddleware(100, 200))

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
		logger.Error("Failed to init gRPC clients: ", "err", err)
	}

	// JWT 工具
	jwtUtil := utils.NewJWTUtil(cfg.JWT.Secret, cfg.JWT.ExpireHours)

	// 创建处理器实例
	authHandler := v1.NewAuthHandler(clients.AuthService)
	userHandler := v1.NewUserHandler(clients.UserService)
	productHandler := v1.NewProductHandler(clients.ProductService)
	seckillHandler := v1.NewSeckillHandler(clients.SeckillService)

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

		// 秒杀路由（需要JWT认证 + 更严格的限流）
		seckillProtected := api.Group("")
		seckillProtected.Use(middleware.JWTAuthMiddleware(jwtUtil))
		seckillProtected.Use(middleware.SeckillRateLimitMiddleware())
		{
			seckillHandler.RegisterRoutes(seckillProtected)
		}
	}

	// 启动服务器
	serverAddr := fmt.Sprintf("%s:%d", cfg.Services.APIGateway.Host, cfg.Services.APIGateway.Port)
	logger.Info("API Gateway starting on " + serverAddr)
	if err := r.Run(serverAddr); err != nil {
		logger.Error("Failed to start API Gateway: ", "err", err)
	}
}
