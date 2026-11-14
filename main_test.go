package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
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

// newBareTestServer 在临时目录里创建一个还没注册路由的 FileServer。
func newBareTestServer(t *testing.T) *FileServer {
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
	return s
}

// newTestServer 在临时目录里启动一个带路由的 FileServer。
func newTestServer(t *testing.T) *FileServer {
	t.Helper()
	s := newBareTestServer(t)
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

// putFile 以原始字节流上传 body，返回服务端分配的随机路径。target 原样写进请求行。
func putFile(t *testing.T, s *FileServer, target, body string) string {
	t.Helper()
	resp, err := s.app.Test(httptest.NewRequest("PUT", target, strings.NewReader(body)), -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("PUT %s status = %d, want 200", target, resp.StatusCode)
	}
	var r struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		t.Fatal(err)
	}
	return r.Path
}

// getFile 请求 target（可附加请求头），返回响应与响应体。
func getFile(t *testing.T, s *FileServer, target string, header http.Header) (*http.Response, string) {
	t.Helper()
	req := httptest.NewRequest("GET", target, nil)
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := s.app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, string(body)
}

// TestDownloadDoesNotDecodeStoredFilename 是任意文件读取漏洞的回归测试：
// 文件名字面量里的 %2e%2e%2f 在下载时不得被解码成 ../，否则上传者起个名字
// 就能读到别人的文件、data/files.db（全站路径与删除码）乃至上传目录外的任意文件。
func TestDownloadDoesNotDecodeStoredFilename(t *testing.T) {
	s := newTestServer(t)

	victimPath := putFile(t, s, "/secret.txt", "TOP-SECRET")

	for _, name := range []string{
		"%2e%2e%2f" + victimPath + "%2fsecret.txt", // 解码后指向受害者的文件
		"%2e%2e%2f%2e%2e%2ffiles.db",               // 解码后指向 data/files.db
	} {
		// 请求行里再编码一层，服务端解码一次后落盘文件名就是上面的字面量
		target := "/" + url.QueryEscape(name)
		path := putFile(t, s, target, "attacker-own-bytes")

		resp, body := getFile(t, s, "/"+path+target, nil)
		if resp.StatusCode != 200 || body != "attacker-own-bytes" {
			t.Errorf("download of file literally named %q: status = %d, body starts %.20q; want 200 with the file's own content",
				name, resp.StatusCode, body)
		}
	}
}

// TestDownloadSpecialCharFilenames：文件名里的 # 与 % 是普通字符，上传后必须能原样下载回来。
func TestDownloadSpecialCharFilenames(t *testing.T) {
	s := newTestServer(t)

	for _, name := range []string{"issue #12.txt", "report 100%25.txt", "a%41b.txt", "50% off.txt"} {
		target := "/" + url.QueryEscape(name)
		path := putFile(t, s, target, "content of "+name)

		resp, body := getFile(t, s, "/"+path+target, nil)
		if resp.StatusCode != 200 || body != "content of "+name {
			t.Errorf("download of %q: status = %d, body = %q; want 200 with the uploaded content", name, resp.StatusCode, body)
		}
	}
}

// TestDownloadRangeAndNoCompression 锁定下载的两个既有行为：支持 Range（断点续传、
// 视频拖动），以及文件内容不经 compress 中间件压缩（否则丢失 Content-Length 与 Range）。
func TestDownloadRangeAndNoCompression(t *testing.T) {
	s := newTestServer(t)

	const content = "0123456789abcdefghijklmnopqrstuvwxyz"
	path := putFile(t, s, "/sample.txt", content)
	target := "/" + path + "/sample.txt"

	resp, body := getFile(t, s, target, http.Header{"Range": {"bytes=10-19"}})
	if resp.StatusCode != 206 || body != "abcdefghij" || resp.Header.Get("Content-Range") != "bytes 10-19/36" {
		t.Errorf("range download: status = %d, body = %q, Content-Range = %q; want 206, %q, %q",
			resp.StatusCode, body, resp.Header.Get("Content-Range"), "abcdefghij", "bytes 10-19/36")
	}

	resp, body = getFile(t, s, target, http.Header{"Accept-Encoding": {"gzip, br"}})
	if resp.StatusCode != 200 || body != content || resp.Header.Get("Content-Encoding") != "" {
		t.Errorf("download with Accept-Encoding: status = %d, Content-Encoding = %q, body = %q; want 200, uncompressed %q",
			resp.StatusCode, resp.Header.Get("Content-Encoding"), body, content)
	}
}

// TestUploadWithoutBodyStream 是拒绝服务漏洞的回归测试：既无 Content-Length 也非 chunked
// 的 PUT（如 `curl -X PUT host/x`）没有请求体流，曾在 io.Copy 处空指针 panic 打死整个进程。
func TestUploadWithoutBodyStream(t *testing.T) {
	s := newTestServer(t)

	resp, err := s.app.Test(httptest.NewRequest("PUT", "/empty.txt", nil), -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 400 {
		t.Errorf("PUT without body status = %d, want 400", resp.StatusCode)
	}
}

// TestUploadMultipartBodyStoredRaw：上传接口只收原始字节流。multipart 请求体不得被
// fasthttp 预解析（预解析发生在路由之前，不受 maxUploadSize 约束地写临时文件，且不产生
// 请求体流），而是与其他请求体一样原样落盘。
func TestUploadMultipartBodyStoredRaw(t *testing.T) {
	s := newTestServer(t)

	const raw = "--XB\r\nContent-Disposition: form-data; name=\"f\"; filename=\"a.txt\"\r\n\r\nhello\r\n--XB--\r\n"
	req := httptest.NewRequest("PUT", "/form.bin", strings.NewReader(raw))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=XB")
	resp, err := s.app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("multipart PUT status = %d, want 200", resp.StatusCode)
	}
	var r struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		t.Fatal(err)
	}

	if _, body := getFile(t, s, "/"+r.Path+"/form.bin", nil); body != raw {
		t.Errorf("stored multipart upload = %q, want the raw request body %q", body, raw)
	}
}

// recordCount 返回随机路径 path 下还有几条文件记录。
func recordCount(t *testing.T, s *FileServer, path string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM files WHERE path = ?`, path).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// expireNow 把随机路径 path 下的文件改成已过期。
func expireNow(t *testing.T, exec func(query string, args ...any) error, path string) {
	t.Helper()
	if err := exec(`UPDATE files SET expire_time = datetime('now', '-1 minute') WHERE path = ?`, path); err != nil {
		t.Fatal(err)
	}
}

// TestCleanupDoesNotOrphanFilesExpiringMidRun：清理任务查询之后才过期的文件，这一轮不会去删它，
// 那它的记录也不能删。丢了记录，文件就永远留在磁盘上，再没有任何东西会去清理。
func TestCleanupDoesNotOrphanFilesExpiringMidRun(t *testing.T) {
	s := newTestServer(t)
	dbExec := func(query string, args ...any) error {
		_, err := s.db.Exec(query, args...)
		return err
	}

	expiredPath := putFile(t, s, "/expired.txt", "a")
	livePath := putFile(t, s, "/live.txt", "b")
	liveFile := "data/uploads/" + livePath + "/live.txt"
	expireNow(t, dbExec, expiredPath)

	// 持有写锁：清理任务能查询、能删文件，但写库时会等在这里
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	connExec := func(query string, args ...any) error {
		_, err := conn.ExecContext(ctx, query, args...)
		return err
	}
	if err := connExec("BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- s.cleanupExpiredFiles() }()

	// expired.txt 从磁盘上消失，说明清理任务已经查询完毕，正等着写库
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat("data/uploads/" + expiredPath + "/expired.txt"); os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cleanup did not remove expired.txt in time")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// 就在这个空档里 live.txt 过期了，然后放行
	expireNow(t, connExec, livePath)
	if err := connExec("COMMIT"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(liveFile); err != nil {
		t.Fatalf("live.txt should still be on disk after the first run: %v", err)
	}
	if recordCount(t, s, livePath) != 1 {
		t.Fatal("record of live.txt was deleted while its file is still on disk: the file is orphaned")
	}

	// 下一轮把它的文件和记录一起清掉
	if err := s.cleanupExpiredFiles(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(liveFile); !os.IsNotExist(err) {
		t.Errorf("live.txt still on disk after the second cleanup run")
	}
	if recordCount(t, s, livePath) != 0 {
		t.Errorf("record of live.txt still in db after the second cleanup run")
	}
}

// TestCleanupKeepsRecordWhenFileCannotBeRemoved：文件删不掉时记录要留着，下一轮还能再试。
func TestCleanupKeepsRecordWhenFileCannotBeRemoved(t *testing.T) {
	s := newTestServer(t)

	path := putFile(t, s, "/stuck.txt", "a")
	expireNow(t, func(query string, args ...any) error {
		_, err := s.db.Exec(query, args...)
		return err
	}, path)

	// 把文件换成非空目录，os.Remove 会失败
	file := "data/uploads/" + path + "/stuck.txt"
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(file+"/child", 0755); err != nil {
		t.Fatal(err)
	}

	if err := s.cleanupExpiredFiles(); err != nil {
		t.Fatal(err)
	}
	if recordCount(t, s, path) != 1 {
		t.Fatal("record was deleted although its file could not be removed")
	}

	// 障碍消失后，下一轮正常清掉
	if err := os.Remove(file + "/child"); err != nil {
		t.Fatal(err)
	}
	if err := s.cleanupExpiredFiles(); err != nil {
		t.Fatal(err)
	}
	if recordCount(t, s, path) != 0 {
		t.Errorf("record still in db after the file became removable")
	}
}

// TestPanicInHandlerDoesNotKillServer：处理函数里的 panic 只应让这一个请求失败，
// 不能让整个进程退出、掐断其他人正在进行的传输。
func TestPanicInHandlerDoesNotKillServer(t *testing.T) {
	s := newBareTestServer(t)
	s.app.Get("/boom", func(c *fiber.Ctx) error { panic("boom") })
	s.setupRoutes()

	resp, err := s.app.Test(httptest.NewRequest("GET", "/boom", nil), -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 500 {
		t.Errorf("request hitting a panicking handler: status = %d, want 500", resp.StatusCode)
	}

	// 服务仍然可用
	path := putFile(t, s, "/after.txt", "still alive")
	if _, body := getFile(t, s, "/"+path+"/after.txt", nil); body != "still alive" {
		t.Errorf("download after a handler panic = %q, want %q", body, "still alive")
	}
}
