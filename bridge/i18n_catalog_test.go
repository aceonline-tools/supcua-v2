package bridge

import (
	"strings"
	"testing"

	"github.com/yanmad27/supcua-v2/locale"
)

// The English the app shows today must be exactly the catalogue's English: the Go messages stay
// the fallback and log text, the catalogue is what the UI renders.
func TestCatalogEnglishMatchesBridgeMessages(t *testing.T) {
	c := locale.Embedded()
	for code, msg := range messages {
		e, ok := c["error."+strings.ToLower(code)]
		if !ok {
			t.Errorf("no catalogue entry for %s", code)
			continue
		}
		if e.EN.Plain != msg {
			t.Errorf("error.%s: catalogue en %q != bridge message %q", strings.ToLower(code), e.EN.Plain, msg)
		}
	}
	if got := c["error.update_stale"].EN.Plain; got != UpdateStaleMessage {
		t.Errorf("error.update_stale en %q != UpdateStaleMessage %q", got, UpdateStaleMessage)
	}
}
