#!/bin/bash

# 停止并删除旧容器（如果存在）
if [ "$(docker ps -aq -f name=tiny-upload)" ]; then
    docker stop tiny-upload
    docker rm tiny-upload
fi

# 删除旧镜像（如果存在）
if [ "$(docker images -q tiny-upload 2>/dev/null)" ]; then
    docker rmi tiny-upload
fi

# 构建新镜像
echo "Building Docker image..."
docker build -t tiny-upload .

# 运行新容器
echo "Starting container on port 3080..."
docker run -d \
    --name tiny-upload \
    -p 3080:8080 \
    -v $(pwd)/data:/app/data \
    tiny-upload

echo "Container is running on http://localhost:3080"

# 显示容器日志
docker logs -f tiny-upload