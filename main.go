package main

import (
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
	// 创建 data 目录
	if err := os.MkdirAll("data", 0755); err != nil {
		return nil, err
	}

	// 创建上传文件目录
	if err := os.MkdirAll("data/uploads", 0755); err != nil {
		return nil, err
	}

	// 使用 data 目录下的 files.db
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
		uploadDir: "data/uploads", // 修改上传目录路径
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
	// 检查是否是命令行工具
	if strings.HasPrefix(userAgent, "curl/") ||
		strings.HasPrefix(userAgent, "Wget/") {
		return true
	}

	// 其他客户端返回 false，使用 HTML 界面
	return false
}

func (s *FileServer) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 获取文件名
	filename := filepath.Base(r.URL.Path)
	if filename == "/" || filename == "." {
		// 从 Content-Disposition 头获取原始文件名
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Disposition"))
		if err == nil && params["filename"] != "" {
			filename = params["filename"]
		} else {
			http.Error(w, "No filename specified", http.StatusBadRequest)
			return
		}
	}

	// 生成唯一的4位路径
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

	// 创建目录
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

	// 使用路径和文件名组合作为存储路径
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
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message":    "Upload success",
			"filename":   filename,
			"url":        fmt.Sprintf("http://%s/%s/%s", r.Host, path, filename),
			"deleteCode": deleteCode,
			"size":       written,
			"mimeType":   mimeType,
		})
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
	// 尝试删除空目录
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
  curl -T 文件名 http://%s/
  curl -T 文件名 http://%s/新文件名

下载文件:
  curl -O http://%s/xxxx/文件名
  wget http://%s/xxxx/文件名

查看文件列表:
  curl http://%s/files

删除文件:
  curl -X DELETE "http://%s/delete/xxxx/文件名?code=删除码"

服务器时间: %s
`, r.Host, r.Host, r.Host, r.Host, r.Host, r.Host, time.Now().Format("2006-01-02 15:04:05"))
	} else {
		http.ServeFile(w, r, "index.html")
	}
}

func main() {
	server, err := NewFileServer()
	if err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			server.handleUpload(w, r)
		default:
			server.handleRoot(w, r)
		}
	})

	http.HandleFunc("/files", server.handleFiles)
	http.HandleFunc("/delete/", server.handleDelete)

	addr := ":8080"
	log.Printf("Starting server on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
