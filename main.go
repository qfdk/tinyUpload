package main

import (
	"compress/gzip"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type gzipResponseWriter struct {
	io.Writer
	http.ResponseWriter
}

func (w gzipResponseWriter) Write(b []byte) (int, error) {
	return w.Writer.Write(b)
}

func shouldCompress(contentType string) bool {
	switch {
	case strings.Contains(contentType, "text/"):
		return true
	case strings.Contains(contentType, "application/json"):
		return true
	case strings.Contains(contentType, "application/javascript"):
		return true
	case strings.Contains(contentType, "application/x-javascript"):
		return true
	default:
		return false
	}
}

func gzipMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 跳过文件上传请求
		if r.Method == http.MethodPut {
			next.ServeHTTP(w, r)
			return
		}

		// 检查客户端是否支持 gzip
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		// 继续处理请求
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Vary", "Accept-Encoding")
		gz := gzip.NewWriter(w)
		defer gz.Close()

		next.ServeHTTP(gzipResponseWriter{Writer: gz, ResponseWriter: w}, r)
	}
}

func (s *FileServer) cleanupExpiredFiles() error {
	// 获取3天前的文件
	query := `
        SELECT path, filename 
        FROM files 
        WHERE upload_time < datetime('now', '-1 days')
    `

	// 开始事务
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 查询需要删除的文件
	rows, err := tx.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()

	// 删除物理文件
	for rows.Next() {
		var path, filename string
		if err := rows.Scan(&path, &filename); err != nil {
			log.Printf("读取文件记录失败: %v", err)
			continue
		}

		filePath := filepath.Join(s.uploadDir, path, filename)
		if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
			log.Printf("删除文件失败 %s: %v", filePath, err)
		}

		dirPath := filepath.Join(s.uploadDir, path)
		os.Remove(dirPath) // 忽略错误，如果目录不为空会失败
	}

	// 删除数据库中的记录
	_, err = tx.Exec(`DELETE FROM files WHERE upload_time < datetime('now', '-3 days')`)
	if err != nil {
		return err
	}

	return tx.Commit()
}

type FileServer struct {
	db        *sql.DB
	uploadDir string
}

type FileInfo struct {
	ID            int64     `json:"id"`
	Path          string    `json:"path"`
	Filename      string    `json:"filename"`
	DeleteCode    string    `json:"deleteCode,omitempty"`
	UploadTime    time.Time `json:"uploadTime"`
	FileSize      int64     `json:"fileSize"`
	MimeType      string    `json:"mimeType"`
	DownloadCount int64     `json:"downloadCount"`
}

func NewFileServer() (*FileServer, error) {
	if err := os.MkdirAll("data", 0755); err != nil {
		return nil, err
	}

	if err := os.MkdirAll("data/uploads", 0755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite3", "data/files.db")
	if err != nil {
		return nil, err
	}

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

	return &FileServer{
		db:        db,
		uploadDir: "data/uploads",
	}, nil
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

func isTextPreferred(r *http.Request) bool {
	userAgent := r.Header.Get("User-Agent")
	return strings.HasPrefix(userAgent, "curl/") || strings.HasPrefix(userAgent, "Wget/")
}

func (s *FileServer) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	filename := filepath.Base(r.URL.Path)
	if filename == "/" || filename == "." {
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Disposition"))
		if err == nil && params["filename"] != "" {
			filename = params["filename"]
		} else {
			http.Error(w, "No filename specified", http.StatusBadRequest)
			return
		}
	}

	var path string
	for {
		path = generateRandomPath()
		var exists int
		err := s.db.QueryRow("SELECT COUNT(*) FROM files WHERE path = ? AND filename = ?",
			path, filename).Scan(&exists)
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		if exists == 0 {
			break
		}
	}

	dirPath := filepath.Join(s.uploadDir, path)
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	tx, err := s.db.Begin()
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	filePath := filepath.Join(dirPath, filename)
	file, err := os.Create(filePath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer file.Close()

	written, err := io.Copy(file, r.Body)
	if err != nil {
		os.Remove(filePath)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	file.Seek(0, 0)
	buffer := make([]byte, 512)
	_, err = file.Read(buffer)
	if err != nil && err != io.EOF {
		os.Remove(filePath)
		http.Error(w, "Error reading file", http.StatusInternalServerError)
		return
	}
	mimeType := http.DetectContentType(buffer)

	deleteCode := generateRandomString(8)

	_, err = tx.Exec(`
        INSERT INTO files (path, filename, delete_code, upload_time, file_size, mime_type)
        VALUES (?, ?, ?, ?, ?, ?)
    `, path, filename, deleteCode, time.Now(), written, mimeType)

	if err != nil {
		os.Remove(filePath)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if err = tx.Commit(); err != nil {
		os.Remove(filePath)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if isTextPreferred(r) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, `上传成功！
文件名: %s
访问链接: http://%s/%s/%s
删除码: %s
大小: %d bytes
类型: %s

删除命令:
curl -X DELETE "http://%s/delete/%s/%s?code=%s"
`,
			filename,
			r.Host, path, filename,
			deleteCode,
			written, mimeType,
			r.Host, path, filename, deleteCode,
		)
	} else {
		w.Header().Set("Content-Type", "application/json")
		response := map[string]interface{}{
			"path":       path,
			"filename":   filename,
			"deleteCode": deleteCode,
			"size":       written,
			"mimeType":   mimeType,
			"uploadTime": time.Now().Format("2006-01-02 15:04:05"),
		}
		json.NewEncoder(w).Encode(response)
	}
}

func (s *FileServer) handleDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	urlPath := strings.TrimPrefix(r.URL.Path, "/")
	parts := strings.SplitN(urlPath, "/", 2)
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	path := parts[0]
	filename := parts[1]

	var fileInfo FileInfo
	err := s.db.QueryRow(`
        SELECT id, filename, file_size, mime_type, download_count
        FROM files WHERE path = ? AND filename = ?
    `, path, filename).Scan(&fileInfo.ID, &fileInfo.Filename, &fileInfo.FileSize, &fileInfo.MimeType, &fileInfo.DownloadCount)

	if err == sql.ErrNoRows {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	filePath := filepath.Join(s.uploadDir, path, filename)
	file, err := os.Open(filePath)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	defer file.Close()

	w.Header().Set("Content-Type", fileInfo.MimeType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileInfo.Filename))

	_, err = io.Copy(w, file)
	if err != nil {
		log.Printf("Error sending file: %v", err)
		return
	}

	_, err = s.db.Exec("UPDATE files SET download_count = download_count + 1 WHERE id = ?", fileInfo.ID)
	if err != nil {
		log.Printf("Error updating download count: %v", err)
	}
}

func (s *FileServer) handleDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	urlPath := strings.TrimPrefix(r.URL.Path, "/delete/")
	parts := strings.SplitN(urlPath, "/", 2)
	if len(parts) != 2 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	path := parts[0]
	filename := parts[1]
	code := r.URL.Query().Get("code")

	if path == "" || filename == "" || code == "" {
		http.Error(w, "Missing path, filename or delete code", http.StatusBadRequest)
		return
	}

	tx, err := s.db.Begin()
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	var exists bool
	err = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM files WHERE path = ? AND filename = ? AND delete_code = ?)",
		path, filename, code).Scan(&exists)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if !exists {
		http.Error(w, "Invalid path, filename or delete code", http.StatusNotFound)
		return
	}

	_, err = tx.Exec("DELETE FROM files WHERE path = ? AND filename = ? AND delete_code = ?",
		path, filename, code)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	filePath := filepath.Join(s.uploadDir, path, filename)
	err = os.Remove(filePath)
	if err != nil && !os.IsNotExist(err) {
		http.Error(w, "Error deleting file", http.StatusInternalServerError)
		return
	}
	os.Remove(filepath.Join(s.uploadDir, path))

	if err = tx.Commit(); err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if isTextPreferred(r) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "文件已成功删除\n")
	} else {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]string{"message": "File deleted successfully"})
	}
}

func (s *FileServer) handleFiles(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`
        SELECT id, path, filename, upload_time, file_size, mime_type, download_count
        FROM files ORDER BY upload_time DESC
    `)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var files []FileInfo
	for rows.Next() {
		var file FileInfo
		err := rows.Scan(&file.ID, &file.Path, &file.Filename, &file.UploadTime,
			&file.FileSize, &file.MimeType, &file.DownloadCount)
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		files = append(files, file)
	}

	if isTextPreferred(r) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "文件列表:")
		for _, file := range files {
			fmt.Fprintf(w, "\n文件名: %s\n链接: http://%s/%s/%s\n大小: %d bytes\n下载次数: %d\n上传时间: %s\n",
				file.Filename, r.Host, file.Path, file.Filename, file.FileSize, file.DownloadCount,
				file.UploadTime.Format("2006-01-02 15:04:05"))
		}
	} else {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(files)
	}
}

func (s *FileServer) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		if r.Method == http.MethodGet {
			s.handleDownload(w, r)
			return
		}
		http.NotFound(w, r)
		return
	}

	if isTextPreferred(r) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, `文件服务器使用说明:

上传文件:
  curl -T 文件名 %s
  curl -T 文件名 %s/新文件名

下载文件:
  curl -O %s/xxxx/文件名
  wget %s/xxxx/文件名

删除文件:
  curl -X DELETE "http://%s/delete/xxxx/文件名?code=删除码"

服务器时间: %s
`, r.Host, r.Host, r.Host, r.Host, r.Host, r.Host, time.Now().Format("2006-01-02 15:04:05"))
	} else {
		http.ServeFile(w, r, "static/index.html")
	}
}

func main() {
	server, err := NewFileServer()
	if err != nil {
		log.Fatal(err)
	}

	// 启动定期清理任务
	go func() {
		for {
			if err := server.cleanupExpiredFiles(); err != nil {
				log.Printf("清理过期文件失败: %v", err)
			}
			time.Sleep(1 * time.Hour)
		}
	}()

	// 设置路由处理
	http.HandleFunc("/", gzipMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			http.ServeFile(w, r, "static/index.html")
			return
		}

		switch r.Method {
		case http.MethodPut:
			server.handleUpload(w, r)
		default:
			server.handleDownload(w, r)
		}
	}))

	http.HandleFunc("/static/style.css", gzipMiddleware(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/css; charset=utf-8")
		http.ServeFile(writer, request, "static/style.css")
	}))

	http.HandleFunc("/delete/", gzipMiddleware(server.handleDelete))

	addr := ":8080"
	log.Printf("Starting server on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
