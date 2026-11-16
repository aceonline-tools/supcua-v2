(function () {
  var t = 'dark';
  try {
    var s = localStorage.getItem('supcua.theme');
    if (s === 'dark' || s === 'light') t = s;
    else if (window.matchMedia && window.matchMedia('(prefers-color-scheme: light)').matches) t = 'light';
  } catch (e) {}
  document.documentElement.setAttribute('data-theme', t);
})();
