package locale

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func mustRead(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.FromSlash(rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestCatalogParity(t *testing.T) {
	problems, warnings := Validate(Embedded())
	for _, p := range problems {
		t.Error(p)
	}
	for _, w := range warnings {
		t.Log("warning:", w)
	}
}

// Negative proofs (contract 8 C1): the parity check must fail when a translation is missing or wrong.
func TestParityDetectsBreakage(t *testing.T) {
	mutate := func(f func(Catalog)) []string {
		c, err := Load(RawCatalog())
		if err != nil {
			t.Fatal(err)
		}
		f(c)
		p, _ := Validate(c)
		return p
	}
	for name, f := range map[string]func(Catalog){
		"vi value deleted": func(c Catalog) { e := c["tray.exit"]; e.VI = Text{}; c["tray.exit"] = e },
		"vi equals en":     func(c Catalog) { e := c["tray.exit"]; e.VI = e.EN; c["tray.exit"] = e },
		"param dropped in vi": func(c Catalog) {
			e := c["tray.tooltipRunning"]
			e.VI = Text{Plain: "supcua-v2 - Đang bật"}
			c["tray.tooltipRunning"] = e
		},
		"id removed from vi plural": func(c Catalog) {
			c["x.n"] = Entry{EN: Text{Forms: map[string]string{"one": "{n} a", "other": "{n} as"}}, VI: Text{Forms: map[string]string{"one": "{n} b"}}}
		},
		"TODO in vi": func(c Catalog) { e := c["tray.exit"]; e.VI = Text{Plain: "TODO"}; c["tray.exit"] = e },
	} {
		if len(mutate(f)) == 0 {
			t.Errorf("%s: Validate did not fail", name)
		}
	}
}

func TestBaselineEnglishUnchanged(t *testing.T) {
	var base map[string]string
	if err := json.Unmarshal([]byte(mustRead(t, "en-baseline.json")), &base); err != nil {
		t.Fatal(err)
	}
	c := Embedded()
	for id, want := range base {
		e, ok := c[id]
		if !ok {
			t.Errorf("baseline id %s missing from catalogue", id)
			continue
		}
		if e.EN.Plain != want {
			t.Errorf("%s: en changed from baseline %q to %q", id, want, e.EN.Plain)
		}
	}
}

var (
	goCodeRe = regexp.MustCompile(`(?m)^\s*Code\w+\s*=\s*"([A-Z_]+)"`)
	tsUnion  = regexp.MustCompile(`(?s)export type ErrorCode =(.*?);`)
	tsCode   = regexp.MustCompile(`'([A-Z_]+)'`)
)

func TestErrorCodesHaveEntries(t *testing.T) {
	codes := map[string]string{}
	for _, f := range []string{"../bridge/errors.go", "../ui/webview/rpc/rpc.go"} {
		for _, m := range goCodeRe.FindAllStringSubmatch(mustRead(t, f), -1) {
			codes[m[1]] = f
		}
	}
	u := tsUnion.FindStringSubmatch(mustRead(t, "../frontend/src/api/types.ts"))
	if u == nil {
		t.Fatal("ErrorCode union not found in types.ts")
	}
	for _, m := range tsCode.FindAllStringSubmatch(u[1], -1) {
		codes[m[1]] = "types.ts"
	}
	if len(codes) < 20 {
		t.Fatalf("found only %d codes, scanner is broken", len(codes))
	}
	c := Embedded()
	for code, src := range codes {
		if _, ok := c["error."+strings.ToLower(code)]; !ok {
			t.Errorf("no catalogue entry error.%s (code in %s)", strings.ToLower(code), src)
		}
	}
}

var (
	goRef = regexp.MustCompile(`locale\.T\(\s*"([^"]+)"|locale\.TL\([^,]+,\s*"([^"]+)"`)
	tsRef = regexp.MustCompile("(?:^|[^\\w.$])t\\(\\s*['\"`]([A-Za-z][\\w.]*)['\"`]")
	// quotedID finds ids held in data (for example {label: 'settings.languageEnglish'}); it only counts as a use when the id exists.
	quotedID = regexp.MustCompile("['\"`]([a-z][A-Za-z0-9]*\\.[A-Za-z0-9_.]+)['\"`]")
)

func collectRefs(t *testing.T) (refs, uses map[string]string) {
	refs, uses = map[string]string{}, map[string]string{}
	walk := func(root string, re *regexp.Regexp, exts ...string) {
		_ = filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if fi.IsDir() {
				if n := fi.Name(); n == "node_modules" || n == "dist" || n == ".git" || n == "third_party" || n == "sim" {
					return filepath.SkipDir
				}
				return nil
			}
			ok := false
			for _, e := range exts {
				ok = ok || strings.HasSuffix(p, e)
			}
			if !ok || strings.HasSuffix(p, "_test.go") || strings.HasSuffix(p, ".test.ts") || strings.HasSuffix(p, "ids.gen.ts") || strings.Contains(filepath.ToSlash(p), "/i18n/lint") {
				return nil
			}
			b, _ := os.ReadFile(p)
			for _, m := range quotedID.FindAllStringSubmatch(string(b), -1) {
				uses[m[1]] = p
			}
			for _, m := range re.FindAllStringSubmatch(string(b), -1) {
				for _, id := range m[1:] {
					if id != "" {
						refs[id] = p
					}
				}
			}
			return nil
		})
	}
	walk("..", goRef, ".go")
	walk("../frontend/src", tsRef, ".ts", ".svelte")
	return refs, uses
}

func TestReferencedIDsExistAndNoDeadIDs(t *testing.T) {
	c := Embedded()
	refs, uses := collectRefs(t)
	for id, f := range refs {
		if _, ok := c[id]; !ok {
			t.Errorf("%s references unknown id %s", f, id)
		}
	}
	var dead []string
	for id, e := range c {
		_, used := refs[id]
		if _, ok := uses[id]; ok {
			used = true
		}
		dynamic := strings.HasPrefix(id, "error.") || e.HasScope("boot")
		switch {
		case e.Pending != "" && used:
			t.Errorf("%s is referenced but still marked pending=%q: drop the marker", id, e.Pending)
		case e.Pending == "" && !used && !dynamic:
			dead = append(dead, id)
		}
	}
	sort.Strings(dead)
	for _, id := range dead {
		t.Errorf("dead id %s: not referenced (mark pending:<chunk> if a later chunk will use it)", id)
	}
}
