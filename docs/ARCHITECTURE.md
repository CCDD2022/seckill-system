# 系统架构文档

## 系统概览

本项目是一个基于 Go 语言构建的高性能、高并发秒杀商城系统，采用微服务架构设计。

## 架构图

```
┌─────────────┐
│   客户端     │
└──────┬──────┘
       │ HTTP
       ▼
┌─────────────────┐
│  API Gateway    │ (Gin)
│  - JWT 认证     │
│  - 限流控制     │
│  - 路由转发     │
└────────┬────────┘
         │ gRPC
    ┌────┴────┬──────────┬───────────┬──────────┐
    ▼         ▼          ▼           ▼          ▼
┌────────┐ ┌──────┐ ┌─────────┐ ┌─────────┐ ┌──────┐
│  Auth  │ │ User │ │Product  │ │Seckill  │ │Order │
│Service │ │Service│ │Service  │ │Service  │ │Service│
└────┬───┘ └───┬──┘ └────┬────┘ └────┬────┘ └───┬──┘
     │         │         │            │           │
     └─────────┴─────────┴────────────┴───────────┘
                        │
           ┌────────────┼────────────┐
           ▼            ▼            ▼
       ┌──────┐    ┌───────┐    ┌──────────┐
       │MySQL │    │ Redis │    │ RabbitMQ │
       └──────┘    └───────┘    └────┬─────┘
                                      │
                                      ▼
                              ┌──────────────┐
                              │Order Consumer│
                              └──────────────┘
```

## 核心组件

### 1. API Gateway (端口: 8080)
- **技术栈**: Gin Web Framework
- **职责**:
  - 统一入口，处理所有外部 HTTP 请求
  - JWT 认证和授权
  - 全局限流 (100 req/s)
  - 路由转发到相应的微服务
  - 请求日志和监控

### 2. Auth Service (端口: 50053)
- **技术栈**: gRPC, GORM
- **职责**:
  - 用户注册
  - 用户登录
  - JWT Token 生成
  - 密码加密 (bcrypt)

### 3. User Service (端口: 50051)
- **技术栈**: gRPC, GORM
- **职责**:
  - 用户信息查询
  - 用户信息更新
  - 密码修改

### 4. Product Service (端口: 50052)
- **技术栈**: gRPC, GORM, Redis
- **职责**:
  - 商品 CRUD 操作
  - 商品信息缓存 (Redis)
  - 缓存自动刷新
  - 库存查询

### 5. Seckill Service (端口: 50054)
- **技术栈**: gRPC, Redis, RabbitMQ
- **职责**:
  - 处理秒杀请求
  - Redis 库存预扣减 (Lua 脚本)
  - 分布式锁 (防止重复下单)
  - 发送订单消息到 RabbitMQ
  - 严格限流 (5 req/s per IP)

### 6. Order Consumer
- **技术栈**: RabbitMQ, GORM
- **职责**:
  - 监听订单队列
  - 消费订单消息
  - 持久化订单到 MySQL
  - 消息确认机制 (ACK)

## 数据流程

### 秒杀流程

```
1. 用户请求秒杀
   ↓
2. API Gateway 验证 JWT + 限流
   ↓
3. 转发到 Seckill Service
   ↓
4. 分布式锁检查 (Redis SetNX)
   ↓
5. Redis Lua 脚本原子扣减库存
   ↓
6. 发送订单消息到 RabbitMQ
   ↓
7. 返回成功响应
   ↓
8. Order Consumer 异步消费
   ↓
9. 订单持久化到 MySQL
```

## 技术亮点

### 1. 高并发处理
- **Redis 预扣减**: 库存操作直接在 Redis 完成，避免数据库压力
- **Lua 脚本**: 保证库存扣减的原子性
- **分布式锁**: 防止用户重复下单

### 2. 流量削峰
- **RabbitMQ**: 异步处理订单，避免数据库瞬时压力
- **限流机制**: 全局限流 + 秒杀接口限流

### 3. 高可用性
- **微服务架构**: 服务独立部署，故障隔离
- **消息确认**: RabbitMQ 手动 ACK，保证消息不丢失
- **健康检查**: gRPC 健康检查

### 4. 数据一致性
- **最终一致性**: 通过消息队列实现
- **事务处理**: GORM 事务保证数据完整性
- **幂等性**: 分布式锁保证操作幂等

## 性能优化

### 1. 缓存策略
- 商品信息缓存到 Redis (TTL: 10分钟)
- 秒杀库存预加载到 Redis
- 缓存穿透保护

### 2. 数据库优化
- 索引优化 (用户名、商品ID等)
- 连接池配置 (MaxOpenConns: 100)
- 读写分离 (可扩展)

### 3. 限流策略
- 全局限流: 100 req/s
- 秒杀限流: 5 req/s per IP
- 令牌桶算法

## 部署架构

### Docker Compose 部署
```yaml
services:
  - mysql (数据持久化)
  - redis (缓存 + 库存)
  - rabbitmq (消息队列)
  - auth-service
  - user-service
  - product-service
  - seckill-service
  - order-consumer
  - api-gateway
```

### 服务依赖关系
```
API Gateway → Auth Service
            → User Service
            → Product Service
            → Seckill Service

Seckill Service → Product Service (商品信息)
                → Redis (库存)
                → RabbitMQ (订单消息)

Order Consumer → RabbitMQ (消费消息)
               → MySQL (订单持久化)
```

## 安全设计

### 1. 认证授权
- JWT Token 认证
- Token 过期时间: 72小时
- 密码 bcrypt 加密

### 2. 防刷保护
- IP 限流
- 分布式锁防止重复下单
- 用户资格验证

### 3. 数据安全
- SQL 注入防护 (GORM ORM)
- XSS 防护
- CORS 配置

## 监控和日志

### 1. 日志系统
- 结构化日志 (Zap)
- 日志级别: Debug, Info, Error
- 日志轮转 (按大小和时间)

### 2. 监控指标
- API 请求量
- 响应时间
- 错误率
- 服务健康状态

## 扩展性

### 水平扩展
- 微服务可独立扩展
- 无状态设计
- 负载均衡支持

### 功能扩展
- 支付接口集成
- 订单状态追踪
- 用户积分系统
- 推荐系统
