package webview

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
	"unsafe"

	"github.com/jchv/go-webview2/webviewloader"
	"golang.org/x/sys/windows"
)

const (
	// RuntimeInstallerURL is the Evergreen bootstrapper download link.
	RuntimeInstallerURL = "https://go.microsoft.com/fwlink/p/?LinkId=2124703"
	runtimeFolderEnv    = "WEBVIEW2_BROWSER_EXECUTABLE_FOLDER"
)

// ErrRuntimeMissing is returned by CheckRuntime when no WebView2 runtime is installed.
var ErrRuntimeMissing = errors.New("WebView2 runtime is not installed")

// CheckRuntime returns the installed Evergreen runtime version, ErrRuntimeMissing when it
// is absent, or the loader error. WEBVIEW2_BROWSER_EXECUTABLE_FOLDER is honoured: a folder
// without msedgewebview2.exe counts as missing (also the test seam for -selftest-noruntime).
func CheckRuntime() (string, error) {
	if dir := os.Getenv(runtimeFolderEnv); dir != "" {
		if _, err := os.Stat(filepath.Join(dir, "msedgewebview2.exe")); err != nil {
			return "", ErrRuntimeMissing
		}
	}
	v, err := webviewloader.GetInstalledVersion()
	if err != nil {
		return "", fmt.Errorf("WebView2 detection: %w", err)
	}
	if v == "" {
		return "", ErrRuntimeMissing
	}
	return v, nil
}

// RuntimeMissingText is the user-facing message (kept free of Go error text).
func RuntimeMissingText() string {
	return "supcua needs the Microsoft Edge WebView2 Runtime, which is not installed on this PC.\n\n" +
		"Download the Evergreen installer from:\n" + RuntimeInstallerURL +
		"\n\nChoose Yes to open the download page now, then start the app again."
}

// StartupFailureText explains a runtime that is present but did not start.
func StartupFailureText() string {
	return "supcua could not start its WebView2 user interface (the runtime did not respond in time).\n\n" +
		"Reinstall or repair the Microsoft Edge WebView2 Runtime:\n" + RuntimeInstallerURL +
		"\n\nChoose Yes to open the download page now, then start the app again."
}

// ShowRuntimeMissing shows the native MessageBox (Yes opens the installer link). It is
// the only place the pump is intentionally blocked: call it before any window exists.
func ShowRuntimeMissing() { showInstallerBox("WebView2 Runtime missing", RuntimeMissingText()) }

// ShowStartupFailure is the same box for a runtime that failed or timed out.
func ShowStartupFailure() { showInstallerBox("WebView2 failed to start", StartupFailureText()) }

// ShowLoadTimeout tells the user the window never rendered the app, with where to look. It
// blocks until dismissed (modal MessageBox); call it off the UI thread.
func ShowLoadTimeout(logPath, stage string) {
	t, _ := windows.UTF16PtrFromString("supcua-v2 is taking unusually long to load its window.\n\nIt keeps trying and this message closes by itself if it succeeds. Last startup stage: " + stage + "\n\nIf the window stays blank, close the app and send this log file:\n" + logPath)
	windows.MessageBox(0, t, loadTimeoutTitle, windows.MB_OK|windows.MB_ICONWARNING|windows.MB_TOPMOST)
}

const loadTimeoutTitleText = "supcua-v2 is still loading"

var loadTimeoutTitle = func() *uint16 { p, _ := windows.UTF16PtrFromString(loadTimeoutTitleText); return p }()

var (
	procFindWindowEx       = user32.NewProc("FindWindowExW")
	procGetWindowThreadPID = user32.NewProc("GetWindowThreadProcessId")
)

// closeOurLoadBoxes posts WM_CLOSE to every top-level #32770 dialog of THIS process with the
// load-timeout title. It walks the windows with FindWindowEx (no callback: the process's
// NewCallback count is a self-test invariant).
func closeOurLoadBoxes() {
	const wmClose = 0x0010
	class, _ := windows.UTF16PtrFromString("#32770")
	var prev uintptr
	for i := 0; i < 16; i++ {
		hwnd, _, _ := procFindWindowEx.Call(0, prev, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(loadTimeoutTitle)))
		if hwnd == 0 {
			return
		}
		var pid uint32
		procGetWindowThreadPID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if pid == windows.GetCurrentProcessId() {
			procPostMessage.Call(hwnd, wmClose, 0, 0)
		}
		prev = hwnd
	}
}

// DismissLoadTimeout closes the ShowLoadTimeout box of this process if it is open (the page
// mounted after all). It keeps looking for up to 2 s, so a box created just after the mount
// (the deadline raced it) is still closed. Blocking and bounded: call it from a goroutine.
func DismissLoadTimeout() {
	deadline := time.Now().Add(2 * time.Second)
	for {
		closeOurLoadBoxes()
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func showInstallerBox(title, text string) {
	t, _ := windows.UTF16PtrFromString(text)
	c, _ := windows.UTF16PtrFromString(title)
	ret, err := windows.MessageBox(0, t, c, windows.MB_YESNO|windows.MB_ICONERROR|windows.MB_TOPMOST)
	if err == nil && ret == 6 { // 6 = IDYES
		verb, _ := windows.UTF16PtrFromString("open")
		url, _ := windows.UTF16PtrFromString(RuntimeInstallerURL)
		_ = windows.ShellExecute(0, verb, url, nil, nil, windows.SW_SHOWNORMAL)
	}
}

var (
	user32            = windows.NewLazySystemDLL("user32.dll")
	kernel32          = windows.NewLazySystemDLL("kernel32.dll")
	procPostMessage   = user32.NewProc("PostMessageW")
	procGuiResources  = user32.NewProc("GetGuiResources")
	procHandleCount   = kernel32.NewProc("GetProcessHandleCount")
	procAttachConsole = kernel32.NewProc("AttachConsole")
)

// Resources is a GDI/USER/handle sample for leak checks.
type Resources struct {
	GDI     uint32 `json:"gdi"`
	User    uint32 `json:"user"`
	Handles uint32 `json:"handles"`
}

// SampleResources reads GetGuiResources (GDI, USER) and GetProcessHandleCount.
func SampleResources() Resources {
	p := uintptr(windows.CurrentProcess())
	gdi, _, _ := procGuiResources.Call(p, 0)
	usr, _, _ := procGuiResources.Call(p, 1)
	var h uint32
	procHandleCount.Call(p, uintptr(unsafe.Pointer(&h)))
	return Resources{GDI: uint32(gdi), User: uint32(usr), Handles: h}
}

// AttachParentConsole lets a GUI-subsystem exe print to its parent's console (self-test).
func AttachParentConsole() { procAttachConsole.Call(^uintptr(0)) }
