package locale

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
)

//go:embed catalog.json
var catalogJSON []byte

// Text is one language's message: a plain string or plural forms keyed by CLDR category.
type Text struct {
	Plain string
	Forms map[string]string
}

func (t *Text) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '{' {
		return json.Unmarshal(b, &t.Forms)
	}
	return json.Unmarshal(b, &t.Plain)
}

func (t Text) MarshalJSON() ([]byte, error) {
	if t.Forms != nil {
		return json.Marshal(t.Forms)
	}
	return json.Marshal(t.Plain)
}

func (t Text) empty() bool { return t.Plain == "" && len(t.Forms) == 0 }

// Entry is one catalogue message. Scope defaults to ["web"]; Pending names the chunk that will
// first reference an id (the dead-id check skips it); Same marks an intentionally identical vi.
type Entry struct {
	EN      Text     `json:"en"`
	VI      Text     `json:"vi"`
	Scope   []string `json:"scope,omitempty"`
	Tight   bool     `json:"tight,omitempty"`
	Ctx     string   `json:"ctx,omitempty"`
	Same    bool     `json:"same,omitempty"`
	Why     string   `json:"why,omitempty"`
	Pending string   `json:"pending,omitempty"`
}

func (e Entry) text(l Locale) Text {
	if l == VI {
		return e.VI
	}
	return e.EN
}

func (e Entry) HasScope(s string) bool {
	if len(e.Scope) == 0 {
		return s == "web"
	}
	for _, x := range e.Scope {
		if x == s {
			return true
		}
	}
	return false
}

type Catalog map[string]Entry

var (
	loadOnce sync.Once
	loaded   Catalog
)

// Load parses catalog.json. Invalid JSON is a programming error caught by the tests.
func Load(data []byte) (Catalog, error) {
	var c Catalog
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return c, nil
}

// Embedded returns the embedded catalogue.
func Embedded() Catalog {
	loadOnce.Do(func() {
		c, err := Load(catalogJSON)
		if err != nil {
			panic("locale: catalog.json: " + err.Error())
		}
		loaded = c
	})
	return loaded
}

// RawCatalog is the embedded catalog.json bytes (tests, tooling).
func RawCatalog() []byte { return catalogJSON }

func plural(l Locale, n float64) string {
	if l == EN && n == 1 {
		return "one"
	}
	return "other"
}

func pick(t Text, l Locale, p Params) string {
	if t.Forms == nil {
		return t.Plain
	}
	n, _ := toFloat(p["n"])
	if s, ok := t.Forms[plural(l, n)]; ok {
		return s
	}
	return t.Forms["other"]
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint32:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}

// Interpolate replaces {name} placeholders in one pass (values are never re-expanded); an
// unknown placeholder is left as written.
func Interpolate(s string, p Params) string {
	if !strings.Contains(s, "{") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '{' {
			if j := strings.IndexByte(s[i:], '}'); j > 0 {
				if v, ok := p[s[i+1:i+j]]; ok {
					b.WriteString(format(v))
					i += j + 1
					continue
				}
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func format(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case int:
		return strconv.Itoa(x)
	default:
		return fmt.Sprint(x)
	}
}

// TL renders id in language l: the l text, else the English text, else the id itself.
func TL(l Locale, id string, params ...Params) string {
	var p Params
	if len(params) > 0 {
		p = params[0]
	}
	e, ok := Embedded()[id]
	if !ok {
		return id
	}
	t := e.text(l)
	if t.empty() {
		t = e.EN
	}
	return Interpolate(pick(t, l, p), p)
}

// T renders id in the current language.
func T(id string, params ...Params) string { return TL(Current(), id, params...) }

// BootJSON is the JSON object {id: text} of the entries scoped "boot", for the page bootstrap
// (the static loader text before any module runs).
func BootJSON(l Locale) string {
	c := Embedded()
	ids := make([]string, 0, 8)
	for id, e := range c {
		if e.HasScope("boot") {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	m := make(map[string]string, len(ids))
	for _, id := range ids {
		m[id] = TL(l, id)
	}
	b, _ := json.Marshal(m)
	return string(b)
}
