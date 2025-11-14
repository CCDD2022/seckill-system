package v1

import (
	"context"
	"fmt"
	"log"

	"github.com/CCDD2022/seckill-system/config"
	"github.com/CCDD2022/seckill-system/proto_output/auth"
	"github.com/CCDD2022/seckill-system/proto_output/product"
	"github.com/CCDD2022/seckill-system/proto_output/user"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
)

type Clients struct {
	// 客户端接口 供业务处理器直接使用  线程安全 可并发调用
	AuthService    auth.AuthServiceClient       // 认证服务客户端
	UserService    user.UserServiceClient       // 用户服务客户端
	ProductService product.ProductServiceClient // 商品服务客户端
	// 保存底层的*grpc.ClientConn 以便在关闭时释放资源或需要访问底层连接选项时使用
	authConn    *grpc.ClientConn // 认证服务连接
	userConn    *grpc.ClientConn // 用户服务连接
	productConn *grpc.ClientConn // 商品服务连接
}

func InitClients(cfg *config.Config) (*Clients, error) {
	clients := &Clients{}

	// createConnection  与后端的gRPC服务端建立一个底层的TCP连接
	// authConn代表一个活跃的 通向底层地址的微服务的网络连接
	authConn, err := createConnection("auth", cfg.Services.AuthService.Host, cfg.Services.AuthService.Port)
	if err != nil {
		// 资源回收  依次检查底层连接是否为nil  如果非nil 则需要依次关闭
		clients.Close()
		return nil, err
	}
	// 在与底层连接的基础上 建立一个gRPC客户端
	// .proto文件定义了AuthService服务 以及他所拥有的方法(Login等等)
	// protoc根据这些定义生成了AuthServiceClient结构体
	// 它知道如何把go的方法调用 例如Client.Login() 转换成gRPC的二进制消息格式
	// 并且通过authConn网络连接发送出去
	clients.authConn = authConn
	// 使用工厂方法 创建具体的gRPC客户端
	clients.AuthService = auth.NewAuthServiceClient(authConn)

	// ✅ 连接UserService
	userConn, err := createConnection("user", cfg.Services.UserService.Host, cfg.Services.UserService.Port)
	if err != nil {
		clients.Close()
		return nil, err
	}
	clients.userConn = userConn
	clients.UserService = user.NewUserServiceClient(userConn)

	// ✅ 连接ProductService
	productConn, err := createConnection("product", cfg.Services.ProductService.Host, cfg.Services.ProductService.Port)
	if err != nil {
		clients.Close()
		return nil, err
	}
	clients.productConn = productConn
	clients.ProductService = product.NewProductServiceClient(productConn)

	return clients, nil
}
func createConnection(serviceName string, host string, port int) (*grpc.ClientConn, error) {
	// 地址
	addr := fmt.Sprintf("%s:%d", host, port)

	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect %s at %s: %w", serviceName, addr, err)
	}

	// 健康检查
	healthClient := grpc_health_v1.NewHealthClient(conn)
	healthResp, err := healthClient.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
	if err == nil {
		if healthResp.Status != grpc_health_v1.HealthCheckResponse_SERVING {
			conn.Close()
			return nil, fmt.Errorf("service %s is not serving", serviceName)
		}
	} else {
		// 如果服务未实现健康检查，不作为致命错误
		log.Printf("warn: health check unavailable for %s at %s: %v", serviceName, addr, err)
	}

	log.Printf("Successfully connected to %s service at %s", serviceName, addr)
	return conn, nil
}

func (c *Clients) Close() error {
	var errs []error
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
	if c.authConn != nil {
		if err := c.authConn.Close(); err != nil {
			errs = append(errs, fmt.Errorf("auth conn: %w", err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("close errors: %v", errs)
	}
	return nil
}
