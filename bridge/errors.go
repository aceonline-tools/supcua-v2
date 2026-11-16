package bridge

import (
	"bytes"
	"encoding/json"
)

// Error codes (contract §9). Messages are fixed strings: never Go error text or type names.
const (
	CodeBadRequest         = "BAD_REQUEST"
	CodeUnknownMethod      = "UNKNOWN_METHOD"
	CodeBusy               = "BUSY"
	CodeInternal           = "INTERNAL"
	CodeNotFound           = "NOT_FOUND"
	CodeNameEmpty          = "NAME_EMPTY"
	CodeNameTaken          = "NAME_TAKEN"
	CodeHotkeyConflict     = "HOTKEY_CONFLICT"
	CodeHotkeyRegisterFail = "HOTKEY_REGISTER_FAILED"
	CodeEditorBusy         = "EDITOR_BUSY"
	CodeEditorClosed       = "EDITOR_CLOSED"
	CodeSaveFailed         = "SAVE_FAILED"
	CodeInvalidSetting     = "INVALID_SETTING"
	CodeUpdateFailed       = "UPDATE_FAILED"
	CodeNotLicensed        = "NOT_LICENSED"
)

var messages = map[string]string{
	CodeBadRequest:         "invalid request",
	CodeUnknownMethod:      "unknown method",
	CodeBusy:               "busy",
	CodeInternal:           "internal error",
	CodeNotFound:           "not found",
	CodeNameEmpty:          "script name cannot be empty",
	CodeNameTaken:          "a script with this name already exists",
	CodeHotkeyConflict:     "hotkey conflicts with another script",
	CodeHotkeyRegisterFail: "could not register hotkey",
	CodeEditorBusy:         "an editor is already open",
	CodeEditorClosed:       "editor session is not open",
	CodeSaveFailed:         "could not write script file",
	CodeInvalidSetting:     "invalid setting",
	CodeUpdateFailed:       "update failed",
	CodeNotLicensed:        "not licensed",
}

// Error is the only error type handlers return; it marshals to the contract's ApiErrorBody.
type Error struct {
	Code      string           `json:"code"`
	Message   string           `json:"message"`
	Field     string           `json:"field,omitempty"`
	Conflicts []HotkeyConflict `json:"conflicts,omitempty"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func newErr(code string) *Error { return &Error{Code: code, Message: messages[code]} }

func invalidSetting(field string) *Error {
	e := newErr(CodeInvalidSetting)
	e.Field = field
	return e
}

func badRequest() *Error { return newErr(CodeBadRequest) }

// decode strictly decodes params into P: unknown fields and trailing data are rejected.
func decode[P any](raw json.RawMessage) (P, error) {
	var p P
	if len(raw) == 0 {
		return p, badRequest()
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil || dec.More() {
		return p, badRequest()
	}
	return p, nil
}
