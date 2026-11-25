package rpc

import "encoding/json"

// Go -> JS frames are delivered with PostWebMessageAsJson (no script evaluation):
//
//	{"k":"r","d":<response>}          resolves a call
//	{"k":"e","n":"<event>","p":<json>} pushes an event
//
// BootstrapJS (injected before page scripts) forwards them to the page's window.__resolve /
// window.__emit contract (frontend/src/api/client.ts), retrying briefly until the page has
// installed them, so early events are not lost.
const BootstrapJS = `(function(){var w=window;if(!w.chrome||!w.chrome.webview)return;` +
	`function d(m,n){var f=m.k==='r'?w.__resolve:w.__emit;` +
	`if(typeof f==='function'){if(m.k==='r')f(m.d);else f(m.n,m.p);return}` +
	`if(n<200)setTimeout(function(){d(m,n+1)},25)}` +
	`w.chrome.webview.addEventListener('message',function(e){var m=e.data;if(m&&(m.k==='r'||m.k==='e'))d(m,0)})})();`

// EncodeResolve wraps an encoded response for the page.
func EncodeResolve(response []byte) []byte {
	out := make([]byte, 0, len(response)+16)
	out = append(out, `{"k":"r","d":`...)
	out = append(out, response...)
	return append(out, '}')
}

// EncodeEvent builds an event frame; payload is marshalled with encoding/json only.
func EncodeEvent(name string, payload any) ([]byte, error) {
	n, err := json.Marshal(name)
	if err != nil {
		return nil, err
	}
	p, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(n)+len(p)+24)
	out = append(out, `{"k":"e","n":`...)
	out = append(out, n...)
	out = append(out, `,"p":`...)
	out = append(out, p...)
	return append(out, '}'), nil
}
