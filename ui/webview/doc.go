// Package webview hosts the WebView2 UI: a raw top-level window carrying the WebView2
// controller, a hidden walk MainWindow that owns the message pump, one RPC worker, the
// capture hooks and the self-test hooks. Windows only; see docs/design/phase2-architecture.md.
package webview
