package bridge

import (
	"testing"

	"github.com/yanmad27/supcua-v2/model"
)

func TestKeyPressValidationAndDescribe(t *testing.T) {
	known := knownActionTypes()
	d := 100
	good := []model.Action{{Type: model.ActionLoop, LoopCount: 1, Actions: []model.Action{
		{Type: model.ActionKeyPress, Keys: []model.KeyStroke{{Key: "a", Mods: []string{"Shift", "ctrl"}}, {Key: "b"}}, KeyDelayMs: &d},
	}}}
	c, i := 0, 0
	if err := validateActions(good, known, 0, &c, &i); err != nil {
		t.Fatalf("valid rejected: %v", err)
	}
	inner := good[0].Actions[0]
	if got := describeAction(inner); got != "Key Press: ctrl+shift+a → b" {
		t.Fatalf("describe: %q", got)
	}
	if got := describeAction(model.Action{Type: model.ActionKeyPress, Key: "7"}); got != "Key Press: 7" {
		t.Fatalf("legacy describe: %q", got)
	}
	bad := -1
	for name, a := range map[string]model.Action{
		"empty":     {Type: model.ActionKeyPress},
		"neg delay": {Type: model.ActionKeyPress, Key: "a", KeyDelayMs: &bad},
	} {
		c, i := 0, 0
		if validateActions([]model.Action{a}, known, 0, &c, &i) == nil {
			t.Errorf("%s accepted", name)
		}
	}
}
