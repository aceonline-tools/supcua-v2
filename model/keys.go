package model

// keyNames mirrors executor's virtualKeyCodeMap (Windows-only) so key names can be
// validated on every platform; executor/keynames_windows_test.go guards drift.
var keyNames = func() map[string]bool {
	m := map[string]bool{}
	for _, n := range []string{
		"ctrl",
		"lctrl",
		"rctrl",
		"shift",
		"lshift",
		"rshift",
		"alt",
		"lalt",
		"ralt",
		"win",
		"enter",
		"return",
		"tab",
		"escape",
		"esc",
		"space",
		"backspace",
		"delete",
		"insert",
		"home",
		"end",
		"pageup",
		"pagedown",
		"up",
		"down",
		"left",
		"right",
		"f1",
		"f2",
		"f3",
		"f4",
		"f5",
		"f6",
		"f7",
		"f8",
		"f9",
		"f10",
		"f11",
		"f12",
		"0",
		"1",
		"2",
		"3",
		"4",
		"5",
		"6",
		"7",
		"8",
		"9",
		"a",
		"b",
		"c",
		"d",
		"e",
		"f",
		"g",
		"h",
		"i",
		"j",
		"k",
		"l",
		"m",
		"n",
		"o",
		"p",
		"q",
		"r",
		"s",
		"t",
		"u",
		"v",
		"w",
		"x",
		"y",
		"z",
	} {
		m[n] = true
	}
	return m
}()

// IsKnownKey reports whether name resolves to a key: a table name or a single-byte
// character (the executor resolves single characters byte-wise).
func IsKnownKey(name string) bool {
	return keyNames[name] || len(name) == 1
}

// KeyNames returns a copy of the named-key table.
func KeyNames() map[string]bool {
	out := make(map[string]bool, len(keyNames))
	for k := range keyNames {
		out[k] = true
	}
	return out
}
