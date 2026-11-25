(function () {
  var MAX = 20, sent = 0, seq = 8000000;
  function send(level, message) {
    if (sent >= MAX) return;
    sent++;
    try {
      var wv = window.chrome && window.chrome.webview;
      if (!wv) return;
      wv.postMessage(JSON.stringify({ id: ++seq, method: 'app.log', params: { level: level, message: String(message).slice(0, 500) } }));
    } catch (e) {}
  }
  window.addEventListener('error', function (e) {
    var t = e.target;
    if (t && t !== window && (t.src || t.href)) send('error', 'resource failed to load: ' + (t.src || t.href));
    else send('error', (e.message || 'error') + (e.filename ? ' at ' + e.filename + ':' + e.lineno + ':' + e.colno : ''));
  }, true);
  window.addEventListener('unhandledrejection', function (e) {
    var r = e.reason;
    send('error', 'unhandled rejection: ' + ((r && (r.stack || r.message)) || r));
  });
})();
