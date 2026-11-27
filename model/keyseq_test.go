package model

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func marshal(t *testing.T, a Action) string {
	t.Helper()
	b, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestKeyPressEncoding(t *testing.T) {
	d := 80
	cases := map[string]struct {
		in   Action
		want string
	}{
		"legacy":      {Action{Type: ActionKeyPress, Key: "a"}, `{"type":"key_press","key":"a"}`},
		"single":      {Action{Type: ActionKeyPress, Keys: []KeyStroke{{Key: "a"}}}, `{"type":"key_press","key":"a"}`},
		"single+mods": {Action{Type: ActionKeyPress, Keys: []KeyStroke{{Key: "a", Mods: []string{"ctrl"}}}}, `{"type":"key_press","keys":[{"key":"a","mods":["ctrl"]}]}`},
		"multi":       {Action{Type: ActionKeyPress, Keys: []KeyStroke{{Key: "a"}, {Key: "b"}}, KeyDelayMs: &d}, `{"type":"key_press","keys":[{"key":"a"},{"key":"b"}],"key_delay_ms":80}`},
		"both":        {Action{Type: ActionKeyPress, Key: "x", Keys: []KeyStroke{{Key: "a"}, {Key: "b"}}}, `{"type":"key_press","keys":[{"key":"a"},{"key":"b"}]}`},
	}
	for name, c := range cases {
		if got := marshal(t, c.in); got != c.want {
			t.Errorf("%s: got %s want %s", name, got, c.want)
		}
	}
}

func TestKeyPressRoundTripNested(t *testing.T) {
	d := 0
	in := []Action{{Type: ActionLoop, LoopCount: 2, Actions: []Action{
		{Type: ActionIfImage, ThenActions: []Action{{Type: ActionKeyPress, Keys: []KeyStroke{{Key: "a", Mods: []string{"alt"}}, {Key: "b"}}, KeyDelayMs: &d}}},
		{Type: ActionKeyPress, Key: "q"},
	}}}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out []Action
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round trip differs:\n%s", b)
	}
}

func TestLegacyReadPrecedence(t *testing.T) {
	var a Action
	_ = json.Unmarshal([]byte(`{"type":"key_press","key":"x","keys":[{"key":"a"}]}`), &a)
	if s := a.Sequence(); len(s) != 1 || s[0].Key != "a" {
		t.Fatalf("keys must win: %+v", s)
	}
	_ = json.Unmarshal([]byte(`{"type":"key_press","key":"x"}`), &a)
	a.Keys = nil
	if s := a.Sequence(); len(s) != 1 || s[0].Key != "x" {
		t.Fatalf("legacy: %+v", s)
	}
}

func TestNormalizeKeys(t *testing.T) {
	ok := func(a Action) bool { return a.NormalizeKeys() }
	long := make([]KeyStroke, MaxKeySequence+1)
	for i := range long {
		long[i] = KeyStroke{Key: "a"}
	}
	neg, big, max := -1, MaxKeyDelayMs+1, MaxKeyDelayMs
	for name, c := range map[string]struct {
		a    Action
		want bool
	}{
		"empty":       {Action{Type: ActionKeyPress}, false},
		"too long":    {Action{Type: ActionKeyPress, Keys: long}, false},
		"max length":  {Action{Type: ActionKeyPress, Keys: long[:MaxKeySequence]}, true},
		"neg delay":   {Action{Type: ActionKeyPress, Key: "a", KeyDelayMs: &neg}, false},
		"big delay":   {Action{Type: ActionKeyPress, Key: "a", KeyDelayMs: &big}, false},
		"max delay":   {Action{Type: ActionKeyPress, Key: "a", KeyDelayMs: &max}, true},
		"bad mod":     {Action{Type: ActionKeyPress, Keys: []KeyStroke{{Key: "a", Mods: []string{"meta"}}}}, false},
		"dup mod":     {Action{Type: ActionKeyPress, Keys: []KeyStroke{{Key: "a", Mods: []string{"ctrl", "CTRL"}}}}, false},
		"empty key":   {Action{Type: ActionKeyPress, Keys: []KeyStroke{{Key: ""}}}, false},
		"key too big": {Action{Type: ActionKeyPress, Key: strings.Repeat("k", 33)}, false},
	} {
		if got := ok(c.a); got != c.want {
			t.Errorf("%s: got %v", name, got)
		}
	}
	a := Action{Type: ActionKeyPress, Keys: []KeyStroke{{Key: "a", Mods: []string{"SHIFT", "Ctrl"}}}}
	a.NormalizeKeys()
	if !reflect.DeepEqual(a.Keys[0].Mods, []string{"ctrl", "shift"}) {
		t.Fatalf("mods not canonical: %v", a.Keys[0].Mods)
	}
}
