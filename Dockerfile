# 第一阶段：Node.js 处理 HTML
FROM node:slim AS html-builder
WORKDIR /build
COPY static/ ./static/
RUN npm install -g inline-source-cli && \
    inline-source --root ./static --compress false ./static/index.html ./static/index.inlined.html

# 第二阶段：Go 编译
FROM golang:1.22-alpine AS builder
RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download -x

COPY . .
# 从 html-builder 复制处理好的 HTML 文件
COPY --from=html-builder /build/static/index.inlined.html ./static/index.html
# 编译
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-w -s" -o tinyUpload

# 最终阶段
FROM alpine:latest
RUN apk add --no-cache sqlite-libs ca-certificates tzdata

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
