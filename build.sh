#!/bin/bash
set -e

# 拉取最新代码
echo "Pulling latest code..."
git pull origin main

# 确保数据目录存在且权限正确
echo "Preparing data directory..."
sudo mkdir -p /data/tinyUpload/uploads
sudo chown -R 1000:1000 /data/tinyUpload
sudo chmod -R 755 /data/tinyUpload

# 备份运行中的镜像
echo "Backing up current image..."
docker tag tiny-upload:latest tiny-upload:rollback 2>/dev/null || true

# 构建新镜像
echo "Building new image..."
docker build -t tiny-upload:new .

echo "Starting new container for testing..."
docker run -d \
   --name tiny-upload-new \
   -p 3081:8080 \
   -v /data/tinyUpload:/app/data \
   tiny-upload:new

# 等待启动
echo "Waiting for new container to start..."
sleep 5

# 检查新容器是否正常运行
if docker ps | grep tiny-upload-new > /dev/null; then
   echo "New container started successfully, updating main service..."

   # 停止并移除旧容器
   docker stop tiny-upload 2>/dev/null || true
   docker rm tiny-upload 2>/dev/null || true

   # 启动新的主服务
   echo "Starting main service with new version..."
   docker run -d \
       --name tiny-upload \
       -p 3080:8080 \
       -v /data/tinyUpload:/app/data \
       --restart=unless-stopped \
       tiny-upload:new

   # 更新 latest 标签
   echo "Updating latest tag..."
   docker tag tiny-upload:new tiny-upload:latest

   # 清理旧的备份
   echo "Cleaning up old backup..."
   docker rmi tiny-upload:rollback 2>/dev/null || true

   echo "Deployment successful!"
else
   echo "New container failed to start, rolling back..."
   docker rm -f tiny-upload-new 2>/dev/null || true
   docker rmi tiny-upload:new 2>/dev/null || true
   exit 1
fi

# 清理
echo "Cleaning up..."
docker stop tiny-upload-new 2>/dev/null || true
docker rm tiny-upload-new 2>/dev/null || true
docker rmi tiny-upload:new 2>/dev/null || true
docker image prune -f

echo "Service is running at http://localhost:3080"
docker ps | grep tiny-upload
