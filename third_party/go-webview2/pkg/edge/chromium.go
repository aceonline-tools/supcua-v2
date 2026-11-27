//go:build windows
// +build windows

package edge

import (
	"errors"
	"log"
	"fmt"
	"time"
	"os"
	"path/filepath"
	"sync/atomic"
	"unsafe"

	"github.com/jchv/go-webview2/internal/w32"
	"golang.org/x/sys/windows"
)

type Chromium struct {
	hwnd                  uintptr
	focusOnInit           bool
	controller            *ICoreWebView2Controller
	webview               *ICoreWebView2
	inited                uintptr
	envCompleted          *iCoreWebView2CreateCoreWebView2EnvironmentCompletedHandler
	controllerCompleted   *iCoreWebView2CreateCoreWebView2ControllerCompletedHandler
	webMessageReceived    *iCoreWebView2WebMessageReceivedEventHandler
	permissionRequested   *iCoreWebView2PermissionRequestedEventHandler
	webResourceRequested  *iCoreWebView2WebResourceRequestedEventHandler
	acceleratorKeyPressed *ICoreWebView2AcceleratorKeyPressedEventHandler
	navigationCompleted   *ICoreWebView2NavigationCompletedEventHandler
	navigationStarting    *iCoreWebView2NavigationStartingEventHandler  // PATCH
	newWindowRequested    *iCoreWebView2NewWindowRequestedEventHandler  // PATCH
	initErr               error                                         // PATCH
	processFailed         *iCoreWebView2ProcessFailedEventHandler       // PATCH
	cancelled             uint32                                        // PATCH: set when init was abandoned; late completions must not recreate resources

	environment *ICoreWebView2Environment

	// Settings
	DataPath string

	// permissions
	permissions      map[CoreWebView2PermissionKind]CoreWebView2PermissionState
	globalPermission *CoreWebView2PermissionState

	// Callbacks
	MessageCallback              func(string)
	WebResourceRequestedCallback func(request *ICoreWebView2WebResourceRequest, args *ICoreWebView2WebResourceRequestedEventArgs)
	NavigationCompletedCallback  func(sender *ICoreWebView2, args *ICoreWebView2NavigationCompletedEventArgs)
	AcceleratorKeyCallback       func(uint) bool

	// PATCH: return false to cancel the navigation. Runs on the UI thread, must not block.
	NavigationStartingCallback func(uri string) bool
	// PATCH: a security handler failed (e.g. a navigation could not be cancelled). Runs on the
	// UI thread inside a WebView2 event: post the exit, do not block.
	FatalCallback func(err error)
	// PATCH: browser/renderer process failure (kind = COREWEBVIEW2_PROCESS_FAILED_KIND).
	ProcessFailedCallback func(kind int32)
	// PATCH: popups are always denied; this only observes.
	NewWindowDeniedCallback func(uri string)
	// PATCH: runs once, on the UI thread inside a WebView2 completion callback, when controller
	// creation finished (err == nil) or failed. Post work from it, never block. Used by the
	// non-blocking EmbedFromEnvironment.
	InitDoneCallback func(err error)
}

func NewChromium() *Chromium {
	e := &Chromium{}
	/*
	 All these handlers are passed to native code through syscalls with 'uintptr(unsafe.Pointer(handler))' and we know
	 that a pointer to those will be kept in the native code. Furthermore these handlers als contain pointer to other Go
	 structs like the vtable.
	 This violates the unsafe.Pointer rule '(4) Conversion of a Pointer to a uintptr when calling syscall.Syscall.' because
	 theres no guarantee that Go doesn't move these objects.
	 AFAIK currently the Go runtime doesn't move HEAP objects, so we should be safe with these handlers. But they don't
	 guarantee it, because in the future Go might use a compacting GC.
	 There's a proposal to add a runtime.Pin function, to prevent moving pinned objects, which would allow to easily fix
	 this issue by just pinning the handlers. The https://go-review.googlesource.com/c/go/+/367296/ should land in Go 1.19.
	*/
	e.envCompleted = newICoreWebView2CreateCoreWebView2EnvironmentCompletedHandler(e)
	e.controllerCompleted = newICoreWebView2CreateCoreWebView2ControllerCompletedHandler(e)
	e.webMessageReceived = newICoreWebView2WebMessageReceivedEventHandler(e)
	e.permissionRequested = newICoreWebView2PermissionRequestedEventHandler(e)
	e.webResourceRequested = newICoreWebView2WebResourceRequestedEventHandler(e)
	e.acceleratorKeyPressed = newICoreWebView2AcceleratorKeyPressedEventHandler(e)
	e.navigationCompleted = newICoreWebView2NavigationCompletedEventHandler(e)
	e.navigationStarting = newICoreWebView2NavigationStartingEventHandler(e)
	e.newWindowRequested = newICoreWebView2NewWindowRequestedEventHandler(e)
	e.processFailed = newICoreWebView2ProcessFailedEventHandler(e)
	e.permissions = make(map[CoreWebView2PermissionKind]CoreWebView2PermissionState)

	return e
}

// Embed is EmbedTimeout with a 60 s bound.
func (e *Chromium) Embed(hwnd uintptr) bool {
	return e.EmbedTimeout(hwnd, 60*time.Second) == nil
}

// PATCH: EmbedTimeout creates the WebView2 environment+controller inside hwnd. It pumps
// messages on the calling (UI) thread until the controller exists, fails, or timeout
// elapses. Must not be called from inside a WebView2 callback.
func (e *Chromium) EmbedTimeout(hwnd uintptr, timeout time.Duration) error {
	e.hwnd = hwnd

	dataPath := e.DataPath
	if dataPath == "" {
		currentExePath := make([]uint16, windows.MAX_PATH)
		if _, err := windows.GetModuleFileName(windows.Handle(0), &currentExePath[0], windows.MAX_PATH); err != nil {
			return err
		}
		dataPath = filepath.Join(os.Getenv("AppData"), filepath.Base(windows.UTF16ToString(currentExePath)))
	}

	// PATCH: any failure below abandons the init: resources created so far are released and
	// a late completion callback is ignored (cancelled).
	var embedErr error
	defer func() {
		if embedErr != nil {
			atomic.StoreUint32(&e.cancelled, 1)
			e.Close()
		}
	}()
	res, err := createCoreWebView2EnvironmentWithOptions(nil, windows.StringToUTF16Ptr(dataPath), 0, e.envCompleted)
	if err != nil {
		embedErr = fmt.Errorf("WebView2 loader: %w", err)
		return embedErr
	}
	if int32(res) < 0 {
		embedErr = fmt.Errorf("CreateCoreWebView2EnvironmentWithOptions: HRESULT 0x%08X", uint32(res))
		return embedErr
	}
	deadline := time.Now().Add(timeout)
	var msg w32.Msg
	for atomic.LoadUintptr(&e.inited) == 0 {
		left := time.Until(deadline)
		if left <= 0 {
			embedErr = ErrEmbedTimeout
			return embedErr
		}
		// Wait for input (or a posted message) without blocking past the deadline.
		_, _, _ = procMsgWaitForMultipleObjects.Call(0, 0, 0, uintptr(left.Milliseconds()+1), qsAllInput)
		for {
			r, _, _ := procPeekMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0, pmRemove)
			if r == 0 {
				break
			}
			if msg.Message == wmQuit {
				embedErr = errors.New("quit received while creating WebView2")
				return embedErr
			}
			_, _, _ = w32.User32TranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
			_, _, _ = w32.User32DispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
		}
	}
	if e.initErr != nil {
		embedErr = e.initErr
		return embedErr
	}
	return nil
}

func (e *Chromium) Navigate(url string) {
	_, _, _ = e.webview.vtbl.Navigate.Call(
		uintptr(unsafe.Pointer(e.webview)),
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(url))),
	)
}

func (e *Chromium) NavigateToString(htmlContent string) {
	_, _, _ = e.webview.vtbl.NavigateToString.Call(
		uintptr(unsafe.Pointer(e.webview)),
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(htmlContent))),
	)
}

func (e *Chromium) Init(script string) {
	_, _, _ = e.webview.vtbl.AddScriptToExecuteOnDocumentCreated.Call(
		uintptr(unsafe.Pointer(e.webview)),
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(script))),
		0,
	)
}

func (e *Chromium) Eval(script string) {
	_script, err := windows.UTF16PtrFromString(script)
	if err != nil { // PATCH: no log.Fatal
		return
	}

	_, _, _ = e.webview.vtbl.ExecuteScript.Call(
		uintptr(unsafe.Pointer(e.webview)),
		uintptr(unsafe.Pointer(_script)),
		0,
	)
}

func (e *Chromium) Show() error {
	return e.controller.PutIsVisible(true)
}

func (e *Chromium) Hide() error {
	return e.controller.PutIsVisible(false)
}

func (e *Chromium) QueryInterface(_, _ uintptr) uintptr {
	return 0
}

func (e *Chromium) AddRef() uintptr {
	return 1
}

func (e *Chromium) Release() uintptr {
	return 1
}

func (e *Chromium) EnvironmentCompleted(res uintptr, env *ICoreWebView2Environment) uintptr {
	if atomic.LoadUint32(&e.cancelled) != 0 { // PATCH: init abandoned (timeout/failure)
		return 0
	}
	if int32(res) < 0 { // PATCH: no log.Fatalf
		e.initErr = fmt.Errorf("creating environment failed: HRESULT 0x%08X", uint32(res))
		e.markInited()
		return 0
	}
	_, _, _ = env.vtbl.AddRef.Call(uintptr(unsafe.Pointer(env)))
	e.environment = env

	r, _, _ := env.vtbl.CreateCoreWebView2Controller.Call( // PATCH: fail fast on the creation HRESULT
		uintptr(unsafe.Pointer(env)),
		e.hwnd,
		uintptr(unsafe.Pointer(e.controllerCompleted)),
	)
	if int32(r) < 0 {
		e.initErr = fmt.Errorf("CreateCoreWebView2Controller: HRESULT 0x%08X", uint32(r))
		e.markInited()
	}
	return 0
}

func (e *Chromium) CreateCoreWebView2ControllerCompleted(res uintptr, controller *ICoreWebView2Controller) uintptr {
	if atomic.LoadUint32(&e.cancelled) != 0 { // PATCH: late completion after timeout: do not keep it
		if int32(res) >= 0 && controller != nil {
			_, _, _ = controller.vtbl.Close.Call(uintptr(unsafe.Pointer(controller)))
		}
		return 0
	}
	if int32(res) < 0 { // PATCH: no log.Fatalf
		e.initErr = fmt.Errorf("creating controller failed: HRESULT 0x%08X", uint32(res))
		e.markInited()
		return 0
	}
	_, _, _ = controller.vtbl.AddRef.Call(uintptr(unsafe.Pointer(controller)))
	e.controller = controller

	var token _EventRegistrationToken
	// PATCH: every step is HRESULT-checked; any failure aborts init (fail closed).
	fail := func(op string, r uintptr) bool {
		if int32(r) >= 0 {
			return false
		}
		e.initErr = fmt.Errorf("%s: HRESULT 0x%08X", op, uint32(r))
		e.markInited()
		return true
	}
	r, _, _ := controller.vtbl.GetCoreWebView2.Call(
		uintptr(unsafe.Pointer(controller)),
		uintptr(unsafe.Pointer(&e.webview)), // out-param already holds one reference (released in Close)
	)
	if fail("get_CoreWebView2", r) || e.webview == nil {
		if e.initErr == nil {
			e.initErr = errors.New("get_CoreWebView2 returned no webview")
			e.markInited()
		}
		return 0
	}
	add := func(op string, call ComProc, h unsafe.Pointer) bool {
		r, _, _ := call.Call(uintptr(unsafe.Pointer(e.webview)), uintptr(h), uintptr(unsafe.Pointer(&token)))
		return !fail(op, r)
	}
	if !add("add_WebMessageReceived", e.webview.vtbl.AddWebMessageReceived, unsafe.Pointer(e.webMessageReceived)) ||
		!add("add_PermissionRequested", e.webview.vtbl.AddPermissionRequested, unsafe.Pointer(e.permissionRequested)) ||
		!add("add_WebResourceRequested", e.webview.vtbl.AddWebResourceRequested, unsafe.Pointer(e.webResourceRequested)) ||
		!add("add_NavigationCompleted", e.webview.vtbl.AddNavigationCompleted, unsafe.Pointer(e.navigationCompleted)) ||
		!add("add_NavigationStarting", e.webview.vtbl.AddNavigationStarting, unsafe.Pointer(e.navigationStarting)) ||
		!add("add_NewWindowRequested", e.webview.vtbl.AddNewWindowRequested, unsafe.Pointer(e.newWindowRequested)) ||
		!add("add_ProcessFailed", e.webview.vtbl.AddProcessFailed, unsafe.Pointer(e.processFailed)) {
		return 0
	}
	r, _, _ = e.controller.vtbl.AddAcceleratorKeyPressed.Call(
		uintptr(unsafe.Pointer(e.controller)),
		uintptr(unsafe.Pointer(e.acceleratorKeyPressed)),
		uintptr(unsafe.Pointer(&token)),
	)
	if fail("add_AcceleratorKeyPressed", r) {
		return 0
	}

	e.markInited()

	if e.focusOnInit {
		e.Focus()
	}

	return 0
}

func (e *Chromium) MessageReceived(sender *ICoreWebView2, args *iCoreWebView2WebMessageReceivedEventArgs) uintptr {
	var message *uint16
	_, _, _ = args.vtbl.TryGetWebMessageAsString.Call(
		uintptr(unsafe.Pointer(args)),
		uintptr(unsafe.Pointer(&message)),
	)
	if e.MessageCallback != nil {
		e.MessageCallback(w32.Utf16PtrToString(message))
	}
	// PATCH: upstream echoed every message back to the page; removed.
	windows.CoTaskMemFree(unsafe.Pointer(message))
	return 0
}

func (e *Chromium) SetPermission(kind CoreWebView2PermissionKind, state CoreWebView2PermissionState) {
	e.permissions[kind] = state
}

func (e *Chromium) SetGlobalPermission(state CoreWebView2PermissionState) {
	e.globalPermission = &state
}

func (e *Chromium) PermissionRequested(_ *ICoreWebView2, args *iCoreWebView2PermissionRequestedEventArgs) uintptr {
	var kind CoreWebView2PermissionKind
	_, _, _ = args.vtbl.GetPermissionKind.Call(
		uintptr(unsafe.Pointer(args)),
		uintptr(kind),
	)
	var result CoreWebView2PermissionState
	if e.globalPermission != nil {
		result = *e.globalPermission
	} else {
		var ok bool
		result, ok = e.permissions[kind]
		if !ok {
			result = CoreWebView2PermissionStateDefault
		}
	}
	_, _, _ = args.vtbl.PutState.Call(
		uintptr(unsafe.Pointer(args)),
		uintptr(result),
	)
	return 0
}

func (e *Chromium) WebResourceRequested(sender *ICoreWebView2, args *ICoreWebView2WebResourceRequestedEventArgs) uintptr {
	// PATCH: HRESULT-checked GetRequest, no log.Fatal; failures leave the request unanswered.
	var req *ICoreWebView2WebResourceRequest
	r, _, _ := args.vtbl.GetRequest.Call(uintptr(unsafe.Pointer(args)), uintptr(unsafe.Pointer(&req)))
	if int32(r) < 0 || req == nil {
		return 0
	}
	defer comRelease(uintptr(unsafe.Pointer(req)))
	if e.WebResourceRequestedCallback != nil {
		e.WebResourceRequestedCallback(req, args)
	}
	return 0
}

func (e *Chromium) AddWebResourceRequestedFilter(filter string, ctx COREWEBVIEW2_WEB_RESOURCE_CONTEXT) {
	if err := e.webview.AddWebResourceRequestedFilter(filter, ctx); err != nil {
		log.Printf("AddWebResourceRequestedFilter: %v", err) // PATCH: was log.Fatal
	}
}

func (e *Chromium) Environment() *ICoreWebView2Environment {
	return e.environment
}

// AcceleratorKeyPressed is called when an accelerator key is pressed.
// If the AcceleratorKeyCallback method has been set, it will defer handling of the keypress
// to the callback. That callback returns a bool indicating if the event was handled.
func (e *Chromium) AcceleratorKeyPressed(sender *ICoreWebView2Controller, args *ICoreWebView2AcceleratorKeyPressedEventArgs) uintptr {
	if e.AcceleratorKeyCallback == nil {
		return 0
	}
	eventKind, _ := args.GetKeyEventKind()
	if eventKind == COREWEBVIEW2_KEY_EVENT_KIND_KEY_DOWN ||
		eventKind == COREWEBVIEW2_KEY_EVENT_KIND_SYSTEM_KEY_DOWN {
		virtualKey, _ := args.GetVirtualKey()
		status, _ := args.GetPhysicalKeyStatus()
		if !status.WasKeyDown {
			_ = args.PutHandled(e.AcceleratorKeyCallback(virtualKey))
			return 0
		}
	}
	_ = args.PutHandled(false)
	return 0
}

func (e *Chromium) GetSettings() (*ICoreWebViewSettings, error) {
	return e.webview.GetSettings()
}

func (e *Chromium) GetController() *ICoreWebView2Controller {
	return e.controller
}

func boolToInt(input bool) int {
	if input {
		return 1
	}
	return 0
}

func (e *Chromium) NavigationCompleted(sender *ICoreWebView2, args *ICoreWebView2NavigationCompletedEventArgs) uintptr {
	if e.NavigationCompletedCallback != nil {
		e.NavigationCompletedCallback(sender, args)
	}
	return 0
}

func (e *Chromium) NotifyParentWindowPositionChanged() error {
	//It looks like the wndproc function is called before the controller initialization is complete.
	//Because of this the controller is nil
	if e.controller == nil {
		return nil
	}
	return e.controller.NotifyParentWindowPositionChanged()
}

func (e *Chromium) Focus() {
	if e.controller == nil {
		e.focusOnInit = true
		return
	}
	_ = e.controller.MoveFocus(COREWEBVIEW2_MOVE_FOCUS_REASON_PROGRAMMATIC)
}
