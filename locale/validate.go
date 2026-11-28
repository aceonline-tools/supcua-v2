package locale

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

var placeholderRe = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)

var idRe = regexp.MustCompile(`^[a-z][A-Za-z0-9]*(\.[A-Za-z0-9_]+)+$`)

func params(t Text) map[string]bool {
	out := map[string]bool{}
	add := func(s string) {
		for _, m := range placeholderRe.FindAllStringSubmatch(s, -1) {
			out[m[1]] = true
		}
	}
	add(t.Plain)
	for _, s := range t.Forms {
		add(s)
	}
	return out
}

func texts(t Text) []string {
	if t.Forms == nil {
		return []string{t.Plain}
	}
	out := make([]string, 0, len(t.Forms))
	for _, s := range t.Forms {
		out = append(out, s)
	}
	return out
}

func sameSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// Params returns the sorted placeholder names of an entry (from en).
func (e Entry) Params() []string {
	var out []string
	for k := range params(e.EN) {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Validate is the parity check (contract 7.1 a-d, i): it returns one problem per violation.
// Length warnings (non-tight ids) are returned separately.
func Validate(c Catalog) (problems, warnings []string) {
	ids := make([]string, 0, len(c))
	for id := range c {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	bad := func(id, f string, a ...any) { problems = append(problems, id+": "+fmt.Sprintf(f, a...)) }
	for _, id := range ids {
		e := c[id]
		if !idRe.MatchString(id) {
			bad(id, "malformed id")
		}
		if e.EN.empty() {
			bad(id, "missing en")
		}
		if e.VI.empty() {
			bad(id, "missing vi")
		}
		if !e.EN.empty() && !e.VI.empty() {
			if !sameSet(params(e.EN), params(e.VI)) {
				bad(id, "param sets differ between en and vi")
			}
			if e.EN.Forms != nil {
				if e.EN.Forms["one"] == "" || e.EN.Forms["other"] == "" {
					bad(id, "en plural needs one and other")
				}
				if _, ok := params(e.EN)["n"]; !ok {
					bad(id, "plural message needs {n}")
				}
			}
			if e.VI.Forms != nil && (len(e.VI.Forms) != 1 || e.VI.Forms["other"] == "") {
				bad(id, "vi plural must be exactly {other}")
			}
			if reflectEqual(e.EN, e.VI) && !e.Same {
				bad(id, "vi equals en (set same:true with why if intentional)")
			}
			if e.Same && e.Why == "" {
				bad(id, "same needs why")
			}
		}
		for _, l := range []struct {
			n string
			t Text
		}{{"en", e.EN}, {"vi", e.VI}} {
			for _, s := range texts(l.t) {
				if s == "" && !l.t.empty() {
					bad(id, "%s has an empty form", l.n)
				}
				if s != strings.TrimSpace(s) && !strings.Contains(s, "\n") {
					bad(id, "%s has leading/trailing space", l.n)
				}
				if strings.Contains(s, "  ") {
					bad(id, "%s has a double space", l.n)
				}
				if strings.Contains(s, "TODO") || strings.Contains(s, "XXX") {
					bad(id, "%s contains a TODO marker", l.n)
				}
				if !utf8.ValidString(s) {
					bad(id, "%s is not valid UTF-8", l.n)
				}
			}
		}
		if !e.EN.empty() && !e.VI.empty() {
			en, vi := longest(e.EN), longest(e.VI)
			if float64(utf8.RuneCountInString(vi)) > 1.5*float64(utf8.RuneCountInString(en))+4 {
				if e.Tight {
					bad(id, "tight: vi is more than 1.5x+4 the length of en")
				} else {
					warnings = append(warnings, id+": vi is much longer than en")
				}
			}
		}
	}
	return problems, warnings
}

func longest(t Text) string {
	best := ""
	for _, s := range texts(t) {
		if len(s) > len(best) {
			best = s
		}
	}
	return best
}

func reflectEqual(a, b Text) bool {
	if a.Plain != b.Plain || len(a.Forms) != len(b.Forms) {
		return false
	}
	for k, v := range a.Forms {
		if b.Forms[k] != v {
			return false
		}
	}
	return true
}
