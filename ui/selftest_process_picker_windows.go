//go:build windows

package ui

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/lxn/win"

	"github.com/yanmad27/supcua-v2/winlist"
)

const pickerShotsEnv = "SUPCUA_PICKER_SHOTS"

const helperExe = "powershell.exe"

var procGetWindowTextW = syscall.NewLazyDLL("user32.dll").NewProc("GetWindowTextW")

func startWindowHelper(title string) (*exec.Cmd, error) {
	script := "Add-Type -AssemblyName System.Windows.Forms;$f=New-Object System.Windows.Forms.Form;$f.Text='" + title +
		"';$f.StartPosition='CenterScreen';$f.Width=420;$f.Height=160;[System.Windows.Forms.Application]::Run($f)"
	u := utf16.Encode([]rune(script))
	b := make([]byte, 0, len(u)*2)
	for _, c := range u {
		b = append(b, byte(c), byte(c>>8))
	}
	cmd := exec.Command(helperExe, "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-EncodedCommand", base64.StdEncoding.EncodeToString(b))
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	return cmd, cmd.Start()
}

func findWindowByTitle(title string) win.HWND {
	p, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return 0
	}
	return win.FindWindow(nil, p)
}

func foregroundTitle() string {
	h := win.GetForegroundWindow()
	buf := make([]uint16, 256)
	n, _, _ := procGetWindowTextW.Call(uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf[:n])
}

// activate brings hwnd to the foreground and reports whether GetForegroundWindow confirms it.
func activate(hwnd win.HWND) bool {
	for attempt := 0; attempt < 3; attempt++ {
		win.ShowWindow(hwnd, win.SW_RESTORE)
		if win.SetForegroundWindow(hwnd) && win.GetForegroundWindow() == hwnd {
			return true
		}
		fg := win.GetForegroundWindow()
		var fgPid uint32
		fgThread := win.GetWindowThreadProcessId(fg, &fgPid)
		self := int32(win.GetCurrentThreadId())
		if fgThread != 0 && win.AttachThreadInput(self, int32(fgThread), true) {
			win.SetForegroundWindow(hwnd)
			win.AttachThreadInput(self, int32(fgThread), false)
		}
		if waitFor(time.Second, func() bool { return win.GetForegroundWindow() == hwnd }) {
			return true
		}
	}
	return false
}

// processPickerSuite proves the Settings process picker end to end under the production policy:
// a helper process with a unique window title is listed, picked through the real UI, saved, and
// then auto-active follows its real foreground state.
func (s *SelfTest) processPickerSuite(h ProductionHooks, step func(string, bool, string) bool) {
	name := func(n string) string { return "process picker: " + n }
	title := fmt.Sprintf("supcua-picker-%d", time.Now().UnixNano())
	helper, err := startWindowHelper(title)
	if err != nil {
		step(name("start helper process"), false, err.Error())
		return
	}
	defer func() { _ = helper.Process.Kill(); _ = helper.Wait() }()
	var helperWnd win.HWND
	if !step(name("helper window with a unique title appears"), waitFor(20*time.Second, func() bool { helperWnd = findWindowByTitle(title); return helperWnd != 0 }), title) {
		return
	}

	mainWnd := win.HWND(s.UI.HWND())
	if !step(name("main window is the foreground window before picking (GetForegroundWindow)"), activate(mainWnd), "foreground="+foregroundTitle()) {
		return
	}

	t0 := time.Now()
	direct := winlist.List(winlist.SystemSource{}, winlist.Self(), winlist.Budget, nil)
	elapsed := time.Since(t0)
	found := false
	for _, r := range direct.Rows {
		found = found || (strings.EqualFold(r.Exe, helperExe) && r.Title == title)
	}
	step(name("enumeration finds the helper (exe basename + title) within 250 ms"), found && elapsed <= winlist.Budget && !direct.Truncated,
		fmt.Sprintf("enumeration_ms=%d rows=%d truncated=%v", elapsed.Milliseconds(), len(direct.Rows), direct.Truncated))

	str := func(tag, expr string, d time.Duration) string {
		r, err := s.drive(tag, expr, d)
		var out string
		if err != nil || json.Unmarshal(r.V, &out) != nil {
			return "ERR:" + fmt.Sprint(err)
		}
		return out
	}
	gotoPage := func(label string) string {
		return str("pp-nav", `(function(){var b=document.querySelector('button.nav[aria-label="`+label+`"]');if(!b)return "nonav";b.click();return "ok"})()`, 5*time.Second)
	}
	savedName := func() string {
		return str("pp-saved", `(async function(){return (await __st.call('settings.get')).auto_active_process_name})()`, 5*time.Second)
	}
	fieldValue := func() string {
		return str("pp-field", `(async function(){for(var i=0;i<30;i++){var f=document.querySelector('input[aria-label="Game process name"]');if(f)return f.value;await __st.sleep(100)}return "nofield"})()`, 8*time.Second)
	}
	if r := gotoPage("Settings"); r != "ok" {
		step(name("open Settings"), false, r)
		return
	}
	q := fmt.Sprintf("%q", title)
	listed := str("pp-open", `(async function(){var b=document.querySelector('button[aria-label="Pick from running processes"]');if(!b)return "nobutton";b.click();
var f=null;for(var i=0;i<30&&!f;i++){f=document.querySelector('input[aria-label="Filter processes"]');await __st.sleep(50)}
if(!f)return "nofilter";f.value=`+q+`;f.dispatchEvent(new Event('input',{bubbles:true}));
for(var j=0;j<60;j++){var rows=[].slice.call(document.querySelectorAll('.picker-row'));
if(rows.some(function(r){return r.querySelector('.picker-title').textContent===`+q+`}))return "listed";await __st.sleep(100)}
return "norow:"+document.querySelector('.picker-body').textContent})()`, 15*time.Second)
	if !step(name("picker opened through the UI lists the helper row"), listed == "listed", listed) {
		return
	}
	if dir := os.Getenv(pickerShotsEnv); dir != "" {
		time.Sleep(300 * time.Millisecond)
		var rc win.RECT
		var pt win.POINT
		win.GetClientRect(win.HWND(s.UI.HWND()), &rc)
		win.ClientToScreen(win.HWND(s.UI.HWND()), &pt)
		_ = os.MkdirAll(dir, 0o755)
		if px, err := captureScreen(int(pt.X), int(pt.Y), int(rc.Right-rc.Left), int(rc.Bottom-rc.Top)); err == nil {
			savePNG(filepath.Join(dir, "picker-open.png"), px, int(rc.Right-rc.Left), int(rc.Bottom-rc.Top), 1)
		}
	}
	openRes := str("pp-pick", `(function(){var row=[].slice.call(document.querySelectorAll('.picker-row')).filter(function(r){return r.querySelector('.picker-title').textContent===`+q+`})[0];
if(!row)return "norow";var exe=row.querySelector('.picker-exe').textContent;row.click();return "picked:"+exe})()`, 5*time.Second)
	exe, picked := strings.CutPrefix(openRes, "picked:")
	if !step(name("the helper row is picked in the UI"), picked && strings.EqualFold(exe, helperExe), openRes) {
		return
	}
	saved := ""
	waitFor(5*time.Second, func() bool { saved = savedName(); return strings.EqualFold(saved, helperExe) })
	step(name("saved auto_active_process_name is the exe basename"), strings.EqualFold(saved, helperExe) && !strings.ContainsAny(saved, `/\`), "saved="+saved)

	gotoPage("Scripts")
	time.Sleep(200 * time.Millisecond)
	gotoPage("Settings")
	field := fieldValue()
	step(name("saved basename shown after reopening Settings"), strings.EqualFold(field, helperExe) && strings.EqualFold(savedName(), helperExe), "field="+field)

	rt := str("pp-rtt", `(async function(){var t=performance.now();var r=await __st.call('processes.list');return JSON.stringify({ms:Math.round(performance.now()-t),rows:r.rows.length,truncated:r.truncated,keys:Object.keys(r.rows[0]||{}).join(",")})})()`, 5*time.Second)
	step(name("processes.list RPC round trip returns names and titles only"), strings.Contains(rt, `\"keys\":\"exe,title\"`) || strings.Contains(rt, `"keys":"exe,title"`), "rpc="+rt)

	if !step(name("global active is off while another process holds the foreground"), waitFor(4*time.Second, func() bool { return !h.GlobalActive() }), "") {
		return
	}
	if !step(name("helper window is the foreground window (GetForegroundWindow)"), activate(helperWnd), "foreground="+foregroundTitle()) {
		return
	}
	step(name("auto-active turns on while the picked process is in the foreground"), waitFor(6*time.Second, h.GlobalActive), "")
	if !step(name("main window regains the foreground (GetForegroundWindow)"), activate(mainWnd), "foreground="+foregroundTitle()) {
		return
	}
	step(name("auto-active turns off when the foreground leaves"), waitFor(6*time.Second, func() bool { return !h.GlobalActive() }), "")
}
