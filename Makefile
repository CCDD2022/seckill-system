.PHONY: proto build run test clean docker-up docker-down help

# 默认目标
.DEFAULT_GOAL := help

# 生成 proto 文件
proto:
	@echo "Generating protobuf files..."
	@mkdir -p proto_output/{auth,user,product,seckill,order}
	@protoc --go_out=. --go_opt=paths=import \
	        --go-grpc_out=. --go-grpc_opt=paths=import \
	        proto/*.proto
	@echo "Proto files generated successfully!"

# 构建所有服务
build:
	@echo "Building all services..."
	@go build -o bin/auth_service ./cmd/auth_service
	@go build -o bin/user_service ./cmd/user_service
	@go build -o bin/product_service ./cmd/product_service
	@go build -o bin/seckill_service ./cmd/seckill_service
	@go build -o bin/order_consumer ./cmd/order_consumer
	@go build -o bin/api_gateway ./cmd/api_gateway
	@echo "All services built successfully!"

# 运行测试
test:
	@echo "Running tests..."
	@go test -v ./...

# 清理构建产物
clean:
	@echo "Cleaning build artifacts..."
	@rm -rf bin/
	@rm -rf proto_output/
	@echo "Cleaned successfully!"

# 启动 Docker 环境
docker-up:
	@echo "Starting Docker containers..."
	@docker-compose up -d
	@echo "Docker containers started!"

# 停止 Docker 环境
docker-down:
	@echo "Stopping Docker containers..."
	@docker-compose down
	@echo "Docker containers stopped!"

# 重新构建并启动 Docker 环境
docker-rebuild:
	@echo "Rebuilding and starting Docker containers..."
	@docker-compose up --build -d
	@echo "Docker containers rebuilt and started!"

# 查看日志
logs:
	@docker-compose logs -f

# 初始化配置
init-config:
	@echo "Initializing configuration..."
	@cp config/config.yaml.example config/config.yaml
	@echo "Configuration initialized! Please edit config/config.yaml"

# 下载依赖
deps:
	@echo "Downloading dependencies..."
	@go mod download
	@go mod tidy
	@echo "Dependencies downloaded!"

# 格式化代码
fmt:
	@echo "Formatting code..."
	@go fmt ./...
	@echo "Code formatted!"

# 代码检查
lint:
	@echo "Running linter..."
	@golangci-lint run ./...

# 帮助信息
help:
	@echo "Available commands:"
	@echo "  make proto          - Generate protobuf files"
	@echo "  make build          - Build all services"
	@echo "  make test           - Run tests"
	@echo "  make clean          - Clean build artifacts"
	@echo "  make docker-up      - Start Docker containers"
	@echo "  make docker-down    - Stop Docker containers"
	@echo "  make docker-rebuild - Rebuild and start Docker containers"
	@echo "  make logs           - View Docker logs"
	@echo "  make init-config    - Initialize configuration file"
	@echo "  make deps           - Download dependencies"
	@echo "  make fmt            - Format code"
	@echo "  make lint           - Run linter"
	@echo "  make help           - Show this help message"
