# 使用多阶段构建
# 第一阶段：构建
FROM golang:1.22-alpine AS builder

# 安装 gcc 和必要的工具，因为 sqlite3 需要 CGO
RUN apk add --no-cache gcc musl-dev

# 设置工作目录
WORKDIR /app

# 复制 go.mod 和 go.sum (如果存在)
COPY go.mod ./
COPY go.sum ./

# 下载依赖
RUN go mod download

# 复制源代码
COPY . .

# 构建应用
RUN CGO_ENABLED=1 GOOS=linux go build -o tinyUpload

# 第二阶段：运行
FROM alpine:latest

LABEL maintainer="Your Name <your.email@example.com>"
LABEL description="A tiny file upload server with random path generation"
LABEL version="1.0"

# 安装运行时依赖
RUN apk add --no-cache sqlite-libs

# 创建非 root 用户
RUN adduser -D appuser

# 创建必要的目录
RUN mkdir /app && mkdir /app/uploads && chown -R appuser:appuser /app

# 从构建阶段复制二进制文件
COPY --from=builder /app/tinyUpload /app/tinyUpload

# 切换到非 root 用户
USER appuser

# 设置工作目录
WORKDIR /app

# 暴露端口
EXPOSE 8080

# 声明卷，用于持久化上传的文件和数据库
VOLUME ["/app/uploads", "/app/data"]

# 运行应用
CMD ["./tinyUpload"]
