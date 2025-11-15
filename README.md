
# 高并发秒杀商城 (Go-Seckill-Shop)

这是一个基于 Go 语言构建的高性能、高并发秒杀商城后端系统。项目采用现代化的 **API Gateway + gRPC 微服务** 架构，旨在模拟并解决真实世界秒杀场景下的高流量和数据一致性挑战。

##  项目特色

*   **高性能:** 核心链路采用 `gRPC` + `Protocol Buffers` 进行内部通信，性能远超传统 HTTP/JSON。
*   **高并发:** 通过 `Redis` 内存缓存 + 原子操作进行库存预扣减，并结合 `消息队列` 进行流量削峰，从容应对瞬时高流量。
*   **高扩展性:** 业务逻辑被拆分为独立的微服务，每个服务都可以独立开发、部署和扩容。
*   **强一致性:** 通过消息队列的可靠投递和消费者端的数据库事务，确保订单数据的最终一致性。
*   **现代化架构:** 遵循 Go 社区最佳实践，结构清晰，易于维护和二次开发。

## ️ 架构设计

项目采用 **API Gateway + gRPC 微服务** 模式，职责分离，性能卓越。



*   **API Gateway (Gin):** 作为系统的唯一入口，负责处理外部 HTTP 请求、用户认证(JWT)、限流，并将请求转换为 gRPC 调用转发给内部服务。
*   **gRPC 微服务:**
    *   **用户服务:** 负责用户注册、登录等身份认证相关功能。
    *   **商品服务:** 负责商品信息的管理，并利用 Redis 进行数据缓存。
    *   **秒杀服务:** 核心服务，处理秒杀请求，通过 Redis 完成库存预扣减并将订单任务推送到消息队列。
*   **消息队列 (RabbitMQ):** 流量的缓冲池，用于异步处理订单创建请求，实现流量削峰，保护下游数据库。
*   **订单服务 (消费者):** 后台服务，监听消息队列，负责将订单数据可靠地持久化到数据库。
*   **基础设施:**
    *   **MySQL:** 持久化存储用户、商品、订单等核心数据。
    *   **Redis:** 用于热点数据缓存、分布式锁、以及秒杀库存的原子操作。

##  技术栈

| 分类 | 技术 | 描述 |
| :--- | :--- | :--- |
| **语言** | Go | 项目主要开发语言 |
| **Web 框架** | Gin | 用于构建高性能的 API Gateway |
| **RPC 框架** | gRPC | 用于微服务之间的高性能通信 |
| **ORM** | GORM | 方便、高效的数据库操作工具 |
| **数据库** | MySQL | 关系型数据库，用于持久化存储 |
| **缓存** | Redis | 内存数据库，用于缓存和高并发控制 |
| **消息队列** | RabbitMQ | 用于服务解耦和流量削峰 |
| **配置管理** | Viper | 用于加载和管理项目配置 |
| **日志** | Zap | 高性能的结构化日志库 |
| **容器化** | Docker, Docker Compose | 用于项目环境的打包、部署和一键启动 |

##  快速开始

### 环境依赖

*   Go (1.21+)
*   Docker
*   Docker Compose
*   MySQL 8.0
*   Redis 7+
*   RabbitMQ 3+

### 方式一：Docker Compose 启动（推荐）

这是最简单的启动方式，它会自动构建并启动所有服务（包括 Go 应用、MySQL、Redis、RabbitMQ）。

1.  **克隆项目到本地**
    ```bash
    git clone https://github.com/CCDD2022/seckill-system.git
    cd seckill-system
    ```

2.  **启动所有服务**
    ```bash
    docker-compose up --build
    ```

3.  **服务状态检查**
    *   API Gateway: `http://localhost:8080`
    *   RabbitMQ 管理后台: `http://localhost:15672` (guest/guest)
    *   MySQL: `localhost:3306` (root/root123)
    *   Redis: `localhost:6379`

### 方式二：本地开发启动

1.  **克隆项目到本地**
    ```bash
    git clone https://github.com/CCDD2022/seckill-system.git
    cd seckill-system
    ```

2.  **安装依赖**
    ```bash
    go mod download
    ```

3.  **配置环境**
    ```bash
    cp config/config.yaml.example config/config.yaml
    # 修改 config/config.yaml 中的数据库、Redis、RabbitMQ 连接信息
    ```

4.  **初始化数据库**
    ```bash
    # 执行 scripts/init.sql 中的 SQL 语句
    mysql -u root -p < scripts/init.sql
    ```

5.  **启动各个服务**（需要在不同的终端窗口中运行）
    ```bash
    # 终端1: 启动 Auth Service
    go run cmd/auth_service/main.go
    
    # 终端2: 启动 User Service
    go run cmd/user_service/main.go
    
    # 终端3: 启动 Product Service
    go run cmd/product_service/main.go
    
    # 终端4: 启动 Seckill Service
    go run cmd/seckill_service/main.go
    
    # 终端5: 启动 Order Consumer
    go run cmd/order_consumer/main.go
    
    # 终端6: 启动 API Gateway
    go run cmd/api_gateway/main.go
    ```

### API 使用示例

项目成功启动后，您可以使用以下 API 进行测试：

#### 1. 用户注册
```bash
curl -X POST http://localhost:8080/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{
    "username": "testuser",
    "password": "password123",
    "email": "test@example.com",
    "phone": "13800138000"
  }'
```

#### 2. 用户登录
```bash
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "username": "testuser1",
    "password": "password123"
  }'
```

#### 3. 获取商品列表（需要登录）
```bash
curl -X GET "http://localhost:8080/api/v1/products?page=1&page_size=10" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

#### 4. 执行秒杀（需要登录）
```bash
curl -X POST http://localhost:8080/api/v1/seckill/execute \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "product_id": 1,
    "quantity": 1
  }'
```

### 测试账户

系统已预置测试账户（密码均为 `password123`）：
- `testuser1` / `password123`
- `testuser2` / `password123`
- `testuser3` / `password123`

##  项目结构

```
seckill-shop/
├── api/                  # API Gateway (Gin) 的路由和处理器
├── cmd/                  # 各个服务的启动入口 (main.go)
├── config/               # 配置文件
├── internal/             # 内部私有代码 (核心业务逻辑)
├── pkg/                  # 公共库
├── proto/                # Protocol Buffers (.proto 文件)
├── scripts/              # 脚本文件 (如 SQL 初始化)
└── docker-compose.yml    # Docker 编排文件
```

## 开源许可

本项目采用 [MIT License](https://opensource.org/licenses/MIT) 开源许可。