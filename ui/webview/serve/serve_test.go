package serve

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
)

func TestCleanPath(t *testing.T) {
	good := map[string]string{
		"https://app.invalid/":                  "index.html",
		"https://app.invalid":                   "index.html",
		"https://APP.invalid/index.html":        "index.html",
		"https://app.invalid/assets/a-1.js":     "assets/a-1.js",
		"https://app.invalid/assets/a.js?x=1#h": "assets/a.js",
		"https://app.invalid/sub/":              "sub/index.html",
		"https://app.invalid/a%20b.txt":         "a b.txt",
	}
	for in, want := range good {
		got, ok := CleanPath(in)
		if !ok || got != want {
			t.Errorf("CleanPath(%q) = %q,%v want %q", in, got, ok, want)
		}
	}
	bad := []string{
		"https://app.invalid/../secret",
		"https://app.invalid/a/../../secret",
		"https://app.invalid/%2e%2e/secret",
		"https://app.invalid/%2E%2E%2Fsecret",
		"https://app.invalid/a/%2e%2e/b",
		"https://app.invalid/./x",
		"https://app.invalid/a\\b",
		"https://app.invalid/%5Cwindows",
		"https://app.invalid/C:/windows/win.ini",
		"https://app.invalid/c%3A/x",
		"https://app.invalid/a%00b",
		"https://app.invalid@evil.com/",
		"https://evil.com/",
		"https://app.invalid.evil.com/",
		"https://app.invalid:8443/",
		"http://app.invalid/",
		"file:///C:/x",
		"about:blank",
		"//app.invalid/x",
		"/index.html",
		"",
		"https://user:pw@app.invalid/",
	}
	for _, in := range bad {
		if got, ok := CleanPath(in); ok {
			t.Errorf("CleanPath(%q) accepted as %q", in, got)
		}
	}
}

func TestAllowNavigation(t *testing.T) {
	if !AllowNavigation("https://app.invalid/settings") {
		t.Fatal("app origin must be allowed")
	}
	for _, u := range []string{"https://example.com/", "http://app.invalid/", "javascript:alert(1)", "data:text/html,x", "ms-appx://x"} {
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
	r := s.Handle("GET", "https://app.invalid/assets/app.js")
	if r.Status != 200 || string(r.Body) != "1" {
		t.Fatalf("js: %+v", r)
	}
	for _, h := range []string{"Content-Type: text/javascript", "Content-Security-Policy: " + CSP, "X-Content-Type-Options: nosniff", "Cache-Control: no-store"} {
		if !strings.Contains(r.Header, h) {
			t.Errorf("header lacks %q:\n%s", h, r.Header)
		}
	}
	if r := s.Handle("GET", "https://app.invalid/settings/profile"); r.Status != 200 || string(r.Body) != "<html>" {
		t.Errorf("SPA fallback: %+v", r)
	}
	if r := s.Handle("GET", "https://app.invalid/missing.js"); r.Status != 404 {
		t.Errorf("missing file with extension must 404: %+v", r)
	}
	if r := s.Handle("GET", "https://app.invalid/../x"); r.Status != 404 {
		t.Errorf("traversal must 404: %+v", r)
	}
	if r := s.Handle("POST", "https://app.invalid/"); r.Status != 405 {
		t.Errorf("POST must 405: %+v", r)
	}
	if r := s.Handle("HEAD", "https://app.invalid/"); r.Status != 200 || r.Body != nil {
		t.Errorf("HEAD: %+v", r)
	}
	if r := s.Handle("GET", "https://evil.com/"); r.Status != 404 {
		t.Errorf("foreign host must 404: %+v", r)
	}
}

func TestLoggedURLsAreSanitisedAndCapped(t *testing.T) {
	var lines []string
	s := &Server{FS: fstest.MapFS{}, Logf: func(f string, a ...any) { lines = append(lines, fmt.Sprintf(f, a...)) }}
	s.Handle("GET", "https://app.invalid/x\r\nforged: line"+strings.Repeat("a", 5000))
	if len(lines) != 1 || strings.ContainsAny(lines[0], "\r\n") || len(lines[0]) > maxLoggedURL+80 {
		t.Fatalf("%d lines: %q", len(lines), lines)
	}
	for k := range s.logged {
		if len(k) > maxLoggedURL+40 {
			t.Fatalf("retained key too long: %d", len(k))
		}
	}
}

func TestMissesAreLoggedOnceEachAndBounded(t *testing.T) {
	var lines []string
	s := &Server{FS: fstest.MapFS{"index.html": {Data: []byte("x")}}, Logf: func(f string, a ...any) { lines = append(lines, fmt.Sprintf(f, a...)) }}
	for i := 0; i < 3; i++ {
		s.Handle("GET", "https://app.invalid/assets/missing.js")
	}
	s.Handle("POST", "https://app.invalid/")
	s.Handle("GET", "https://app.invalid/") // success: not logged
	if len(lines) != 2 || !strings.Contains(lines[0], "GET https://app.invalid/assets/missing.js -> 404") || !strings.Contains(lines[1], "-> 405") {
		t.Fatalf("lines = %q", lines)
	}
	for i := 0; i < 200; i++ {
		s.Handle("GET", fmt.Sprintf("https://app.invalid/a%d.js", i))
	}
	if len(lines) > maxLoggedMisses {
		t.Fatalf("%d lines, cap %d", len(lines), maxLoggedMisses)
	}
}

func TestHostIsNotMDNS(t *testing.T) {
	if strings.HasSuffix(strings.ToLower(Host), ".local") {
		t.Fatalf("Host %q is under .local: Windows resolves it via mDNS and stalls navigation ~2s", Host)
	}
	if Origin != "https://"+Host || Filter != Origin+"/*" {
		t.Fatalf("Origin/Filter not derived from Host: %q %q", Origin, Filter)
	}
}
