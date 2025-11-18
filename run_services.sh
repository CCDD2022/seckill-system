#!/bin/bash

# 停止所有可能正在运行的服务
echo "清理现有服务..."
pkill -f "go" 2>/dev/null
sleep 2

echo "开始启动所有微服务..."

# 启动顺序：基础服务 -> 业务服务 -> 网关
services=(
    "user_service"
    "auth_service" 
    "product_service"
    "order_service"
    "seckill_service"
    "order_consumer"
    "product_consumer"
    "stock_reconciler"
    "api_gateway"
)

for service in "${services[@]}"; do
    if [ -f "./cmd/$service/main.go" ]; then
        echo "🚀 启动 $service..."
        go run ./cmd/$service/main.go &
        echo "    PID: $!"
        sleep 2  # 给每个服务2秒启动时间
    else
        echo "❌ 跳过 $service: ./cmd/$service/main.go 不存在"
    fi
done

echo ""
echo "✅ 所有服务启动完成！"
echo "📊 使用以下命令检查运行状态:"
echo "   ps aux | grep 'go run' | grep -v grep"
echo "   ./stop_services.sh  # 停止所有服务"
echo ""
echo "🔍 等待服务初始化..."
sleep 5
ps aux | grep "go run" | grep -v grep