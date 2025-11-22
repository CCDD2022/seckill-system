# Build stage
FROM golang:1.25.3-alpine AS builder

# 设置工作目录
WORKDIR /build

# sed流编辑器  原地替换文件内容 把官方源换为中科大镜像源
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.ustc.edu.cn/g' /etc/apk/repositories

# 设置 Go 模块代理（国内镜像）
ENV GOPROXY=https://goproxy.cn,direct

# 安装必要的构建工具
RUN apk add --no-cache git

# 复制go mod文件
COPY go.mod go.sum ./
RUN go mod download

# 复制源代码
COPY . .
# 构建参数（由 docker-compose 传入）
ARG SERVICE_NAME

# 构建二进制文件
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /app/service ./cmd/${SERVICE_NAME}

# Runtime stage
FROM alpine:latest

# 安装ca证书
RUN apk --no-cache add ca-certificates tzdata

# 设置时区
ENV TZ=Asia/Shanghai

WORKDIR /app

# 从构建阶段复制二进制文件
COPY --from=builder /app/service .

# 复制配置文件
COPY config/config.docker.yaml /app/config/config.yaml

# 运行服务
CMD ["./service"]
