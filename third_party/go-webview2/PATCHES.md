# Vendored jchv/go-webview2 (MIT, see LICENSE)

Upstream: github.com/jchv/go-webview2 @ v0.0.0-20260205173254-56598839c808
Kept: pkg/edge, internal/w32, webviewloader (+ SDK loader DLLs). Dropped: top-level webview.go, cmd/, examples, tests.
Local changes are listed in third_party/go-webview2/pkg/edge/patch_*.go and in the "// PATCH" markers of chromium.go.
Rule: every COM vtable is a package-level `var`; no `NewCallback` outside package init (fixed callback count).

## Patch list (pkg/edge)
- patch_handlers.go: NavigationStarting (cancel) + NewWindowRequested (always deny) handlers; `Chromium.Close`, `PostWebMessageAsJSON`, `Lockdown(debug)` (DevTools, context menu, accelerator keys, status bar, zoom, script dialogs), `Respond` (in-memory WebResourceResponse), `AddResourceFilter`, `ICoreWebView2WebResourceRequest.RequestInfo`, `ErrEmbedTimeout`. HRESULTs are checked from the call result, never from GetLastError.
- chromium.go: `EmbedTimeout` (bounded pump on the UI thread; `Embed` = 60 s wrapper), `log.Fatal*` removed from environment/controller creation and resource handling (errors are returned), upstream echo of every web message removed, handler registration for the two new events.
- Callback count: 36 `NewComProc` sites (28 upstream + 8 new), all in package-level `var` vtables; guarded by ui/webview/callbacks_guard_test.go.
