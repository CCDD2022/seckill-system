# 更新日志

本文档记录项目的所有重要更改。

## [1.0.0] - 2024-11-15

### ✨ 新增功能

#### 核心服务
- **Auth Service**: 实现用户注册和登录功能
  - 密码 bcrypt 加密
  - JWT Token 生成和验证
  - gRPC 服务接口

- **User Service**: 实现用户信息管理
  - 获取用户信息
  - 更新用户资料
  - 修改密码功能

- **Product Service**: 实现商品管理
  - 商品 CRUD 操作
  - 分页查询支持
  - Redis 缓存集成
  - 缓存自动刷新机制

- **Seckill Service**: 实现秒杀核心功能
  - Redis 库存预扣减（Lua 脚本）
  - 分布式锁防止重复下单
  - RabbitMQ 消息发送
  - 库存自动回滚机制
  - 严格限流保护

- **Order Consumer**: 实现订单异步处理
  - RabbitMQ 消息消费
  - 订单持久化到 MySQL
  - 手动 ACK 确认机制
  - 错误重试支持

- **API Gateway**: 实现统一网关
  - HTTP 路由转发
  - JWT 认证中间件
  - 全局限流（100 req/s）
  - 秒杀限流（5 req/s per IP）
  - gRPC 客户端管理

#### 基础设施
- **数据库**: MySQL 8.0
  - 用户表（users）
  - 商品表（products）
  - 订单表（orders）
  - 索引优化
  - 外键约束

- **缓存**: Redis 7+
  - 商品信息缓存
  - 库存预扣减
  - 分布式锁
  - TTL 自动过期

- **消息队列**: RabbitMQ 3+
  - 订单队列（order.create）
  - 持久化配置
  - 手动确认机制
  - 消息可靠投递

#### 中间件
- **JWT 认证中间件**
  - Token 验证
  - 用户信息注入
  - 过期时间检查
  - 错误处理

- **限流中间件**
  - IP 级别限流
  - 令牌桶算法
  - 全局限流保护
  - 秒杀专用限流
  - 自动清理机制

### 📚 文档

- **README.md**: 项目介绍和快速开始指南
- **docs/API.md**: 完整的 API 接口文档
- **docs/ARCHITECTURE.md**: 系统架构设计文档
- **docs/SUMMARY.md**: 项目完成总结

### 🔧 配置和部署

- **Docker 支持**
  - Dockerfile（多阶段构建）
  - docker-compose.yml（一键部署）
  - 服务依赖管理
  - 健康检查配置

- **配置文件**
  - config.yaml（本地开发）
  - config.docker.yaml（Docker 环境）
  - config.yaml.example（配置模板）

- **构建工具**
  - Makefile（命令简化）
  - 自动化构建脚本
  - proto 文件生成

- **数据初始化**
  - scripts/init.sql（数据库初始化）
  - 测试用户数据
  - 测试商品数据

### 🔒 安全特性

- **认证授权**
  - JWT Token 认证
  - 密码 bcrypt 加密
  - Token 过期机制

- **防护措施**
  - SQL 注入防护（ORM）
  - XSS 防护
  - 限流保护
  - 分布式锁

- **数据安全**
  - 事务处理
  - 数据验证
  - 错误处理

### 🚀 性能优化

- **缓存策略**
  - Redis 商品缓存
  - 库存预加载
  - 缓存穿透保护

- **并发控制**
  - Lua 脚本原子操作
  - 分布式锁
  - 消息队列削峰

- **限流策略**
  - 全局限流
  - 接口限流
  - IP 级别限流

### 📦 依赖更新

- `github.com/gin-gonic/gin` v1.11.0
- `github.com/golang-jwt/jwt/v4` v4.5.2
- `google.golang.org/grpc` v1.65.0
- `google.golang.org/protobuf` v1.36.10
- `gorm.io/gorm` v1.31.1
- `gorm.io/driver/mysql` v1.6.0
- `github.com/redis/go-redis/v9` v9.16.0
- `github.com/streadway/amqp` v1.1.0
- `github.com/spf13/viper` v1.21.0
- `golang.org/x/crypto` v0.44.0
- `golang.org/x/time` v0.14.0

### 🐛 Bug 修复

- 修复 seckill.proto 文件语法错误
- 修复 Redis 客户端版本冲突
- 修复错误码定义缺失
- 修复 proto 文件输出路径问题

### 🎨 代码改进

- 统一错误处理
- 优化日志输出
- 改进代码结构
- 完善注释文档

### ✅ 测试

- 所有服务构建成功
- CodeQL 安全扫描通过（0 问题）
- 依赖安全检查通过

---

## 技术栈

- **语言**: Go 1.21+
- **Web 框架**: Gin
- **RPC 框架**: gRPC
- **ORM**: GORM
- **数据库**: MySQL 8.0
- **缓存**: Redis 7+
- **消息队列**: RabbitMQ 3+
- **容器化**: Docker + Docker Compose

## 贡献者

- CCDD2022

## 许可证

MIT License
