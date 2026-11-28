//go:build windows

package ui

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yanmad27/supcua-v2/updater"
)

func (s *SelfTest) ucstep(name string, ok bool, detail string) bool {
	return s.Res.Step("check for updates: "+name, ok, detail)
}

// updateCheckSuite clicks the real "Check for updates" button in the Settings page against a
// loopback GitHub stub (updater.SetAPIBaseForSelftest accepts only http://127.0.0.1). It never
// clicks Install: no update is applied.
func (s *SelfTest) updateCheckSuite() {
	var mu sync.Mutex
	tag := "v0.0.0"
	var hits atomic.Int32
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		s.ucstep("start stub", false, err.Error())
		return
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		time.Sleep(400 * time.Millisecond) // keeps the first check in flight while the others arrive
		mu.Lock()
		t := tag
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"tag_name":%q,"draft":false,"prerelease":false,"assets":[{"name":"supcua-v2.exe","browser_download_url":"https://github.com/aceonline-tools/supcua-v2/releases/download/%s/supcua-v2.exe","size":10}]}`, t, t)
	})}
	go srv.Serve(ln)
	defer srv.Close()
	if err := updater.SetAPIBaseForSelftest("http://" + ln.Addr().String()); err != nil {
		s.ucstep("point the updater at the stub", false, err.Error())
		return
	}

	str := func(tag, expr string) string {
		r, err := s.drive(tag, expr, 5*time.Second)
		var out string
		if err != nil || json.Unmarshal(r.V, &out) != nil {
			return ""
		}
		return out
	}
	status := func() string {
		return str("uc-status", `(function(){var e=document.querySelector('.upd-ctl .upd-status');return e?e.textContent:""})()`)
	}
	buttonLabel := func() string {
		return str("uc-btn", `(function(){var b=document.querySelector('.upd-ctl button.upd-btn');return b?(b.getAttribute('aria-label')+'|'+(b.disabled?'disabled':'enabled')):""})()`)
	}
	click := func() bool {
		r, err := s.drive("uc-click", `(function(){var b=document.querySelector('.upd-ctl button[aria-label="Check for updates"]');if(!b||b.disabled)return false;b.click();return true})()`, 5*time.Second)
		var ok bool
		return err == nil && json.Unmarshal(r.V, &ok) == nil && ok
	}

	if _, err := s.drive("uc-nav", `(function(){var b=document.querySelector('button.nav[aria-label="Settings"]');if(!b)return false;b.click();return true})()`, 5*time.Second); err != nil {
		s.ucstep("open Settings", false, err.Error())
		return
	}
	if !waitFor(5*time.Second, func() bool { return buttonLabel() == "Check for updates|enabled" }) {
		s.ucstep("button present in Settings", false, "label="+buttonLabel())
		return
	}

	// Scenario 1: stub serves v0.0.0; the build is 0.0.0-selftest, so it is up to date. While the
	// click's request is in flight, two more RPC checks must join it (single-flight).
	if !click() {
		s.ucstep("click", false, "the button was not clickable")
		return
	}
	waitFor(3*time.Second, func() bool { return status() == "Checking…" })
	s.ucstep("checking state disables the button", buttonLabel() == "Check for updates|disabled", "button="+buttonLabel())
	joined := str("uc-join", `(async function(){var r=await Promise.all([__st.call('update.check'),__st.call('update.check')]);return r.map(function(x){return x.status}).join(',')})()`)
	var final string
	waitFor(5*time.Second, func() bool { final = status(); return strings.HasPrefix(final, "Up to date (") })
	s.ucstep(`stub v0.0.0 -> "Up to date (v…)"`, strings.HasPrefix(final, "Up to date (") && strings.HasSuffix(final, ")") && strings.Contains(final, "0.0.0-selftest"), fmt.Sprintf("status=%q", final))
	s.ucstep("concurrent checks share one request (single-flight)", hits.Load() == 1 && joined == "up_to_date,up_to_date", fmt.Sprintf("stub hits=%d joined=%q", hits.Load(), joined))

	// Scenario 2: stub serves v9.9.9 -> update available, offering Install (never clicked).
	mu.Lock()
	tag = "v9.9.9"
	mu.Unlock()
	if !click() {
		s.ucstep("second click", false, "the button was not clickable")
		return
	}
	waitFor(5*time.Second, func() bool { return strings.HasPrefix(status(), "Update available") })
	got := status()
	s.ucstep(`stub v9.9.9 -> "Update available v9.9.9"`, got == "Update available v9.9.9", fmt.Sprintf("status=%q", got))
	s.ucstep("Install action is offered, no dialog, nothing applied", buttonLabel() == "Install update|enabled" &&
		str("uc-dlg", `document.querySelector('[role="dialog"]')?'dialog':'none'`) == "none", "button="+buttonLabel())
}
