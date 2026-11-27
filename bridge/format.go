package bridge

import (
	"fmt"
	"strings"

	"github.com/yanmad27/supcua-v2/model"
	"github.com/yanmad27/supcua-v2/storage"
)

// validKeys is the key vocabulary hotkey.Manager can register (hotkey/manager.go
// convertKeyNameToVirtualKeyCode); it is mirrored here because that package is Windows-only.
var validKeys = func() map[string]bool {
	m := map[string]bool{}
	for _, k := range []string{
		"space", "enter", "escape", "tab", "lctrl", "rctrl", "lshift", "rshift", "lalt", "ralt", "lwin", "rwin",
		";", "=", ",", "-", ".", "/", "`", "[", `\`, "]", "'",
	} {
		m[k] = true
	}
	for c := 'a'; c <= 'z'; c++ {
		m[string(c)] = true
	}
	for c := '0'; c <= '9'; c++ {
		m[string(c)] = true
	}
	for n := 1; n <= 12; n++ {
		m[fmt.Sprintf("f%d", n)] = true
	}
	return m
}()

var modifierOrder = []string{"ctrl", "alt", "shift", "win"}

// normalizeHotkey lowercases, de-duplicates and canonically orders modifiers and checks the key
// against the manager vocabulary. It reports false for an unusable hotkey.
func normalizeHotkey(hk *model.Hotkey) bool {
	key := strings.ToLower(hk.Key)
	if !validKeys[key] || len(hk.Modifiers) > len(modifierOrder) {
		return false
	}
	have := map[string]bool{}
	for _, m := range hk.Modifiers {
		m = strings.ToLower(m)
		if !modifierSet(m) {
			return false
		}
		have[m] = true
	}
	mods := []string{}
	for _, m := range modifierOrder {
		if have[m] {
			mods = append(mods, m)
		}
	}
	hk.Key, hk.Modifiers = key, mods
	return true
}

func modifierSet(m string) bool {
	for _, o := range modifierOrder {
		if o == m {
			return true
		}
	}
	return false
}

var keyDisplay = map[string]string{
	"lctrl": "LCtrl", "rctrl": "RCtrl", "lshift": "LShift", "rshift": "RShift",
	"lalt": "LAlt", "ralt": "RAlt",
}

// DisplayHotkey renders a hotkey as "Ctrl + O". The result round-trips through ParseHotkey.
func DisplayHotkey(hk model.Hotkey) string {
	parts := make([]string, 0, len(hk.Modifiers)+1)
	for _, m := range hk.Modifiers {
		parts = append(parts, titleCase(m))
	}
	return strings.Join(append(parts, displayKey(hk.Key)), " + ")
}

func displayKey(key string) string {
	k := strings.ToLower(key)
	if d, ok := keyDisplay[k]; ok {
		return d
	}
	if len(k) == 1 || (len(k) > 1 && k[0] == 'f' && isDigits(k[1:])) {
		return strings.ToUpper(k)
	}
	return titleCase(k)
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
}

// ParseHotkey parses a display string ("Ctrl + O", "RCtrl", "alt+f5", "Ctrl + ;") into a model
// hotkey, rejecting unknown keys and duplicate modifiers.
func ParseHotkey(text string) (model.Hotkey, bool) {
	if len(text) > 64 {
		return model.Hotkey{}, false
	}
	tokens := strings.Split(text, "+")
	hk := model.Hotkey{Key: strings.TrimSpace(tokens[len(tokens)-1])}
	seen := map[string]bool{}
	for _, tok := range tokens[:len(tokens)-1] {
		tok = strings.ToLower(strings.TrimSpace(tok))
		if seen[tok] {
			return model.Hotkey{}, false
		}
		seen[tok] = true
		hk.Modifiers = append(hk.Modifiers, tok)
	}
	if !normalizeHotkey(&hk) {
		return model.Hotkey{}, false
	}
	return hk, true
}

func scriptHotkeyLabel(s model.Script) string {
	switch s.HotkeyMode {
	case model.HotkeyModeToggle:
		if s.ToggleHotkey != nil {
			return DisplayHotkey(*s.ToggleHotkey)
		}
	case model.HotkeyModeSeparate:
		start, stop := "", ""
		if s.StartHotkey != nil {
			start = DisplayHotkey(*s.StartHotkey)
		}
		if s.StopHotkey != nil {
			stop = DisplayHotkey(*s.StopHotkey)
		}
		if start != "" && stop != "" {
			return start + " / " + stop
		}
		return start + stop
	}
	return ""
}

var simpleLabels = map[string]string{
	model.ActionMouseLeftClick:   "Mouse Left Click",
	model.ActionMouseRightClick:  "Mouse Right Click",
	model.ActionMouseLeftDouble:  "Mouse Left Double Click",
	model.ActionMouseRightDouble: "Mouse Right Double Click",
	model.ActionMouseLeftDown:    "Mouse Left Down",
	model.ActionMouseLeftUp:      "Mouse Left Up",
	model.ActionMouseRightDown:   "Mouse Right Down",
	model.ActionMouseRightUp:     "Mouse Right Up",
	model.ActionMouseStorePos:    "Mouse Store Position",
	model.ActionMouseMoveStored:  "Mouse Move to Stored",
	model.ActionLockMouse:        "Lock Mouse",
	model.ActionUnlockMouse:      "Unlock Mouse",
	model.ActionLockKeyboard:     "Lock Keyboard",
	model.ActionUnlockKeyboard:   "Unlock Keyboard",
	"loop_end":                   "End Loop",
}

// describeAction single-sources the action list labels (parity with ui/script_edit_dialog.go).
func describeAction(a model.Action) string {
	if l, ok := simpleLabels[a.Type]; ok {
		return l
	}
	switch a.Type {
	case model.ActionMouseMove:
		if a.Source == "found" {
			return "Mouse Move -> found position"
		}
		return fmt.Sprintf("Mouse Move -> (%d, %d)", a.X, a.Y)
	case model.ActionKeyPress:
		return describeKeyPress(a)
	case model.ActionKeyDown:
		return "Key Down: " + a.Key
	case model.ActionKeyUp:
		return "Key Up: " + a.Key
	case model.ActionDelay:
		return fmt.Sprintf("Delay: %dms", a.DelayMs)
	case model.ActionLoop:
		if a.LoopCount == 0 {
			return "Loop (infinite)"
		}
		return fmt.Sprintf("Loop (%dx)", a.LoopCount)
	case model.ActionWaitForImage:
		return fmt.Sprintf("Wait For Image (timeout: %dms)", a.TimeoutMs)
	case model.ActionIfImage:
		return fmt.Sprintf("If Image (then: %d, else: %d)", len(a.ThenActions), len(a.ElseActions))
	}
	return a.Type
}

func describeKeyPress(a model.Action) string {
	seq := a.Sequence()
	parts := make([]string, len(seq))
	for i, k := range seq {
		parts[i] = strings.Join(append(append([]string(nil), k.Mods...), k.Key), "+")
	}
	return "Key Press: " + strings.Join(parts, " → ")
}

func intp(v int) *int { return &v }

func buildCatalog() []ActionSpec {
	xy := []ParamSpec{{Name: "x", Kind: "int", Default: 0}, {Name: "y", Kind: "int", Default: 0}}
	key := []ParamSpec{{Name: "key", Kind: "string", Default: ""}}
	none := []ParamSpec{}
	return []ActionSpec{
		{model.ActionMouseMove, "mouse", xy},
		{model.ActionMouseLeftClick, "mouse", none},
		{model.ActionMouseRightClick, "mouse", none},
		{model.ActionMouseLeftDouble, "mouse", none},
		{model.ActionMouseRightDouble, "mouse", none},
		{model.ActionMouseLeftDown, "mouse", none},
		{model.ActionMouseLeftUp, "mouse", none},
		{model.ActionMouseRightDown, "mouse", none},
		{model.ActionMouseRightUp, "mouse", none},
		{model.ActionMouseStorePos, "mouse", none},
		{model.ActionMouseMoveStored, "mouse", none},
		{model.ActionKeyPress, "keyboard", []ParamSpec{
			{Name: "key", Kind: "string", Default: ""},
			{Name: "keys", Kind: "keys"},
			{Name: "key_delay_ms", Kind: "int", Min: intp(0), Max: intp(model.MaxKeyDelayMs), Default: model.DefaultKeyDelayMs},
		}},
		{model.ActionKeyDown, "keyboard", key},
		{model.ActionKeyUp, "keyboard", key},
		{model.ActionDelay, "wait", []ParamSpec{{Name: "ms", Kind: "int", Min: intp(1), Max: intp(catalogMaxMs), Default: 100}}},
		{model.ActionLoop, "loop", []ParamSpec{{Name: "count", Kind: "int", Min: intp(0), Max: intp(maxLoopCount), Default: 0}}},
		{model.ActionWaitForImage, "image", []ParamSpec{
			{Name: "timeout_ms", Kind: "int", Min: intp(0), Max: intp(catalogMaxMs), Default: 5000},
			{Name: "use_found_position", Kind: "bool", Default: false},
		}},
		{model.ActionIfImage, "image", []ParamSpec{{Name: "use_found_position", Kind: "bool", Default: false}}},
		{model.ActionLockMouse, "lock", none},
		{model.ActionUnlockMouse, "lock", none},
		{model.ActionLockKeyboard, "lock", none},
		{model.ActionUnlockKeyboard, "lock", none},
	}
}

const (
	maxDelayMs    = 3_600_000
	catalogMaxMs  = 600_000
	maxLoopCount  = 1_000_000
	maxNameLen    = 120
	maxActions    = 5000
	maxActionDeep = 16
	maxImageBytes = 4 << 20
	maxImageTotal = 32 << 20
)

func knownActionTypes() map[string]bool {
	m := map[string]bool{}
	for _, s := range buildCatalog() {
		m[s.Type] = true
	}
	return m
}

// validateScript normalises (trims name/group) and validates a client-supplied script.
func validateScript(s *model.Script, known map[string]bool) *Error {
	s.Name = strings.TrimSpace(s.Name)
	s.Group = strings.TrimSpace(s.Group)
	if s.Name == "" || storage.SanitizeName(s.Name) == "" {
		return newErr(CodeNameEmpty)
	}
	if len(s.Name) > maxNameLen || len(s.Group) > maxNameLen || len(s.ID) > 64 {
		return badRequest()
	}
	if s.RunMode != model.RunModeOnce && s.RunMode != model.RunModeInfinite {
		return badRequest()
	}
	if s.HotkeyMode != model.HotkeyModeToggle && s.HotkeyMode != model.HotkeyModeSeparate {
		return badRequest()
	}
	for _, hk := range []*model.Hotkey{s.ToggleHotkey, s.StartHotkey, s.StopHotkey} {
		if hk != nil && !normalizeHotkey(hk) {
			return badRequest()
		}
	}
	if s.Actions == nil {
		s.Actions = []model.Action{}
	}
	count, imgTotal := 0, 0
	return validateActions(s.Actions, known, 0, &count, &imgTotal)
}

func validateActions(actions []model.Action, known map[string]bool, depth int, count, imgTotal *int) *Error {
	if depth > maxActionDeep {
		return badRequest()
	}
	for i := range actions {
		a := &actions[i]
		*count++
		if *count > maxActions || (!known[a.Type] && a.Type != "loop_end") {
			return badRequest()
		}
		if a.DelayMs < 0 || a.DelayMs > maxDelayMs || a.TimeoutMs < 0 || a.TimeoutMs > maxDelayMs ||
			a.LoopCount < 0 || a.LoopCount > maxLoopCount || len(a.Key) > 32 || len(a.Image) > maxImageBytes {
			return badRequest()
		}
		if a.Type == model.ActionKeyPress && !a.NormalizeKeys() {
			return badRequest()
		}
		*imgTotal += len(a.Image)
		if *imgTotal > maxImageTotal {
			return badRequest()
		}
		for _, children := range [][]model.Action{a.Actions, a.ThenActions, a.ElseActions} {
			if err := validateActions(children, known, depth+1, count, imgTotal); err != nil {
				return err
			}
		}
	}
	return nil
}
