package model

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
	ActionKeyPress       = "key_press"
	ActionKeyDown        = "key_down"
	ActionKeyUp          = "key_up"
	ActionDelay          = "delay"
	ActionLoop           = "loop"
	ActionWaitForImage   = "wait_for_image"
	ActionIfImage        = "if_image"
)

// Region defines a rectangular area on screen for image search.
type Region struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
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
	Type             string   `json:"type"`
	X                int      `json:"x,omitempty"`
	Y                int      `json:"y,omitempty"`
	Source           string   `json:"source,omitempty"`
	OffsetX          int      `json:"offset_x,omitempty"`
	OffsetY          int      `json:"offset_y,omitempty"`
	Key              string   `json:"key,omitempty"`
	DelayMs          int      `json:"ms,omitempty"`
	LoopCount        int      `json:"count,omitempty"`
	Actions          []Action `json:"actions,omitempty"`
	Image            string   `json:"image,omitempty"`
	SearchRegion     *Region  `json:"region,omitempty"`
	TimeoutMs        int      `json:"timeout_ms,omitempty"`
	UseFoundPosition bool     `json:"use_found_position,omitempty"`
	ThenActions      []Action `json:"then,omitempty"`
	ElseActions      []Action `json:"else,omitempty"`
}
