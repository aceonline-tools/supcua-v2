package webview

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/yanmad27/supcua-v2/ui/webview/serve"
)

// fixturePath is the self-test fixture page, served only to a window created with a fixture.
const fixturePath = "/__selftest/editor-fixture.html"

// editorURL is the one destination the editor window may navigate to (contract §2.1).
func editorURL(gen uint64) string {
	return fmt.Sprintf("%s/%s?window=editor&gen=%d", serve.Origin, serve.Index, gen)
}

// sameDestination reports whether got is want: same scheme, host, path and exact query; no
// fragment (an empty "#" marker included), userinfo or port (an empty ":" included).
func sameDestination(got, want string) bool {
	g, err1 := url.Parse(got)
	w, err2 := url.Parse(want)
	if err1 != nil || err2 != nil || g.User != nil || g.Opaque != "" || g.Fragment != "" || g.RawFragment != "" ||
		strings.Contains(got, "#") || g.Host != g.Hostname() { // any fragment marker (even empty); any explicit port (even empty)
		return false
	}
	return g.Scheme == w.Scheme && strings.EqualFold(g.Hostname(), w.Hostname()) && g.Path == w.Path && g.RawQuery == w.RawQuery
}

// editorNavAllowed is the editor's NavigationStarting origin check: exactly its own page, or the
// self-test fixture when one is installed.
func editorNavAllowed(uri string, gen uint64, fixture bool) bool {
	if fixture && sameDestination(uri, serve.Origin+fixturePath) {
		return true
	}
	return sameDestination(uri, editorURL(gen))
}

// The editor's compact-density minimum window size in CSS px (bridge.MinCssW × MinCssH).
const editorMinCssW, editorMinCssH = 720, 480
