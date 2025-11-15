# API 文档

## 基础信息

- **Base URL**: `http://localhost:8080/api/v1`
- **认证方式**: JWT Token (Bearer Token)
- **Content-Type**: `application/json`

## 认证相关接口

### 1. 用户注册

**接口地址**: `POST /auth/register`

**请求参数**:
```json
{
  "username": "string",
  "password": "string",
  "email": "string",
  "phone": "string"
}
```

**响应示例**:
```json
{
  "code": 0,
  "message": "成功",
  "user": {
    "id": 1,
    "username": "testuser",
    "email": "test@example.com",
    "phone": "13800138000"
  }
}
```

### 2. 用户登录

**接口地址**: `POST /auth/login`

**请求参数**:
```json
{
  "username": "string",
  "password": "string"
}
```

**响应示例**:
```json
{
  "code": 0,
  "message": "成功",
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
}
```

## 商品相关接口 (需要认证)

### 3. 获取商品列表

**接口地址**: `GET /products?page=1&page_size=10`

**响应示例**:
```json
{
  "code": 0,
  "message": "成功",
  "products": [
    {
      "id": 1,
      "name": "iPhone 15 Pro Max (秒杀)",
      "price": 6999.00,
      "stock": 100
    }
  ],
  "total": 5
}
```

## 秒杀相关接口 (需要认证 + 严格限流)

### 4. 执行秒杀

**接口地址**: `POST /seckill/execute`

**请求参数**:
```json
{
  "product_id": 1,
  "quantity": 1
}
```

**响应示例**:
```json
{
  "code": 0,
  "message": "秒杀成功，订单处理中",
  "success": true
}
```

完整API文档请参考项目根目录。
