package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
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
