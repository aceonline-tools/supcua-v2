package main

import (
	"os"
	"path/filepath"
	"unsafe"

	"github.com/jchv/go-webview2/webviewloader"
	"golang.org/x/sys/windows"
)

const (
	runtimeInstallerURL = "https://go.microsoft.com/fwlink/p/?LinkId=2124703"
	runtimeFolderEnv    = "WEBVIEW2_BROWSER_EXECUTABLE_FOLDER"
)

// detectRuntime returns the installed Evergreen WebView2 runtime version, or
// "" when it is missing. It honours WEBVIEW2_BROWSER_EXECUTABLE_FOLDER.
func detectRuntime() (string, error) {
	if dir := os.Getenv(runtimeFolderEnv); dir != "" {
		// Fixed-version/override folder: usable only if it really holds the browser.
		if _, err := os.Stat(filepath.Join(dir, "msedgewebview2.exe")); err != nil {
			return "", nil
		}
	}
	return webviewloader.GetInstalledVersion()
}

func missingRuntimeText() string {
	return "supcua needs the Microsoft Edge WebView2 Runtime, which is not installed on this PC.\n\n" +
		"Download the Evergreen installer from:\n" + runtimeInstallerURL +
		"\n\nChoose Yes to open the download page now, then start the app again."
}

// showMissingRuntime shows the native MessageBox (Yes opens the installer).
// It runs before any window exists, so the modal loop cannot starve anything.
func showMissingRuntime() {
	text, _ := windows.UTF16PtrFromString(missingRuntimeText())
	title, _ := windows.UTF16PtrFromString("WebView2 Runtime missing")
	ret, err := windows.MessageBox(0, text, title, windows.MB_YESNO|windows.MB_ICONERROR|windows.MB_TOPMOST)
	if err == nil && ret == 6 { // 6 = IDYES
		verb, _ := windows.UTF16PtrFromString("open")
		url, _ := windows.UTF16PtrFromString(runtimeInstallerURL)
		_ = windows.ShellExecute(0, verb, url, nil, nil, windows.SW_SHOWNORMAL)
	}
}

var (
	user32            = windows.NewLazySystemDLL("user32.dll")
	kernel32          = windows.NewLazySystemDLL("kernel32.dll")
	procGuiResources  = user32.NewProc("GetGuiResources")
	procHandleCount   = kernel32.NewProc("GetProcessHandleCount")
	procAttachConsole = kernel32.NewProc("AttachConsole")
)

type resources struct {
	GDI     uint32 `json:"gdi"`
	User    uint32 `json:"user"`
	Handles uint32 `json:"handles"`
}

func sampleResources() resources {
	p := uintptr(windows.CurrentProcess())
	gdi, _, _ := procGuiResources.Call(p, 0)
	usr, _, _ := procGuiResources.Call(p, 1)
	var h uint32
	procHandleCount.Call(p, uintptr(unsafe.Pointer(&h)))
	return resources{GDI: uint32(gdi), User: uint32(usr), Handles: h}
}

func attachParentConsole() { procAttachConsole.Call(^uintptr(0)) }
