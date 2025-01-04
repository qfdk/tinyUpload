FROM golang:1.22-alpine AS builder
RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /build
COPY go.mod go.sum ./
# 添加 -x 参数可以看到下载过程
RUN go mod download -x

COPY . .
# 添加一些编译优化参数
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-w -s" -o tinyUpload

FROM alpine:latest
RUN apk add --no-cache sqlite-libs ca-certificates tzdata

# 合并 RUN 命令减少层数
RUN adduser -D -u 1000 appuser && \
    mkdir -p /app/{data/uploads,static} && \
    chown -R appuser:appuser /app

COPY --chown=appuser:appuser --from=builder /build/tinyUpload /app/
COPY --chown=appuser:appuser --from=builder /build/static /app/static

USER appuser
WORKDIR /app

VOLUME ["/app/data"]
EXPOSE 8080

ENTRYPOINT ["./tinyUpload"]