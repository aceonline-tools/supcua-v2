package bridge

import (
	"github.com/yanmad27/supcua-v2/model"
)

// maxDraftBytes bounds every draft kind: checkpoint, failed-save draft, recovered file.
const maxDraftBytes = 1 << 20

type checkpoint struct {
	Revision uint64
	Script   model.Script
	Valid    bool
}

type checkpointResult struct {
	Applied  bool   `json:"applied"`
	Revision uint64 `json:"revision"`
}

// editorCheckpoint stores the newest draft the page reported so a hung renderer cannot lose edits
// (contract §6.2). Stale revisions are a no-op success; invalid scripts are kept (valid=false).
func (b *Bridge) editorCheckpoint(p struct {
	SessionID string       `json:"sessionId"`
	Revision  uint64       `json:"revision"`
	Script    model.Script `json:"script"`
}) (any, error) {
	if p.Revision == 0 || p.Revision > maxRevision {
		return nil, newErr(CodeBadRequest)
	}
	if draftBytes(p.Script) > maxDraftBytes { // the one compact measure, shared by every draft kind
		return nil, newErr(CodeDraftTooLarge)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	sess := b.session
	if sess == nil || sess.id != p.SessionID || p.SessionID == "" {
		return nil, newErr(CodeEditorClosed)
	}
	if sess.isClosing() {
		return nil, newErr(CodeSessionClosing)
	}
	if p.Script.ID != sess.scriptID {
		return nil, newErr(CodeBadRequest)
	}
	stored := sess.lastSavedRev
	if sess.checkpoint != nil && sess.checkpoint.Revision > stored {
		stored = sess.checkpoint.Revision
	}
	if p.Revision <= stored {
		return checkpointResult{Applied: false, Revision: stored}, nil
	}
	valid := validateScript(&p.Script, b.known) == nil
	sess.checkpoint = &checkpoint{Revision: p.Revision, Script: p.Script, Valid: valid}
	if p.Revision > sess.dirtyRev {
		sess.dirtyRev = p.Revision
	}
	sess.publishLocked()
	return checkpointResult{Applied: true, Revision: p.Revision}, nil
}

// newestDraftLocked is the highest-revision unsaved draft: the checkpoint or the last failed save.
func (s *editorSession) newestDraftLocked() (script model.Script, rev uint64, ok bool) {
	if s.checkpoint != nil && s.checkpoint.Revision > s.lastSavedRev {
		script, rev, ok = s.checkpoint.Script, s.checkpoint.Revision, true
	}
	if s.draft != nil && s.draftRev > s.lastSavedRev && (!ok || s.draftRev >= rev) {
		script, rev, ok = *s.draft, s.draftRev, true
	}
	return
}
