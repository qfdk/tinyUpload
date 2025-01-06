package main

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"log"
	"math/big"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	_ "github.com/mattn/go-sqlite3"
)

type FileServer struct {
	db        *sql.DB
	uploadDir string
	app       *fiber.App
}

func NewFileServer() (*FileServer, error) {
	// 创建必要的目录
	if err := os.MkdirAll("data", 0755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll("data/uploads", 0755); err != nil {
		return nil, err
	}

	// 初始化数据库
	db, err := sql.Open("sqlite3", "data/files.db")
	if err != nil {
		return nil, err
	}

	// 创建数据表
	_, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS files (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            path TEXT NOT NULL,
            filename TEXT NOT NULL,
            delete_code TEXT NOT NULL,
            upload_time DATETIME NOT NULL,
            file_size INTEGER NOT NULL,
            mime_type TEXT,
            download_count INTEGER DEFAULT 0,
            UNIQUE(path, filename)
        )
    `)
	if err != nil {
		return nil, err
	}

	// 初始化 Fiber 应用
	app := fiber.New(fiber.Config{
		Prefork:        true,
		ServerHeader:   "FileServer",
		BodyLimit:      1024 * 1024 * 1024, // 1G
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		IdleTimeout:    60 * time.Second,
		ReadBufferSize: 4096,
	})

	// 添加中间件
	app.Use(logger.New())
	app.Use(compress.New(compress.Config{
		Level: compress.LevelBestSpeed,
	}))
	app.Use(cors.New())

	return &FileServer{
		db:        db,
		uploadDir: "data/uploads",
		app:       app,
	}, nil
}

func (s *FileServer) handleRoot(c *fiber.Ctx) error {
    if isTextPreferred(c) {
        host := c.Hostname()
        now := time.Now().Format("2006-01-02 15:04:05")
        return c.Type("text").SendString(fmt.Sprintf(`File Server Usage Instructions:

Upload File:
  curl -T filename %s
  curl -T filename %s/new_filename

Download File:
  curl -O %s/xxxx/filename
  wget %s/xxxx/filename

Delete File:
  curl -X DELETE "%s/delete/xxxx/filename?code=delete_code"

Server Time: %s`, host, host, host, host, host, now))
    }

    // If not accessed from command line, return static file
    return c.SendFile("static/index.html")
}

func (s *FileServer) setupRoutes() {
	// 静态文件配置
	s.app.Static("/static", "./static") // 注意这里的路径配置

	// 首页路由
	s.app.Get("/", s.handleRoot)

	// 文件上传路由 - 直接使用根路径
	s.app.Put("/:filename", s.handleUpload) // 直接接收文件名
	s.app.Get("/:path/:filename", s.handleDownload)
	s.app.Delete("/delete/:path/:filename", s.handleDelete)
}

func (s *FileServer) handleUpload(c *fiber.Ctx) error {
	// 从路由参数获取文件名
	filename := c.Params("filename")
	if filename == "" {
		// 尝试从 Content-Disposition 获取文件名
		if cd := c.Get("Content-Disposition"); cd != "" {
			if _, params, err := mime.ParseMediaType(cd); err == nil {
				if fn := params["filename"]; fn != "" {
					filename = fn
				}
			}
		}
		if filename == "" {
			return c.Status(400).SendString("Error: No filename specified")
		}
	}

	// 生成随机路径
	path := generateRandomPath()
	deleteCode := generateRandomString(8)

	// 确保上传目录存在
	dirPath := filepath.Join(s.uploadDir, path)
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return c.Status(500).SendString("Error: Failed to create directory")
	}

	// 保存文件
	filePath := filepath.Join(dirPath, filename)
	fileContent := c.Body()
	if len(fileContent) == 0 {
		return c.Status(400).SendString("Error: Empty file content")
	}

	if err := os.WriteFile(filePath, fileContent, 0644); err != nil {
		return c.Status(500).SendString("Error: Failed to save file")
	}

	// 获取文件大小和MIME类型
	fileSize := int64(len(fileContent))
	mimeType := c.Get("Content-Type")
	if mimeType == "" {
		mimeType = mime.TypeByExtension(filepath.Ext(filename))
		if mimeType == "" {
			mimeType = http.DetectContentType(fileContent)
		}
	}

	// 保存到数据库
	_, err := s.db.Exec(`
        INSERT INTO files (path, filename, delete_code, upload_time, file_size, mime_type)
        VALUES (?, ?, ?, datetime('now'), ?, ?)
    `, path, filename, deleteCode, fileSize, mimeType)

	if err != nil {
		// 如果数据库操作失败，删除已上传的文件
		os.Remove(filePath)
		return c.Status(500).SendString("Error: Failed to save file information")
	}

	// 检查是否是命令行请求
	if isTextPreferred(c) {
	    // Return text format
	    return c.Type("text").SendString(fmt.Sprintf(`Upload successful!
	Filename: %s
	Access URL: http://%s/%s/%s
	Delete Code: %s
	Size: %d bytes
	Type: %s
	
	Delete Command:
	curl -X DELETE "http://%s/delete/%s/%s?code=%s"
	`,
	        filename,
	        c.Hostname(), path, filename,
	        deleteCode,
	        fileSize, mimeType,
	        c.Hostname(), path, filename, deleteCode,
	    ))
	}

	// 返回 JSON 格式
	return c.JSON(fiber.Map{
		"path":       path,
		"filename":   filename,
		"deleteCode": deleteCode,
		"size":       fileSize,
		"mimeType":   mimeType,
		"uploadTime": time.Now().Format("2006-01-02 15:04:05"),
	})
}

func (s *FileServer) handleDownload(c *fiber.Ctx) error {
	path := c.Params("path")
	filename := c.Params("filename")

	// 更新下载计数
	_, err := s.db.Exec("UPDATE files SET download_count = download_count + 1 WHERE path = ? AND filename = ?",
		path, filename)
	if err != nil {
		return c.Status(500).SendString("Failed to update download count")
	}

	return c.SendFile(filepath.Join(s.uploadDir, path, filename))
}

func (s *FileServer) handleDelete(c *fiber.Ctx) error {
	path := c.Params("path")
	filename := c.Params("filename")
	deleteCode := c.Query("code")

	if deleteCode == "" {
		return c.Status(400).SendString("Delete code is required")
	}

	// 验证删除码
	var exists bool
	err := s.db.QueryRow("SELECT EXISTS(SELECT 1 FROM files WHERE path = ? AND filename = ? AND delete_code = ?)",
		path, filename, deleteCode).Scan(&exists)
	if err != nil || !exists {
		return c.Status(403).SendString("Invalid delete code")
	}

	// 删除文件
	filePath := filepath.Join(s.uploadDir, path, filename)
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return c.Status(500).SendString("Failed to delete file")
	}

	// 删除数据库记录
	_, err = s.db.Exec("DELETE FROM files WHERE path = ? AND filename = ?", path, filename)
	if err != nil {
		return c.Status(500).SendString("Failed to delete file record")
	}

	// 尝试删除空目录
	os.Remove(filepath.Join(s.uploadDir, path))

	return c.SendString("File deleted successfully")
}

func (s *FileServer) cleanupExpiredFiles() error {
	// 获取3天前的文件
	query := `
        SELECT path, filename 
        FROM files 
        WHERE upload_time < datetime('now', '-3 days')
    `

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var path, filename string
		if err := rows.Scan(&path, &filename); err != nil {
			log.Printf("Failed to read file record: %v", err)
			continue
		}

		filePath := filepath.Join(s.uploadDir, path, filename)
		if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
			log.Printf("Failed to delete file %s: %v", filePath, err)
		}

		dirPath := filepath.Join(s.uploadDir, path)
		os.Remove(dirPath) // 忽略错误
	}

	_, err = tx.Exec(`DELETE FROM files WHERE upload_time < datetime('now', '-3 days')`)
	if err != nil {
		return err
	}

	return tx.Commit()
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
	return generateRandomString(4)
}

func isTextPreferred(c *fiber.Ctx) bool {
	userAgent := c.Get("User-Agent")
	return strings.HasPrefix(userAgent, "curl/") || strings.HasPrefix(userAgent, "Wget/")
}

func main() {
	server, err := NewFileServer()
	if err != nil {
		log.Fatal(err)
	}

	// 设置路由
	server.setupRoutes()

	// 启动定期清理
	go func() {
		for {
			if err := server.cleanupExpiredFiles(); err != nil {
				log.Printf("Cleanup failed: %v", err)
			}
			time.Sleep(1 * time.Hour)
		}
	}()

	// 启动服务器
	log.Fatal(server.app.Listen(":8080"))
}
