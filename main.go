package main

import (
   "crypto/rand"
   "database/sql"
   "fmt"
   "log"
   "math/big"
   "mime"
   "net/http"
   "net/url"
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
   if err := os.MkdirAll("data", 0755); err != nil {
       return nil, fmt.Errorf("failed to create data directory: %v", err)
   }
   if err := os.MkdirAll("data/uploads", 0755); err != nil {
       return nil, fmt.Errorf("failed to create uploads directory: %v", err)
   }

   db, err := sql.Open("sqlite3", "data/files.db")
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
           UNIQUE(path, encoded_filename)
       )
   `)
   if err != nil {
       return nil, fmt.Errorf("failed to create table: %v", err)
   }

   app := fiber.New(fiber.Config{
       Prefork:      false,
       ServerHeader: "FileServer",
       BodyLimit:    1024 * 1024 * 1024,
       ReadTimeout:  30 * time.Second,
       WriteTimeout: 30 * time.Second,
       IdleTimeout:  60 * time.Second,
       ProxyHeader:   "X-Real-IP",
       EnableTrustedProxyCheck: true,
       TrustedProxies: []string{"127.0.0.1", "::1","172.17.0.1","192.168.1.8"},
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
   app.Use(cors.New())

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
   s.app.Put("/:filename", s.handleUpload)
   s.app.Get("/:path/:filename", s.handleDownload)
   s.app.Delete("/delete/:path/:filename", s.handleDelete)
   
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

Download File:
 curl -O %s/xxxx/filename
 wget %s/xxxx/filename

Delete File:
 curl -X DELETE "%s/delete/xxxx/filename?code=delete_code"

Server Time: %s\n`, host, host, host, host, host, now))
   }

   return c.SendFile("static/index.html")
}

func (s *FileServer) handleUpload(c *fiber.Ctx) error {
   filename := c.Params("filename")
   decodedFilename, err := url.QueryUnescape(filename)
   if err != nil {
       return c.Status(400).SendString("Invalid filename\n")
   }

   if decodedFilename == "" {
       if cd := c.Get("Content-Disposition"); cd != "" {
           if _, params, err := mime.ParseMediaType(cd); err == nil {
               if fn := params["filename"]; fn != "" {
                   decodedFilename = fn
               }
           }
       }
       if decodedFilename == "" {
           return c.Status(400).SendString("No filename specified\n")
       }
   }

   path := generateRandomPath()
   dirPath := filepath.Join(s.uploadDir, path)
   if err := os.MkdirAll(dirPath, 0755); err != nil {
       return c.Status(500).SendString("Failed to create directory\n")
   }

   encodedFilename := url.QueryEscape(decodedFilename)
   log.Printf("Saving to DB - path: %s, filename: %s, encoded: %s", 
       path, decodedFilename, encodedFilename)
       
   filePath := filepath.Join(dirPath, decodedFilename)
   fileContent := c.Body()
   if len(fileContent) == 0 {
       return c.Status(400).SendString("Empty file content\n")
   }

   if err := os.WriteFile(filePath, fileContent, 0644); err != nil {
       return c.Status(500).SendString("Failed to save file\n")
   }

   fileSize := int64(len(fileContent))
   mimeType := c.Get("Content-Type")
   if mimeType == "" {
       mimeType = mime.TypeByExtension(filepath.Ext(decodedFilename))
       if mimeType == "" {
           mimeType = http.DetectContentType(fileContent)
       }
   }

   deleteCode := generateRandomString(8)

   _, err = s.db.Exec(`
       INSERT INTO files (path, filename, encoded_filename, delete_code, upload_time, file_size, mime_type)
       VALUES (?, ?, ?, ?, datetime('now'), ?, ?)
   `, path, decodedFilename, encodedFilename, deleteCode, fileSize, mimeType)

   if err != nil {
       os.Remove(filePath)
       return c.Status(500).SendString("Failed to save file information\n")
   }

   if isTextPreferred(c) {
       return c.Type("text").SendString(fmt.Sprintf(`Upload successful!
Filename: %s
Access URL: http://%s/%s/%s
Delete Code: %s
Size: %d bytes
Type: %s

Delete Command:
curl -X DELETE "http://%s/delete/%s/%s?code=%s"
                                                    `,
           decodedFilename,
           c.Hostname(), path, encodedFilename,
           deleteCode,
           fileSize, mimeType,
           c.Hostname(), path, encodedFilename, deleteCode,
       ))
   }

   return c.JSON(fiber.Map{
       "path":       path,
       "filename":   decodedFilename,
       "deleteCode": deleteCode,
       "size":       fileSize,
       "mimeType":   mimeType,
       "uploadTime": time.Now().Format("2006-01-02 15:04:05"),
   })
}

func (s *FileServer) handleDownload(c *fiber.Ctx) error {
   path := c.Params("path")
   requestFilename := c.Params("filename")
   
   decodedRequestFilename, err := url.QueryUnescape(requestFilename)
   if err != nil {
       return c.Status(404).SendString("File not found\n")
   }
   
   encodedRequestFilename := url.QueryEscape(decodedRequestFilename)
   
   var originalFilename string
   err = s.db.QueryRow("SELECT filename FROM files WHERE path = ? AND encoded_filename = ?",
       path, encodedRequestFilename).Scan(&originalFilename)
   if err != nil {
       return c.Status(404).SendString("File not found\n")
   }

   filePath := filepath.Join(s.uploadDir, path, originalFilename)
   if _, err := os.Stat(filePath); os.IsNotExist(err) {
       return c.Status(404).SendString("File not found\n")
   }

   _, err = s.db.Exec("UPDATE files SET download_count = download_count + 1 WHERE path = ? AND encoded_filename = ?",
       path, encodedRequestFilename)
   if err != nil {
       log.Printf("Error updating download count: %v", err)
   }

   return c.SendFile(filePath)
}

func (s *FileServer) handleDelete(c *fiber.Ctx) error {
   path := c.Params("path")
   requestFilename := c.Params("filename")
   encodedDeleteCode := c.Query("code")

   decodedFilename, err := url.QueryUnescape(requestFilename)
   if err != nil {
       return c.Status(404).SendString("File not found\n")
   }
   
   encodedFilename := url.QueryEscape(decodedFilename)

   decodedDeleteCode, err := url.QueryUnescape(encodedDeleteCode)
   if err != nil {
       return c.Status(400).SendString("Invalid delete code\n")
   }

   var filename string
   err = s.db.QueryRow(
       "SELECT filename FROM files WHERE path = ? AND encoded_filename = ? AND delete_code = ?",
       path, encodedFilename, decodedDeleteCode,
   ).Scan(&filename)

   if err != nil {
       if err == sql.ErrNoRows {
           return c.Status(403).SendString("Invalid delete code\n")
       }
       return c.Status(500).SendString("Internal server error\n")
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
       return c.Status(500).SendString("Failed to delete file record\n")
   }

   dirPath := filepath.Join(s.uploadDir, path)
   if err := os.Remove(dirPath); err != nil {
       log.Printf("Failed to remove directory (may not be empty): %v", err)
   }

   return c.Status(200).SendString("OK\n")
}

func (s *FileServer) cleanupExpiredFiles() error {
   rows, err := s.db.Query(`
       SELECT path, encoded_filename, filename 
       FROM files 
       WHERE upload_time < datetime('now', '-3 days')
   `)
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

   _, err = s.db.Exec(`DELETE FROM files WHERE upload_time < datetime('now', '-3 days')`)
   if err != nil {
       return fmt.Errorf("failed to delete expired records: %v", err)
   }

   return nil
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
