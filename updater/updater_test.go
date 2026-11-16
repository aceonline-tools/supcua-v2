package updater

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseStrict(t *testing.T) {
	cases := []struct {
		in string
		ok bool
		v  [3]int
	}{
		{"1.0.29", true, [3]int{1, 0, 29}},
		{"v1.0.29", true, [3]int{1, 0, 29}},
		{"v10.20.30", true, [3]int{10, 20, 30}},
		{"1.0.29-rc.1", false, [3]int{}},
		{"0.0.0-ci.abc", false, [3]int{}},
		{"1.0", false, [3]int{}},
		{"1.0.0.1", false, [3]int{}},
		{"1.a.0", false, [3]int{}},
		{"1..0", false, [3]int{}},
		{"-1.0.0", false, [3]int{}},
		{"", false, [3]int{}},
		{"dev", false, [3]int{}},
	}
	for _, c := range cases {
		v, ok := parseStrict(c.in)
		if ok != c.ok || (ok && v != c.v) {
			t.Errorf("parseStrict(%q) = %v,%v want %v,%v", c.in, v, ok, c.v, c.ok)
		}
	}
}

func TestParseCurrent(t *testing.T) {
	cases := map[string][3]int{
		"1.0.28":       {1, 0, 28},
		"v1.0.28":      {1, 0, 28},
		"0.0.0":        {0, 0, 0},
		"dev":          {0, 0, 0},
		"":             {0, 0, 0},
		"1.0.29-rc.1":  {1, 0, 29},
		"0.0.0-ci.abc": {0, 0, 0},
		"1.2.3+build":  {1, 2, 3},
	}
	for in, want := range cases {
		if got := parseCurrent(in); got != want {
			t.Errorf("parseCurrent(%q) = %v want %v", in, got, want)
		}
	}
}

func TestGT(t *testing.T) {
	cases := []struct {
		a, b [3]int
		want bool
	}{
		{[3]int{1, 0, 29}, [3]int{1, 0, 28}, true},
		{[3]int{1, 0, 28}, [3]int{1, 0, 28}, false},
		{[3]int{1, 0, 9}, [3]int{1, 0, 10}, false},
		{[3]int{1, 1, 0}, [3]int{1, 0, 99}, true},
		{[3]int{2, 0, 0}, [3]int{1, 99, 99}, true},
		{[3]int{0, 9, 0}, [3]int{1, 0, 0}, false},
	}
	for _, c := range cases {
		if got := gt(c.a, c.b); got != c.want {
			t.Errorf("gt(%v,%v) = %v want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestPickAsset(t *testing.T) {
	if _, ok := pickAsset(nil); ok {
		t.Fatal("empty assets matched")
	}
	if _, ok := pickAsset([]asset{{Name: "supcua-v2.exe.sha256", BrowserDownloadURL: "x"}, {Name: "other.exe", BrowserDownloadURL: "y"}}); ok {
		t.Fatal("non-exact name matched")
	}
	if _, ok := pickAsset([]asset{{Name: "supcua-v2.exe"}}); ok {
		t.Fatal("asset without url matched")
	}
	a, ok := pickAsset([]asset{{Name: "notes.txt", BrowserDownloadURL: "n"}, {Name: "supcua-v2.exe", BrowserDownloadURL: "u", Size: 5}})
	if !ok || a.BrowserDownloadURL != "u" || a.Size != 5 {
		t.Fatalf("got %+v %v", a, ok)
	}
}

func fakeAPI(t *testing.T, h http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(h)
	old := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { srv.Close(); apiBase = old })
}

func releaseJSON(tag string, draft, pre bool, assets string) string {
	return fmt.Sprintf(`{"tag_name":%q,"draft":%t,"prerelease":%t,"assets":[%s]}`, tag, draft, pre, assets)
}

const goodAsset = `{"name":"supcua-v2.exe","browser_download_url":"https://example.test/supcua-v2.exe","size":10}`

func serveJSON(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}
}

func TestCheckUpdate_HappyPathAndHeaders(t *testing.T) {
	var got *http.Request
	fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		got = r
		serveJSON(releaseJSON("v1.0.29", false, false, goodAsset))(w, r)
	})
	v, u, err := CheckUpdate("1.0.28")
	if err != nil || v != "1.0.29" || u != "https://example.test/supcua-v2.exe" {
		t.Fatalf("got %q %q %v", v, u, err)
	}
	if got.URL.Path != "/repos/aceonline-tools/supcua-v2/releases/latest" {
		t.Errorf("path %s", got.URL.Path)
	}
	if got.Header.Get("Accept") != "application/vnd.github+json" ||
		got.Header.Get("X-GitHub-Api-Version") == "" || got.Header.Get("User-Agent") == "" {
		t.Errorf("headers %v", got.Header)
	}
}

func TestCheckUpdate_NoUpdate(t *testing.T) {
	cases := []struct {
		name, current, body string
	}{
		{"same version", "1.0.29", releaseJSON("v1.0.29", false, false, goodAsset)},
		{"older release", "1.0.30", releaseJSON("v1.0.29", false, false, goodAsset)},
		{"draft", "1.0.0", releaseJSON("v1.0.29", true, false, goodAsset)},
		{"prerelease flag", "1.0.0", releaseJSON("v1.0.29", false, true, goodAsset)},
		{"prerelease tag", "1.0.0", releaseJSON("v1.0.29-rc.1", false, false, goodAsset)},
		{"ci tag", "1.0.0", releaseJSON("v0.0.0-ci.abc", false, false, goodAsset)},
		{"non semver tag", "1.0.0", releaseJSON("nightly", false, false, goodAsset)},
		{"rc current same core", "1.0.29-rc.1", releaseJSON("v1.0.29", false, false, goodAsset)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeAPI(t, serveJSON(c.body))
			v, u, err := CheckUpdate(c.current)
			if err != nil || v != "" || u != "" {
				t.Fatalf("got %q %q %v", v, u, err)
			}
		})
	}
}

func TestCheckUpdate_DevBuildGetsUpdate(t *testing.T) {
	for _, cur := range []string{"0.0.0", "dev", "0.0.0-ci.abc"} {
		fakeAPI(t, serveJSON(releaseJSON("v1.0.1", false, false, goodAsset)))
		v, _, err := CheckUpdate(cur)
		if err != nil || v != "1.0.1" {
			t.Errorf("cur %q: got %q %v", cur, v, err)
		}
	}
}

func TestCheckUpdate_NotFound(t *testing.T) {
	fakeAPI(t, func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	v, u, err := CheckUpdate("1.0.0")
	if err != nil || v != "" || u != "" {
		t.Fatalf("got %q %q %v", v, u, err)
	}
}

func TestCheckUpdate_RateLimited(t *testing.T) {
	cases := []struct {
		name   string
		status int
		hdr    map[string]string
	}{
		{"403 remaining 0", 403, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "1700000000"}},
		{"403 retry-after", 403, map[string]string{"Retry-After": "60"}},
		{"429", 429, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var hits int32
			fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&hits, 1)
				for k, v := range c.hdr {
					w.Header().Set(k, v)
				}
				w.WriteHeader(c.status)
			})
			_, _, err := CheckUpdate("1.0.0")
			if !errors.Is(err, ErrRateLimited) {
				t.Fatalf("err = %v", err)
			}
			if hits != 1 {
				t.Errorf("hits = %d, want 1 (no retries)", hits)
			}
		})
	}
}

func TestCheckUpdate_Forbidden_NotRateLimit(t *testing.T) {
	fakeAPI(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) })
	_, _, err := CheckUpdate("1.0.0")
	if err == nil || errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v", err)
	}
}

func TestCheckUpdate_ServerError(t *testing.T) {
	fakeAPI(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	if _, _, err := CheckUpdate("1.0.0"); err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("err = %v", err)
	}
}

func TestCheckUpdate_MalformedJSON(t *testing.T) {
	for _, body := range []string{`{not json`, ``, `[]`} {
		fakeAPI(t, serveJSON(body))
		if _, _, err := CheckUpdate("1.0.0"); err == nil {
			t.Errorf("body %q: expected error", body)
		}
	}
}

func TestCheckUpdate_MissingAsset(t *testing.T) {
	for _, assets := range []string{``, `{"name":"other.zip","browser_download_url":"u","size":1}`} {
		fakeAPI(t, serveJSON(releaseJSON("v1.0.29", false, false, assets)))
		_, _, err := CheckUpdate("1.0.28")
		if !errors.Is(err, ErrNoAsset) {
			t.Errorf("assets %q: err = %v", assets, err)
		}
	}
}

func TestCheckUpdate_NoNetwork(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	old := apiBase
	apiBase = url
	t.Cleanup(func() { apiBase = old })
	if _, _, err := CheckUpdate("1.0.0"); err == nil {
		t.Fatal("expected error")
	}
}

func TestCheckUpdate_Timeout(t *testing.T) {
	done := make(chan struct{})
	fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	})
	oldTO := checkTO
	checkTO = 50 * time.Millisecond
	t.Cleanup(func() { checkTO = oldTO; close(done) })
	if _, _, err := CheckUpdate("1.0.0"); err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestDownloadTo_Success(t *testing.T) {
	payload := strings.Repeat("MZ", 4096)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, payload)
	}))
	defer srv.Close()
	dst := filepath.Join(t.TempDir(), "u.exe")
	if err := downloadTo(srv.URL, dst); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(dst)
	if string(b) != payload {
		t.Fatalf("content mismatch, len %d", len(b))
	}
}

func TestDownloadTo_Truncated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(200)
		w.Write([]byte("short"))
		conn, _, _ := w.(http.Hijacker).Hijack()
		conn.Close()
	}))
	defer srv.Close()
	dst := filepath.Join(t.TempDir(), "u.exe")
	if err := downloadTo(srv.URL, dst); err == nil {
		t.Fatal("expected error")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatal("partial file not removed")
	}
}

func TestDownloadTo_BadStatus(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	dst := filepath.Join(t.TempDir(), "u.exe")
	if err := downloadTo(srv.URL, dst); err == nil {
		t.Fatal("expected error")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatal("file created on bad status")
	}
}
