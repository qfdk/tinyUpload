# 构建阶段
FROM golang:1.22-alpine AS builder
RUN apk add --no-cache gcc musl-dev
WORKDIR /app
COPY . .
RUN go mod download
RUN CGO_ENABLED=1 GOOS=linux go build -o tinyUpload

# 运行阶段
FROM alpine:latest
RUN apk add --no-cache sqlite-libs

# 创建非 root 用户和目录
RUN adduser -D -u 1000 appuser && \
    mkdir -p /app/data/uploads && \
    chown -R appuser:appuser /app

# 复制二进制文件和前端文件
COPY --from=builder /app/tinyUpload /app/
COPY --from=builder /app/static /app/

USER appuser
WORKDIR /app

EXPOSE 8080
VOLUME ["/app/data"]

CMD ["./tinyUpload"]