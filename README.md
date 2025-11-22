
# 高并发秒杀商城 (Go-Seckill-Shop)

这是一个基于 Go 语言构建的高性能、高并发秒杀商城后端系统。项目采用现代化的 **API Gateway + gRPC 微服务** 架构，旨在模拟并解决真实世界秒杀场景下的高流量和数据一致性挑战。

##  项目特色

*   **高性能:** 核心链路采用 `gRPC` + `Protocol Buffers` 进行内部通信，性能远超传统 HTTP/JSON。
*   **高并发:** 使用 **Redis Cluster** + Lua 原子扣减实现热点库存低延迟处理，结合 **RabbitMQ 发布确认 + 批消费** 做削峰与最终一致。
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
*   **消息队列 (RabbitMQ):** Topic Exchange + 发布确认；消息含唯一 `MessageId` 支持消费端幂等；批量消费与批量落库减少写放大。
*   **订单消费者 / 库存对账:** 批量解析订单消息写入 MySQL；独立库存对账服务从 Redis 脏集合批量同步真实库存到 MySQL，保障最终一致。
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
| **缓存** | Redis Cluster | 分布式高可用，高并发库存与热数据缓存 |
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

### 生产环境一键部署

仅保留生产模式：Redis 与 RabbitMQ 在宿主机自启动，Compose 负责 MySQL 与全部 Go 微服务。

#### 步骤 1 克隆项目

```bash
git clone https://github.com/CCDD2022/seckill-system.git
cd seckill-system/backend
```

#### 步骤 2 构建并启动

```bash
docker compose up -d --build
```

#### 步骤 3 检查服务

```bash
docker compose ps
```

#### 步骤 4 Nginx 前端与反代

确保 `/api/` 指向本机 `8080`。

### 配置说明

环境变量 `CONFIG_PATH=/app/config/config.docker.yaml` 在容器内指向生产配置；本地开发继续使用 `config/config.yaml`。

RabbitMQ 注意：默认 `guest/guest` 仅允许从 127.0.0.1 访问，容器内通过 `host.docker.internal` 会被视为远程。
请在宿主机 RabbitMQ 中创建生产账号，例如：
```bash
# 假设已安装 rabbitmqctl
rabbitmqctl add_user seckill_prod strong_password_here
rabbitmqctl set_user_tags seckill_prod administrator
rabbitmqctl set_permissions -p / seckill_prod ".*" ".*" ".*"
```
如果创建了新的 RabbitMQ 用户，请修改 `config/config.docker.yaml` 中 `mq.user` 与 `mq.password`（开发环境用本地 `config.yaml` 另行调整）。

Nginx 示例（已部署，复述要点）:
```nginx
server {
  listen 80;
  server_name 175.27.226.213;
  location / { root /home/cwx/seckill-system/fronted/dist; index index.html; try_files $uri $uri/ /index.html; gzip on; gzip_types text/plain text/css application/javascript application/json; }
  location /api/ { proxy_pass http://127.0.0.1:8080; proxy_set_header Host $host; proxy_set_header X-Real-IP $remote_addr; proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for; proxy_set_header X-Forwarded-Proto $scheme; }
  location ~* \.(js|css|png|jpg|jpeg|gif|ico|svg)$ { root /home/cwx/seckill-system/fronted/dist; expires 30d; add_header Cache-Control "public, immutable"; access_log off; }
}
```

配置文件说明：
* `config.yaml` —— 本地非 Docker 开发使用（localhost 等）。
* `config.docker.yaml` —— 生产容器使用（容器名与 host.docker.internal）。

### （可选）本地源码调试

仍可直接 `go run cmd/<service>/main.go` 启动单个服务，需自行提供外部依赖地址。
    
    # 终端2: 启动 User Service
    go run cmd/user_service/main.go
    
    # 终端3: 启动 Product Service
    go run cmd/product_service/main.go
    
    # 终端4: 启动 Seckill Service
    go run cmd/seckill_service/main.go
    
    # 终端5: 启动 Order Consumer
    go run cmd/order_create_consumer/main.go
    
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
* `testuser1` / `password123`
* `testuser2` / `password123`
* `testuser3` / `password123`

##  项目结构

```
seckill-shop/
├── api/                  # API Gateway (Gin) 路由与处理器
├── cmd/                  # 各个服务的启动入口 (main.go)
├── config/               # 配置文件
├── internal/             # 内部私有代码 (核心业务逻辑)
├── pkg/                  # 公共库
├── proto/                # Protocol Buffers (.proto 文件)
├── scripts/              # 脚本文件 (如 SQL 初始化)
├── docs/                 # 架构/压测/部署文档
└── docker-compose.yml    # 生产编排（MySQL + 微服务，外部 Redis/RabbitMQ）
## Redis Cluster 初始化指引

项目已提供基于 Docker Compose 的 3 主 3 从拓扑（服务名 `redis-node-1` .. `redis-node-6`）。`redis-cluster-init` 服务在启动后自动执行：

```bash
redis-cli --cluster create redis-node-1:6379 redis-node-2:6379 redis-node-3:6379 redis-node-4:6379 redis-node-5:6379 redis-node-6:6379 --cluster-replicas 1 --cluster-yes
```

如需在物理/Windows环境自行构建，可分别启动六个实例并执行同上述命令（确保端口互通）。

## 秒杀链路优化要点

1. 入口限流配置化 (`rate_limits`)，支持按场景调优。
2. Redis Lua 单键操作确保 Cluster 下无跨槽；库存脏集合 SPOP 批处理降低对账频率开销。
3. RabbitMQ 发布确认 + `MessageId` 幂等，防止“库存扣减成功但消息丢失”引起的不一致。
4. 订单与库存对账采用批处理与单条 CASE WHEN 更新，降低行锁竞争与慢 SQL 频率。
5. 频道池 (Channel Pool) 提升并发发布吞吐；可根据压测调整 `channel_pool_size`。

## 压测与调优

主要参数：

* `mq.consumer_prefetch` 控制消费者预取批量；过小降低吞吐，过大增加延迟。
* `mq.order_batch_size` 与 `order_batch_interval_ms` 控制批落库；需要在吞吐与延迟之间权衡。
* `rate_limits.seckill` 在压测阶段可临时提升防止限流成为瓶颈。
* MySQL `max_open_conns` 与 Redis IO 线程、内核参数（容器外）需与目标并发匹配。

压测脚本：详见 `docs/LOAD_TEST.md` 与 `loadtest/seckill_vegeta.go`。

## 开源许可

本项目采用 [MIT License](https://opensource.org/licenses/MIT) 开源许可。
