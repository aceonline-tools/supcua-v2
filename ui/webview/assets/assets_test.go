package assets

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/yanmad27/supcua-v2/ui/webview/serve"
)

func TestPlaceholderServedWithMatchingCSP(t *testing.T) {
	b, err := fs.ReadFile(FS(), "index.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), serve.CSP) {
		t.Fatal("index.html meta CSP must equal serve.CSP")
	}
	r := (&serve.Server{FS: FS()}).Handle("GET", "https://app.local/")
	if r.Status != 200 {
		t.Fatalf("%+v", r)
	}
}
