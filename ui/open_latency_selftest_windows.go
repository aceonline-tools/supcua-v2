//go:build windows

package ui

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unsafe"

	"github.com/lxn/win"

	"github.com/yanmad27/supcua-v2/otrace"
)

type mouseInput struct {
	typ   uint32
	_     uint32
	dx    int32
	dy    int32
	data  uint32
	flags uint32
	time  uint32
	_     uint32
	extra uintptr
}

const (
	inputMouse       = 0
	mouseEventLeftDn = 0x0002
	mouseEventLeftUp = 0x0004
)

// SendMouseButton sends one left-button event at the current cursor position.
func SendMouseButton(up bool) error {
	fl := uint32(mouseEventLeftDn)
	if up {
		fl = mouseEventLeftUp
	}
	in := mouseInput{typ: inputMouse, flags: fl}
	n, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	if n != 1 {
		return err
	}
	return nil
}

// clickAt moves the real cursor to the screen point and clicks it (clicks=2 is a double-click). It
// returns the time just before the button-up that completes the last click.
func clickAt(x, y, clicks int) (time.Time, error) {
	if !win.SetCursorPos(int32(x), int32(y)) {
		return time.Time{}, fmt.Errorf("SetCursorPos(%d,%d) failed", x, y)
	}
	time.Sleep(60 * time.Millisecond)
	var t time.Time
	for i := 0; i < clicks; i++ {
		if err := SendMouseButton(false); err != nil {
			return t, err
		}
		time.Sleep(15 * time.Millisecond)
		t = time.Now()
		if err := SendMouseButton(true); err != nil {
			return t, err
		}
		if i+1 < clicks {
			time.Sleep(40 * time.Millisecond)
		}
	}
	return t, nil
}

// elemJS returns the centre of the first element matching the selector (optionally whose text starts
// with the prefix) in CSS pixels, after scrolling it into view; the host converts it to screen pixels.
func elemJS(sel, prefix string) string {
	return fmt.Sprintf(`(function(){var l=[].slice.call(document.querySelectorAll(%q));var e=null;
for(var i=0;i<l.length;i++){if(!%q||(l[i].textContent||'').trim().indexOf(%q)===0){e=l[i];break}}
if(!e)return null;e.scrollIntoView({block:'nearest'});var r=e.getBoundingClientRect();
return {x:r.left+r.width/2,y:r.top+r.height/2,dpr:window.devicePixelRatio,disabled:!!e.disabled}})()`, sel, prefix, prefix)
}

func (s *SelfTest) screenPoint(sel, prefix string) (x, y int, err error) {
	r, err := s.drive(fmt.Sprintf("pt-%d", time.Now().UnixNano()), elemJS(sel, prefix), 5*time.Second)
	if err != nil {
		return 0, 0, err
	}
	var p *struct {
		X, Y, Dpr float64
		Disabled  bool
	}
	if err := json.Unmarshal(r.V, &p); err != nil || p == nil {
		return 0, 0, fmt.Errorf("element %q %q not found", sel, prefix)
	}
	if p.Disabled {
		return 0, 0, fmt.Errorf("element %q is disabled", sel)
	}
	pt := win.POINT{}
	win.ClientToScreen(win.HWND(s.UI.HWND()), &pt)
	return int(pt.X) + int(math.Round(p.X*p.Dpr)), int(pt.Y) + int(math.Round(p.Y*p.Dpr)), nil
}

// frameStats classifies a BGRA frame: dominant is the share of the single most common colour, white
// the share of near-white pixels. A frame that is uniform or mostly white is blank.
type frameStats struct {
	W, H     int
	Dominant float64
	White    float64
}

func (f frameStats) Blank() bool { return f.W == 0 || f.Dominant > 0.985 || f.White > 0.97 }
func (f frameStats) String() string {
	return fmt.Sprintf("%dx%d dominant=%.3f white=%.3f", f.W, f.H, f.Dominant, f.White)
}

func analyzeFrame(pix []byte, w, h int) frameStats {
	st := frameStats{W: w, H: h}
	if w*h == 0 || len(pix) < w*h*4 {
		return st
	}
	cnt := map[uint32]int{}
	white := 0
	for i := 0; i < w*h; i++ {
		b, g, r := pix[i*4], pix[i*4+1], pix[i*4+2]
		cnt[uint32(b)|uint32(g)<<8|uint32(r)<<16]++
		if b >= 0xF4 && g >= 0xF4 && r >= 0xF4 {
			white++
		}
	}
	top := 0
	for _, c := range cnt {
		if c > top {
			top = c
		}
	}
	st.Dominant, st.White = float64(top)/float64(w*h), float64(white)/float64(w*h)
	return st
}

func captureClient(h uintptr) ([]byte, int, int, error) {
	var cr win.RECT
	if !win.GetClientRect(win.HWND(h), &cr) {
		return nil, 0, 0, fmt.Errorf("GetClientRect failed")
	}
	pt := win.POINT{}
	win.ClientToScreen(win.HWND(h), &pt)
	w, ht := int(cr.Right-cr.Left), int(cr.Bottom-cr.Top)
	if w <= 0 || ht <= 0 {
		return nil, 0, 0, fmt.Errorf("empty client %dx%d", w, ht)
	}
	pix, err := captureScreen(int(pt.X), int(pt.Y), w, ht)
	return pix, w, ht, err
}

type openTrial struct {
	Kind        string
	Visible     time.Duration
	Usable      time.Duration
	Frame       frameStats
	AnyBlank    bool
	BlankFrames int
	Stages      string
	err         error
}

const readyJS = `(function(){var n=document.getElementById('ed-name');var m=window.__editorMetrics?window.__editorMetrics():null;
return {name:n?n.value:null,measuring:m?m.measuring:true,w:innerWidth,h:innerHeight}})()`

// openByClick clicks the real main-page control(s) and times the editor's appearance and readiness.
func (s *SelfTest) openByClick(kind, wantName string, shotName string) openTrial {
	tr := openTrial{Kind: kind}
	main := win.HWND(s.UI.HWND())
	win.ShowWindow(main, win.SW_RESTORE)
	win.SetForegroundWindow(main)
	time.Sleep(150 * time.Millisecond)
	row := func() (int, int, error) { return s.screenPoint(".card.table .tr.row", wantName) }
	var clicks int
	var x, y int
	var err error
	switch kind {
	case "new":
		x, y, err = s.screenPoint(".page-head button.primary", "")
		clicks = 1
	case "dblclick":
		x, y, err = row()
		clicks = 2
	case "edit":
		var rx, ry int
		if rx, ry, err = row(); err == nil {
			if _, err = clickAt(rx, ry, 1); err == nil {
				time.Sleep(250 * time.Millisecond)
				x, y, err = s.screenPoint(".toolbar button", "Edit")
			}
		}
		clicks = 1
	}
	if err != nil {
		tr.err = err
		return tr
	}
	otrace.Reset()
	otrace.At("armed")
	type vis struct {
		t  time.Time
		h  uintptr
		fr frameStats
		px []byte
		ok bool
	}
	visCh := make(chan vis, 1)
	stop := make(chan struct{})
	go func() {
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			select {
			case <-stop:
				return
			default:
			}
			if ed := s.UI.Editor(); ed != nil {
				if hw := ed.HWND(); hw != 0 && win.IsWindowVisible(win.HWND(hw)) {
					v := vis{t: time.Now(), h: hw}
					if px, w, h, err := captureClient(hw); err == nil {
						v.px, v.fr, v.ok = px, analyzeFrame(px, w, h), true
					}
					visCh <- v
					return
				}
			}
			time.Sleep(time.Millisecond)
		}
		visCh <- vis{}
	}()
	t0, err := clickAt(x, y, clicks)
	if err != nil {
		close(stop)
		tr.err = err
		return tr
	}
	v := <-visCh
	if v.h == 0 {
		tr.err = fmt.Errorf("editor never became visible")
		return tr
	}
	tr.Visible, tr.Frame = v.t.Sub(t0), v.fr
	if !v.ok || v.fr.Blank() {
		tr.AnyBlank = true
		tr.BlankFrames++
	}
	if dir := os.Getenv(edShotsEnv); dir != "" && shotName != "" && v.ok {
		_ = os.MkdirAll(dir, 0o755)
		savePNG(filepath.Join(dir, shotName+".png"), v.px, v.fr.W, v.fr.H, 1)
	}
	// Usable: the page reports the real content (name field, out of measure mode) AND a screen
	// capture of the editor right then is not blank.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := s.driveEd(readyJS, 3*time.Second)
		if err == nil {
			var r struct {
				Name      *string
				Measuring bool
			}
			_ = json.Unmarshal(raw, &r)
			if r.Name != nil && !r.Measuring && (wantName == "" && kind == "new" || *r.Name == wantName) {
				px, w, h, cerr := captureClient(v.h)
				if cerr == nil {
					fr := analyzeFrame(px, w, h)
					if !fr.Blank() {
						tr.Usable = time.Since(t0)
						break
					}
					tr.AnyBlank, tr.BlankFrames = true, tr.BlankFrames+1
				}
			}
		}
	}
	tr.Stages = s.stageLine(t0)
	return tr
}

const marksJS = `(function(){var o=performance.timeOrigin,r={};performance.getEntriesByType('mark').forEach(function(m){if(m.name.indexOf('eo:')===0)r[m.name.slice(3)]=o+m.startTime});
var n=performance.getEntriesByType('navigation')[0];if(n){r.dcl=o+n.domContentLoadedEventEnd;r.nav=o+n.responseStart}return r})()`

// stageLine merges the host marks and the page marks into one ordered "name +ms" line from the click.
func (s *SelfTest) stageLine(t0 time.Time) string {
	type m struct {
		n string
		t float64
	}
	var all []m
	rel := func(t time.Time) float64 { return ms(t.Sub(t0)) }
	for _, k := range otrace.Marks() {
		all = append(all, m{k.Name, rel(k.At)})
	}
	if raw, err := s.driveEd(marksJS, 3*time.Second); err == nil {
		var pm map[string]float64
		if json.Unmarshal(raw, &pm) == nil {
			for k, v := range pm {
				all = append(all, m{"page:" + k, v - float64(t0.UnixNano())/1e6})
			}
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].t < all[j].t })
	var b strings.Builder
	for _, x := range all {
		fmt.Fprintf(&b, " %s=%.0f", x.n, x.t)
	}
	return b.String()
}

func (s *SelfTest) closeEditorAndWait() bool {
	ed := s.UI.Editor()
	if ed == nil {
		return true
	}
	h := ed.HWND()
	win.PostMessage(win.HWND(h), win.WM_CLOSE, 0, 0)
	_, gone := s.waitEditorGone(h, 15*time.Second)
	return gone && waitFor(5*time.Second, func() bool { return !s.EditOpen() })
}

// openLatency clicks the real main-page New button, double-clicks a script row and clicks Edit, n+1
// times each (the first open of each kind, then n repeats), and reports click→visible and
// click→usable plus the stage breakdown.
func (s *SelfTest) openLatency(scriptName string, step func(string, bool, string) bool) {
	otrace.Enable()
	const repeats = 5
	for _, kind := range []string{"new", "dblclick", "edit"} {
		var first openTrial
		var vis, use []float64
		okAll, blank := true, 0
		for i := 0; i <= repeats; i++ {
			want := scriptName
			if kind == "new" {
				want = ""
			}
			shot := ""
			if i == 0 {
				shot = "open-first-frame-" + kind
			}
			tr := s.openByClick(kind, want, shot)
			if tr.err != nil || tr.Usable == 0 {
				step(fmt.Sprintf("open latency: %s #%d", kind, i), false, fmt.Sprint(tr.err))
				okAll = false
				s.closeEditorAndWait()
				continue
			}
			fmt.Printf("open-latency %s #%d visible=%.0fms usable=%.0fms firstFrame=[%s] blankFrames=%d |%s\n",
				kind, i, ms(tr.Visible), ms(tr.Usable), tr.Frame, tr.BlankFrames, tr.Stages)
			blank += tr.BlankFrames
			if i == 0 {
				first = tr
			} else {
				vis, use = append(vis, ms(tr.Visible)), append(use, ms(tr.Usable))
			}
			if !s.closeEditorAndWait() {
				step("open latency: close "+kind, false, "editor did not close")
				return
			}
			time.Sleep(400 * time.Millisecond)
		}
		vm, vx := medMax(vis)
		um, ux := medMax(use)
		step("open latency: "+kind, okAll, fmt.Sprintf("first visible=%.0fms usable=%.0fms | repeat n=%d visible med=%.0f max=%.0f usable med=%.0f max=%.0f ms | blank frames=%d",
			ms(first.Visible), ms(first.Usable), len(vis), vm, vx, um, ux, blank))
	}
}
