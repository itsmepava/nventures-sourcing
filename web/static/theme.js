// Applies the saved theme and density before first paint (no flash).
(function () {
  var t = 'system', c = '0';
  try { t = localStorage.getItem('nv.theme') || 'system'; c = localStorage.getItem('nv.compact') || '0'; } catch (e) {}
  var dark = t === 'dark' || (t === 'system' && window.matchMedia && matchMedia('(prefers-color-scheme: dark)').matches);
  var r = document.documentElement;
  r.dataset.theme = dark ? 'dark' : 'light';
  r.dataset.themePref = t;
  if (c === '1') r.classList.add('compact');
  var m = document.querySelector('meta[name="theme-color"]');
  if (m) m.setAttribute('content', dark ? '#0d1218' : '#f4f6f8');
})();
