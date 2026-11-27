package model

import (
	"fmt"

	"github.com/google/uuid"
)

// CloneScript returns an independent copy of source: new ID, a unique
// "<name> (copy)" / "(copy N)" name, disabled, with all hotkeys cleared.
// nameTaken reports whether a candidate name is already in use.
func CloneScript(source Script, nameTaken func(name string) bool) Script {
	clone := source
	clone.ID = uuid.New().String()
	clone.Enabled = false
	clone.ToggleHotkey = nil
	clone.StartHotkey = nil
	clone.StopHotkey = nil
	clone.Actions = cloneActions(source.Actions)

	candidateName := source.Name + " (copy)"
	for copyNumber := 2; nameTaken != nil && nameTaken(candidateName); copyNumber++ {
		candidateName = fmt.Sprintf("%s (copy %d)", source.Name, copyNumber)
	}
	clone.Name = candidateName
	return clone
}

func cloneActions(actions []Action) []Action {
	if actions == nil {
		return nil
	}
	clonedActions := make([]Action, len(actions))
	for index, action := range actions {
		clonedAction := action
		clonedAction.Actions = cloneActions(action.Actions)
		clonedAction.ThenActions = cloneActions(action.ThenActions)
		clonedAction.ElseActions = cloneActions(action.ElseActions)
		clonedAction.Keys = cloneKeys(action.Keys)
		if action.KeyDelayMs != nil {
			delayCopy := *action.KeyDelayMs
			clonedAction.KeyDelayMs = &delayCopy
		}
		if action.SearchRegion != nil {
			regionCopy := *action.SearchRegion
			clonedAction.SearchRegion = &regionCopy
		}
		clonedActions[index] = clonedAction
	}
	return clonedActions
}

func cloneKeys(keys []KeyStroke) []KeyStroke {
	if keys == nil {
		return nil
	}
	out := make([]KeyStroke, len(keys))
	for i, k := range keys {
		out[i] = KeyStroke{Key: k.Key, Mods: append([]string(nil), k.Mods...)}
	}
	return out
}
