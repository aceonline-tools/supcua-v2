package serve

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
)

func TestCleanPath(t *testing.T) {
	good := map[string]string{
		"https://app.local/":                  "index.html",
		"https://app.local":                   "index.html",
		"https://APP.local/index.html":        "index.html",
		"https://app.local/assets/a-1.js":     "assets/a-1.js",
		"https://app.local/assets/a.js?x=1#h": "assets/a.js",
		"https://app.local/sub/":              "sub/index.html",
		"https://app.local/a%20b.txt":         "a b.txt",
	}
	for in, want := range good {
		got, ok := CleanPath(in)
		if !ok || got != want {
			t.Errorf("CleanPath(%q) = %q,%v want %q", in, got, ok, want)
		}
	}
	bad := []string{
		"https://app.local/../secret",
		"https://app.local/a/../../secret",
		"https://app.local/%2e%2e/secret",
		"https://app.local/%2E%2E%2Fsecret",
		"https://app.local/a/%2e%2e/b",
		"https://app.local/./x",
		"https://app.local/a\\b",
		"https://app.local/%5Cwindows",
		"https://app.local/C:/windows/win.ini",
		"https://app.local/c%3A/x",
		"https://app.local/a%00b",
		"https://app.local@evil.com/",
		"https://evil.com/",
		"https://app.local.evil.com/",
		"https://app.local:8443/",
		"http://app.local/",
		"file:///C:/x",
		"about:blank",
		"//app.local/x",
		"/index.html",
		"",
		"https://user:pw@app.local/",
	}
	for _, in := range bad {
		if got, ok := CleanPath(in); ok {
			t.Errorf("CleanPath(%q) accepted as %q", in, got)
		}
	}
}

func TestAllowNavigation(t *testing.T) {
	if !AllowNavigation("https://app.local/settings") {
		t.Fatal("app origin must be allowed")
	}
	for _, u := range []string{"https://example.com/", "http://app.local/", "javascript:alert(1)", "data:text/html,x", "ms-appx://x"} {
		if AllowNavigation(u) {
			t.Errorf("navigation to %q must be blocked", u)
		}
	}
}

func TestMimeType(t *testing.T) {
	cases := map[string]string{
		"a.html": "text/html; charset=utf-8", "A.JS": "text/javascript; charset=utf-8",
		"x.css": "text/css; charset=utf-8", "m.json": "application/json; charset=utf-8",
		"i.svg": "image/svg+xml", "f.woff2": "font/woff2", "w.wasm": "application/wasm",
		"noext": "application/octet-stream", "x.bin": "application/octet-stream",
	}
	for name, want := range cases {
		if got := MimeType(name); got != want {
			t.Errorf("MimeType(%q) = %q want %q", name, got, want)
		}
	}
}

func TestCSP(t *testing.T) {
	for _, must := range []string{"default-src 'none'", "script-src 'self'", "frame-ancestors 'none'", "base-uri 'none'", "object-src 'none'"} {
		if !strings.Contains(CSP, must) {
			t.Errorf("CSP lacks %q", must)
		}
	}
	for _, mustNot := range []string{"unsafe-eval", "http:", "https:", "*", "script-src 'self' 'unsafe-inline'"} {
		if strings.Contains(CSP, mustNot) {
			t.Errorf("CSP must not contain %q", mustNot)
		}
	}
}

func TestHandle(t *testing.T) {
	s := &Server{FS: fstest.MapFS{
		"index.html":    {Data: []byte("<html>")},
		"assets/app.js": {Data: []byte("1")},
	}}
	r := s.Handle("GET", "https://app.local/assets/app.js")
	if r.Status != 200 || string(r.Body) != "1" {
		t.Fatalf("js: %+v", r)
	}
	for _, h := range []string{"Content-Type: text/javascript", "Content-Security-Policy: " + CSP, "X-Content-Type-Options: nosniff", "Cache-Control: no-store"} {
		if !strings.Contains(r.Header, h) {
			t.Errorf("header lacks %q:\n%s", h, r.Header)
		}
	}
	if r := s.Handle("GET", "https://app.local/settings/profile"); r.Status != 200 || string(r.Body) != "<html>" {
		t.Errorf("SPA fallback: %+v", r)
	}
	if r := s.Handle("GET", "https://app.local/missing.js"); r.Status != 404 {
		t.Errorf("missing file with extension must 404: %+v", r)
	}
	if r := s.Handle("GET", "https://app.local/../x"); r.Status != 404 {
		t.Errorf("traversal must 404: %+v", r)
	}
	if r := s.Handle("POST", "https://app.local/"); r.Status != 405 {
		t.Errorf("POST must 405: %+v", r)
	}
	if r := s.Handle("HEAD", "https://app.local/"); r.Status != 200 || r.Body != nil {
		t.Errorf("HEAD: %+v", r)
	}
	if r := s.Handle("GET", "https://evil.com/"); r.Status != 404 {
		t.Errorf("foreign host must 404: %+v", r)
	}
}

func TestMissesAreLoggedOnceEachAndBounded(t *testing.T) {
	var lines []string
	s := &Server{FS: fstest.MapFS{"index.html": {Data: []byte("x")}}, Logf: func(f string, a ...any) { lines = append(lines, fmt.Sprintf(f, a...)) }}
	for i := 0; i < 3; i++ {
		s.Handle("GET", "https://app.local/assets/missing.js")
	}
	s.Handle("POST", "https://app.local/")
	s.Handle("GET", "https://app.local/") // success: not logged
	if len(lines) != 2 || !strings.Contains(lines[0], "GET https://app.local/assets/missing.js -> 404") || !strings.Contains(lines[1], "-> 405") {
		t.Fatalf("lines = %q", lines)
	}
	for i := 0; i < 200; i++ {
		s.Handle("GET", fmt.Sprintf("https://app.local/a%d.js", i))
	}
	if len(lines) > maxLoggedMisses {
		t.Fatalf("%d lines, cap %d", len(lines), maxLoggedMisses)
	}
}
