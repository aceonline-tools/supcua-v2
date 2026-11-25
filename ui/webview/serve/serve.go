// Package serve answers the WebView2 resource requests for https://app.local/ out of an
// fs.FS. It is platform-neutral: no COM, no Windows calls.
package serve

import (
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"strings"
	"sync"
)

const (
	Host   = "app.local"
	Origin = "https://" + Host
	// Filter is the WebView2 WebResourceRequested filter covering the whole origin.
	Filter = Origin + "/*"
	Index  = "index.html"
)

// CSP is sent as a response header on every served resource (and mirrored in the page's
// <meta>). No remote origins, no eval, no inline script.
const CSP = "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; font-src 'self'; connect-src 'self'; " +
	"base-uri 'none'; form-action 'none'; frame-ancestors 'none'; object-src 'none'"

var mimeTypes = map[string]string{
	".html":  "text/html; charset=utf-8",
	".htm":   "text/html; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".mjs":   "text/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".json":  "application/json; charset=utf-8",
	".map":   "application/json; charset=utf-8",
	".txt":   "text/plain; charset=utf-8",
	".svg":   "image/svg+xml",
	".png":   "image/png",
	".jpg":   "image/jpeg",
	".jpeg":  "image/jpeg",
	".gif":   "image/gif",
	".webp":  "image/webp",
	".ico":   "image/x-icon",
	".woff":  "font/woff",
	".woff2": "font/woff2",
	".ttf":   "font/ttf",
	".otf":   "font/otf",
	".wasm":  "application/wasm",
}

// MimeType returns the content type for name by extension (case-insensitive).
func MimeType(name string) string {
	if t, ok := mimeTypes[strings.ToLower(path.Ext(name))]; ok {
		return t
	}
	return "application/octet-stream"
}

// AllowNavigation reports whether a top-level navigation may proceed: only the app origin.
func AllowNavigation(uri string) bool {
	_, ok := CleanPath(uri)
	return ok
}

// CleanPath maps a request URL to an fs.FS name. It rejects anything that is not
// https://app.local/<relative path>: other schemes/hosts/ports/userinfo, "..", backslashes,
// NUL, drive letters and encoded variants of those. "/" maps to index.html.
func CleanPath(rawURL string) (string, bool) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" {
		return "", false
	}
	if !strings.EqualFold(u.Hostname(), Host) || u.Port() != "" {
		return "", false
	}
	p := u.Path // already percent-decoded by url.Parse
	if strings.ContainsAny(p, "\\\x00") || strings.Contains(u.EscapedPath(), "%00") {
		return "", false
	}
	if p == "" || p == "/" {
		return Index, true
	}
	if !strings.HasPrefix(p, "/") {
		return "", false
	}
	rel := p[1:]
	for _, seg := range strings.Split(rel, "/") {
		if seg == ".." || seg == "." || strings.Contains(seg, ":") {
			return "", false
		}
	}
	if strings.HasSuffix(rel, "/") {
		rel += Index
	}
	if !fs.ValidPath(rel) {
		return "", false
	}
	return rel, true
}

// Response is a fully built answer, independent of any COM type.
type Response struct {
	Status int
	Reason string
	Header string // CRLF separated "Name: value"
	Body   []byte
}

// Server serves an fs.FS under the app origin.
type Server struct {
	FS fs.FS
	// Logf, when set, receives one line per distinct failing request (404/405, capped), so a
	// missing asset is visible in the host log instead of only in the page console.
	Logf func(format string, args ...any)

	mu     sync.Mutex
	logged map[string]struct{}
}

const maxLoggedMisses = 50

func (s *Server) logMiss(method, rawURL string, status int) {
	if s.Logf == nil {
		return
	}
	key := fmt.Sprintf("%s %s %d", method, rawURL, status)
	s.mu.Lock()
	if s.logged == nil {
		s.logged = map[string]struct{}{}
	}
	_, seen := s.logged[key]
	if seen || len(s.logged) >= maxLoggedMisses {
		s.mu.Unlock()
		return
	}
	s.logged[key] = struct{}{}
	s.mu.Unlock()
	s.Logf("webview: resource %s %s -> %d", method, rawURL, status)
}

// Handle answers one request. Unknown methods get 405, bad URLs 404, unknown paths with no
// file extension fall back to index.html (client-side routes), other missing files 404.
func (s *Server) Handle(method, rawURL string) Response {
	if method != "GET" && method != "HEAD" {
		s.logMiss(method, rawURL, 405)
		return s.plain(405, "Method Not Allowed")
	}
	name, ok := CleanPath(rawURL)
	if !ok {
		s.logMiss(method, rawURL, 404)
		return s.plain(404, "Not Found")
	}
	data, err := fs.ReadFile(s.FS, name)
	if err != nil && path.Ext(name) == "" {
		name = Index
		data, err = fs.ReadFile(s.FS, name)
	}
	if err != nil {
		s.logMiss(method, rawURL, 404)
		return s.plain(404, "Not Found")
	}
	resp := Response{Status: 200, Reason: "OK", Header: headers(MimeType(name)), Body: data}
	if method == "HEAD" {
		resp.Body = nil
	}
	return resp
}

func (s *Server) plain(status int, reason string) Response {
	return Response{Status: status, Reason: reason, Header: headers("text/plain; charset=utf-8"), Body: []byte(reason)}
}

func headers(contentType string) string {
	h := []string{
		"Content-Type: " + contentType,
		"Content-Security-Policy: " + CSP,
		"X-Content-Type-Options: nosniff",
		"Cache-Control: no-store",
		"Cross-Origin-Resource-Policy: same-origin",
		"Referrer-Policy: no-referrer",
		"Permissions-Policy: camera=(), microphone=(), geolocation=(), usb=()",
	}
	return strings.Join(h, "\r\n")
}
