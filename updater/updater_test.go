package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
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
		{"v01.2.3", false, [3]int{}},
		{"v1.02.3", false, [3]int{}},
		{"1.2.03", false, [3]int{}},
		{"0.0.0", true, [3]int{}},
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

const goodAsset = `{"name":"supcua-v2.exe","browser_download_url":"https://github.com/aceonline-tools/supcua-v2/releases/download/v1.0.29/supcua-v2.exe","size":10}`

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
	if err != nil || v != "1.0.29" || u != "https://github.com/aceonline-tools/supcua-v2/releases/download/v1.0.29/supcua-v2.exe" {
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
	if !errors.Is(err, ErrNoRelease) || v != "" || u != "" {
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

func tlsServer(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	oi, or, ot := initialAllowed, redirectAllowed, transport
	initialAllowed = func(string) bool { return true }
	redirectAllowed = func(u *url.URL) bool { return u.Host == srv.Listener.Addr().String() }
	transport = srv.Client().Transport
	t.Cleanup(func() { srv.Close(); initialAllowed, redirectAllowed, transport = oi, or, ot })
	return srv
}

func mz(n int) string { return "MZ" + strings.Repeat("x", n-2) }

func dl(t *testing.T, url string) (string, error) {
	dst := filepath.Join(t.TempDir(), "u.exe")
	err := downloadTo(url, dst)
	if err != nil {
		if _, serr := os.Stat(dst); !os.IsNotExist(serr) {
			t.Error("partial file not removed")
		}
	}
	return dst, err
}

func TestDownloadTo_Success(t *testing.T) {
	payload := mz(8192)
	srv := tlsServer(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, payload) })
	dst, err := dl(t, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dst); string(b) != payload {
		t.Fatal("content mismatch")
	}
}

func TestDownloadTo_SizeAndDigest(t *testing.T) {
	payload := mz(1000)
	sum := sha256.Sum256([]byte(payload))
	good := "sha256:" + hex.EncodeToString(sum[:])
	srv := tlsServer(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, payload) })
	cases := []struct {
		name string
		a    asset
		ok   bool
	}{
		{"match", asset{Size: 1000, Digest: good}, true},
		{"size only", asset{Size: 1000}, true},
		{"size smaller than body", asset{Size: 900}, false},
		{"size larger than body", asset{Size: 1100}, false},
		{"digest mismatch", asset{Size: 1000, Digest: "sha256:" + strings.Repeat("0", 64)}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			expected.Store(srv.URL, c.a)
			t.Cleanup(func() { expected.Delete(srv.URL) })
			if _, err := dl(t, srv.URL); (err == nil) != c.ok {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestDownloadTo_ChunkedUnknownLength(t *testing.T) {
	srv := tlsServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("MZ"))
		w.(http.Flusher).Flush()
		w.Write([]byte("rest"))
	})
	if _, err := dl(t, srv.URL); err != nil {
		t.Fatal(err)
	}
	expected.Store(srv.URL, asset{Size: 10})
	defer expected.Delete(srv.URL)
	if _, err := dl(t, srv.URL); err == nil {
		t.Fatal("size mismatch accepted")
	}
}

func TestDownloadTo_Oversized(t *testing.T) {
	srv := tlsServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("MZ"))
		w.(http.Flusher).Flush()
		buf := make([]byte, 1<<20)
		for i := 0; i < 205; i++ {
			if _, err := w.Write(buf); err != nil {
				return
			}
		}
	})
	if _, err := dl(t, srv.URL); err == nil {
		t.Fatal("expected cap error")
	}
}

func TestDownloadTo_NonMZ(t *testing.T) {
	srv := tlsServer(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "<html>not an exe</html>") })
	if _, err := dl(t, srv.URL); err == nil {
		t.Fatal("expected MZ error")
	}
}

func TestDownloadTo_Truncated(t *testing.T) {
	srv := tlsServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(200)
		w.Write([]byte("MZshort"))
		conn, _, _ := w.(http.Hijacker).Hijack()
		conn.Close()
	})
	if _, err := dl(t, srv.URL); err == nil {
		t.Fatal("expected error")
	}
}

func TestDownloadTo_BadStatus(t *testing.T) {
	srv := tlsServer(t, func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	if _, err := dl(t, srv.URL); err == nil {
		t.Fatal("expected error")
	}
}

func TestDownloadTo_Redirects(t *testing.T) {
	evil := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, mz(10)) }))
	defer evil.Close()
	var hops int32
	srv := tlsServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/evil":
			http.Redirect(w, r, evil.URL, 302)
		case "/loop":
			atomic.AddInt32(&hops, 1)
			http.Redirect(w, r, "/loop", 302)
		case "/ok":
			http.Redirect(w, r, "/final", 302)
		default:
			fmt.Fprint(w, mz(10))
		}
	})
	if _, err := dl(t, srv.URL+"/ok"); err != nil {
		t.Fatalf("allowed redirect: %v", err)
	}
	if _, err := dl(t, srv.URL+"/evil"); !errors.Is(err, ErrUntrustedURL) {
		t.Fatalf("evil redirect: %v", err)
	}
	if _, err := dl(t, srv.URL+"/loop"); err == nil || hops > maxRedirects+1 {
		t.Fatalf("loop: %v hops %d", err, hops)
	}
}

func TestDefaultURLTrust(t *testing.T) {
	p := "https://github.com/aceonline-tools/supcua-v2/releases/download/v1.0.29/supcua-v2.exe"
	good := []string{p}
	bad := []string{
		"http://github.com/aceonline-tools/supcua-v2/releases/download/v1/supcua-v2.exe",
		"https://evil.com/aceonline-tools/supcua-v2/releases/download/v1/supcua-v2.exe",
		"https://github.com.evil.com/aceonline-tools/supcua-v2/releases/download/v1/x",
		"https://github.com/other/repo/releases/download/v1/supcua-v2.exe",
		"https://github.com/aceonline-tools/supcua-v2/../x",
		"https://github.com/aceonline-tools/supcua-v2/releases/download/v1/../../../../evil/x",
		"https://github.com/aceonline-tools/supcua-v2/releases/download//v1/x",
		"", "::bad",
	}
	for _, u := range good {
		if !defaultInitialAllowed(u) {
			t.Errorf("rejected %s", u)
		}
	}
	for _, u := range bad {
		if defaultInitialAllowed(u) {
			t.Errorf("accepted %s", u)
		}
	}
	for u, want := range map[string]bool{
		"https://objects.githubusercontent.com/x": true,
		"https://github.com/x":                    true,
		"http://objects.githubusercontent.com/x":  false,
		"https://githubusercontent.com.evil.com/": false,
		"https://evilgithubusercontent.com/":      false,
	} {
		pu, _ := url.Parse(u)
		if defaultRedirectAllowed(pu) != want {
			t.Errorf("redirect %s want %v", u, want)
		}
	}
}

func TestCheckUpdate_UntrustedAssetURL(t *testing.T) {
	a := `{"name":"supcua-v2.exe","browser_download_url":"https://evil.com/supcua-v2.exe","size":10}`
	fakeAPI(t, serveJSON(releaseJSON("v1.0.29", false, false, a)))
	if _, _, err := CheckUpdate("1.0.0"); !errors.Is(err, ErrUntrustedURL) {
		t.Fatalf("err = %v", err)
	}
}

func TestCheckUpdate_TrailingData(t *testing.T) {
	fakeAPI(t, serveJSON(releaseJSON("v1.0.29", false, false, goodAsset)+` {"x":1}`))
	if _, _, err := CheckUpdate("1.0.0"); err == nil {
		t.Fatal("trailing data accepted")
	}
}

func TestCheckUpdate_StoresAssetMetadata(t *testing.T) {
	a := `{"name":"supcua-v2.exe","browser_download_url":"https://github.com/aceonline-tools/supcua-v2/releases/download/v1.0.29/supcua-v2.exe","size":42,"digest":"sha256:ab"}`
	fakeAPI(t, serveJSON(releaseJSON("v1.0.29", false, false, a)))
	_, u, err := CheckUpdate("1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	v, ok := expected.Load(u)
	if !ok || v.(asset).Size != 42 || v.(asset).Digest != "sha256:ab" {
		t.Fatalf("%v %v", v, ok)
	}
}

func TestDownload_UnpredictableExclusiveNameAndRecordedHash(t *testing.T) {
	payload := mz(4096)
	srv := tlsServer(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, payload) })
	dir := t.TempDir()
	exe := filepath.Join(dir, "supcua-v2.exe")
	os.WriteFile(exe, []byte("MZcurrent"), 0o755)
	d1, err := Download(srv.URL, exe)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := Download(srv.URL, exe)
	if err != nil {
		t.Fatal(err)
	}
	if d1.Path == d2.Path || filepath.Dir(d1.Path) != dir || !strings.HasPrefix(filepath.Base(d1.Path), UpdateTempPrefix) {
		t.Fatalf("names must be random and next to the exe: %q %q", d1.Path, d2.Path)
	}
	if _, serr := os.Stat(d1.Path); serr != nil {
		t.Fatal("a fresh earlier download must not be swept by a later Download")
	}
	sum := sha256.Sum256([]byte(payload))
	if d2.Size != int64(len(payload)) || d2.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("recorded size/hash wrong: %+v", d2)
	}
	if b, _ := os.ReadFile(d2.Path); string(b) != payload {
		t.Fatal("content mismatch")
	}
	if b, _ := os.ReadFile(exe); string(b) != "MZcurrent" {
		t.Fatal("Download must not modify the running executable")
	}
}

func TestDownloadTo_RefusesExistingFile(t *testing.T) {
	srv := tlsServer(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, mz(512)) })
	dst := filepath.Join(t.TempDir(), "planted.exe")
	os.WriteFile(dst, []byte("attacker"), 0o600)
	if err := downloadTo(srv.URL, dst); err == nil {
		t.Fatal("O_EXCL: an existing (pre-planted) file must not be reused")
	}
	if b, _ := os.ReadFile(dst); string(b) != "attacker" {
		t.Fatal("planted file must be left alone")
	}
}

func TestDownload_RejectsUntrustedAndNonPE(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "supcua-v2.exe")
	if _, err := Download("https://evil.example/x.exe", exe); !errors.Is(err, ErrUntrustedURL) {
		t.Fatalf("untrusted URL: %v", err)
	}
	srv := tlsServer(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "<html>not an exe</html>") })
	if _, err := Download(srv.URL, exe); err == nil {
		t.Fatal("non-PE download must fail")
	}
	if m, _ := filepath.Glob(filepath.Join(dir, UpdateTempPrefix+"*")); len(m) != 0 {
		t.Fatalf("failed download must leave no temp file: %v", m)
	}
}

func downloaded(t *testing.T) (Downloaded, string) {
	t.Helper()
	srv := tlsServer(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, mz(2048)) })
	exe := filepath.Join(t.TempDir(), "supcua-v2.exe")
	os.WriteFile(exe, []byte("MZcurrent"), 0o755)
	d, err := Download(srv.URL, exe)
	if err != nil {
		t.Fatal(err)
	}
	return d, exe
}

func downloadedIn(t *testing.T, exe string) Downloaded {
	t.Helper()
	srv := tlsServer(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, mz(2048)) })
	d, err := Download(srv.URL, exe)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestReplace_RefusesTamperedDownloadBeforeTouchingExe(t *testing.T) {
	for name, mutate := range map[string]func(string){
		"same size, other bytes": func(p string) { os.WriteFile(p, []byte(mz(2048)[:2]+strings.Repeat("y", 2046)), 0o600) },
		"truncated":              func(p string) { os.WriteFile(p, []byte("MZ"), 0o600) },
		"not PE":                 func(p string) { os.WriteFile(p, []byte(strings.Repeat("z", 2048)), 0o600) },
	} {
		t.Run(name, func(t *testing.T) {
			d, exe := downloaded(t)
			mutate(d.Path)
			if err := Replace(d, exe); !errors.Is(err, ErrUpdateChanged) {
				t.Fatalf("want ErrUpdateChanged, got %v", err)
			}
			if b, _ := os.ReadFile(exe); string(b) != "MZcurrent" {
				t.Fatal("running executable must be untouched")
			}
			if _, err := os.Stat(exe + ".old"); !os.IsNotExist(err) {
				t.Fatal("no rename may happen before verification")
			}
			if _, err := os.Stat(d.Path); !os.IsNotExist(err) {
				t.Fatal("tampered file must be deleted")
			}
		})
	}
}

func TestReplace_RejectedPathIsNotDeleted(t *testing.T) {
	d, exe := downloaded(t)
	other := filepath.Join(t.TempDir(), filepath.Base(d.Path))
	os.WriteFile(other, []byte(mz(2048)), 0o600)
	if err := Replace(Downloaded{Path: other, Size: d.Size, SHA256: d.SHA256}, exe); !errors.Is(err, ErrUpdatePath) {
		t.Fatalf("outside dir: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("a path rejected for its directory must not be deleted")
	}
	renamed := filepath.Join(filepath.Dir(exe), "evil.exe")
	os.Rename(d.Path, renamed)
	if err := Replace(Downloaded{Path: renamed, Size: d.Size, SHA256: d.SHA256}, exe); !errors.Is(err, ErrUpdatePath) {
		t.Fatalf("wrong name: %v", err)
	}
	if _, err := os.Stat(renamed); err != nil {
		t.Fatal("a path rejected for its name must not be deleted")
	}
	if b, _ := os.ReadFile(exe); string(b) != "MZcurrent" {
		t.Fatal("running executable must be untouched")
	}
}

func TestSweepStale_OnlyOldRandomNamesAreDeleted(t *testing.T) {
	dir := t.TempDir()
	mk := func(name string, age time.Duration) string {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte("x"), 0o600)
		old := time.Now().Add(-age)
		os.Chtimes(p, old, old)
		return p
	}
	oldRandom := mk(UpdateTempPrefix+"0123456789abcdef.exe", 48*time.Hour)
	freshRandom := mk(UpdateTempPrefix+"fedcba9876543210.exe", time.Hour)
	oldLegacy := mk("supcua-v2-update.exe", 48*time.Hour)
	oldOther := mk("notes-update-0123456789abcdef.exe", 48*time.Hour)
	oldBadHex := mk(UpdateTempPrefix+"zzzzzzzzzzzzzzzz.exe", 48*time.Hour)
	sweepStale(dir, time.Now())
	if _, err := os.Stat(oldRandom); !os.IsNotExist(err) {
		t.Error("an old file with the random pattern must be swept")
	}
	for name, p := range map[string]string{"fresh": freshRandom, "legacy name": oldLegacy, "other name": oldOther, "bad hex": oldBadHex} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s file must be left alone", name)
		}
	}
}

func TestDownload_DoesNotDeleteAFreshFileOfAnotherInstance(t *testing.T) {
	srv := tlsServer(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, mz(1024)) })
	dir := t.TempDir()
	exe := filepath.Join(dir, "supcua-v2.exe")
	d1, err := Download(srv.URL, exe)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := Download(srv.URL, exe)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []Downloaded{d1, d2} {
		if _, err := os.Stat(d.Path); err != nil {
			t.Fatalf("fresh download %s must survive another Download: %v", d.Path, err)
		}
	}
}

func TestBuildBatch_FailedMoveRestoresOldAndNeverDeletesIt(t *testing.T) {
	b, err := buildBatch(42, "new.exe", "app.exe", "app.exe.old")
	if err != nil {
		t.Fatal(err)
	}
	move := strings.Index(b, `move /y "new.exe" "app.exe"`)
	fail := strings.Index(b, "if errorlevel 1 (")
	restore := strings.Index(b, `move /y "app.exe.old" "app.exe"`)
	closeFail := strings.Index(b, "exit /b 1")
	delOld := strings.Index(b, `del "app.exe.old"`)
	if !(move >= 0 && fail > move && restore > fail && closeFail > restore && delOld > closeFail) {
		t.Fatalf("unexpected script:\n%s", b)
	}
	if strings.Count(b, `del "app.exe.old"`) != 1 {
		t.Fatal(".old may only be deleted once, after the success path")
	}
}

func TestReplace_LaunchFailureRestoresExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("would launch the real batch script")
	}
	d, exe := downloaded(t)
	if err := Replace(d, exe); err == nil { // no cmd.exe off Windows: launch fails after verification
		t.Fatal("expected launch failure")
	}
	if b, _ := os.ReadFile(exe); string(b) != "MZcurrent" {
		t.Fatalf("executable not restored: %q", b)
	}
	if _, err := os.Stat(exe + ".old"); !os.IsNotExist(err) {
		t.Fatal(".old must not remain after a restore")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(exe), "update.bat")); !os.IsNotExist(err) {
		t.Fatal("update.bat must be removed after a failed launch")
	}
}

func TestDiscardRemovesFile(t *testing.T) {
	d, _ := downloaded(t)
	Discard(d)
	if _, err := os.Stat(d.Path); !os.IsNotExist(err) {
		t.Fatal("Discard must delete the download")
	}
}

func TestBuildBatch_EmbedsHostilePathsLiterally(t *testing.T) {
	dir := `C:\Users\A B\%TEMP%\!x!\R&D (1)^`
	b, err := buildBatch(7, dir+`\.supcua-update-1.exe`, dir+`\app.exe`, dir+`\app.exe.old`)
	if err != nil {
		t.Fatal(err)
	}
	esc := `C:\Users\A B\%%TEMP%%\!x!\R&D (1)^`
	want := strings.Join([]string{
		"@echo off",
		"setlocal DisableDelayedExpansion",
		"taskkill /pid 7 /f >nul 2>&1",
		"timeout /t 2 /nobreak >nul",
		`move /y "` + esc + `\.supcua-update-1.exe" "` + esc + `\app.exe"`,
		"if errorlevel 1 (",
		`  move /y "` + esc + `\app.exe.old" "` + esc + `\app.exe"`,
		`  start "" "` + esc + `\app.exe"`,
		`  del "%~f0"`,
		"  exit /b 1",
		")",
		`start "" "` + esc + `\app.exe"`,
		"timeout /t 3 /nobreak >nul",
		`del "` + esc + `\app.exe.old"`,
		`del "%~f0"`,
	}, "\r\n") + "\r\n"
	if b != want {
		t.Fatalf("got:\n%s\nwant:\n%s", b, want)
	}
	if strings.Contains(b, "EnableDelayedExpansion") || strings.Contains(strings.ReplaceAll(b, "%%", ""), "%TEMP") {
		t.Fatal("paths must not be expandable")
	}
}

func TestBuildBatch_RefusesUnquotablePaths(t *testing.T) {
	for _, bad := range []string{`C:\a"b\app.exe`, "C:\\a\nb\\app.exe", "C:\\a\rb\\app.exe", "C:\\a\x00b\\app.exe", ""} {
		if _, err := buildBatch(1, `C:\x\new.exe`, bad, `C:\x\old`); !errors.Is(err, ErrUpdateBatchPath) {
			t.Errorf("%q: want ErrUpdateBatchPath, got %v", bad, err)
		}
	}
}

func TestReplace_UnquotablePathRefusedBeforeTouchingExe(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, `bad"dir`)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Skip("filesystem rejects the name")
	}
	exe := filepath.Join(dir, "app.exe")
	if err := os.WriteFile(exe, []byte("MZcurrent"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := downloadedIn(t, exe)
	if err := Replace(d, exe); !errors.Is(err, ErrUpdateBatchPath) {
		t.Fatalf("want ErrUpdateBatchPath, got %v", err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "MZcurrent" {
		t.Fatalf("executable touched: %q", b)
	}
	if _, err := os.Stat(exe + ".old"); !os.IsNotExist(err) {
		t.Fatal("no .old may be created")
	}
}
