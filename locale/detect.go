package locale

import "strings"

const (
	langVietnamese = 0x2A // LANG_VIETNAMESE, the primary language id (langid & 0x3FF)
	primaryMask    = 0x3FF
)

// Detector is the platform boundary. UILanguage is GetUserDefaultUILanguage; Preferred is
// GetUserPreferredUILanguages(MUI_LANGUAGE_NAME), used only when UILanguage is a custom-range id
// (supplemental locales). Either may be nil (tests, non-Windows).
type Detector struct {
	UILanguage func() (langID uint16, ok bool)
	Preferred  func() []string
}

// DefaultDetector is the production detector (Windows) or an always-English one elsewhere.
var DefaultDetector = platformDetector()

// customRange reports LANGIDs that carry no real primary language: LOCALE_CUSTOM_DEFAULT (0x0C00),
// LOCALE_CUSTOM_UNSPECIFIED (0x1000), LOCALE_CUSTOM_UI_DEFAULT (0x1400) and the neutral id 0.
func customRange(id uint16) bool { return id&primaryMask == 0 }

// Detect maps the Windows display language to a supported locale: Vietnamese primary language
// gives VI; a custom-range id consults the first preferred UI language tag; everything else,
// and every failure, gives EN.
func (d Detector) Detect() Locale {
	if d.UILanguage != nil {
		if id, ok := d.UILanguage(); ok {
			if id&primaryMask == langVietnamese {
				return VI
			}
			if !customRange(id) {
				return EN
			}
		}
	}
	if d.Preferred != nil {
		if tags := d.Preferred(); len(tags) > 0 {
			if l, ok := Parse(strings.TrimSpace(tags[0])); ok {
				return l
			}
		}
	}
	return EN
}
