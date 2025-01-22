# 第一阶段：Node.js 处理 HTML
FROM node:slim AS html-builder
WORKDIR /build
COPY static/ ./static/

# 安装工具并处理 HTML 文件
RUN npm install -g inline-source-cli html-minifier-terser && \
    # 先进行内联处理
    inline-source --root ./static --compress false ./static/index.html ./static/index.inlined.html && \
    # 然后进行 HTML 压缩
    html-minifier-terser \
    --collapse-whitespace \
    --remove-comments \
    --remove-redundant-attributes \
    --remove-script-type-attributes \
    --remove-style-link-type-attributes \
    --use-short-doctype \
    --minify-css true \
    --minify-js true \
    < ./static/index.inlined.html > ./static/index.final.html

# 第二阶段：Go 编译
FROM golang:1.22-alpine AS builder
RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /build
COPY go.mod go.sum ./
# 添加 -x 参数可以看到下载过程
RUN go mod download -x

COPY . .
# 从 html-builder 复制处理好的 HTML 文件
COPY --from=html-builder /build/static/index.final.html ./static/index.html
# 添加编译优化参数
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-w -s" -o tinyUpload

# 最终阶段
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
