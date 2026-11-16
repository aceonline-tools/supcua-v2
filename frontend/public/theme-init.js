(function () {
  var t = 'dark';
  try {
    var s = localStorage.getItem('supcua.theme');
    var light = window.matchMedia && window.matchMedia('(prefers-color-scheme: light)').matches;
    if (s === 'dark' || s === 'light') t = s;
    else if (s === 'system' || !s) t = light ? 'light' : 'dark';
  } catch (e) {}
  document.documentElement.setAttribute('data-theme', t);
})();
