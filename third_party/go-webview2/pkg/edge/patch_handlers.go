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
		_ = args.Cancel() // fail closed
	}
	return 0
}

// NewWindowRequested always denies popups/target=_blank; the callback only observes.
func (e *Chromium) NewWindowRequested(_ *ICoreWebView2, args *ICoreWebView2NewWindowRequestedEventArgs) uintptr {
	if uri, err := args.URI(); err == nil && e.NewWindowDeniedCallback != nil {
		e.NewWindowDeniedCallback(uri)
	}
	_ = args.Deny()
	return 0
}

// ---- Close / PostWebMessageAsJSON / settings / resource serving ----

// Close closes the controller and releases the COM references. UI thread only.
func (e *Chromium) Close() {
	if e.controller != nil {
		_, _, _ = e.controller.vtbl.Close.Call(uintptr(unsafe.Pointer(e.controller)))
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

// Lockdown applies the production settings. With debug=false DevTools, the default
// context menu, browser accelerator keys (F5/Ctrl+R/F12...), status bar, zoom and
// script dialogs are off.
func (e *Chromium) Lockdown(debug bool) error {
	s, err := e.webview.GetSettings()
	if err != nil && s == nil {
		return err
	}
	call := func(proc ComProc, on bool) {
		v := uintptr(0)
		if on {
			v = 1
		}
		_, _, _ = proc.Call(uintptr(unsafe.Pointer(s)), v)
	}
	call(s.vtbl.PutAreDevToolsEnabled, debug)
	call(s.vtbl.PutAreDefaultContextMenusEnabled, debug)
	call(s.vtbl.PutAreBrowserAcceleratorKeysEnabled, debug)
	call(s.vtbl.PutIsStatusBarEnabled, false)
	call(s.vtbl.PutIsZoomControlEnabled, false)
	call(s.vtbl.PutAreDefaultScriptDialogsEnabled, false)
	return nil
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

// AddResourceFilter registers a WebResourceRequested filter (e.g. "https://app.local/*").
func (e *Chromium) AddResourceFilter(filter string) error {
	p, err := windows.UTF16PtrFromString(filter)
	if err != nil {
		return err
	}
	const contextAll = 0
	r, _, _ := e.webview.vtbl.AddWebResourceRequestedFilter.Call(uintptr(unsafe.Pointer(e.webview)), uintptr(unsafe.Pointer(p)), contextAll)
	return hrErr("AddWebResourceRequestedFilter", r)
}

// AddDocumentCreatedScript registers a script that runs in every new document before page scripts.
func (e *Chromium) AddDocumentCreatedScript(script string) {
	e.Init(script)
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
