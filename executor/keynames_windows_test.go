//go:build windows

package executor

import (
	"testing"

	"github.com/yanmad27/supcua-v2/model"
)

func TestModelKeyNamesMatchVirtualKeyMap(t *testing.T) {
	names := model.KeyNames()
	for k := range virtualKeyCodeMap {
		if !names[k] {
			t.Errorf("model key table lacks %q", k)
		}
	}
	for k := range names {
		if _, ok := virtualKeyCodeMap[k]; !ok {
			t.Errorf("virtualKeyCodeMap lacks %q", k)
		}
	}
}
