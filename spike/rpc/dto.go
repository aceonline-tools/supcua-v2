package rpc

// Every DTO field carries an explicit json tag: the wire name must not depend
// on the Go identifier, which garble renames.

type PingParams struct {
	Text string `json:"text"`
}

type PingResult struct {
	Echo  string `json:"echo"`
	Upper string `json:"upper"`
	Seq   int    `json:"seq"`
}

type DialogParams struct {
	Cycle int `json:"cycle"`
}

type OverlayParams struct {
	Draggable bool `json:"draggable"`
}

type OverlayState struct {
	Visible   bool `json:"visible"`
	Draggable bool `json:"draggable"`
}

type Report struct {
	Ping   string `json:"ping"`
	Events int    `json:"events"`
	Cycles int    `json:"cycles"`
	Error  string `json:"error,omitempty"`
}

// Status is the payload of the "status" event.
type Status struct {
	Active  bool   `json:"active"`
	Running string `json:"running"`
	Tick    int    `json:"tick"`
}
