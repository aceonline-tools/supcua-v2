package locale

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
)

const earlyReadCap = 1 << 20

// Early reads only the "language" key of dir/config.json, before the config service exists
// (Already Running box, WebView2 startup failures, load timeout). Every error is ignored: a
// missing, unreadable, oversized or corrupt file resolves exactly like an unset language. It
// also stores the result as the current language.
func Early(dir string, d Detector) Locale {
	l := Resolve(earlyValue(filepath.Join(dir, "config.json")), d)
	Set(l)
	return l
}

func earlyValue(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, earlyReadCap+1))
	if err != nil || len(data) > earlyReadCap {
		return ""
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(data, &m) != nil {
		return ""
	}
	var s string
	if json.Unmarshal(m["language"], &s) != nil {
		return ""
	}
	return s
}
