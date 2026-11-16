package webview

// navGate is the NavigationStarting policy (UI thread only). Only allowed origins are ever
// navigated to; after the first load the page may not navigate at all (reload, link, form,
// location change), so a document — and the backend state it owns — is only ever replaced by a
// host Reload, which the host issues after its reset job. Reload's one-shot permit is the single
// exception. Quarantine blocks everything for good.
type navGate struct {
	loaded      bool // first navigation completed
	permit      bool // one-shot: host Reload issued, its NavigationStarting not yet seen
	quarantined bool
}

// allow decides one NavigationStarting; originOK is the origin check of its URI.
func (g *navGate) allow(originOK bool) bool {
	permit := g.permit
	g.permit = false
	switch {
	case g.quarantined || !originOK:
		return false
	case !g.loaded:
		return true
	default:
		return permit
	}
}

// completed records a finished navigation. The permit is left alone: it belongs to the next
// NavigationStarting (WebView2 raises one for every Reload that returned S_OK), and a completion
// of an earlier, cancelled navigation may arrive in between.
func (g *navGate) completed() { g.loaded = true }
