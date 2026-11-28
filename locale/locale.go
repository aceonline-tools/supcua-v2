// Package locale is the single i18n implementation for the Go side: locale values, resolution
// (persisted, then Windows UI language, then English), the embedded message catalogue and the
// process-wide current language used by native UI (tray, MessageBox, window titles).
package locale

import (
	"strings"
	"sync/atomic"
)

type Locale string

const (
	EN Locale = "en"
	VI Locale = "vi"
)

var Supported = []Locale{EN, VI}

// Parse normalises a persisted or requested value: trimmed, lower-cased, primary subtag only.
// ok is false for anything that is not a supported language.
func Parse(s string) (Locale, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.IndexAny(s, "-_"); i >= 0 {
		s = s[:i]
	}
	for _, l := range Supported {
		if s == string(l) {
			return l, true
		}
	}
	return EN, false
}

// Resolve applies the resolution order: a valid persisted value, then the detector, then EN.
func Resolve(persisted string, d Detector) Locale {
	if l, ok := Parse(persisted); ok {
		return l
	}
	return d.Detect()
}

// Effective resolves a persisted value with the production detector (DefaultDetector).
func Effective(persisted string) Locale { return Resolve(persisted, DefaultDetector) }

var current atomic.Value

func init() { current.Store(EN) }

// Current is the language native UI renders in; Set changes it for the whole process.
func Current() Locale { return current.Load().(Locale) }

func Set(l Locale) {
	if p, ok := Parse(string(l)); ok {
		current.Store(p)
	}
}

// Params are the interpolation values of a message ({name} placeholders). "n" selects plurals.
type Params map[string]any
