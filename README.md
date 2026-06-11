# 极简上传 (tinyUpload)

> Drop it. Share it.

轻量级临时文件分享服务：拖进来一个文件，拿走一条链接。单二进制 + SQLite，自托管只需一个容器。

## 功能

**网页端**
- 整页拖拽上传，点击上传区选文件，支持多文件
- ⌘V / Ctrl+V 粘贴上传：文件、截图，或剪贴板文本（自动存为 `paste.txt`）
- 上传成功自动复制链接；删除码本地保存，文件列表随时下载/删除
- 中 / 英 / 法三语（跟随浏览器语言，可手动切换）
- 白天 / 夜间主题（跟随系统，可手动切换）
- 终端风格命令行用法卡片，命令点击即复制

**命令行**
- `curl -T` 直接上传，无需任何客户端
- 管道上传（`curl -T -`）自动生成随机文件名，按 Content-Type 补全后缀
- 公网访问时返回的分享链接强制 HTTPS

**服务端**
- 随机 8 位路径 + 8 位删除码，同一地址用 HTTP 方法区分下载/删除
- 文件保存 3 天后自动清理
- 流式写盘，单文件上限 512MB，无内存峰值
- 纯文本/图片/PDF/音视频等惰性类型浏览器内联预览，可执行类型强制下载并加 CSP 沙箱
- 静态资源版本号随进程启动更新，部署后浏览器缓存自动失效

## 快速开始

### Docker

```bash
docker build -t tiny-upload .
docker run -d \
  --name tiny-upload \
  -p 8080:8080 \
  -v tiny-upload-data:/app/data \
  tiny-upload
```

### 手动构建

需求：Go 1.22+、SQLite3（CGO）

```bash
git clone https://github.com/qfdk/tinyUpload
cd tinyUpload
go build
./tinyUpload
```

访问 `http://localhost:8080`。

## 命令行用法

```bash
# 上传
curl -T 文件名 localhost:8080
curl -T 文件名 localhost:8080/新文件名

# 管道上传（自动生成随机文件名）
echo "hello" | curl -T - localhost:8080 -H "Content-Type: text/plain"

# 下载
curl -O http://localhost:8080/xxxx/文件名

# 删除（删除码在上传响应中返回）
curl -X DELETE "http://localhost:8080/xxxx/文件名?code=删除码"
```

## API

| 操作 | 接口 | 说明 |
|---|---|---|
| 上传 | `PUT /` 或 `PUT /:filename` | 浏览器客户端返回 JSON，curl/wget 返回纯文本 |
| 下载 | `GET /:path/:filename` | 白名单类型内联预览，其余强制下载 |
| 删除 | `DELETE /:path/:filename` | 删除码经 `?code=` 查询参数或 `X-Delete-Code` 头 |

## 数据存储

- 文件存储在 `data/uploads/`，按随机路径分目录
- 元数据存于 SQLite（`data/files.db`），含下载计数
- 上传超过 3 天的文件由后台任务自动删除
- Docker 部署时通过 volume 持久化 `data/`

## 安全说明

- 文件名经 `filepath.Base` 清理，阻断路径穿越
- 内联预览按扩展名白名单判定，HTML/SVG/JS 等可承载脚本的类型一律强制下载
- 下载响应附 `X-Content-Type-Options: nosniff`，非预览类型加 CSP 沙箱
- 删除需正确删除码；建议在可信网络环境使用，不建议存储敏感数据

## 许可证

[GNU General Public License v3.0](LICENSE)（GPL-3.0）——衍生作品须以相同许可证开源。
