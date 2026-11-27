//go:build windows

// PATCH (supcua): additions on top of jchv/go-webview2. Every COM vtable below is a
// package-level var, so syscall.NewCallback runs only at package init (fixed count).
// Every HRESULT is checked from the call's return value, not from GetLastError.

package edge

import (
	"errors"
	"fmt"
	"unsafe"

	"github.com/jchv/go-webview2/internal/w32"
	"golang.org/x/sys/windows"
)

func hrErr(op string, r uintptr) error {
	if int32(r) < 0 {
		return fmt.Errorf("%s: HRESULT 0x%08X", op, uint32(r))
	}
	return nil
}

// comRelease calls IUnknown::Release on a raw COM interface pointer (vtable slot 2).
// The address is reinterpreted (not converted) so vet's uintptr->Pointer rule is not tripped;
// the object is owned by native code and never moves.
func comRelease(p uintptr) {
	if p == 0 {
		return
	}
	obj := *(*unsafe.Pointer)(unsafe.Pointer(&p))
	vtbl := *(*unsafe.Pointer)(obj)
	rel := *(*uintptr)(unsafe.Add(vtbl, 2*unsafe.Sizeof(uintptr(0))))
	_, _, _ = ComProc(rel).Call(p)
}

func readCoString(call func(out **uint16) uintptr, op string) (string, error) {
	var p *uint16
	if err := hrErr(op, call(&p)); err != nil {
		return "", err
	}
	s := windows.UTF16PtrToString(p)
	windows.CoTaskMemFree(unsafe.Pointer(p))
	return s, nil
}

// ---- NavigationStarting ----

type _ICoreWebView2NavigationStartingEventArgsVtbl struct {
	_IUnknownVtbl
	GetUri             ComProc
	GetIsUserInitiated ComProc
	GetIsRedirected    ComProc
	GetRequestHeaders  ComProc
	GetCancel          ComProc
	PutCancel          ComProc
	GetNavigationId    ComProc
}

type ICoreWebView2NavigationStartingEventArgs struct {
	vtbl *_ICoreWebView2NavigationStartingEventArgsVtbl
}

func (i *ICoreWebView2NavigationStartingEventArgs) URI() (string, error) {
	return readCoString(func(out **uint16) uintptr {
		r, _, _ := i.vtbl.GetUri.Call(uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(out)))
		return r
	}, "NavigationStarting.get_Uri")
}

func (i *ICoreWebView2NavigationStartingEventArgs) Cancel() error {
	r, _, _ := i.vtbl.PutCancel.Call(uintptr(unsafe.Pointer(i)), 1)
	return hrErr("NavigationStarting.put_Cancel", r)
}

type _ICoreWebView2NavigationStartingEventHandlerVtbl struct {
	_IUnknownVtbl
	Invoke ComProc
}

type iCoreWebView2NavigationStartingEventHandlerImpl interface {
	_IUnknownImpl
	NavigationStarting(sender *ICoreWebView2, args *ICoreWebView2NavigationStartingEventArgs) uintptr
}

type iCoreWebView2NavigationStartingEventHandler struct {
	vtbl *_ICoreWebView2NavigationStartingEventHandlerVtbl
	impl iCoreWebView2NavigationStartingEventHandlerImpl
}

func _navStartQI(this *iCoreWebView2NavigationStartingEventHandler, refiid, object uintptr) uintptr {
	return this.impl.QueryInterface(refiid, object)
}
func _navStartAddRef(this *iCoreWebView2NavigationStartingEventHandler) uintptr {
	return this.impl.AddRef()
}
func _navStartRelease(this *iCoreWebView2NavigationStartingEventHandler) uintptr {
	return this.impl.Release()
}
func _navStartInvoke(this *iCoreWebView2NavigationStartingEventHandler, sender *ICoreWebView2, args *ICoreWebView2NavigationStartingEventArgs) uintptr {
	return this.impl.NavigationStarting(sender, args)
}

var _navStartingFn = _ICoreWebView2NavigationStartingEventHandlerVtbl{
	_IUnknownVtbl{
		NewComProc(_navStartQI),
		NewComProc(_navStartAddRef),
		NewComProc(_navStartRelease),
	},
	NewComProc(_navStartInvoke),
}

func newICoreWebView2NavigationStartingEventHandler(impl iCoreWebView2NavigationStartingEventHandlerImpl) *iCoreWebView2NavigationStartingEventHandler {
	return &iCoreWebView2NavigationStartingEventHandler{vtbl: &_navStartingFn, impl: impl}
}

// ---- NewWindowRequested ----

type _ICoreWebView2NewWindowRequestedEventArgsVtbl struct {
	_IUnknownVtbl
	GetUri             ComProc
	PutNewWindow       ComProc
	GetNewWindow       ComProc
	PutHandled         ComProc
	GetHandled         ComProc
	GetIsUserInitiated ComProc
	GetDeferral        ComProc
	GetWindowFeatures  ComProc
}

type ICoreWebView2NewWindowRequestedEventArgs struct {
	vtbl *_ICoreWebView2NewWindowRequestedEventArgsVtbl
}

func (i *ICoreWebView2NewWindowRequestedEventArgs) URI() (string, error) {
	return readCoString(func(out **uint16) uintptr {
		r, _, _ := i.vtbl.GetUri.Call(uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(out)))
		return r
	}, "NewWindowRequested.get_Uri")
}

// Deny marks the request handled without supplying a new window: the popup is dropped.
func (i *ICoreWebView2NewWindowRequestedEventArgs) Deny() error {
	r, _, _ := i.vtbl.PutHandled.Call(uintptr(unsafe.Pointer(i)), 1)
	return hrErr("NewWindowRequested.put_Handled", r)
}

type _ICoreWebView2NewWindowRequestedEventHandlerVtbl struct {
	_IUnknownVtbl
	Invoke ComProc
}

type iCoreWebView2NewWindowRequestedEventHandlerImpl interface {
	_IUnknownImpl
	NewWindowRequested(sender *ICoreWebView2, args *ICoreWebView2NewWindowRequestedEventArgs) uintptr
}

type iCoreWebView2NewWindowRequestedEventHandler struct {
	vtbl *_ICoreWebView2NewWindowRequestedEventHandlerVtbl
	impl iCoreWebView2NewWindowRequestedEventHandlerImpl
}

func _newWinQI(this *iCoreWebView2NewWindowRequestedEventHandler, refiid, object uintptr) uintptr {
	return this.impl.QueryInterface(refiid, object)
}
func _newWinAddRef(this *iCoreWebView2NewWindowRequestedEventHandler) uintptr {
	return this.impl.AddRef()
}
func _newWinRelease(this *iCoreWebView2NewWindowRequestedEventHandler) uintptr {
	return this.impl.Release()
}
func _newWinInvoke(this *iCoreWebView2NewWindowRequestedEventHandler, sender *ICoreWebView2, args *ICoreWebView2NewWindowRequestedEventArgs) uintptr {
	return this.impl.NewWindowRequested(sender, args)
}

var _newWindowFn = _ICoreWebView2NewWindowRequestedEventHandlerVtbl{
	_IUnknownVtbl{
		NewComProc(_newWinQI),
		NewComProc(_newWinAddRef),
		NewComProc(_newWinRelease),
	},
	NewComProc(_newWinInvoke),
}

func newICoreWebView2NewWindowRequestedEventHandler(impl iCoreWebView2NewWindowRequestedEventHandlerImpl) *iCoreWebView2NewWindowRequestedEventHandler {
	return &iCoreWebView2NewWindowRequestedEventHandler{vtbl: &_newWindowFn, impl: impl}
}

// ---- Chromium implementations ----

// NavigationStarting cancels any navigation the NavigationStartingCallback rejects.
func (e *Chromium) NavigationStarting(_ *ICoreWebView2, args *ICoreWebView2NavigationStartingEventArgs) uintptr {
	if e.NavigationStartingCallback == nil {
		return 0
	}
	uri, err := args.URI()
	if err != nil || !e.NavigationStartingCallback(uri) {
		if cerr := args.Cancel(); cerr != nil { // fail closed: an uncancellable navigation is fatal
			e.fatal(cerr)
		}
	}
	return 0
}

// NewWindowRequested always denies popups/target=_blank; the callback only observes.
func (e *Chromium) NewWindowRequested(_ *ICoreWebView2, args *ICoreWebView2NewWindowRequestedEventArgs) uintptr {
	if uri, err := args.URI(); err == nil && e.NewWindowDeniedCallback != nil {
		e.NewWindowDeniedCallback(uri)
	}
	if err := args.Deny(); err != nil { // fail closed: a popup that could not be denied is fatal
		e.fatal(err)
	}
	return 0
}

// ---- Close / PostWebMessageAsJSON / settings / resource serving ----

// Close closes the controller and releases the COM references (always, even when the
// controller's Close failed). Returns the ICoreWebView2Controller::Close HRESULT as an error.
// UI thread only.
func (e *Chromium) Close() error {
	var err error
	if e.controller != nil {
		r, _, _ := e.controller.vtbl.Close.Call(uintptr(unsafe.Pointer(e.controller)))
		err = hrErr("Controller.Close", r)
		_, _, _ = e.controller.vtbl.Release.Call(uintptr(unsafe.Pointer(e.controller)))
		e.controller = nil
	}
	if e.webview != nil {
		_, _, _ = e.webview.vtbl.Release.Call(uintptr(unsafe.Pointer(e.webview)))
		e.webview = nil
	}
	if e.environment != nil {
		_, _, _ = e.environment.vtbl.Release.Call(uintptr(unsafe.Pointer(e.environment)))
		e.environment = nil
	}
	return err
}

// PostWebMessageAsJSON delivers a JSON document to page listeners of
// chrome.webview 'message' (event.data is the parsed value). No script is evaluated.
func (e *Chromium) PostWebMessageAsJSON(doc string) error {
	if e.webview == nil {
		return errors.New("webview closed")
	}
	p, err := windows.UTF16PtrFromString(doc)
	if err != nil {
		return err
	}
	r, _, _ := e.webview.vtbl.PostWebMessageAsJSON.Call(uintptr(unsafe.Pointer(e.webview)), uintptr(unsafe.Pointer(p)))
	return hrErr("PostWebMessageAsJson", r)
}

// settings3IID is ICoreWebView2Settings3 (AreBrowserAcceleratorKeysEnabled).
var settings3IID = windows.GUID{Data1: 0xFDB5AB74, Data2: 0xAF33, Data3: 0x4854, Data4: [8]byte{0x84, 0xF0, 0x0A, 0x63, 0x1D, 0xEB, 0x5E, 0xBA}}

// queryInterface calls IUnknown::QueryInterface (vtable slot 0) on a raw COM pointer.
func queryInterface(p uintptr, iid *windows.GUID) (uintptr, error) {
	obj := *(*unsafe.Pointer)(unsafe.Pointer(&p))
	vtbl := *(*unsafe.Pointer)(obj)
	qi := *(*uintptr)(vtbl)
	var out uintptr
	r, _, _ := ComProc(qi).Call(p, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out)))
	if err := hrErr("QueryInterface", r); err != nil {
		return 0, err
	}
	return out, nil
}

// vtableSlot reads function pointer n of a COM object.
func vtableSlot(p uintptr, n int) ComProc {
	obj := *(*unsafe.Pointer)(unsafe.Pointer(&p))
	vtbl := *(*unsafe.Pointer)(obj)
	return ComProc(*(*uintptr)(unsafe.Add(vtbl, uintptr(n)*unsafe.Sizeof(uintptr(0)))))
}

// Lockdown applies the production settings and FAILS CLOSED: every setter's HRESULT is
// checked and the first failure is returned (the caller must not show the page then). With
// debug=false DevTools, the default context menu, browser accelerator keys (F5/Ctrl+R/F12),
// status bar, zoom and script dialogs are off. Accelerator keys live on ICoreWebView2Settings3,
// reached by QueryInterface (calling its slot on the Settings1 pointer would be invalid).
func (e *Chromium) Lockdown(debug bool) error {
	if e.webview == nil {
		return errors.New("webview closed")
	}
	var s1 *ICoreWebViewSettings
	r, _, _ := e.webview.vtbl.GetSettings.Call(uintptr(unsafe.Pointer(e.webview)), uintptr(unsafe.Pointer(&s1)))
	if err := hrErr("get_Settings", r); err != nil {
		return err
	}
	if s1 == nil {
		return errors.New("get_Settings returned nil")
	}
	p1 := uintptr(unsafe.Pointer(s1))
	defer comRelease(p1)
	b := func(v bool) uintptr {
		if v {
			return 1
		}
		return 0
	}
	var first error
	put := func(op string, proc ComProc, self uintptr, on bool) {
		r, _, _ := proc.Call(self, b(on))
		if err := hrErr(op, r); err != nil && first == nil {
			first = err
		}
	}
	put("put_AreDevToolsEnabled", s1.vtbl.PutAreDevToolsEnabled, p1, debug)
	put("put_AreDefaultContextMenusEnabled", s1.vtbl.PutAreDefaultContextMenusEnabled, p1, debug)
	put("put_IsStatusBarEnabled", s1.vtbl.PutIsStatusBarEnabled, p1, false)
	put("put_IsZoomControlEnabled", s1.vtbl.PutIsZoomControlEnabled, p1, false)
	put("put_AreDefaultScriptDialogsEnabled", s1.vtbl.PutAreDefaultScriptDialogsEnabled, p1, false)

	p3, err := queryInterface(p1, &settings3IID)
	if err != nil {
		if first == nil {
			first = err
		}
	} else {
		defer comRelease(p3)
		// IUnknown(3) + Settings1(18) + Settings2(2): Get at 23, Put at 24.
		put("put_AreBrowserAcceleratorKeysEnabled", vtableSlot(p3, 24), p3, debug)
	}
	return first
}

// AddDocumentCreatedScript registers a script that runs in every new document before page
// scripts; the HRESULT is checked.
func (e *Chromium) AddDocumentCreatedScript(script string) error {
	if e.webview == nil {
		return errors.New("webview closed")
	}
	p, err := windows.UTF16PtrFromString(script)
	if err != nil {
		return err
	}
	r, _, _ := e.webview.vtbl.AddScriptToExecuteOnDocumentCreated.Call(uintptr(unsafe.Pointer(e.webview)), uintptr(unsafe.Pointer(p)), 0)
	return hrErr("AddScriptToExecuteOnDocumentCreated", r)
}

// NavigateChecked navigates the top-level page and reports the HRESULT.
func (e *Chromium) NavigateChecked(url string) error {
	if e.webview == nil {
		return errors.New("webview closed")
	}
	p, err := windows.UTF16PtrFromString(url)
	if err != nil {
		return err
	}
	r, _, _ := e.webview.vtbl.Navigate.Call(uintptr(unsafe.Pointer(e.webview)), uintptr(unsafe.Pointer(p)))
	return hrErr("Navigate", r)
}

// Reload reloads the current page.
func (e *Chromium) Reload() error {
	if e.webview == nil {
		return errors.New("webview closed")
	}
	r, _, _ := e.webview.vtbl.Reload.Call(uintptr(unsafe.Pointer(e.webview)))
	return hrErr("Reload", r)
}

// ---- ProcessFailed ----

type _ICoreWebView2ProcessFailedEventArgsVtbl struct {
	_IUnknownVtbl
	GetProcessFailedKind ComProc
}

type ICoreWebView2ProcessFailedEventArgs struct {
	vtbl *_ICoreWebView2ProcessFailedEventArgsVtbl
}

type _ICoreWebView2ProcessFailedEventHandlerVtbl struct {
	_IUnknownVtbl
	Invoke ComProc
}

type iCoreWebView2ProcessFailedEventHandlerImpl interface {
	_IUnknownImpl
	ProcessFailed(sender *ICoreWebView2, args *ICoreWebView2ProcessFailedEventArgs) uintptr
}

type iCoreWebView2ProcessFailedEventHandler struct {
	vtbl *_ICoreWebView2ProcessFailedEventHandlerVtbl
	impl iCoreWebView2ProcessFailedEventHandlerImpl
}

func _procFailQI(this *iCoreWebView2ProcessFailedEventHandler, refiid, object uintptr) uintptr {
	return this.impl.QueryInterface(refiid, object)
}
func _procFailAddRef(this *iCoreWebView2ProcessFailedEventHandler) uintptr { return this.impl.AddRef() }
func _procFailRelease(this *iCoreWebView2ProcessFailedEventHandler) uintptr {
	return this.impl.Release()
}
func _procFailInvoke(this *iCoreWebView2ProcessFailedEventHandler, sender *ICoreWebView2, args *ICoreWebView2ProcessFailedEventArgs) uintptr {
	return this.impl.ProcessFailed(sender, args)
}

var _processFailedFn = _ICoreWebView2ProcessFailedEventHandlerVtbl{
	_IUnknownVtbl{
		NewComProc(_procFailQI),
		NewComProc(_procFailAddRef),
		NewComProc(_procFailRelease),
	},
	NewComProc(_procFailInvoke),
}

func newICoreWebView2ProcessFailedEventHandler(impl iCoreWebView2ProcessFailedEventHandlerImpl) *iCoreWebView2ProcessFailedEventHandler {
	return &iCoreWebView2ProcessFailedEventHandler{vtbl: &_processFailedFn, impl: impl}
}

// ProcessFailed forwards the failure kind; it runs inside a WebView2 event, so the callback
// must only record/post work.
func (e *Chromium) ProcessFailed(_ *ICoreWebView2, args *ICoreWebView2ProcessFailedEventArgs) uintptr {
	var kind int32
	r, _, _ := args.vtbl.GetProcessFailedKind.Call(uintptr(unsafe.Pointer(args)), uintptr(unsafe.Pointer(&kind)))
	if int32(r) < 0 {
		kind = -1
	}
	if e.ProcessFailedCallback != nil {
		e.ProcessFailedCallback(kind)
	}
	return 0
}

// RequestInfo returns the URI and HTTP method of a web resource request.
func (r *ICoreWebView2WebResourceRequest) RequestInfo() (uri, method string, err error) {
	uri, err = readCoString(func(out **uint16) uintptr {
		x, _, _ := r.vtbl.GetUri.Call(uintptr(unsafe.Pointer(r)), uintptr(unsafe.Pointer(out)))
		return x
	}, "Request.get_Uri")
	if err != nil {
		return
	}
	method, err = readCoString(func(out **uint16) uintptr {
		x, _, _ := r.vtbl.GetMethod.Call(uintptr(unsafe.Pointer(r)), uintptr(unsafe.Pointer(out)))
		return x
	}, "Request.get_Method")
	return
}

// Respond answers an intercepted request with an in-memory response. headers is a
// CRLF-separated "Name: value" list. UI thread only (inside the event handler).
func (e *Chromium) Respond(args *ICoreWebView2WebResourceRequestedEventArgs, status int, reason, headers string, body []byte) error {
	if e.environment == nil {
		return errors.New("no environment")
	}
	var stream uintptr
	if len(body) > 0 {
		var err error
		if stream, err = w32.SHCreateMemStream(body); err != nil && stream == 0 {
			return fmt.Errorf("SHCreateMemStream: %w", err)
		}
		defer comRelease(stream)
	}
	rp, err := windows.UTF16PtrFromString(reason)
	if err != nil {
		return err
	}
	hp, err := windows.UTF16PtrFromString(headers)
	if err != nil {
		return err
	}
	var resp *ICoreWebView2WebResourceResponse
	r, _, _ := e.environment.vtbl.CreateWebResourceResponse.Call(
		uintptr(unsafe.Pointer(e.environment)), stream, uintptr(status),
		uintptr(unsafe.Pointer(rp)), uintptr(unsafe.Pointer(hp)), uintptr(unsafe.Pointer(&resp)))
	if err := hrErr("CreateWebResourceResponse", r); err != nil {
		return err
	}
	defer comRelease(uintptr(unsafe.Pointer(resp)))
	r, _, _ = args.vtbl.PutResponse.Call(uintptr(unsafe.Pointer(args)), uintptr(unsafe.Pointer(resp)))
	return hrErr("put_Response", r)
}

// AddResourceFilter registers a WebResourceRequested filter (e.g. "https://app.invalid/*").
func (e *Chromium) AddResourceFilter(filter string) error {
	p, err := windows.UTF16PtrFromString(filter)
	if err != nil {
		return err
	}
	const contextAll = 0
	r, _, _ := e.webview.vtbl.AddWebResourceRequestedFilter.Call(uintptr(unsafe.Pointer(e.webview)), uintptr(unsafe.Pointer(p)), contextAll)
	return hrErr("AddWebResourceRequestedFilter", r)
}

// fatal reports a security-handler failure (the host must leave, not run half-protected).
func (e *Chromium) fatal(err error) {
	if e.FatalCallback != nil {
		e.FatalCallback(err)
	}
}

// ErrEmbedTimeout is returned by EmbedTimeout when the runtime never answered.
var ErrEmbedTimeout = errors.New("WebView2 did not start in time")

var (
	procPeekMessageW              = windows.NewLazySystemDLL("user32.dll").NewProc("PeekMessageW")
	procMsgWaitForMultipleObjects = windows.NewLazySystemDLL("user32.dll").NewProc("MsgWaitForMultipleObjects")
)

const (
	qsAllInput = 0x04FF
	pmRemove   = 0x0001
	wmQuit     = 0x0012
)
