# 构建阶段
FROM golang:1.22-alpine AS builder
# 添加必要的构建依赖
RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /build
# 首先复制依赖文件
COPY go.mod go.sum ./
RUN go mod download

# 然后复制源代码和静态文件
COPY . .
# 构建应用
RUN CGO_ENABLED=1 GOOS=linux go build -o tinyUpload

# 运行阶段
FROM alpine:latest
# 添加运行时依赖
RUN apk add --no-cache sqlite-libs ca-certificates

# 创建非 root 用户和必要的目录
RUN adduser -D -u 1000 appuser && \
    mkdir -p /app/data/uploads && \
    mkdir -p /app/static && \
    chown -R appuser:appuser /app

# 复制构建产物和静态文件
COPY --chown=appuser:appuser --from=builder /build/tinyUpload /app/
COPY --chown=appuser:appuser --from=builder /build/static /app/static

# 切换到非 root 用户
USER appuser
WORKDIR /app

# 声明数据卷和端口
VOLUME ["/app/data"]
EXPOSE 8080

CMD ["./tinyUpload"]