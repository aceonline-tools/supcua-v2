package webview

import "testing"

func TestNavGateBlocksPageNavigationAfterFirstLoad(t *testing.T) {
	var g navGate
	if !g.allow(true) {
		t.Fatal("initial app navigation must be allowed")
	}
	if g.allow(false) {
		t.Fatal("foreign origin must be blocked even before load")
	}
	g.completed()
	if g.allow(true) {
		t.Fatal("page-initiated navigation after the first load must be cancelled")
	}
}

func TestNavGateHostReloadPermitIsOneShot(t *testing.T) {
	g := navGate{loaded: true}
	g.permit = true
	if g.allow(false) {
		t.Fatal("a permit never allows a foreign origin")
	}
	if g.allow(true) {
		t.Fatal("the permit was consumed by the previous NavigationStarting")
	}
	g.permit = true
	if !g.allow(true) {
		t.Fatal("the host reload must be allowed")
	}
	if g.allow(true) {
		t.Fatal("a second navigation must not reuse the permit")
	}
	g.permit = true
	g.completed() // a cancelled navigation's completion arrives before the reload starts
	if !g.allow(true) {
		t.Fatal("the permit must survive an unrelated completion")
	}
}

func TestNavGateQuarantineBlocksEverything(t *testing.T) {
	g := navGate{quarantined: true, permit: true}
	if g.allow(true) {
		t.Fatal("quarantine blocks the initial navigation and any permit")
	}
}
