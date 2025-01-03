# Tiny Upload

轻量级Tar.tn服务器，支持本地和命令行上传。

## 功能

- 文件上传：支持拖拽和点击上传
- 随机路径：每个文件生成唯一4位路径
- 删除控制：上传时生成删除码，仅持有删除码者可删除
- CLI支持：完整支持curl等命令行工具
- 文件管理：查看上传历史、下载次数统计
- 响应式设计：适配移动端和桌面端

## 安装

### Docker安装

```bash
# 构建镜像
docker build -t tiny-upload .

# 运行容器
docker run -d \
  --name tiny-upload \
  -p 8080:8080 \
  -v tiny-upload-data:/app/data \
  tiny-upload
```

### 手动安装

需求:
- Go 1.22+
- SQLite3

```bash
git clone https://github.com/yourusername/tiny-upload
cd tiny-upload
go mod download
go build
./tiny-upload
```

## 使用方法

### Web界面

访问 `http://localhost:8080`，可以：
- 拖拽文件或点击上传
- 查看文件列表
- 下载或删除文件

### 命令行

上传文件:
```bash
curl -T 文件名 localhost:8080
```

下载文件:
```bash
curl -O http://localhost:8080/xxxx/文件名
wget http://localhost:8080/xxxx/文件名
```

删除文件:
```bash
curl -X DELETE "http://localhost:8080/delete/xxxx/文件名?code=删除码"
```

## 数据存储

- 文件存储在 `data/uploads` 目录
- SQLite数据库位于 `data/files.db`
- Docker部署时通过volume持久化

## 安全说明

- 每个文件生成唯一4位路径和8位删除码
- 删除操作需要正确的删除码
- 建议在可信网络环境使用
- 不建议用于存储敏感数据

## 许可证

MIT License