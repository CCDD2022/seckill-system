package grpc

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/CCDD2022/seckill-system/config"
	"github.com/CCDD2022/seckill-system/pkg/logger"
	"github.com/CCDD2022/seckill-system/proto_output/auth"
	"github.com/CCDD2022/seckill-system/proto_output/order"
	"github.com/CCDD2022/seckill-system/proto_output/product"
	"github.com/CCDD2022/seckill-system/proto_output/seckill"
	"github.com/CCDD2022/seckill-system/proto_output/user"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

type Clients struct {
	// 客户端接口 供业务处理器直接使用  线程安全 可并发调用
	AuthService    auth.AuthServiceClient       // 认证服务客户端
	UserService    user.UserServiceClient       // 用户服务客户端
	ProductService product.ProductServiceClient // 商品服务客户端
	SeckillService seckill.SeckillServiceClient // 秒杀服务客户端
	OrderService   order.OrderServiceClient     // 订单服务客户端

	// 保存底层的*grpc.ClientConn 以便在关闭时释放资源或需要访问底层连接选项时使用
	authConn    *grpc.ClientConn // 认证服务连接
	userConn    *grpc.ClientConn // 用户服务连接
	productConn *grpc.ClientConn // 商品服务连接
	seckillConn *grpc.ClientConn // 秒杀服务连接
	orderConn   *grpc.ClientConn // 订单服务连接

	// 控制字段
	ctx    context.Context    // 用于通知监控协程退出
	cancel context.CancelFunc // 调用后关闭所有监控
	wg     sync.WaitGroup     // 等待监控协程优雅退出
}

func InitClients(cfg *config.Config) (*Clients, error) {
	// 创建用于管理监控协程生命周期的上下文
	ctx, cancel := context.WithCancel(context.Background())
	clients := &Clients{
		ctx:    ctx,
		cancel: cancel,
	}

	// 任意一个连接建立失败就回滚

	//  建立AuthService连接
	authConn, err := createConnection("auth", cfg.Services.AuthService.Host, cfg.Services.AuthService.Port)
	if err != nil {
		clients.Close() // 初始化失败时也要正确释放资源
		return nil, err
	}
	clients.authConn = authConn
	clients.AuthService = auth.NewAuthServiceClient(authConn)

	// 建立UserService连接
	userConn, err := createConnection("user", cfg.Services.UserService.Host, cfg.Services.UserService.Port)
	if err != nil {
		clients.Close()
		return nil, err
	}
	clients.userConn = userConn
	clients.UserService = user.NewUserServiceClient(userConn)

	//  建立ProductService连接
	productConn, err := createConnection("product", cfg.Services.ProductService.Host, cfg.Services.ProductService.Port)
	if err != nil {
		clients.Close()
		return nil, err
	}
	clients.productConn = productConn
	clients.ProductService = product.NewProductServiceClient(productConn)

	//  建立SeckillService连接
	seckillConn, err := createConnection("seckill", cfg.Services.SeckillService.Host, cfg.Services.SeckillService.Port)
	if err != nil {
		clients.Close()
		return nil, err
	}
	clients.seckillConn = seckillConn
	clients.SeckillService = seckill.NewSeckillServiceClient(seckillConn)

	//  建立OrderService连接
	orderConn, err := createConnection("order", cfg.Services.OrderService.Host, cfg.Services.OrderService.Port)
	if err != nil {
		clients.Close()
		return nil, err
	}
	clients.orderConn = orderConn
	clients.OrderService = order.NewOrderServiceClient(orderConn)

	// 启动后台连接状态监控
	clients.watchServiceState("auth", authConn)
	clients.watchServiceState("user", userConn)
	clients.watchServiceState("product", productConn)
	clients.watchServiceState("seckill", seckillConn)
	clients.watchServiceState("order", orderConn)

	logger.Info("All gRPC clients initialized and state watchers started")
	return clients, nil
}

// createConnection 与后端gRPC服务端建立连接
func createConnection(serviceName string, host string, port int) (*grpc.ClientConn, error) {
	addr := fmt.Sprintf("%s:%d", host, port)

	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithReadBufferSize(64<<10),  // 64KB
		grpc.WithWriteBufferSize(64<<10), // 64KB
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(16<<20), // 16MB
			grpc.MaxCallSendMsgSize(16<<20),
		),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                30 * time.Second,
			Timeout:             10 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.WithConnectParams(grpc.ConnectParams{
			MinConnectTimeout: 5 * time.Second,
			Backoff: backoff.Config{
				BaseDelay:  100 * time.Millisecond,
				Multiplier: 1.6,
				Jitter:     0.2,
				MaxDelay:   2 * time.Second,
			},
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect %s at %s: %w", serviceName, addr, err)
	}

	logger.Info(fmt.Sprintf("Successfully connected to %s service at %s", serviceName, addr))
	return conn, nil
}

// watchServiceState 监控单个服务的连接状态变化
// 在独立协程中运行，通过WaitForStateChange阻塞等待状态变更事件
func (c *Clients) watchServiceState(serviceName string, conn *grpc.ClientConn) {
	// 确保Close()方法会等待此协程退出
	c.wg.Add(1)

	go func() {
		defer c.wg.Done()
		defer logger.Info(fmt.Sprintf("State watcher stopped for %s service", serviceName))

		for {
			// 获取当前连接状态
			state := conn.GetState()
			logger.Debug("Service connection state", "service", serviceName, "state", state.String())

			// 检查不健康状态并记录
			if state == connectivity.TransientFailure {
				logger.Error("Service connection is in transient failure state",
					"service", serviceName,
					"state", state.String())
				// 实际场景可在此添加熔断、告警逻辑
			} else if state == connectivity.Shutdown {
				// 连接已关闭，监控协程退出
				logger.Warn("Service connection shutdown", "service", serviceName)
				return
			}

			// 阻塞 等待状态变化或上下文取消
			// 返回false表示ctx.Done()被触发，需要退出
			if !conn.WaitForStateChange(c.ctx, state) {
				return
			}
		}
	}()
}

// Close 关闭所有连接并停止状态监控
func (c *Clients) Close() error {
	// 1. 通知所有监控协程退出
	if c.cancel != nil {
		c.cancel()
	}

	// 2. 等待所有监控协程优雅退出（防止goroutine泄漏）
	c.wg.Wait()

	// 3. 关闭底层连接
	var errs []error
	if c.authConn != nil {
		if err := c.authConn.Close(); err != nil {
			errs = append(errs, fmt.Errorf("auth conn: %w", err))
		}
	}
	if c.userConn != nil {
		if err := c.userConn.Close(); err != nil {
			errs = append(errs, fmt.Errorf("user conn: %w", err))
		}
	}
	if c.productConn != nil {
		if err := c.productConn.Close(); err != nil {
			errs = append(errs, fmt.Errorf("product conn: %w", err))
		}
	}
	if c.seckillConn != nil {
		if err := c.seckillConn.Close(); err != nil {
			errs = append(errs, fmt.Errorf("seckill conn: %w", err))
		}
	}
	if c.orderConn != nil {
		if err := c.orderConn.Close(); err != nil {
			errs = append(errs, fmt.Errorf("order conn: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("close errors: %v", errs)
	}
	return nil
}
