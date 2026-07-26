package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestInlineContentType_Whitelist(t *testing.T) {
	inline := map[string]string{
		"photo.png":  "image/png",
		"PHOTO.PNG":  "image/png",
		"a.jpg":      "image/jpeg",
		"a.jpeg":     "image/jpeg",
		"anim.gif":   "image/gif",
		"pic.webp":   "image/webp",
		"icon.ico":   "image/x-icon",
		"notes.txt":  "text/plain; charset=utf-8",
		"server.LOG": "text/plain; charset=utf-8",
		"readme.md":  "text/plain; charset=utf-8",
		"data.csv":   "text/plain; charset=utf-8",
		"doc.pdf":    "application/pdf",
		"clip.mp4":   "video/mp4",
		"song.mp3":   "audio/mpeg",
	}
	for name, want := range inline {
		got, ok := inlineContentType(name)
		if !ok {
			t.Errorf("expected %q to be inline-previewable, got blocked", name)
			continue
		}
		if got != want {
			t.Errorf("inlineContentType(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestExtFromContentType(t *testing.T) {
	cases := map[string]string{
		"image/png":                 ".png",
		"IMAGE/PNG":                 ".png",
		"image/jpeg":                ".jpg", // 不能是 Go mime 包默认的 .jpe
		"image/gif":                 ".gif",
		"text/plain":                ".txt",
		"text/plain; charset=utf-8": ".txt",
		"application/pdf":           ".pdf",
		"video/mp4":                 ".mp4",
		"audio/mpeg":                ".mp3",
		// 推不出或不该推的：返回空串，保持裸随机名
		"":                         "",
		"application/octet-stream": "",
		"application/x-unknown":    "",
		"not a media type":         "",
		// 可脚本类型不造预览后缀
		"text/html":     "",
		"image/svg+xml": "",
	}
	for ct, want := range cases {
		if got := extFromContentType(ct); got != want {
			t.Errorf("extFromContentType(%q) = %q, want %q", ct, got, want)
		}
	}
}

func TestInlineContentType_BlocksScriptable(t *testing.T) {
	// 关键安全断言：可承载脚本/同源执行的类型必须落入强制下载分支。
	blocked := []string{
		"index.html",
		"page.htm",
		"evil.svg",
		"data.xml",
		"app.xhtml",
		"hack.js",
		"shell.exe",
		"archive.zip",
		"noextension",
		"trailingdot.",
		"evil.svg.txt.svg",
	}
	for _, name := range blocked {
		if ct, ok := inlineContentType(name); ok {
			t.Errorf("expected %q to be blocked from inline preview, got Content-Type %q", name, ct)
		}
	}
}

func TestShareProtocol(t *testing.T) {
	cases := []struct {
		proto, host, want string
	}{
		// 公网域名/IP：无论请求协议一律 https
		{"http", "tar.tn", "https"},
		{"http", "tar.tn:8080", "https"},
		{"https", "tar.tn", "https"},
		{"http", "8.8.8.8", "https"},
		// 本机/内网：保持 http 方便调试
		{"http", "localhost:8080", "http"},
		{"http", "localhost", "http"},
		{"http", "127.0.0.1:8080", "http"},
		{"http", "[::1]:8080", "http"},
		{"http", "10.0.0.2", "http"},
		{"http", "192.168.1.5:3000", "http"},
		{"http", "172.16.0.1", "http"},
		// 本机但请求已是 https：保持 https
		{"https", "localhost:8080", "https"},
	}
	for _, tc := range cases {
		if got := shareProtocol(tc.proto, tc.host); got != tc.want {
			t.Errorf("shareProtocol(%q, %q) = %q, want %q", tc.proto, tc.host, got, tc.want)
		}
	}
}

// TestDeleteRoute 验证删除与下载共用同一资源路径：DELETE /:path/:filename。
// 旧的 /delete/:path/:filename 前缀路由已移除（临时上传服务，无兼容负担）。
func TestDeleteRoute(t *testing.T) {
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)

	s, err := NewFileServer()
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	s.setupRoutes()

	upload := func() (path, code string) {
		t.Helper()
		req := httptest.NewRequest("PUT", "/note.txt", strings.NewReader("hello"))
		resp, err := s.app.Test(req, -1)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 200 {
			t.Fatalf("upload status = %d, want 200", resp.StatusCode)
		}
		var r struct {
			Path       string `json:"path"`
			DeleteCode string `json:"deleteCode"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
			t.Fatal(err)
		}
		return r.Path, r.DeleteCode
	}

	// X-Delete-Code 头（网页用法）
	path1, code1 := upload()
	req := httptest.NewRequest("DELETE", "/"+path1+"/note.txt", nil)
	req.Header.Set("X-Delete-Code", code1)
	resp, _ := s.app.Test(req, -1)
	if resp.StatusCode != 200 {
		t.Errorf("DELETE /:path/:filename with header = %d, want 200", resp.StatusCode)
	}

	// ?code= 查询参数（CLI 用法）
	path2, code2 := upload()
	req = httptest.NewRequest("DELETE", "/"+path2+"/note.txt?code="+code2, nil)
	resp, _ = s.app.Test(req, -1)
	if resp.StatusCode != 200 {
		t.Errorf("DELETE /:path/:filename with ?code= = %d, want 200", resp.StatusCode)
	}

	// 错误删除码必须拒绝
	path3, code3 := upload()
	req = httptest.NewRequest("DELETE", "/"+path3+"/note.txt", nil)
	req.Header.Set("X-Delete-Code", "wrong000")
	resp, _ = s.app.Test(req, -1)
	if resp.StatusCode != 403 {
		t.Errorf("DELETE with wrong code = %d, want 403", resp.StatusCode)
	}

	// 旧 /delete/ 前缀路由必须已移除
	req = httptest.NewRequest("DELETE", "/delete/"+path3+"/note.txt?code="+code3, nil)
	resp, _ = s.app.Test(req, -1)
	if resp.StatusCode == 200 {
		t.Errorf("legacy DELETE /delete/... still works (status 200), want removed")
	}
}

func TestParseExpires(t *testing.T) {
	cases := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"", time.Hour, false}, // 默认 1 小时
		// 裸数字 = 天数
		{"1", 24 * time.Hour, false},
		{"2", 48 * time.Hour, false},
		{"3", 72 * time.Hour, false},
		// 带单位:m 分钟 / h 小时 / d 天
		{"30m", 30 * time.Minute, false},
		{"1h", time.Hour, false},
		{"12h", 12 * time.Hour, false},
		{"2d", 48 * time.Hour, false},
		// 超过上限按 3 天截断
		{"7", 72 * time.Hour, false},
		{"7d", 72 * time.Hour, false},
		{"100h", 72 * time.Hour, false},
		// 非法输入必须报错
		{"0", 0, true},
		{"0m", 0, true},
		{"-1h", 0, true},
		{"h", 0, true},
		{"1w", 0, true},
		{"abc", 0, true},
		{"1.5h", 0, true},
	}
	for _, tc := range cases {
		got, err := parseExpires(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseExpires(%q) = %v, want error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseExpires(%q) unexpected error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseExpires(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// newTestServer 在临时目录里启动一个带路由的 FileServer。
func newTestServer(t *testing.T) *FileServer {
	t.Helper()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(oldWd) })

	s, err := NewFileServer()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.db.Close() })
	s.setupRoutes()
	return s
}

func TestUploadExpiresAndLimit(t *testing.T) {
	s := newTestServer(t)

	// 带 expires 与 limit 上传，JSON 返回过期时间与次数
	req := httptest.NewRequest("PUT", "/note.txt?t=1h&n=2", strings.NewReader("hello"))
	resp, err := s.app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("upload status = %d, want 200", resp.StatusCode)
	}
	var r struct {
		Path         string `json:"path"`
		ExpireTime   string `json:"expireTime"`
		MaxDownloads int    `json:"maxDownloads"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		t.Fatal(err)
	}
	if r.ExpireTime == "" {
		t.Errorf("expected non-empty expireTime in upload response")
	}
	if r.MaxDownloads != 2 {
		t.Errorf("maxDownloads = %d, want 2", r.MaxDownloads)
	}

	// 前 2 次下载成功，第 3 次因达到次数上限拒绝
	for i := 1; i <= 2; i++ {
		req = httptest.NewRequest("GET", "/"+r.Path+"/note.txt", nil)
		resp, _ = s.app.Test(req, -1)
		if resp.StatusCode != 200 {
			t.Fatalf("download #%d status = %d, want 200", i, resp.StatusCode)
		}
	}
	req = httptest.NewRequest("GET", "/"+r.Path+"/note.txt", nil)
	resp, _ = s.app.Test(req, -1)
	if resp.StatusCode != 404 {
		t.Errorf("download #3 status = %d, want 404 (limit reached)", resp.StatusCode)
	}

	// 非法参数必须 400
	for _, target := range []string{"/a.txt?t=1w", "/a.txt?t=0m", "/a.txt?n=0", "/a.txt?n=abc"} {
		req = httptest.NewRequest("PUT", target, strings.NewReader("x"))
		resp, _ = s.app.Test(req, -1)
		if resp.StatusCode != 400 {
			t.Errorf("PUT %s status = %d, want 400", target, resp.StatusCode)
		}
	}

	// 不带参数：无次数限制，多次下载都成功
	req = httptest.NewRequest("PUT", "/free.txt", strings.NewReader("hello"))
	resp, _ = s.app.Test(req, -1)
	var r2 struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r2); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		req = httptest.NewRequest("GET", "/"+r2.Path+"/free.txt", nil)
		resp, _ = s.app.Test(req, -1)
		if resp.StatusCode != 200 {
			t.Fatalf("unlimited download #%d status = %d, want 200", i+1, resp.StatusCode)
		}
	}
}

// TestDefaultRetentionByClient 验证不带 t 参数时的默认保留时长:
// 网页与 curl/wget 统一默认 1 小时。
func TestDefaultRetentionByClient(t *testing.T) {
	s := newTestServer(t)

	retentionHours := func(ua string) float64 {
		t.Helper()
		req := httptest.NewRequest("PUT", "/f-"+strings.ReplaceAll(ua, "/", "-")+".txt", strings.NewReader("hello"))
		if ua != "" {
			req.Header.Set("User-Agent", ua)
		}
		resp, err := s.app.Test(req, -1)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 200 {
			t.Fatalf("upload status = %d, want 200", resp.StatusCode)
		}
		// curl UA 返回纯文本,统一从 DB 直接查最新记录
		var hours float64
		if err := s.db.QueryRow(`
           SELECT (julianday(expire_time) - julianday('now')) * 24
           FROM files ORDER BY id DESC LIMIT 1
       `).Scan(&hours); err != nil {
			t.Fatal(err)
		}
		return hours
	}

	for _, ua := range []string{"curl/8.4.0", "Wget/1.21", "Mozilla/5.0"} {
		if h := retentionHours(ua); h < 0.98 || h > 1.02 {
			t.Errorf("%s default retention = %.2fh, want ~1h", ua, h)
		}
	}
}

func TestExpiredFileInaccessibleAndCleaned(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest("PUT", "/note.txt?t=1h", strings.NewReader("hello"))
	resp, err := s.app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	var r struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		t.Fatal(err)
	}

	// 把过期时间改到过去：下载必须立刻 404（不等每小时的清理）
	if _, err := s.db.Exec(`UPDATE files SET expire_time = datetime('now', '-1 minute') WHERE path = ?`, r.Path); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest("GET", "/"+r.Path+"/note.txt", nil)
	resp, _ = s.app.Test(req, -1)
	if resp.StatusCode != 404 {
		t.Errorf("expired file download status = %d, want 404", resp.StatusCode)
	}

	// 清理任务应删掉过期记录与文件
	if err := s.cleanupExpiredFiles(); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM files WHERE path = ?`, r.Path).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("expired record still in db after cleanup")
	}
	if _, err := os.Stat("data/uploads/" + r.Path + "/note.txt"); !os.IsNotExist(err) {
		t.Errorf("expired file still on disk after cleanup")
	}
}

func TestCleanupLegacyNullExpireFallback(t *testing.T) {
	s := newTestServer(t)

	// 模拟历史数据：expire_time 为 NULL，按 upload_time + 3 天兜底
	req := httptest.NewRequest("PUT", "/old.txt", strings.NewReader("hello"))
	resp, err := s.app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	var r struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE files SET expire_time = NULL, upload_time = datetime('now', '-4 days') WHERE path = ?`, r.Path); err != nil {
		t.Fatal(err)
	}

	if err := s.cleanupExpiredFiles(); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM files WHERE path = ?`, r.Path).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("legacy NULL-expire record older than 3 days not cleaned")
	}
}

func TestCleanupDownloadLimitReached(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest("PUT", "/once.txt?n=1", strings.NewReader("hello"))
	resp, err := s.app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	var r struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		t.Fatal(err)
	}

	// 用掉唯一一次下载
	req = httptest.NewRequest("GET", "/"+r.Path+"/once.txt", nil)
	resp, _ = s.app.Test(req, -1)
	if resp.StatusCode != 200 {
		t.Fatalf("first download status = %d, want 200", resp.StatusCode)
	}

	// 清理任务应把用完次数的文件删掉
	if err := s.cleanupExpiredFiles(); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM files WHERE path = ?`, r.Path).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("limit-reached record still in db after cleanup")
	}
}
