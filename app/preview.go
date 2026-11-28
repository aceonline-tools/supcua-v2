package app

import "github.com/yanmad27/supcua-v2/app/status"

// overlayPreview is a presentation-only override of what the overlay shows between a lone RCtrl
// key-down and its release. It never touches controller state, script slots or the gates.
type overlayPreview struct {
	token uint64
	epoch uint64
	base  status.Snapshot // real state when the preview began; any difference drops the preview
	shown status.Snapshot
}

// SetPreviewNotifier sets the function that repaints the overlay when a preview begins or ends
// without a state change. fn must not block and is called without locks held.
func (appController *Controller) SetPreviewNotifier(fn func()) {
	appController.stateMutex.Lock()
	appController.previewNotifier = fn
	appController.stateMutex.Unlock()
}

func (appController *Controller) clearPreviewLocked() bool {
	had := appController.preview != nil
	appController.preview = nil
	return had
}

func (appController *Controller) previewCleared(had bool) {
	if !had {
		return
	}
	appController.stateMutex.RLock()
	fn := appController.previewNotifier
	appController.stateMutex.RUnlock()
	if fn != nil {
		fn()
	}
}

// One preview exists at a time: the latest begun wins and an earlier token becomes a no-op. A
// commit never clears a preview by itself: a real state change drops it (OverlaySnapshot compares
// the base), and whoever began it ends it by token (CancelPreview), so one hotkey's gated or
// unchanged release cannot take another hotkey's preview away.

// beginPreview shows `shown(base)` on the overlay if the gates are open now, under the same lock
// the gates are checked with. It returns the token that cancels it, or 0 when nothing is shown.
func (appController *Controller) beginPreview(epoch uint64, shown func(status.Snapshot) (status.Snapshot, bool)) uint64 {
	appController.stateMutex.Lock()
	if !appController.hotkeyAllowedLocked(epoch) || appController.editDialogOpen {
		appController.stateMutex.Unlock()
		return 0
	}
	base := appController.snapshotLocked()
	snap, ok := shown(base)
	if !ok {
		appController.stateMutex.Unlock()
		return 0
	}
	appController.previewSeq++
	tok := appController.previewSeq
	appController.preview = &overlayPreview{token: tok, epoch: epoch, base: base, shown: snap}
	fn := appController.previewNotifier
	appController.stateMutex.Unlock()
	if fn != nil {
		fn()
	}
	return tok
}

// BeginTogglePreview previews the global toggle (DISABLE <-> IDLE).
func (appController *Controller) BeginTogglePreview(epoch uint64) uint64 {
	return appController.beginPreview(epoch, func(b status.Snapshot) (status.Snapshot, bool) {
		b.GlobalActive = !b.GlobalActive
		if !b.GlobalActive {
			b.RunningScriptID, b.RunningScriptName = "", ""
		}
		return b, true
	})
}

// BeginStopPreview previews stopping scriptID; it shows nothing unless that script is running.
func (appController *Controller) BeginStopPreview(scriptID string, epoch uint64) uint64 {
	return appController.beginPreview(epoch, func(b status.Snapshot) (status.Snapshot, bool) {
		if b.RunningScriptID == "" || b.RunningScriptID != scriptID {
			return b, false
		}
		b.RunningScriptID, b.RunningScriptName = "", ""
		return b, true
	})
}

// CancelPreview ends the preview identified by token (a no-op if it already ended or was replaced).
func (appController *Controller) CancelPreview(token uint64) {
	appController.stateMutex.Lock()
	had := appController.preview != nil && appController.preview.token == token
	if had {
		appController.preview = nil
	}
	appController.stateMutex.Unlock()
	appController.previewCleared(had)
}

// OverlaySnapshot is what the overlay draws: the preview while it is still valid, else the real
// state. A preview whose base state changed underneath, or whose gates closed, is dropped here, so
// it can never outlive the state it was computed from.
func (appController *Controller) OverlaySnapshot() status.Snapshot {
	appController.stateMutex.RLock()
	p := appController.preview
	if p == nil {
		defer appController.stateMutex.RUnlock()
		return appController.snapshotLocked()
	}
	real := appController.snapshotLocked()
	valid := real == p.base && appController.hotkeyAllowedLocked(p.epoch)
	shown := p.shown
	appController.stateMutex.RUnlock()
	if valid {
		return shown
	}
	appController.stateMutex.Lock()
	if appController.preview == p {
		appController.preview = nil
	}
	real = appController.snapshotLocked()
	appController.stateMutex.Unlock()
	return real
}
