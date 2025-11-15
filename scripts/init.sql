-- 初始化数据库
CREATE DATABASE IF NOT EXISTS seckill_shop CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
USE seckill_shop;

-- 用户表
CREATE TABLE IF NOT EXISTS `users` (
  `id` BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `username` VARCHAR(50) NOT NULL UNIQUE COMMENT '用户名',
  `password_hash` VARCHAR(255) NOT NULL COMMENT '密码哈希',
  `email` VARCHAR(100) NOT NULL COMMENT '邮箱',
  `phone` VARCHAR(20) NOT NULL COMMENT '手机号',
  `status` INT NOT NULL DEFAULT 0 COMMENT '状态: 0正常 1禁用 2风控限制',
  `is_seckill_allowed` BOOLEAN NOT NULL DEFAULT TRUE COMMENT '是否有秒杀资格',
  `created_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `updated_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  INDEX idx_username (`username`),
  INDEX idx_email (`email`),
  INDEX idx_phone (`phone`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='用户表';

-- 商品表
CREATE TABLE IF NOT EXISTS `products` (
  `id` BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `name` VARCHAR(200) NOT NULL COMMENT '商品名称',
  `description` TEXT COMMENT '商品描述',
  `price` DECIMAL(10, 2) NOT NULL COMMENT '商品价格',
  `stock` INT NOT NULL DEFAULT 0 COMMENT '库存数量',
  `image_url` VARCHAR(500) COMMENT '商品图片URL',
  `created_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `updated_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  INDEX idx_name (`name`),
  INDEX idx_price (`price`),
  INDEX idx_created_at (`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='商品表';

-- 订单表
CREATE TABLE IF NOT EXISTS `orders` (
  `id` BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `user_id` BIGINT NOT NULL COMMENT '用户ID',
  `product_id` BIGINT NOT NULL COMMENT '商品ID',
  `quantity` INT NOT NULL COMMENT '购买数量',
  `total_price` DECIMAL(10, 2) NOT NULL COMMENT '总价格',
  `status` INT NOT NULL DEFAULT 0 COMMENT '订单状态: 0待支付 1已支付 2已取消 3已完成',
  `created_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `updated_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  INDEX idx_user_id (`user_id`),
  INDEX idx_product_id (`product_id`),
  INDEX idx_status (`status`),
  INDEX idx_created_at (`created_at`),
  FOREIGN KEY (`user_id`) REFERENCES `users`(`id`) ON DELETE CASCADE,
  FOREIGN KEY (`product_id`) REFERENCES `products`(`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='订单表';

-- 插入测试数据
-- 插入测试用户
INSERT INTO `users` (`username`, `password_hash`, `email`, `phone`, `status`, `is_seckill_allowed`) VALUES
('testuser1', '$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy', 'test1@example.com', '13800138001', 0, TRUE),
('testuser2', '$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy', 'test2@example.com', '13800138002', 0, TRUE),
('testuser3', '$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy', 'test3@example.com', '13800138003', 0, TRUE);

-- 插入测试商品（秒杀商品）
INSERT INTO `products` (`name`, `description`, `price`, `stock`, `image_url`) VALUES
('iPhone 15 Pro Max (秒杀)', '最新款iPhone，256GB，深空黑', 6999.00, 100, 'https://example.com/iphone15.jpg'),
('MacBook Pro M3 (秒杀)', '14寸，16GB内存，512GB存储', 12999.00, 50, 'https://example.com/macbook.jpg'),
('AirPods Pro 2 (秒杀)', '第二代降噪耳机', 1899.00, 200, 'https://example.com/airpods.jpg'),
('小米13 Ultra (秒杀)', '旗舰拍照手机，12GB+256GB', 4999.00, 150, 'https://example.com/mi13ultra.jpg'),
('华为Mate 60 Pro (秒杀)', '卫星通信，12GB+512GB', 6999.00, 80, 'https://example.com/mate60.jpg');

-- 说明：测试用户的密码均为 "password123"
-- 密码哈希使用bcrypt生成
