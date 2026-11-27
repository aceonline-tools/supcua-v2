package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// ActionType enumerates all supported script action types.
const (
	ActionMouseMove        = "mouse_move"
	ActionMouseLeftClick   = "mouse_left_click"
	ActionMouseRightClick  = "mouse_right_click"
	ActionMouseLeftDouble  = "mouse_left_double"
	ActionMouseRightDouble = "mouse_right_double"
	ActionMouseLeftDown    = "mouse_left_down"
	ActionMouseLeftUp      = "mouse_left_up"
	ActionMouseRightDown   = "mouse_right_down"
	ActionMouseRightUp     = "mouse_right_up"
	ActionMouseStorePos    = "mouse_store_pos"
	ActionMouseMoveStored  = "mouse_move_stored"
	ActionKeyPress         = "key_press"
	ActionKeyDown          = "key_down"
	ActionKeyUp            = "key_up"
	ActionDelay            = "delay"
	ActionLoop             = "loop"
	ActionWaitForImage     = "wait_for_image"
	ActionIfImage          = "if_image"
	ActionLockMouse        = "lock_mouse"
	ActionUnlockMouse      = "unlock_mouse"
	ActionLockKeyboard     = "lock_keyboard"
	ActionUnlockKeyboard   = "unlock_keyboard"
)

// Region defines a rectangular area on screen for image search.
type Region struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Key sequence limits and defaults for key_press actions.
const (
	MaxKeySequence    = 32
	MaxKeyDelayMs     = 5000
	DefaultKeyDelayMs = 50
	maxKeyNameLen     = 32
)

// KeyModifiers is the modifier vocabulary a KeyStroke accepts.
var KeyModifiers = []string{"ctrl", "alt", "shift", "win"}

// KeyStroke is one key of a key_press sequence, pressed together with Mods.
type KeyStroke struct {
	Key  string   `json:"key"`
	Mods []string `json:"mods,omitempty"`
}

// Sequence returns the keys a key_press action types. Keys wins over the legacy
// single Key field; a legacy-only action is a sequence of one.
func (a Action) Sequence() []KeyStroke {
	if len(a.Keys) > 0 {
		return a.Keys
	}
	if a.Key != "" {
		return []KeyStroke{{Key: a.Key}}
	}
	return nil
}

// KeyDelay returns the pause between keys of a sequence.
func (a Action) KeyDelay() int {
	if a.KeyDelayMs == nil {
		return DefaultKeyDelayMs
	}
	return *a.KeyDelayMs
}

// ResolveKeys validates the key_press sequence without mutating the action and
// returns it with canonical modifiers and key names plus the effective delay.
// With strict, an out-of-range delay is an error; otherwise it is clamped.
func (a Action) ResolveKeys(strict bool) ([]KeyStroke, int, error) {
	seq := a.Sequence()
	if len(seq) == 0 {
		return nil, 0, errors.New("key_press has no keys")
	}
	if len(seq) > MaxKeySequence {
		return nil, 0, fmt.Errorf("key_press has %d keys (max %d)", len(seq), MaxKeySequence)
	}
	delay := a.KeyDelay()
	if delay < 0 || delay > MaxKeyDelayMs {
		if strict {
			return nil, 0, fmt.Errorf("key delay %dms out of range 0-%d", delay, MaxKeyDelayMs)
		}
		delay = min(max(delay, 0), MaxKeyDelayMs)
	}
	out := make([]KeyStroke, len(seq))
	for i, k := range seq {
		name := k.Key
		if len(name) > maxKeyNameLen {
			return nil, 0, errors.New("key name too long")
		}
		if utf8.RuneCountInString(name) > 1 {
			name = strings.ToLower(name)
		}
		if !IsKnownKey(name) {
			return nil, 0, fmt.Errorf("unknown key %q", k.Key)
		}
		if len(k.Mods) > len(KeyModifiers) {
			return nil, 0, errors.New("too many modifiers")
		}
		seen := map[string]bool{}
		for _, m := range k.Mods {
			m = strings.ToLower(m)
			if seen[m] || !slices.Contains(KeyModifiers, m) {
				return nil, 0, fmt.Errorf("invalid modifier %q", m)
			}
			seen[m] = true
		}
		var mods []string
		for _, m := range KeyModifiers {
			if seen[m] {
				mods = append(mods, m)
			}
		}
		out[i] = KeyStroke{Key: name, Mods: mods}
	}
	return out, delay, nil
}

// NormalizeKeys strictly validates a key_press sequence and rewrites it in
// canonical form. It reports false when the sequence is unusable.
func (a *Action) NormalizeKeys() bool {
	seq, _, err := a.ResolveKeys(true)
	if err != nil {
		return false
	}
	a.Keys, a.Key = seq, ""
	return true
}

// MarshalJSON writes a plain single key (no modifiers) in the legacy form so
// older app versions can still read it; anything else uses keys/key_delay_ms.
func (a Action) MarshalJSON() ([]byte, error) {
	type plain Action
	if a.Type == ActionKeyPress {
		seq := a.Sequence()
		if len(seq) == 1 && len(seq[0].Mods) == 0 {
			a.Key, a.Keys, a.KeyDelayMs = seq[0].Key, nil, nil
		} else if len(seq) > 0 {
			a.Key, a.Keys = "", seq
		}
	}
	return json.Marshal(plain(a))
}

// Action represents a single step in a script.
// Fields are used depending on the Type:
//   - mouse_move: X, Y (or Source="found" with OffsetX, OffsetY)
//   - mouse_left_down, mouse_left_up, mouse_right_down, mouse_right_up: no params
//   - key_down, key_up: Key
//   - delay: DelayMs
//   - loop: LoopCount, Actions
//   - wait_for_image: Image, SearchRegion, TimeoutMs, UseFoundPosition
//   - if_image: Image, SearchRegion, UseFoundPosition, ThenActions, ElseActions
type Action struct {
	Type             string      `json:"type"`
	X                int         `json:"x,omitempty"`
	Y                int         `json:"y,omitempty"`
	Source           string      `json:"source,omitempty"`
	OffsetX          int         `json:"offset_x,omitempty"`
	OffsetY          int         `json:"offset_y,omitempty"`
	Key              string      `json:"key,omitempty"`
	Keys             []KeyStroke `json:"keys,omitempty"`
	KeyDelayMs       *int        `json:"key_delay_ms,omitempty"`
	DelayMs          int         `json:"ms,omitempty"`
	LoopCount        int         `json:"count,omitempty"`
	Actions          []Action    `json:"actions,omitempty"`
	Image            string      `json:"image,omitempty"`
	SearchRegion     *Region     `json:"region,omitempty"`
	TimeoutMs        int         `json:"timeout_ms,omitempty"`
	UseFoundPosition bool        `json:"use_found_position,omitempty"`
	ThenActions      []Action    `json:"then,omitempty"`
	ElseActions      []Action    `json:"else,omitempty"`
}
