CREATE DATABASE IF NOT EXISTS seckill_shop DEFAULT CHARSET utf8mb4 COLLATE utf8mb4_unicode_ci;

USE seckill_shop;

-- 用户表
CREATE TABLE users (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    username VARCHAR(50) NOT NULL UNIQUE,
    password_hash VARCHAR(255) NOT NULL,
    email VARCHAR(100),
    phone VARCHAR(20),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_username (username)
);

-- 商品表
CREATE TABLE products (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    description TEXT,
    price DECIMAL(10,2) NOT NULL,
    stock INT NOT NULL DEFAULT 0,
    image_url VARCHAR(255),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_name (name)
);

-- 插入示例数据
INSERT INTO users (username, password_hash, email, phone) VALUES 
('admin', '$2a$10$8K1p/a0dRTlB0Z6bZ8XSz.6Q3Yk3Q3Q3Q3Q3Q3Q3Q3Q3Q3Q3Q3Q3', 'admin@example.com', '13800138000');

INSERT INTO products (name, description, price, stock, image_url) VALUES 
('iPhone 14', '最新款苹果手机', 5999.00, 100, '/images/iphone14.jpg'),
('MacBook Pro', '苹果笔记本电脑', 12999.00, 50, '/images/macbook.jpg');