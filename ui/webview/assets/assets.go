// Package assets embeds the built frontend. The wire-up step copies frontend/dist into
// ui/webview/assets/dist before building; the committed placeholder keeps the package
// building without the frontend.
package assets

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embedded embed.FS

// FS returns the frontend root (index.html at the top level).
func FS() fs.FS {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic(err) // dist is a compile-time embed; cannot fail
	}
	return sub
}
