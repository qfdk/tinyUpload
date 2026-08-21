package main

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"html"
	"io"
	"log"
	"math/big"
	"mime"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/logger"
	_ "github.com/mattn/go-sqlite3"
)

// maxUploadSize 是单次上传写盘上限。StreamRequestBody=true 时 fasthttp 不再用
// BodyLimit 约束流式 body，必须在 io.Copy 处手动强制，否则可被无界磁盘写入 DoS。
const maxUploadSize = 1024 * 1024 * 1024

// maxRetention 是文件最长保留时间，?l= 超过一律按此截断。
const maxRetention = 72 * time.Hour

// defaultRetention 是不带 ?t= 时的默认保留时长（网页与 curl 一致，前端文案同步此值）。
const defaultRetention = time.Hour

// assetVersion 以进程启动时间作为静态资源版本号，部署重启后自动失效浏览器缓存，
// 避免新 HTML 配旧 JS/CSS 的混搭（曾导致 i18n 键名裸显示）。
var assetVersion = fmt.Sprint(time.Now().Unix())

type FileServer struct {
	db        *sql.DB
	uploadDir string
	app       *fiber.App
}

func NewFileServer() (*FileServer, error) {
	if err := os.MkdirAll("data", 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %v", err)
	}
	if err := os.MkdirAll("data/uploads", 0755); err != nil {
		return nil, fmt.Errorf("failed to create uploads directory: %v", err)
	}

	// _busy_timeout 让并发写在锁等待 5s 内重试，避免高并发下 "database is locked" 被放大为可用性故障
	db, err := sql.Open("sqlite3", "data/files.db?_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %v", err)
	}

	_, err = db.Exec(`
       CREATE TABLE IF NOT EXISTS files (
           id INTEGER PRIMARY KEY AUTOINCREMENT,
           path TEXT NOT NULL,
           filename TEXT NOT NULL,
           encoded_filename TEXT NOT NULL,
           delete_code TEXT NOT NULL,
           upload_time DATETIME NOT NULL,
           file_size INTEGER NOT NULL,
           mime_type TEXT,
           download_count INTEGER DEFAULT 0,
           expire_time DATETIME,
           max_downloads INTEGER,
           UNIQUE(path, encoded_filename)
       )
   `)
	if err != nil {
		return nil, fmt.Errorf("failed to create table: %v", err)
	}

	// 旧库迁移：已存在的表补上新列。SQLite 的 ADD COLUMN 不支持 IF NOT EXISTS，
	// 列已存在时报 duplicate column，忽略即可。
	for _, col := range []string{
		`ALTER TABLE files ADD COLUMN expire_time DATETIME`,
		`ALTER TABLE files ADD COLUMN max_downloads INTEGER`,
	} {
		if _, err := db.Exec(col); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return nil, fmt.Errorf("failed to migrate table: %v", err)
		}
	}

	app := fiber.New(fiber.Config{
		Prefork:                 false,
		ServerHeader:            "FileServer",
		BodyLimit:               maxUploadSize,
		StreamRequestBody:       true,
		// StreamRequestBody 下 ReadTimeout 覆盖整个请求体的读取，WriteTimeout 覆盖整个
		// 响应体的写出。1GB 文件在慢速链路上传/下载需要数分钟，30s 会中途掐断连接。
		ReadTimeout:             15 * time.Minute,
		WriteTimeout:            15 * time.Minute,
		IdleTimeout:             60 * time.Second,
		ProxyHeader:             "X-Real-IP",
		EnableTrustedProxyCheck: true,
		TrustedProxies:          []string{"127.0.0.1", "::1", "172.17.0.1", "192.168.1.8"},
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			log.Printf("Error: %v", err)
			return c.Redirect("/", 302)
		},
	})

	app.Use(logger.New(logger.Config{
		Next: func(c *fiber.Ctx) bool {
			return strings.HasPrefix(c.Path(), "/static/") || c.Path() == "/favicon.ico"
		},
	}))

	app.Use(compress.New(compress.Config{
		Level: compress.LevelBestSpeed,
	}))
	// 不启用 CORS：前端与 API 同源，CLI（curl/wget）不受同源策略约束。
	// 移除通配 CORS，避免任意第三方站点跨源调用上传/删除接口。

	return &FileServer{
		db:        db,
		uploadDir: "data/uploads",
		app:       app,
	}, nil
}

func (s *FileServer) setupRoutes() {
	s.app.Static("/static", "./static")
	s.app.Get("/favicon.ico", func(c *fiber.Ctx) error {
		return c.SendStatus(204)
	})
	s.app.Get("/", s.handleRoot)
	// "/:filename" 不匹配空段，单独注册 PUT /，支持 curl -T - 这类无名上传
	s.app.Put("/", s.handleUpload)
	s.app.Put("/:filename", s.handleUpload)
	// 同一资源地址，不同方法：GET 下载，DELETE 删除（携带删除码）
	s.app.Get("/:path/:filename", s.handleDownload)
	s.app.Delete("/:path/:filename", s.handleDelete)

	s.app.Use(func(c *fiber.Ctx) error {
		return c.Redirect("/", 302)
	})
}

func (s *FileServer) handleRoot(c *fiber.Ctx) error {
	if isTextPreferred(c) {
		host := c.Hostname()
		now := time.Now().Format("2006-01-02 15:04:05")
		return c.Type("text").SendString(fmt.Sprintf(`File Server Usage Instructions:

Upload File:
 curl -T filename %s
 curl -T filename %s/new_filename

Upload Options (default: kept 1 hour, unlimited downloads):
 curl -T filename "%s/?t=3"        # keep 3 days (also 30m / 12h / 2d, max 3d)
 curl -T filename "%s/?n=1"        # burn after 1 download
 curl -T filename "%s/?t=12h&n=5"  # combine both (whichever hits first)

Download File:
 curl -O %s/xxxx/filename
 wget %s/xxxx/filename

Delete File:
 curl -X DELETE "%s/xxxx/filename?code=delete_code"

Server Time: %s
`, host, host, host, host, host, host, host, host, now))
	}
	// c.Render 无 Views 引擎时回退到 text/template（不做 HTML 转义），而 c.Hostname()
	// 在受信代理下会返回攻击者可控的 X-Forwarded-Host。必须显式转义，防止反射型 XSS。
	return c.Render("static/index.html", fiber.Map{
		"ServerHost": html.EscapeString(c.Hostname()),
		"Protocol":   shareProtocol(c.Protocol(), c.Hostname()),
		"AssetVer":   assetVersion,
	})
}

// isLocalShareHost 判断 Host 是否为本机/内网地址（含端口形式）。
func isLocalShareHost(host string) bool {
	h := host
	if hp, _, err := net.SplitHostPort(host); err == nil {
		h = hp
	}
	h = strings.Trim(h, "[]")
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

// shareProtocol 返回对外分享链接应使用的协议：公网主机一律 https
// （生产域名经反代终止 TLS，明文 curl 上传时 c.Protocol() 也是 http，
// 但返回给用户的链接应当是 https）；本机/内网保持原协议方便调试。
func shareProtocol(proto, host string) string {
	if proto == "https" {
		return "https"
	}
	if isLocalShareHost(host) {
		return "http"
	}
	return "https"
}

func (s *FileServer) handleUpload(c *fiber.Ctx) error {
	// 先校验参数再收流，参数错了不浪费磁盘写入。
	// t = 保留时长（裸数字按天，支持 m/h/d 后缀），n = 下载次数上限（用完即失效）。
	retention, err := parseExpires(c.Query("t"))
	if err != nil {
		return c.Status(400).SendString("Invalid t parameter (retention: e.g. 1, 3, 30m, 12h, 2d; max 3d)")
	}
	maxDownloads := 0
	if v := c.Query("n"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return c.Status(400).SendString("Invalid n parameter (max downloads: positive integer)")
		}
		maxDownloads = n
	}

	filename := c.Params("filename")
	decodedFilename, err := url.QueryUnescape(filename)
	if err != nil {
		return c.Status(400).SendString("Invalid filename")
	}

	if decodedFilename == "" {
		if cd := c.Get("Content-Disposition"); cd != "" {
			if _, params, err := mime.ParseMediaType(cd); err == nil {
				if fn := params["filename"]; fn != "" {
					decodedFilename = fn
				}
			}
		}
		// 仍无文件名（如 curl -T - 管道上传）：生成随机名，并尽量从请求
		// Content-Type 推断后缀，让图片等类型能命中内联预览白名单。
		if decodedFilename == "" {
			decodedFilename = generateRandomString(8) + extFromContentType(c.Get("Content-Type"))
		}
	}

	// 清理文件名以防止路径遍历攻击
	decodedFilename = sanitizeFilename(decodedFilename)
	if decodedFilename == "" {
		return c.Status(400).SendString("Invalid filename after sanitization")
	}

	path := generateRandomPath()
	dirPath := filepath.Join(s.uploadDir, path)
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return c.Status(500).SendString("Failed to create directory")
	}

	encodedFilename := url.QueryEscape(decodedFilename)

	filePath := filepath.Join(dirPath, decodedFilename)
	out, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		os.Remove(dirPath)
		return c.Status(500).SendString("Failed to save file")
	}

	// 流式写盘，避免将整个请求体读入内存（大文件并发会导致 OOM）。
	// 同时用 LimitReader 强制 maxUploadSize 上限：StreamRequestBody 下 BodyLimit 不再生效，
	// 否则攻击者可发送任意大小的流耗尽磁盘。多读 1 字节用于判定是否越界。
	limited := io.LimitReader(c.Context().RequestBodyStream(), maxUploadSize+1)
	fileSize, copyErr := io.Copy(out, limited)
	if cerr := out.Close(); cerr != nil && copyErr == nil {
		copyErr = cerr
	}
	if copyErr != nil {
		os.Remove(filePath)
		os.Remove(dirPath)
		return c.Status(500).SendString("Failed to save file")
	}
	if fileSize > maxUploadSize {
		os.Remove(filePath)
		os.Remove(dirPath)
		return c.Status(413).SendString("File too large")
	}
	if fileSize == 0 {
		os.Remove(filePath)
		os.Remove(dirPath)
		return c.Status(400).SendString("Empty file content")
	}

	mimeType := c.Get("Content-Type")
	if mimeType == "" {
		mimeType = mime.TypeByExtension(filepath.Ext(decodedFilename))
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
	}

	deleteCode := generateRandomString(8)

	// 过期时间用 SQLite 的 'now' 计算，与 upload_time、清理任务同一时钟源（UTC）
	expireModifier := fmt.Sprintf("+%d seconds", int(retention.Seconds()))
	var maxDownloadsVal interface{}
	if maxDownloads > 0 {
		maxDownloadsVal = maxDownloads
	}
	_, err = s.db.Exec(`
       INSERT INTO files (path, filename, encoded_filename, delete_code, upload_time, file_size, mime_type, expire_time, max_downloads)
       VALUES (?, ?, ?, ?, datetime('now'), ?, ?, datetime('now', ?), ?)
   `, path, decodedFilename, encodedFilename, deleteCode, fileSize, mimeType, expireModifier, maxDownloadsVal)

	if err != nil {
		os.Remove(filePath)
		os.Remove(dirPath)
		return c.Status(500).SendString("Failed to save file information")
	}

	expireAt := time.Now().Add(retention).Format("2006-01-02 15:04:05")
	downloadsNote := "unlimited"
	if maxDownloads > 0 {
		downloadsNote = strconv.Itoa(maxDownloads)
	}

	if isTextPreferred(c) {
		proto := shareProtocol(c.Protocol(), c.Hostname())
		return c.Type("text").SendString(fmt.Sprintf(`Upload successful!
Filename: %s
Access URL: %s://%s/%s/%s
Delete Code: %s
Size: %d bytes
Type: %s
Expires: %s
Max Downloads: %s

Delete Command:
curl -X DELETE "%s://%s/%s/%s?code=%s"
`,
			decodedFilename,
			proto, c.Hostname(), path, encodedFilename,
			deleteCode,
			fileSize, mimeType,
			expireAt, downloadsNote,
			proto, c.Hostname(), path, encodedFilename, deleteCode,
		))
	}

	return c.JSON(fiber.Map{
		"path":         path,
		"filename":     decodedFilename,
		"deleteCode":   deleteCode,
		"size":         fileSize,
		"mimeType":     mimeType,
		"uploadTime":   time.Now().Format("2006-01-02 15:04:05"),
		"expireTime":   expireAt,
		"maxDownloads": maxDownloads,
	})
}

func (s *FileServer) handleDownload(c *fiber.Ctx) error {
	path := c.Params("path")
	requestFilename := c.Params("filename")

	decodedRequestFilename, err := url.QueryUnescape(requestFilename)
	if err != nil {
		return c.Status(404).SendString(`File not found`)
	}

	// 清理文件名以防止路径遍历攻击
	decodedRequestFilename = sanitizeFilename(decodedRequestFilename)
	if decodedRequestFilename == "" {
		return c.Status(404).SendString("File not found")
	}

	encodedRequestFilename := url.QueryEscape(decodedRequestFilename)

	// 过期文件立刻不可访问，不等每小时的清理任务；
	// 历史数据 expire_time 为 NULL 时按 upload_time + 3 天兜底。
	var originalFilename string
	err = s.db.QueryRow(`
       SELECT filename FROM files
       WHERE path = ? AND encoded_filename = ?
         AND COALESCE(expire_time, datetime(upload_time, '+3 days')) > datetime('now')
   `, path, encodedRequestFilename).Scan(&originalFilename)
	if err != nil {
		return c.Status(404).SendString("File not found")
	}

	filePath := filepath.Join(s.uploadDir, path, originalFilename)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return c.Status(404).SendString("File not found")
	}

	// 计数与次数上限在同一条 UPDATE 里原子完成，并发下载不会超发；
	// 没抢到名额（RowsAffected=0）说明次数已用完，文件交给清理任务删除。
	res, err := s.db.Exec(`
       UPDATE files SET download_count = download_count + 1
       WHERE path = ? AND encoded_filename = ?
         AND (max_downloads IS NULL OR download_count < max_downloads)
   `, path, encodedRequestFilename)
	if err != nil {
		return c.Status(500).SendString("Internal server error")
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return c.Status(404).SendString("File not found")
	}

	// 默认禁止 MIME 嗅探；只有白名单内的惰性类型（图片/纯文本/PDF/音视频）允许
	// 内联预览，其余一律强制下载并加 CSP 沙箱兜底，防止同源内联执行（存储型 XSS）。
	// 白名单按扩展名判定且显式排除 text/html、image/svg+xml 等可承载脚本的类型，
	// 因此即便上传者完全控制文件内容也无法升级为脚本执行。
	encodedName := strings.ReplaceAll(url.QueryEscape(originalFilename), "+", "%20")
	c.Set("X-Content-Type-Options", "nosniff")
	if ctype, ok := inlineContentType(originalFilename); ok {
		c.Set("Content-Type", ctype)
		c.Set("Content-Disposition", "inline; filename*=UTF-8''"+encodedName)
	} else {
		c.Set("Content-Disposition", "attachment; filename*=UTF-8''"+encodedName)
		c.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	}

	return c.SendFile(filePath)
}

func (s *FileServer) handleDelete(c *fiber.Ctx) error {
	path := c.Params("path")
	requestFilename := c.Params("filename")

	decodedFilename, err := url.QueryUnescape(requestFilename)
	if err != nil {
		return c.Status(404).SendString("File not found")
	}

	// 清理文件名以防止路径遍历攻击
	decodedFilename = sanitizeFilename(decodedFilename)
	if decodedFilename == "" {
		return c.Status(404).SendString("File not found")
	}

	encodedFilename := url.QueryEscape(decodedFilename)

	// 优先从 header 读取删除码（避免出现在访问日志/历史）；保留 query 兼容命令行
	decodedDeleteCode := c.Get("X-Delete-Code")
	if decodedDeleteCode == "" {
		decodedDeleteCode, err = url.QueryUnescape(c.Query("code"))
		if err != nil {
			return c.Status(400).SendString("Invalid delete code")
		}
	}

	var filename string
	err = s.db.QueryRow(
		"SELECT filename FROM files WHERE path = ? AND encoded_filename = ? AND delete_code = ?",
		path, encodedFilename, decodedDeleteCode,
	).Scan(&filename)

	if err != nil {
		if err == sql.ErrNoRows {
			return c.Status(403).SendString("Invalid delete code")
		}
		return c.Status(500).SendString("Internal server error")
	}

	filePath := filepath.Join(s.uploadDir, path, filename)
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		log.Printf("Error deleting file: %v", err)
	}

	_, err = s.db.Exec(
		"DELETE FROM files WHERE path = ? AND encoded_filename = ? AND delete_code = ?",
		path, encodedFilename, decodedDeleteCode,
	)
	if err != nil {
		return c.Status(500).SendString("Failed to delete file record")
	}

	dirPath := filepath.Join(s.uploadDir, path)
	if err := os.Remove(dirPath); err != nil {
		log.Printf("Failed to remove directory (may not be empty): %v", err)
	}

	return c.Status(200).SendString("OK")
}

// expiredCondition 判定记录已失效：过期时间已到（历史 NULL 数据按 upload_time + 3 天
// 兜底），或下载次数已用完。下载/清理共用同一语义。
const expiredCondition = `
       COALESCE(expire_time, datetime(upload_time, '+3 days')) < datetime('now')
       OR (max_downloads IS NOT NULL AND download_count >= max_downloads)
`

func (s *FileServer) cleanupExpiredFiles() error {
	rows, err := s.db.Query(`
       SELECT path, encoded_filename, filename
       FROM files
       WHERE ` + expiredCondition)
	if err != nil {
		return fmt.Errorf("failed to query expired files: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var path, encodedFilename, filename string
		if err := rows.Scan(&path, &encodedFilename, &filename); err != nil {
			log.Printf("Failed to read file record: %v", err)
			continue
		}

		filePath := filepath.Join(s.uploadDir, path, filename)
		if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
			log.Printf("Failed to delete file %s: %v", filePath, err)
		}

		dirPath := filepath.Join(s.uploadDir, path)
		os.Remove(dirPath)
	}

	_, err = s.db.Exec(`DELETE FROM files WHERE ` + expiredCondition)
	if err != nil {
		return fmt.Errorf("failed to delete expired records: %v", err)
	}

	return nil
}

func sanitizeFilename(filename string) string {
	if filename == "" {
		return ""
	}

	// 使用 filepath.Base 移除任何路径组件，防止路径遍历
	filename = filepath.Base(filename)

	// 移除危险的字符序列
	filename = strings.ReplaceAll(filename, "..", "")
	filename = strings.ReplaceAll(filename, "~", "")

	// 移除控制字符和不可见字符
	var sanitized strings.Builder
	for _, r := range filename {
		if r < 32 || r == 127 {
			continue // 跳过控制字符
		}
		if r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' {
			continue // 跳过文件系统不安全字符
		}
		sanitized.WriteRune(r)
	}

	result := strings.TrimSpace(sanitized.String())

	// 确保文件名不为空且不是特殊名称
	if result == "" || result == "." || result == ".." {
		return "unnamed_file"
	}

	// 限制文件名长度（按 rune 边界截断，避免切断多字节 UTF-8 序列）
	if len(result) > 255 {
		ext := filepath.Ext(result)
		if len(ext) > 255 {
			ext = ""
		}
		limit := 255 - len(ext)
		var b strings.Builder
		for _, r := range result[:len(result)-len(ext)] {
			if b.Len()+len(string(r)) > limit {
				break
			}
			b.WriteRune(r)
		}
		result = b.String() + ext
	}

	return result
}

// parseExpires 解析上传参数 t（保留时长）：裸数字按天（t=2 即 2 天），
// 也接受 m/h/d 单位后缀（t=30m、t=12h、t=2d）。空串返回默认 defaultRetention；
// 超过上限按 maxRetention 截断；零、负数或无法解析的格式报错。
func parseExpires(s string) (time.Duration, error) {
	if s == "" {
		return defaultRetention, nil
	}

	num, unit := s, time.Duration(24)*time.Hour
	switch s[len(s)-1] {
	case 'm':
		num, unit = s[:len(s)-1], time.Minute
	case 'h':
		num, unit = s[:len(s)-1], time.Hour
	case 'd':
		num, unit = s[:len(s)-1], 24*time.Hour
	}

	n, err := strconv.Atoi(num)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid retention %q", s)
	}
	d := time.Duration(n) * unit
	if d > maxRetention {
		d = maxRetention
	}
	return d, nil
}

func generateRandomString(length int) string {
	const chars = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	result := make([]byte, length)
	for i := 0; i < length; i++ {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		result[i] = chars[n.Int64()]
	}
	return string(result)
}

func generateRandomPath() string {
	return generateRandomString(8)
}

func isTextPreferred(c *fiber.Ctx) bool {
	userAgent := c.Get("User-Agent")
	return strings.HasPrefix(userAgent, "curl/") || strings.HasPrefix(userAgent, "Wget/")
}

// inlinePreviewTypes 把可安全内联预览的文件扩展名映射到规范 MIME 类型。
// 自建白名单而不依赖系统 mime 表，确保判定确定且只放行惰性内容；
// 故意不含 .html/.htm/.svg/.xml/.xhtml/.js 等可承载脚本的类型。
var inlinePreviewTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".avif": "image/avif",
	".bmp":  "image/bmp",
	".ico":  "image/x-icon",
	".txt":  "text/plain; charset=utf-8",
	".log":  "text/plain; charset=utf-8",
	".md":   "text/plain; charset=utf-8",
	".csv":  "text/plain; charset=utf-8",
	".pdf":  "application/pdf",
	".mp4":  "video/mp4",
	".webm": "video/webm",
	".ogg":  "audio/ogg",
	".mp3":  "audio/mpeg",
	".wav":  "audio/wav",
}

// contentTypeExts 把上传请求的 Content-Type 映射到首选文件后缀，给无名上传的
// 随机文件名补后缀用。只收录 inlinePreviewTypes 对应的惰性类型；不用
// mime.ExtensionsByType，避免 image/jpeg 拿到 ".jpe" 这类非首选后缀。
var contentTypeExts = map[string]string{
	"image/png":       ".png",
	"image/jpeg":      ".jpg",
	"image/gif":       ".gif",
	"image/webp":      ".webp",
	"image/avif":      ".avif",
	"image/bmp":       ".bmp",
	"text/plain":      ".txt",
	"text/csv":        ".csv",
	"text/markdown":   ".md",
	"application/pdf": ".pdf",
	"video/mp4":       ".mp4",
	"video/webm":      ".webm",
	"audio/ogg":       ".ogg",
	"audio/mpeg":      ".mp3",
	"audio/wav":       ".wav",
}

// extFromContentType 从 Content-Type 推断文件后缀；推不出（空值、octet-stream、
// 未收录类型、解析失败）一律返回空串，调用方保持裸随机名。
func extFromContentType(contentType string) string {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ""
	}
	return contentTypeExts[mediaType]
}

// inlineContentType 返回文件可内联预览时的规范 Content-Type；不在白名单内返回 false。
func inlineContentType(filename string) (string, bool) {
	ext := strings.ToLower(filepath.Ext(filename))
	ctype, ok := inlinePreviewTypes[ext]
	return ctype, ok
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	server, err := NewFileServer()
	if err != nil {
		log.Fatal(err)
	}

	server.setupRoutes()

	go func() {
		for {
			if err := server.cleanupExpiredFiles(); err != nil {
				log.Printf("Cleanup failed: %v", err)
			}
			time.Sleep(1 * time.Hour)
		}
	}()

	log.Printf("Server starting on :8080")
	log.Fatal(server.app.Listen(":8080"))
}
