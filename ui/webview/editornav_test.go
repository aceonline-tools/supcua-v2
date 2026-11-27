package webview

import (
	"testing"

	"github.com/yanmad27/supcua-v2/ui/webview/serve"
)

func TestEditorNavigationIsRestrictedToItsExactPage(t *testing.T) {
	o := serve.Origin
	ok := editorURL(7)
	for _, c := range []struct {
		uri  string
		want bool
	}{
		{ok, true},
		{editorURL(8), false}, // another generation
		{ok + "&x=1", false},  // extra query
		{ok + "#frag", false}, // fragment
		{ok + "#", false},     // empty fragment marker
		{"https://" + serve.Host + ":/index.html?window=editor&gen=7", false}, // empty explicit port
		{"https://" + serve.Host + ":443/index.html?window=editor&gen=7", false},
		{o + "/index.html", false}, // the main page
		{o + "/index.html?window=main&gen=7", false},
		{o + "/assets/app.js", false}, // any other asset navigation
		{"https://evil.example/index.html?window=editor&gen=7", false},
		{"about:blank", false},
		{"", false},
	} {
		if got := editorNavAllowed(c.uri, 7, false); got != c.want {
			t.Errorf("%q = %v want %v", c.uri, got, c.want)
		}
	}
}

func TestEditorNavigationFixtureExceptionOnlyWhenInstalled(t *testing.T) {
	f := serve.Origin + fixturePath
	if editorNavAllowed(f, 7, false) {
		t.Fatal("fixture page must be refused without the self-test fixture")
	}
	if !editorNavAllowed(f, 7, true) {
		t.Fatal("fixture page must be allowed when installed")
	}
	if editorNavAllowed(f+"?x=1", 7, true) {
		t.Fatal("fixture exception must be exact")
	}
}
