package main

import "testing"

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
