#!/bin/bash
set -e

echo "1. 启动 MySQL..."
docker compose up -d mysql
sleep 60

echo "2. 启动 auth-service..."
docker compose up -d auth-service
sleep 60

echo "3. 启动 user-service..."
docker compose up -d user-service
sleep 60

echo "4. 启动 product-service..."
docker compose up -d product-service
sleep 60

echo "5. 启动 order-service..."
docker compose up -d --build order-service
sleep 60

echo "6. 启动 seckill-service..."
docker compose up -d seckill-service
sleep 60

echo "7. 启动 order-create-consumer..."
docker compose up -d order-create-consumer
sleep 60

echo "8. 启动 order-cancel-consumer..."
docker compose up -d order-cancel-consumer
sleep 60

echo "9. 启动 stock-reconciler..."
docker compose up -d stock-reconciler
sleep 60

echo "10. 启动 api-gateway..."
docker compose up -d api-gateway
sleep 60

echo "✅ 全部启动完成！"
docker compose ps