package webview

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// Go never frees syscall.NewCallback slots (cap 2000). This guard keeps the count constant
// by source inspection (runs on any OS): in the vendored COM package every NewComProc call
// must be in a package-level var initializer, and in ui/webview newCallback may only be
// called from the two known once-guarded sites.

func callSites(t *testing.T, dir string, names ...string) (sites []struct{ file, fn string }) {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range af.Decls {
			fn := "(package-level)"
			if fd, ok := d.(*ast.FuncDecl); ok {
				fn = fd.Name.Name
			}
			ast.Inspect(d, func(n ast.Node) bool {
				c, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				var id string
				switch f := c.Fun.(type) {
				case *ast.Ident:
					id = f.Name
				case *ast.SelectorExpr:
					id = f.Sel.Name
				}
				for _, w := range names {
					if id == w {
						sites = append(sites, struct{ file, fn string }{filepath.Base(f), fn})
					}
				}
				return true
			})
		}
	}
	return sites
}

func TestVendoredComProcsArePackageLevelOnly(t *testing.T) {
	sites := callSites(t, "../../third_party/go-webview2/pkg/edge", "NewComProc", "NewCallback")
	if len(sites) < 20 {
		t.Fatalf("scan found only %d sites; wrong directory?", len(sites))
	}
	for _, s := range sites {
		if s.fn != "(package-level)" && s.fn != "NewComProc" {
			t.Errorf("%s: callback created inside func %s (per-call slot leak)", s.file, s.fn)
		}
	}
}

func TestHostCallbacksAreTheKnownOnceGuardedSites(t *testing.T) {
	got := map[string]int{}
	for _, s := range callSites(t, ".", "newCallback", "NewCallback") {
		got[s.file+":"+s.fn]++
	}
	want := map[string]int{
		"callbacks_windows.go:newCallback":   1, // the counting wrapper itself
		"callbacks_windows.go:NewCallback":   1, // exported counted wrapper for ui/overlay.go
		"ui_windows.go:createHostWindow":     1, // inside hostClassOnce.Do
		"capture_windows.go:StartKeyCapture": 1, // inside keyHookOnce.Do
	}
	if len(got) != len(want) {
		t.Fatalf("callback sites changed: got %v want %v", got, want)
	}
	for k, n := range want {
		if got[k] != n {
			t.Fatalf("callback sites changed: got %v want %v", got, want)
		}
	}
}

// ui/overlay.go is the only other package creating a callback: once, for the shared overlay
// window procedure (class registration inside overlayClassOnce.Do).
func TestUIPackageCallbackSites(t *testing.T) {
	got := map[string]int{}
	for _, s := range callSites(t, "..", "NewCallback", "newCallback") {
		got[s.file+":"+s.fn]++
	}
	if len(got) != 1 || got["overlay.go:NewOverlayWindow"] != 1 {
		t.Fatalf("ui callback sites changed: %v", got)
	}
}
