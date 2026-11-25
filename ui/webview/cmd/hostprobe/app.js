"use strict";
const pending = new Map(); let seq = 0, events = 0;
function call(method, params) {
  return new Promise((res, rej) => {
    const id = ++seq; pending.set(id, { res, rej });
    window.chrome.webview.postMessage(JSON.stringify({ id, method, params }));
  });
}
window.__resolve = r => { const p = pending.get(r.id); if (!p) return; pending.delete(r.id); r.ok ? p.res(r.result) : p.rej(r.error); };
window.__emit = (name) => { if (name === "tick") events++; };
window.__probe = {
  async run(n) {
    try {
      const r = await call("ping", { text: "probe" });
      for (let i = 0; i < n; i++) await call("cycle", { i });
      await call("report", { pong: r.upper, events, cycles: n });
    } catch (e) {
      await call("report", { pong: "", events, cycles: 0, error: String((e && e.message) || JSON.stringify(e)) });
    }
  },
};
call("ready", {});
