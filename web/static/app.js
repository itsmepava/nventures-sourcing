// nVentures Sourcing UI — native JavaScript, no framework, no external assets.
// All untrusted text is inserted with textContent via h(); never innerHTML.

// replaceChildren/append stringify null; skip empty optional children instead.
for (const m of ['replaceChildren', 'append', 'prepend']) {
  const orig = Element.prototype[m];
  Element.prototype[m] = function (...kids) { return orig.apply(this, kids.flat(Infinity).filter((k) => k !== null && k !== undefined && k !== false)); };
}
const $ = (s, el = document) => el.querySelector(s);
const $$ = (s, el = document) => Array.from(el.querySelectorAll(s));

function h(tag, attrs, ...kids) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v === undefined || v === null || v === false) continue;
    if (k === 'class') el.className = v;
    else if (k.startsWith('on') && typeof v === 'function') el.addEventListener(k.slice(2), v);
    else if (k === 'dataset') Object.assign(el.dataset, v);
    else if (k === 'value') el.value = v;
    else if (k === 'style') el.style.cssText = v; // CSSOM: allowed under the strict CSP
    else if (k === 'checked') el.checked = !!v;
    else el.setAttribute(k, v === true ? '' : v);
  }
  for (const kid of kids.flat(Infinity)) {
    if (kid === null || kid === undefined || kid === false) continue;
    el.append(kid instanceof Node ? kid : document.createTextNode(String(kid)));
  }
  return el;
}
const icon = (name) => { const s = document.createElementNS('http://www.w3.org/2000/svg', 'svg'); s.setAttribute('aria-hidden', 'true'); const u = document.createElementNS('http://www.w3.org/2000/svg', 'use'); u.setAttribute('href', '#i-' + name); s.append(u); return s; };

async function api(path, opts = {}) {
  const init = { method: opts.method || 'GET', headers: { 'X-NV-Request': '1' } };
  if (opts.body !== undefined) { init.headers['Content-Type'] = 'application/json'; init.body = JSON.stringify(opts.body); }
  let res;
  try { res = await fetch(path, init); } catch { throw new Error('The local nVentures server is not reachable. Is it still running?'); }
  const text = await res.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch { data = null; }
  if (!res.ok) throw new Error((data && data.error) || `Request failed (${res.status})`);
  return data;
}

// ---------- formatting ----------
const fmtDate = (s) => { if (!s) return '—'; const d = new Date(s); return isNaN(d) ? s : d.toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' }); };
const fmtTime = (s) => { const d = new Date(s); return isNaN(d) ? '' : d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' }); };
const fmtUSD = (n) => (n === null || n === undefined || n === '') ? 'Unknown' : '$' + Number(n).toLocaleString(undefined, { maximumFractionDigits: 0 });
const parse = (v) => { if (typeof v === 'string') { try { return JSON.parse(v); } catch { return {}; } } return v || {}; };
const STATUS = {
  ready: ['Ready', 'info'], review: ['Needs review', 'warn'], skipped: ['Skipped', ''], duplicate: ['Duplicate', ''], verified: ['In sheet ✓', 'ok'],
  already_exists: ['Already in sheet', 'ok'], demo_verified: ['In sheet (legacy demo)', 'ok'], error: ['Error', 'bad'], uncertain: ['Write uncertain', 'bad'],
  conflict: ['Conflict', 'bad'], syncing: ['Syncing…', 'info'], pending: ['Not synced', ''], blocked: ['Blocked', 'bad'], demo: ['Demo', ''],
  running: ['Running', 'info'], completed: ['Completed', 'ok'], completed_with_errors: ['Completed with errors', 'warn'], partial: ['Partial', 'warn'],
  cancelled: ['Cancelled', ''], failed: ['Failed', 'bad'], interrupted: ['Interrupted', 'warn'],
  accepted: ['Accepted', 'ok'], rejected: ['Rejected', 'bad'], needs_research: ['Needs research', 'warn'],
};
const badge = (s) => { const [t, c] = STATUS[s] || [s || '—', '']; return h('span', { class: 'badge ' + c }, t); };
const JOB_LABEL = { inbound_import: 'Inbox import', inbound_sync: 'Inbound sync', outbound_run: 'Sourcing run', outbound_sync: 'Outbound sync', outbound_research: 'Company research' };

// ---------- preferences ----------
const prefs = {
  get(k, d) { try { const v = localStorage.getItem('nv.' + k); return v === null ? d : v; } catch { return d; } },
  set(k, v) { try { localStorage.setItem('nv.' + k, v); } catch {} api('/api/preferences', { method: 'POST', body: { [k]: String(v) } }).catch(() => {}); },
  json(k, d) { try { return JSON.parse(this.get(k, '')) ?? d; } catch { return d; } },
};
function applyTheme(pref) {
  const dark = pref === 'dark' || (pref === 'system' && matchMedia('(prefers-color-scheme: dark)').matches);
  document.documentElement.dataset.theme = dark ? 'dark' : 'light';
  document.documentElement.dataset.themePref = pref;
  const b = $('#theme-btn'); if (b) b.setAttribute('aria-label', 'Theme: ' + pref + ' (click to change)');
}
function cycleTheme() {
  const order = ['system', 'light', 'dark'];
  const next = order[(order.indexOf(prefs.get('theme', 'system')) + 1) % 3];
  prefs.set('theme', next); applyTheme(next); toast('Theme: ' + next);
}
function toggleCompact() {
  const on = !document.documentElement.classList.contains('compact');
  document.documentElement.classList.toggle('compact', on); prefs.set('compact', on ? '1' : '0'); toast(on ? 'Compact density' : 'Comfortable density');
}
matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => applyTheme(prefs.get('theme', 'system')));

// ---------- toasts ----------
function toast(msg, kind = '') {
  const t = h('div', { class: 'toast ' + kind }, kind === 'bad' ? icon('alert') : icon('check'), h('span', {}, msg));
  $('#toasts').append(t); setTimeout(() => t.remove(), kind === 'bad' ? 9000 : 4000);
}

// ---------- overlays with focus management ----------
const overlays = [];
function focusables(el) { return $$('a[href],button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])', el).filter((x) => x.offsetParent !== null || x === document.activeElement); }
function openOverlay({ kind = 'drawer', title, sub, body, foot, label, onClose, className = '' }) {
  const prev = document.activeElement;
  const id = 'ov-' + Math.random().toString(36).slice(2);
  const panel = h('div', { class: (kind === 'drawer' ? 'drawer' : 'dialog') + ' ' + className, role: 'dialog', 'aria-modal': 'true', 'aria-labelledby': title ? id : null, 'aria-label': title ? null : label, tabindex: '-1' },
    title ? h('div', { class: 'drawer-head' }, h('div', { style: 'flex:1;min-width:0' }, h('h2', { id }, title), sub ? h('p', { class: 'sub' }, sub) : null), h('button', { class: 'icon-btn', 'aria-label': 'Close', onclick: () => close() }, icon('close'))) : null,
    body, foot ? h('div', { class: 'drawer-foot' }, foot) : null);
  const ov = h('div', { class: 'overlay ' + (kind === 'drawer' ? '' : 'dialog-wrap') }, panel);
  ov.addEventListener('mousedown', (e) => { if (e.target === ov) close(); });
  const rec = { ov, panel, close, prev };
  function close() {
    const i = overlays.indexOf(rec); if (i < 0) return;
    overlays.splice(i, 1); ov.remove(); onClose && onClose();
    if (prev && document.contains(prev)) prev.focus();
  }
  overlays.push(rec);
  (kind === 'drawer' ? $('#drawer-root') : $('#dialog-root')).append(ov);
  requestAnimationFrame(() => { const f = focusables(panel).find((x) => x.matches('input,textarea,select')) || panel; f.focus(); });
  return rec;
}
document.addEventListener('keydown', (e) => {
  const top = overlays[overlays.length - 1];
  if (!top) return;
  if (e.key === 'Escape') { e.preventDefault(); top.close(); return; }
  if (e.key === 'Tab') {
    const f = focusables(top.panel); if (!f.length) return;
    const first = f[0], last = f[f.length - 1];
    if (e.shiftKey && (document.activeElement === first || document.activeElement === top.panel)) { e.preventDefault(); last.focus(); }
    else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
  }
});
function confirmDialog(title, text, okLabel = 'Confirm') {
  return new Promise((resolve) => {
    let done = false;
    const ok = h('button', { class: 'btn primary', onclick: () => { done = true; d.close(); resolve(true); } }, okLabel);
    const d = openOverlay({ kind: 'dialog', title, body: h('div', { class: 'drawer-body' }, h('p', {}, text)), foot: [h('button', { class: 'btn', onclick: () => d.close() }, 'Cancel'), ok], onClose: () => { if (!done) resolve(false); } });
  });
}

// ---------- global state ----------
const app = { state: null, page: null, watchers: new Map() };
async function loadState() {
  app.state = await api('/api/state');
  const r = app.state.readiness;
  const mb = $('#mode-badge'); mb.textContent = r.mode === 'demo' ? 'Demo data' : 'Live'; mb.classList.toggle('live', r.mode === 'live');
  const pc = app.state.pitch_counts || {};
  const attention = (pc.review || 0) + (pc.ready || 0) + (pc.error || 0) + (pc.uncertain || 0) + (pc.conflict || 0);
  const nc = $('#nav-inbox-count'); nc.hidden = !attention; nc.textContent = attention; nc.setAttribute('aria-label', attention + ' pitches need attention');
  const banner = $('#banner'); banner.replaceChildren();
  if (r.mode === 'demo') banner.append(h('div', { class: 'banner info', role: 'note' }, icon('alert'), h('span', {}, h('b', {}, 'Demo mode. '), 'Gmail, Google Sheets and partner websites are local fixtures; every record is fictional and labelled “(demo)”. Nothing leaves this computer.')));
  else if (!r.google.connected) banner.append(h('div', { class: 'banner', role: 'note' }, icon('alert'), h('span', {}, 'Google is not connected. Sourcing works locally; inbox import and sheet sync need ', h('a', { href: '#/settings' }, 'Settings → Google connection'), '.')));
  return app.state;
}

// ---------- router ----------
const routes = {
  '': ['Overview', renderDashboard], inbox: ['Pitch inbox', renderInbox], sourcing: ['Sourcing', renderSourcing], companies: ['Companies', renderCompanies],
  partners: ['Partners', renderPartners], activity: ['Activity', renderActivity], settings: ['Settings', renderSettings],
};
function currentRoute() { const [path, qs] = location.hash.replace(/^#\/?/, '').split('?'); return { name: path.split('/')[0] || '', params: new URLSearchParams(qs || '') }; }
async function route() {
  const { name, params } = currentRoute();
  const [title, render] = routes[name] || routes[''];
  $('#page-title').textContent = title; document.title = title + ' · nVentures';
  $$('.sidebar nav a').forEach((a) => a.toggleAttribute('aria-current', false));
  const nav = $(`.sidebar nav a[data-nav="${name || 'dashboard'}"]`); if (nav) nav.setAttribute('aria-current', 'page');
  $('#sidebar').classList.remove('open'); $('#menu-btn').setAttribute('aria-expanded', 'false');
  while (overlays.length) overlays[overlays.length - 1].close();
  app.jobsMount = null; app.reloadPage = null; app.keys = null;
  for (const w of app.watchers.values()) w.detach();
  const main = $('#main');
  main.replaceChildren(h('div', { class: 'loading-page', role: 'status' }, 'Loading…'));
  try { await loadState(); app.page = name; await render(main, params); }
  catch (e) { main.replaceChildren(h('div', { class: 'note bad', role: 'alert' }, e.message)); }
  if (app.focusMain) { main.focus({ preventScroll: true }); }
  app.focusMain = true;
}
window.addEventListener('hashchange', route);

// ---------- job progress ----------
class JobWatcher {
  constructor(id, mount, onDone) { this.id = id; this.mount = mount; this.onDone = onDone; this.after = 0; this.events = []; this.timer = null; this.el = h('section', { class: 'job', 'aria-live': 'polite', 'aria-label': 'Job progress' }); mount.prepend(this.el); app.watchers.set(id, this); this.tick(); }
  detach() { clearTimeout(this.timer); app.watchers.delete(this.id); }
  async tick() {
    let data;
    try { data = await api(`/api/jobs/${this.id}?after=${this.after}`); } catch (e) { this.timer = setTimeout(() => this.tick(), 3000); return; }
    const j = data.job;
    for (const ev of data.events) { this.events.push(ev); this.after = Math.max(this.after, ev.id); }
    this.render(j);
    if (j.status === 'running') this.timer = setTimeout(() => this.tick(), 1000);
    else { this.detach(); toast(`${JOB_LABEL[j.kind] || 'Job'}: ${STATUS[j.status]?.[0] || j.status}`, j.status === 'failed' ? 'bad' : ''); this.onDone && this.onDone(j); }
  }
  render(j) {
    const c = j.counters || {};
    const keys = ['imported', 'ready', 'review', 'skipped', 'duplicates', 'accepted', 'rejected', 'needs_research', 'researched', 'pages_visited', 'verified', 'already_exists', 'sync_failed', 'errors', 'previously_rejected', 'already_imported'];
    const running = j.status === 'running';
    this.el.replaceChildren(
      h('div', { class: 'job-head' }, h('b', {}, `${JOB_LABEL[j.kind] || j.kind} #${j.id}`), badge(j.status), h('span', { class: 'msg' }, j.message || ''),
        running ? h('button', { class: 'btn sm', onclick: async () => { await api(`/api/jobs/${j.id}/cancel`, { method: 'POST' }); toast('Cancellation requested'); } }, 'Cancel') : h('a', { class: 'btn sm ghost', href: `#/activity?job=${j.id}` }, 'Details')),
      h('div', { class: 'progress', role: 'progressbar', 'aria-valuemin': 0, 'aria-valuemax': 100, 'aria-valuenow': j.progress, 'aria-label': 'Progress' }, h('div', { style: `width:${j.progress}%` })),
      h('div', { class: 'counters' }, keys.filter((k) => c[k]).map((k) => h('span', {}, k.replace(/_/g, ' ') + ': ', h('b', {}, c[k])))),
      c.more_available ? h('p', { class: 'hint' }, 'More matching messages remain — run the import again to continue.') : null,
      this.events.length ? h('details', { open: running }, h('summary', {}, `Events (${this.events.length})`), eventList(this.events.slice(-80).reverse())) : null);
  }
}
function eventList(events) {
  return h('ul', { class: 'events' }, events.map((e) => h('li', {}, h('time', { datetime: e.created_at, title: fmtDate(e.created_at) }, fmtTime(e.created_at)), h('span', { class: 'kind' }, e.kind.replace(/_/g, ' ')), h('p', {}, e.message))));
}
async function startJob(path, body, mount, onDone) {
  try { const j = await api(path, { method: 'POST', body }); new JobWatcher(j.id, mount, onDone); return j; }
  catch (e) { toast(e.message, 'bad'); return null; }
}

// ---------- generic table ----------
function dataTable({ key, columns, fetchPage, onOpen, selectable, tabs, extraFilters, bulk, emptyText, rowClass }) {
  const saved = prefs.json('view.' + key, {});
  const st = { q: saved.q || '', status: saved.status ?? (tabs ? tabs[0][0] : ''), sort: saved.sort || '', desc: saved.desc ?? false, page: 1, per: Number(prefs.get('per_page', '25')), starred: saved.starred || false, filters: saved.filters || {}, selected: new Set(), cursor: -1, items: [] };
  const root = h('section', { class: 'panel' });
  const search = h('input', { type: 'search', placeholder: 'Search…', 'aria-label': 'Search', value: st.q, 'data-search': '1' });
  let debounce;
  search.addEventListener('input', () => { clearTimeout(debounce); debounce = setTimeout(() => { st.q = search.value; st.page = 1; save(); load(); }, 220); });
  const tabBar = tabs ? h('div', { class: 'tabs', role: 'group', 'aria-label': 'Filter by status' }) : null;
  const starBtn = h('button', { class: 'btn sm', 'aria-pressed': String(st.starred), onclick: () => { st.starred = !st.starred; starBtn.setAttribute('aria-pressed', String(st.starred)); st.page = 1; save(); load(); } }, icon('star'), 'Shortlist');
  const filterEls = (extraFilters || []).map((f) => { const s = h('select', { 'aria-label': f.label, onchange: () => { st.filters[f.key] = s.value; st.page = 1; save(); load(); } }, h('option', { value: '' }, f.label + ': all'), f.options.map((o) => h('option', { value: o }, o))); s.value = st.filters[f.key] || ''; return s; });
  const toolbar = h('div', { class: 'toolbar' }, h('label', { class: 'search' }, icon('search'), search), tabBar, filterEls, starBtn);
  const selBar = h('div', { class: 'selection-bar', hidden: true });
  const tableWrap = h('div', { class: 'table-wrap' });
  const pager = h('div', { class: 'pager' });
  root.append(toolbar, selBar, tableWrap, pager);
  function save() { prefs.set('view.' + key, JSON.stringify({ q: st.q, status: st.status, sort: st.sort, desc: st.desc, starred: st.starred, filters: st.filters })); }
  function renderTabs(counts) {
    if (!tabBar) return;
    tabBar.replaceChildren(...tabs.map(([v, label, countKey]) => h('button', { 'aria-pressed': String(st.status === v), onclick: () => { st.status = v; st.page = 1; st.selected.clear(); save(); load(); } }, label, countKey && counts ? h('span', { class: 'n' }, counts(countKey)) : null)));
  }
  async function load() {
    renderTabs(api.lastCounts);
    tableWrap.setAttribute('aria-busy', 'true');
    let res;
    try { res = await fetchPage(st); } catch (e) { tableWrap.replaceChildren(h('div', { class: 'note bad', role: 'alert' }, e.message)); return; }
    finally { tableWrap.removeAttribute('aria-busy'); }
    st.items = res.items; st.cursor = Math.min(st.cursor, res.items.length - 1);
    renderTabs(res.counts);
    const head = h('tr', {}, selectable ? h('th', { class: 'tight' }, h('input', { type: 'checkbox', 'aria-label': 'Select all on page', checked: res.items.length && res.items.every((i) => st.selected.has(i.id)), onchange: (e) => { res.items.forEach((i) => e.target.checked ? st.selected.add(i.id) : st.selected.delete(i.id)); load(); } })) : null,
      columns.map((c) => c.sort ? h('th', { scope: 'col', class: c.cls, 'aria-sort': st.sort === c.sort ? (st.desc ? 'descending' : 'ascending') : 'none' }, h('button', { onclick: () => { if (st.sort === c.sort) st.desc = !st.desc; else { st.sort = c.sort; st.desc = false; } save(); load(); } }, c.label)) : h('th', { scope: 'col', class: c.cls }, c.label)));
    const rows = res.items.map((item, idx) => {
      const tr = h('tr', { tabindex: '-1', class: (st.selected.has(item.id) ? 'selected ' : '') + (rowClass ? rowClass(item) : ''), onclick: (e) => { if (e.target.closest('button,a,input,label')) return; st.cursor = idx; onOpen(item, load); } },
        selectable ? h('td', { class: 'tight' }, h('input', { type: 'checkbox', 'aria-label': 'Select ' + (item.name || item.id), checked: st.selected.has(item.id), onchange: (e) => { e.target.checked ? st.selected.add(item.id) : st.selected.delete(item.id); updateSel(); tr.classList.toggle('selected', e.target.checked); } })) : null,
        columns.map((c) => h('td', { class: c.cls }, c.render(item, load))));
      tr.dataset.idx = idx; return tr;
    });
    tableWrap.replaceChildren(res.items.length ? h('table', {}, h('thead', {}, head), h('tbody', {}, rows)) : h('div', { class: 'empty' }, h('b', {}, st.q || st.starred ? 'No matches' : 'Nothing here yet'), emptyText || ''));
    const pages = Math.max(1, Math.ceil(res.total / st.per));
    const perSel = h('select', { 'aria-label': 'Rows per page', onchange: () => { st.per = Number(perSel.value); prefs.set('per_page', st.per); st.page = 1; load(); } }, [10, 25, 50, 100].map((n) => h('option', { value: n }, n + ' / page'))); perSel.value = st.per;
    pager.replaceChildren(h('span', {}, `${res.total} total · page ${st.page} of ${pages}`), h('div', { class: 'actions' }, perSel,
      h('button', { class: 'btn sm', disabled: st.page <= 1, onclick: () => { st.page--; load(); } }, 'Previous'), h('button', { class: 'btn sm', disabled: st.page >= pages, onclick: () => { st.page++; load(); } }, 'Next')));
    updateSel(); highlight();
  }
  function updateSel() {
    if (!selectable) return;
    selBar.hidden = st.selected.size === 0;
    selBar.replaceChildren(h('span', {}, `${st.selected.size} selected`), bulk ? bulk([...st.selected], () => { st.selected.clear(); load(); }) : null, h('button', { class: 'btn sm ghost', onclick: () => { st.selected.clear(); load(); } }, 'Clear'));
  }
  function highlight() { $$('tbody tr', tableWrap).forEach((tr, i) => tr.toggleAttribute('aria-current', i === st.cursor)); const cur = $$('tbody tr', tableWrap)[st.cursor]; if (cur) { cur.focus({ preventScroll: false }); } }
  root.keys = (e) => {
    if (e.key === 'j' || e.key === 'ArrowDown' && e.altKey) { st.cursor = Math.min(st.items.length - 1, st.cursor + 1); highlight(); return true; }
    if (e.key === 'k') { st.cursor = Math.max(0, st.cursor - 1); highlight(); return true; }
    if (e.key === 'Enter' && st.cursor >= 0 && document.activeElement?.tagName === 'TR') { onOpen(st.items[st.cursor], load); return true; }
    if (e.key === 'x' && selectable && st.cursor >= 0) { const id = st.items[st.cursor].id; st.selected.has(id) ? st.selected.delete(id) : st.selected.add(id); load(); return true; }
    return false;
  };
  root.reload = load; root.state = st;
  load();
  return root;
}
function starButton(table, item, reload) {
  const b = h('button', { class: 'icon-btn star', 'aria-pressed': String(!!item.starred), 'aria-label': (item.starred ? 'Remove from' : 'Add to') + ' shortlist', title: 'Shortlist (s)', onclick: async () => {
    try { await api(`/api/${table}/${item.id}/star`, { method: 'POST', body: { on: !item.starred } }); item.starred = !item.starred; b.setAttribute('aria-pressed', String(item.starred)); b.setAttribute('aria-label', (item.starred ? 'Remove from' : 'Add to') + ' shortlist'); } catch (e) { toast(e.message, 'bad'); }
  } }, icon('star'));
  return b;
}
function qs(st, extra = {}) {
  const p = new URLSearchParams({ q: st.q, status: st.status, sort: st.sort, desc: st.desc ? '1' : '0', page: st.page, per_page: st.per, starred: st.starred ? '1' : '0', ...st.filters, ...extra });
  for (const [k, v] of [...p]) if (v === '' || v === '0' && k === 'starred') p.delete(k);
  return p.toString();
}
const disabledReason = (btn, reason) => { if (reason) { btn.setAttribute('aria-disabled', 'true'); btn.title = reason; btn.addEventListener('click', (e) => { e.preventDefault(); e.stopImmediatePropagation(); toast(reason, 'bad'); }, true); } return btn; };

// ---------- dashboard ----------
async function renderDashboard(main) {
  const d = await api('/api/dashboard');
  const r = app.state.readiness;
  const pc = d.pitches || {};
  const stat = (label, value, sub, href) => h('div', { class: 'panel stat' }, h('a', { href }, h('div', { class: 'label' }, label), h('div', { class: 'value' }, value), h('div', { class: 'sub' }, sub)));
  const synced = (pc.verified || 0) + (pc.already_exists || 0) + (pc.demo_verified || 0);
  const ready = (ok, warn) => ok ? 'ok' : warn ? 'warn' : 'bad';
  const countries = app.state.countries;
  const max = Math.max(1, ...countries.map((c) => d.by_country[c] || 0));
  main.replaceChildren(
    h('div', { class: 'page-head' }, h('div', {}, h('h2', {}, 'Today'), h('p', {}, 'Two workflows, one local app: proactive sourcing against the mandate, and a broad inbound pitch funnel from Gmail.')),
      h('div', { class: 'actions' }, h('a', { class: 'btn primary', href: '#/inbox?run=1' }, icon('inbox'), 'Import pitches'), h('a', { class: 'btn', href: '#/sourcing?run=1' }, icon('radar'), 'Start sourcing'))),
    h('div', { class: 'grid cols-4' },
      stat('Pitches needing review', (pc.review || 0) + (pc.error || 0) + (pc.uncertain || 0) + (pc.conflict || 0), `${pc.ready || 0} approved, waiting to sync`, '#/inbox'),
      stat('Pitches in sheet', synced, 'verified by full-row read-back', '#/inbox'),
      stat('Sourced companies', d.companies.total, `${d.companies.verified} verified in sheet · ${d.companies.pending_sync} not yet synced`, '#/companies'),
      stat('Partners in rotation', d.partners.relevant, `${d.partners.total} total`, '#/partners')),
    h('div', { class: 'grid cols-2', style: 'margin-top:16px' },
      h('section', { class: 'panel' }, h('div', { class: 'panel-head' }, h('h2', {}, 'Readiness')),
        h('div', { class: 'panel-body' }, h('ul', { class: 'ready-list' },
          h('li', {}, h('span', { class: 'dot ' + ready(r.google.connected) }), h('div', {}, h('b', {}, 'Google connection'), h('span', {}, r.google.connected ? `Connected (${r.google.token_source})${r.google.account ? ' · ' + r.google.account : ''}` : r.google.client_configured ? 'Client configured — click Connect in Settings' : 'Not set up — needs an OAuth client and your consent'))),
          h('li', {}, h('span', { class: 'dot ' + ready(r.inbound.ready, r.inbound.sheet_set) }), h('div', {}, h('b', {}, 'Inbound pitch sheet'), h('span', {}, r.inbound.sheet_set ? `Tab “${r.inbound.tab}”` : 'Sheet ID not set'))),
          h('li', {}, h('span', { class: 'dot ' + ready(r.outbound.ready, r.outbound.sheet_set) }), h('div', {}, h('b', {}, 'Outbound sourcing sheet'), h('span', {}, r.outbound.sheet_set ? `Tab “${r.outbound.tab}” · writing ${r.outbound.sync_enabled ? 'enabled' : 'disabled'}` : 'Optional — companies are saved locally until set'))),
          h('li', {}, h('span', { class: 'dot ok' }), h('div', {}, h('b', {}, 'Discovery & extraction'), h('span', {}, 'First-party crawler and deterministic rules · no paid search or hosted model'))),
          h('li', {}, h('span', { class: 'dot ' + (r.local_model.enabled ? 'ok' : '') }), h('div', {}, h('b', {}, 'Local model (optional)'), h('span', {}, r.local_model.enabled ? `On · ${r.local_model.model || 'no model chosen'}` : 'Off — not needed')))))),
      h('section', { class: 'panel' }, h('div', { class: 'panel-head' }, h('h2', {}, 'Sourced companies by HQ'), h('p', {}, 'Eligible countries only')),
        h('div', { class: 'panel-body bars' }, countries.map((c) => h('div', { class: 'bar-row' }, h('span', {}, c), h('div', { class: 'bar-track', role: 'img', 'aria-label': `${c}: ${d.by_country[c] || 0}` }, h('div', { class: 'bar-fill', style: `width:${((d.by_country[c] || 0) / max) * 100}%` })), h('b', {}, d.by_country[c] || 0)))))),
    h('div', { class: 'grid cols-2', style: 'margin-top:16px' },
      h('section', { class: 'panel' }, h('div', { class: 'panel-head' }, h('h2', {}, 'Recent jobs'), h('a', { href: '#/activity' }, 'All activity')),
        d.jobs.length ? h('div', { class: 'table-wrap' }, h('table', {}, h('tbody', {}, d.jobs.map((j) => h('tr', { onclick: () => { location.hash = '#/activity?job=' + j.id; } }, h('td', {}, h('div', { class: 'cell-main' }, `${JOB_LABEL[j.kind] || j.kind} #${j.id}`), h('div', { class: 'cell-sub' }, j.message)), h('td', { class: 'tight' }, badge(j.status)), h('td', { class: 'tight hide-mobile' }, fmtDate(j.started_at))))))) : h('div', { class: 'empty' }, h('b', {}, 'No jobs yet'), 'Start with an inbox import or a sourcing run.')),
      h('section', { class: 'panel' }, h('div', { class: 'panel-head' }, h('h2', {}, 'Latest activity')), d.events.length ? eventList(d.events) : h('div', { class: 'empty' }, 'No activity yet.'))));
}

// ---------- inbox ----------
async function renderInbox(main, params) {
  const r = app.state.readiness;
  const jobsMount = h('div');
  const connected = r.google.connected;
  const importBtn = h('button', { class: 'btn primary', onclick: () => openImportDialog(jobsMount, () => table.reload()) }, icon('inbox'), 'Import from Gmail');
  const syncReady = h('button', { class: 'btn', onclick: () => startJob('/api/inbox/sync', {}, jobsMount, () => { table.reload(); loadState(); }) }, icon('sync'), 'Sync all approved');
  if (!connected) disabledReason(importBtn, 'Connect Google in Settings first (read-only Gmail access).');
  if (!r.inbound.sheet_set || !connected) disabledReason(syncReady, 'Set the inbound sheet and connect Google in Settings first.');
  const counts = app.state.pitch_counts;
  const table = dataTable({
    key: 'pitches', selectable: true,
    tabs: [['attention', 'Needs attention', 'attention'], ['ready', 'Approved', 'ready'], ['synced', 'In sheet', 'synced'], ['skipped', 'Skipped', 'skipped'], ['duplicate', 'Duplicates', 'duplicate'], ['', 'All', 'all']],
    fetchPage: async (st) => { const res = await api('/api/pitches?' + qs(st)); const c = app.state.pitch_counts || {}; res.counts = (k) => k === 'all' ? Object.values(c).reduce((a, b) => a + b, 0) : k === 'attention' ? (c.review || 0) + (c.error || 0) + (c.uncertain || 0) + (c.conflict || 0) : k === 'synced' ? (c.verified || 0) + (c.already_exists || 0) + (c.demo_verified || 0) : c[k] || 0; return res; },
    columns: [
      { label: '', cls: 'tight', render: (p, reload) => starButton('pitches', p, reload) },
      { label: 'Company', sort: 'name', render: (p) => { const d = parse(p.data); return [h('div', { class: 'cell-main' }, p.name, d.name_guessed ? h('span', { class: 'badge warn', style: 'margin-left:6px' }, 'name guessed') : null), h('div', { class: 'cell-sub' }, d.description || '')]; } },
      { label: 'Contact', cls: 'hide-mobile', render: (p) => { const d = parse(p.data); return [h('div', {}, d.contact || '—'), d.contact_source ? h('div', { class: 'cell-sub' }, 'from ' + d.contact_source) : null]; } },
      { label: 'Signals', cls: 'tight hide-mobile', render: (p) => { const d = parse(p.data); return h('div', { class: 'actions' }, d.structured ? h('span', { class: 'badge' }, 'form') : h('span', { class: 'badge warn' }, 'direct'), (d.attachments || []).length ? h('span', { class: 'badge', title: 'Attachments are not reviewed automatically' }, icon('paperclip'), d.attachments.length) : null, (d.deck_links || []).length ? h('span', { class: 'badge', title: 'Deck links are not reviewed automatically' }, 'deck') : null); } },
      { label: 'Received', sort: 'received', cls: 'tight hide-mobile', render: (p) => fmtDate(p.received_at) },
      { label: 'Status', sort: 'status', cls: 'tight', render: (p) => badge(p.status) },
    ],
    onOpen: (p, reload) => openPitch(p.id, () => { reload(); loadState(); }),
    bulk: (ids, done) => {
      const b = h('button', { class: 'btn sm primary', onclick: () => startJob('/api/inbox/sync', { ids }, jobsMount, () => { done(); loadState(); }) }, icon('sync'), 'Sync selected now');
      if (!r.inbound.sheet_set || !connected) disabledReason(b, 'Set the inbound sheet and connect Google in Settings first.');
      return b;
    },
    emptyText: connected ? 'Import from Gmail to start. Each message is processed once; threads never merge different companies.' : 'Connect Google in Settings, then import.',
  });
  main.replaceChildren(
    h('div', { class: 'page-head' }, h('div', {}, h('h2', {}, 'Inbound pitch funnel'), h('p', {}, 'A broad funnel: any startup can enter. Unclear or established businesses go to review; nothing is written to the sheet until it is approved. Status and manual cells in the sheet are never changed.')),
      h('div', { class: 'actions' }, importBtn, syncReady, h('a', { class: 'btn ghost', href: '/api/pitches/export.csv', download: '' }, icon('download'), 'CSV'))),
    jobsMount, table);
  app.keys = table.keys; app.jobsMount = jobsMount; app.reloadPage = () => table.reload();
  const running = app.state.running.inbound; if (running) new JobWatcher(running, jobsMount, () => { table.reload(); loadState(); });
  if (params.get('run') === '1' && connected) openImportDialog(jobsMount, () => table.reload());
  if (params.get('pitch')) openPitch(Number(params.get('pitch')), () => table.reload());
}
function openImportDialog(mount, after) {
  const r = app.state.readiness;
  const limit = h('input', { type: 'number', min: 1, max: 500, value: prefs.get('import_limit', '50'), id: 'imp-limit' });
  const sync = h('input', { type: 'checkbox', checked: prefs.get('import_sync', '1') === '1' && r.inbound.sheet_set });
  if (!r.inbound.sheet_set) sync.disabled = true;
  const go = async () => { prefs.set('import_limit', limit.value); prefs.set('import_sync', sync.checked ? '1' : '0'); d.close(); await startJob('/api/inbox/import', { limit: Number(limit.value), sync: sync.checked }, mount, () => { after(); loadState(); }); };
  const d = openOverlay({ kind: 'dialog', title: 'Import pitches from Gmail', sub: `Read-only access to ${r.google.expected_account || r.google.account || 'the configured mailbox'}`, body: h('form', { class: 'drawer-body form', onsubmit: (e) => { e.preventDefault(); go(); } },
    h('label', {}, 'Messages to process this run', limit, h('span', { class: 'hint' }, 'Already imported messages are skipped. If more remain, the job says so; run again to continue.')),
    h('label', { class: 'check' }, sync, 'Then sync approved (“Ready”) pitches to the inbound sheet'),
    !r.inbound.sheet_set ? h('p', { class: 'note warn' }, 'Set the inbound sheet in Settings to enable syncing.') : null,
    h('p', { class: 'hint' }, 'Mail is never sent, archived, labelled, marked read or deleted. Private emails are not sent to any hosted model.')),
  foot: [h('button', { class: 'btn', onclick: () => d.close() }, 'Cancel'), h('button', { class: 'btn primary', onclick: go }, 'Start import')] });
}
async function openPitch(id, after) {
  let data;
  try { data = await api('/api/pitches/' + id); } catch (e) { toast(e.message, 'bad'); return; }
  const p = data.pitch, d = parse(p.data);
  const locked = ['verified', 'already_exists', 'demo_verified', 'syncing'].includes(p.status);
  const f = {
    name: h('input', { type: 'text', value: d.name || p.name, maxlength: 300, required: true, disabled: locked }),
    website: h('input', { type: 'url', value: d.website || '', maxlength: 500, disabled: locked }),
    description: h('textarea', { rows: 4, maxlength: 12000, disabled: locked }, d.description || ''),
    hq: h('input', { type: 'text', value: d.hq || '', maxlength: 300, placeholder: 'Unknown', disabled: locked }),
    contact: h('input', { type: 'email', value: d.contact || '', maxlength: 500, disabled: locked }),
    notes: h('textarea', { rows: 5, maxlength: 12000, disabled: locked }, d.notes || ''),
  };
  const save = async (decision) => {
    try {
      await api('/api/pitches/' + id, { method: 'POST', body: { name: f.name.value, website: f.website.value, description: f.description.value, hq: f.hq.value, contact: f.contact.value, notes: f.notes.value, decision } });
      toast(decision === 'ready' ? 'Approved — ready to sync' : decision === 'skipped' ? 'Skipped' : 'Saved for review'); dr.close(); after && after();
      return true;
    } catch (e) { toast(e.message, 'bad'); return false; }
  };
  const syncNow = async () => { if (await save('ready')) { await startJob('/api/inbox/sync', { ids: [id] }, app.jobsMount || $('#main'), () => { after && after(); loadState(); }); } };
  const r = app.state.readiness;
  const syncBtn = h('button', { class: 'btn primary', onclick: syncNow }, icon('sync'), 'Approve & sync now');
  if (!r.inbound.sheet_set || !r.google.connected) disabledReason(syncBtn, 'Set the inbound sheet and connect Google in Settings first.');
  const suggestBox = h('div');
  const suggest = r.local_model.enabled ? h('button', { class: 'btn sm', onclick: async () => {
    suggestBox.replaceChildren(h('p', { class: 'hint' }, 'Asking the local model…'));
    try { const s = await api(`/api/pitches/${id}/suggest`, { method: 'POST' }); suggestBox.replaceChildren(h('div', { class: 'note' }, h('b', {}, `Local model suggestion (${s.model}) — not applied`), s.sector ? h('p', {}, 'Sector: ' + s.sector) : null, s.summary ? h('p', {}, 'Summary: ' + s.summary) : null, (s.rejected || []).length ? h('p', { class: 'hint' }, 'Discarded: ' + s.rejected.join('; ')) : null, s.summary && !locked ? h('button', { class: 'btn sm', onclick: () => { f.description.value = s.summary; } }, 'Use summary as description') : null)); }
    catch (e) { suggestBox.replaceChildren(h('p', { class: 'note bad' }, e.message)); }
  } }, 'Suggest with local model') : null;
  const body = h('div', { class: 'drawer-body' },
    (d.reasons || []).length || p.error ? h('div', { class: 'note ' + (p.status === 'error' || p.status === 'conflict' || p.status === 'uncertain' ? 'bad' : 'warn') }, p.error ? h('p', {}, p.error) : null, (d.reasons || []).length ? h('ul', { class: 'reasons' }, d.reasons.map((x) => h('li', {}, x))) : null) : null,
    locked ? h('p', { class: 'note ok' }, `In the inbound sheet${d.sheet_row ? ' at row ' + d.sheet_row : ''}. Edit the sheet directly — nVentures never overwrites sheet rows.`) : null,
    h('form', { class: 'form', onsubmit: (e) => { e.preventDefault(); save('ready'); } },
      h('div', { class: 'field-row' }, h('label', {}, 'Company name', f.name), h('label', {}, 'Website', f.website)),
      h('label', {}, 'Description', f.description),
      h('div', { class: 'field-row' }, h('label', {}, 'HQ (only if stated)', f.hq, h('span', { class: 'hint' }, 'Country of incorporation is not HQ.')), h('label', {}, 'Contact email', f.contact, d.contact_source ? h('span', { class: 'hint' }, 'Taken from ' + d.contact_source) : null)),
      h('label', {}, 'Notes (written to the sheet)', f.notes)),
    suggest ? h('div', { class: 'section' }, suggest, suggestBox) : null,
    h('div', { class: 'section' }, h('h3', {}, 'Source'), h('div', { class: 'facts' },
      h('div', {}, h('small', {}, 'Sender'), h('span', {}, d.sender || '—')), h('div', {}, h('small', {}, 'Reply-To'), h('span', {}, d.reply_to || '—')),
      d.forwarded_from ? h('div', {}, h('small', {}, 'Forwarded from'), h('span', {}, d.forwarded_from)) : null,
      h('div', {}, h('small', {}, 'Subject'), h('span', {}, d.subject || '—')), h('div', {}, h('small', {}, 'Received'), h('span', {}, fmtDate(d.received))),
      h('div', {}, h('small', {}, 'Stage / sector'), h('span', {}, [d.stage, d.sector].filter(Boolean).join(' · ') || '—')),
      d.incorporation ? h('div', {}, h('small', {}, 'Incorporated in'), h('span', {}, d.incorporation)) : null),
      d.gmail_url ? h('p', { style: 'margin-top:10px' }, h('a', { href: d.gmail_url, target: '_blank', rel: 'noopener noreferrer' }, icon('ext'), ' Open original email in Gmail')) : h('p', { class: 'hint', style: 'margin-top:10px' }, 'Demo message — no Gmail link.')),
    (d.deck_links || []).length || (d.attachments || []).length ? h('div', { class: 'section' }, h('h3', {}, 'Decks & attachments (not reviewed)'), h('ul', { class: 'source-list' },
      (d.deck_links || []).map((u) => h('li', {}, h('span', { class: 'badge warn' }, 'unreviewed'), h('a', { href: u, target: '_blank', rel: 'noopener noreferrer' }, u))),
      (d.attachments || []).map((a) => h('li', {}, h('span', { class: 'badge warn' }, 'unreviewed'), `${a.filename} (${a.mime_type}, ${Math.round((a.size || 0) / 1024)} KB) — open the original email to view`)))) : null,
    d.excerpt ? h('details', { class: 'section' }, h('summary', {}, 'Email text (quoted replies removed)'), h('pre', { class: 'excerpt' }, d.excerpt)) : null,
    (data.sync_ops || []).length ? h('details', { class: 'section' }, h('summary', {}, `Sheet write history (${data.sync_ops.length})`), h('ul', { class: 'source-list' }, data.sync_ops.map((o) => h('li', {}, badge(o.status), `${fmtDate(o.updated_at)} ${o.sheet_row ? '· row ' + o.sheet_row : ''} ${o.error || ''}`)))) : null);
  const foot = locked ? [h('button', { class: 'btn', onclick: () => dr.close() }, 'Close')] : [
    h('button', { class: 'btn ghost', onclick: () => save('skipped') }, 'Skip'), h('button', { class: 'btn', onclick: () => save('review') }, 'Save, keep in review'),
    h('button', { class: 'btn', onclick: () => save('ready'), title: 'Ctrl+Enter' }, icon('check'), 'Approve'), syncBtn];
  const dr = openOverlay({ title: d.name || p.name, sub: `Pitch #${p.id} · ${STATUS[p.status]?.[0] || p.status}`, body, foot });
  dr.panel.addEventListener('keydown', (e) => { if ((e.ctrlKey || e.metaKey) && e.key === 'Enter' && !locked) { e.preventDefault(); save('ready'); } });
}

// ---------- sourcing ----------
async function renderSourcing(main, params) {
  const r = app.state.readiness;
  const jobsMount = h('div');
  const runBtn = h('button', { class: 'btn primary', onclick: () => openRunDialog(jobsMount, () => cands.reload()) }, icon('play'), 'Start sourcing run');
  const syncBtn = h('button', { class: 'btn', onclick: () => startJob('/api/sourcing/sync', {}, jobsMount, () => cands.reload()) }, icon('sync'), 'Sync pending companies');
  if (!r.outbound.ready) disabledReason(syncBtn, r.outbound.sheet_set ? 'Enable outbound sheet writing and connect Google in Settings.' : 'Set the outbound sheet in Settings first.');
  const urls = h('textarea', { rows: 3, placeholder: 'https://company.example\nhttps://another.example', 'aria-label': 'Company URLs, one per line' });
  const manual = h('section', { class: 'panel', style: 'margin-bottom:16px' }, h('div', { class: 'panel-head' }, h('div', {}, h('h2', {}, 'Research specific companies'), h('p', {}, 'For JavaScript-only or blocked portfolios: paste official company URLs. The same evidence rules apply.'))),
    h('div', { class: 'panel-body form' }, urls, h('div', { class: 'actions' }, h('button', { class: 'btn', onclick: async () => { const list = urls.value.split(/\s+/).map((x) => x.trim()).filter(Boolean); if (!list.length) { toast('Paste at least one URL', 'bad'); return; } if (await startJob('/api/sourcing/research', { urls: list }, jobsMount, () => cands.reload())) urls.value = ''; } }, 'Research URLs'))));
  const cands = h('section', { class: 'panel' });
  const outcomeTabs = h('div', { class: 'tabs', role: 'group', 'aria-label': 'Filter outcomes' });
  let outcome = prefs.get('cand_outcome', '');
  async function loadCands() {
    const items = await api('/api/candidates?limit=300' + (outcome ? '&outcome=' + outcome : ''));
    outcomeTabs.replaceChildren(...[['', 'All'], ['accepted', 'Accepted'], ['needs_research', 'Needs research'], ['rejected', 'Rejected'], ['duplicate', 'Duplicates']].map(([v, l]) => h('button', { 'aria-pressed': String(outcome === v), onclick: () => { outcome = v; prefs.set('cand_outcome', v); loadCands(); } }, l)));
    cands.replaceChildren(h('div', { class: 'panel-head' }, h('div', {}, h('h2', {}, 'Research outcomes'), h('p', {}, 'Every company researched, with the deterministic reasons for its outcome.')), outcomeTabs),
      items.length ? h('div', { class: 'table-wrap' }, h('table', {}, h('thead', {}, h('tr', {}, h('th', {}, 'Company'), h('th', { class: 'hide-mobile' }, 'Partner'), h('th', {}, 'Outcome'), h('th', { class: 'hide-mobile' }, 'Reasons'), h('th', { class: 'tight hide-mobile' }, 'When'))),
        h('tbody', {}, items.map((c) => h('tr', { onclick: () => openCandidate(c) }, h('td', {}, h('div', { class: 'cell-main' }, c.name), h('div', { class: 'cell-sub' }, c.url)), h('td', { class: 'hide-mobile' }, c.partner), h('td', { class: 'tight' }, badge(c.outcome)), h('td', { class: 'hide-mobile' }, h('div', { class: 'cell-sub' }, c.reasons.join('; ') || (c.outcome === 'accepted' ? 'All hard requirements evidenced' : ''))), h('td', { class: 'tight hide-mobile' }, fmtDate(c.created_at))))))) : h('div', { class: 'empty' }, h('b', {}, 'No research yet'), 'Start a sourcing run to crawl partner portfolios.'));
  }
  cands.reload = loadCands;
  main.replaceChildren(
    h('div', { class: 'page-head' }, h('div', {}, h('h2', {}, 'Proactive sourcing'), h('p', {}, 'Partners are visited in fair rotation. Their portfolio pages and company sites are crawled directly (robots.txt respected, private addresses refused). A company is accepted only when its own pages evidence every requirement; targets never relax the mandate.')),
      h('div', { class: 'actions' }, runBtn, syncBtn)),
    jobsMount, manual, cands);
  await loadCands();
  app.keys = null; app.jobsMount = jobsMount; app.reloadPage = loadCands;
  const running = app.state.running.outbound; if (running) new JobWatcher(running, jobsMount, () => loadCands());
  if (params.get('run') === '1') openRunDialog(jobsMount, () => loadCands());
}
function openRunDialog(mount, after) {
  const r = app.state.readiness;
  const target = h('input', { type: 'number', min: 1, max: 100, value: prefs.get('run_target', '10') });
  const partners = h('input', { type: 'number', min: 1, max: 30, value: prefs.get('run_partners', '6') });
  const sync = h('input', { type: 'checkbox', checked: r.outbound.ready && prefs.get('run_sync', '0') === '1' });
  if (!r.outbound.ready) sync.disabled = true;
  const recheck = h('input', { type: 'checkbox' });
  const go = async () => { prefs.set('run_target', target.value); prefs.set('run_partners', partners.value); prefs.set('run_sync', sync.checked ? '1' : '0'); d.close(); await startJob('/api/sourcing/run', { target: Number(target.value), max_partners: Number(partners.value), sync: sync.checked, recheck: recheck.checked }, mount, () => { after(); loadState(); }); };
  const d = openOverlay({ kind: 'dialog', title: 'Start a sourcing run', body: h('form', { class: 'drawer-body form', onsubmit: (e) => { e.preventDefault(); go(); } },
    h('div', { class: 'field-row' }, h('label', {}, 'Target accepted companies', target), h('label', {}, 'Partners to visit', partners)),
    h('label', { class: 'check' }, sync, 'Write each accepted company to the outbound sheet and verify it'),
    !r.outbound.ready ? h('p', { class: 'hint' }, 'Sheet writing is unavailable until the outbound sheet is set, Google is connected and writing is enabled in Settings. Accepted companies are still saved locally.') : null,
    h('label', { class: 'check' }, recheck, 'Re-research companies previously rejected'),
    h('p', { class: 'note' }, 'The target is a goal. If fewer companies can be evidenced, the run ends as “Partial” — it never accepts unproven companies.')),
  foot: [h('button', { class: 'btn', onclick: () => d.close() }, 'Cancel'), h('button', { class: 'btn primary', onclick: go }, 'Start run')] });
}
function evidenceBlock(rec) {
  const ev = rec.evidence || {};
  const fields = ['headquarters', 'b2b', 'active', 'stage', 'funding'];
  const names = { headquarters: 'Headquarters', b2b: 'Business model', active: 'Active operations', stage: 'Stage', funding: 'Total funding' };
  return h('div', { class: 'section' }, h('h3', {}, 'Evidence (exact quotes from fetched pages)'), fields.map((f) => h('div', {}, h('b', {}, names[f]),
    (ev[f] || []).length ? ev[f].map((e) => h('div', { class: 'evidence' }, h('blockquote', {}, '“' + e.quote + '”'), h('a', { href: demoLink(e.url), target: '_blank', rel: 'noopener noreferrer' }, e.url))) : h('p', { class: 'hint' }, 'No evidence found'))));
}
function demoLink(u) { try { const x = new URL(u); if (x.hostname.endsWith('.nventures-demo.test')) return `/demo-site/${x.hostname}${x.pathname}${x.search}`; } catch {} return u; }
function recordFacts(rec) {
  return h('div', { class: 'facts' },
    h('div', {}, h('small', {}, 'HQ country'), h('span', {}, rec.hq_country || 'Unknown')), h('div', {}, h('small', {}, 'Stage'), h('span', {}, rec.stage || 'Unknown')),
    h('div', {}, h('small', {}, 'Total funding (USD)'), h('span', {}, fmtUSD(rec.total_funding_usd))), h('div', {}, h('small', {}, 'B2B'), h('span', {}, rec.b2b === true ? 'Yes' : rec.b2b === false ? 'No' : 'Unknown')),
    h('div', {}, h('small', {}, 'Founded'), h('span', {}, rec.founded_year || '—')), h('div', {}, h('small', {}, 'Founders'), h('span', {}, rec.founders || '—')),
    h('div', {}, h('small', {}, 'Website'), rec.website ? h('a', { href: demoLink(rec.website), target: '_blank', rel: 'noopener noreferrer' }, rec.website) : '—'),
    h('div', {}, h('small', {}, 'LinkedIn'), rec.linkedin ? h('a', { href: rec.linkedin, target: '_blank', rel: 'noopener noreferrer' }, 'Company page') : '—'));
}
function sourcesBlock(sources) {
  const list = parse(sources);
  if (!Array.isArray(list) || !list.length) return null;
  return h('details', { class: 'section' }, h('summary', {}, `Sources consulted (${list.length})`), h('ul', { class: 'source-list' }, list.map((s) => h('li', {}, badge(s.status === 'ok' || s.status === 'cached' ? 'completed' : s.status === 'js_only' || s.status === 'truncated' ? 'partial' : 'failed'), h('span', {}, `${s.url} ${s.status !== 'ok' ? '(' + s.status + (s.note ? ': ' + s.note : '') + ')' : ''}`)))));
}
function openCandidate(c) {
  const rec = parse(c.record);
  const body = h('div', { class: 'drawer-body' },
    (c.reasons || []).length ? h('div', { class: 'note ' + (c.outcome === 'rejected' ? 'bad' : 'warn') }, h('b', {}, c.outcome === 'rejected' ? 'Rejected — evidence contradicts the mandate' : c.outcome === 'needs_research' ? 'Needs research — evidence missing (not assumed)' : 'Not saved'), h('ul', { class: 'reasons' }, c.reasons.map((x) => h('li', {}, x)))) : h('p', { class: 'note ok' }, 'Every hard requirement is evidenced.'),
    (rec.warnings || []).length ? h('div', { class: 'note' }, h('b', {}, 'Warnings'), h('ul', { class: 'reasons' }, rec.warnings.map((w) => h('li', {}, w)))) : null,
    rec.name ? recordFacts(rec) : null, rec.description ? h('p', {}, rec.description) : null, rec.evidence ? evidenceBlock(rec) : null, sourcesBlock(c.sources));
  const foot = [h('button', { class: 'btn', onclick: () => d.close() }, 'Close')];
  if (c.company_id) foot.unshift(h('a', { class: 'btn', href: '#/companies?company=' + c.company_id }, 'Open company'));
  if (c.outcome !== 'accepted' && c.outcome !== 'duplicate') foot.unshift(h('button', { class: 'btn', onclick: async () => { d.close(); await startJob('/api/sourcing/research', { urls: [c.url], partner: c.partner }, app.jobsMount || $('#main'), () => app.reloadPage && app.reloadPage()); } }, 'Research again'));
  const d = openOverlay({ title: c.name, sub: `${c.partner} · ${STATUS[c.outcome]?.[0] || c.outcome}`, body, foot });
}

// ---------- companies ----------
async function renderCompanies(main, params) {
  const r = app.state.readiness;
  const jobsMount = h('div');
  const table = dataTable({
    key: 'companies', selectable: true,
    extraFilters: [{ key: 'country', label: 'Country', options: app.state.countries }, { key: 'stage', label: 'Stage', options: ['bootstrapped', 'angel', 'pre-seed', 'seed'] }, { key: 'status', label: 'Sheet', options: ['pending', 'verified', 'already_exists', 'error', 'uncertain', 'conflict', 'blocked'] }],
    fetchPage: async (st) => api('/api/companies?' + qs({ ...st, status: st.filters.status || '' }, {})),
    columns: [
      { label: '', cls: 'tight', render: (c, reload) => starButton('companies', c, reload) },
      { label: 'Company', sort: 'name', render: (c) => { const d = parse(c.data); return [h('div', { class: 'cell-main' }, c.name), h('div', { class: 'cell-sub' }, d.description || d.website || '')]; } },
      { label: 'HQ', sort: 'country', cls: 'tight', render: (c) => parse(c.data).hq_country || '—' },
      { label: 'Stage', cls: 'tight hide-mobile', render: (c) => parse(c.data).stage || '—' },
      { label: 'Funding', sort: 'funding', cls: 'tight hide-mobile', render: (c) => fmtUSD(parse(c.data).total_funding_usd) },
      { label: 'Partner', sort: 'partner', cls: 'hide-mobile', render: (c) => c.partner },
      { label: 'Sheet', sort: 'sync', cls: 'tight', render: (c) => [badge(c.sync_status), c.sheet_row ? h('div', { class: 'cell-sub' }, 'row ' + c.sheet_row) : null] },
    ],
    onOpen: (c, reload) => openCompany(c.id, reload),
    bulk: (ids, done) => { const b = h('button', { class: 'btn sm primary', onclick: () => startJob('/api/sourcing/sync', { ids }, jobsMount, done) }, icon('sync'), 'Sync selected now'); if (!r.outbound.ready) disabledReason(b, 'Set and enable the outbound sheet in Settings first.'); return b; },
    emptyText: 'Accepted companies from sourcing runs appear here.',
  });
  const exportLink = h('a', { class: 'btn ghost', href: '/api/companies/export.csv', download: '' }, icon('download'), 'CSV');
  exportLink.addEventListener('click', () => { exportLink.href = '/api/companies/export.csv?' + qs({ ...table.state, status: table.state.filters.status || '' }); });
  main.replaceChildren(h('div', { class: 'page-head' }, h('div', {}, h('h2', {}, 'Sourced companies'), h('p', {}, 'Companies accepted against the outbound mandate, with their evidence. CSV exports neutralise formula-like cells.')), h('div', { class: 'actions' }, exportLink)), jobsMount, table);
  app.keys = table.keys; app.jobsMount = jobsMount; app.reloadPage = () => table.reload();
  if (params.get('company')) openCompany(Number(params.get('company')), () => table.reload());
}
async function openCompany(id, after) {
  let data;
  try { data = await api('/api/companies/' + id); } catch (e) { toast(e.message, 'bad'); return; }
  const c = data.company, rec = parse(c.data);
  const body = h('div', { class: 'drawer-body' },
    (data.qualification || []).length ? h('div', { class: 'note bad' }, h('b', {}, 'No longer qualifies'), h('ul', { class: 'reasons' }, data.qualification.map((x) => h('li', {}, x)))) : h('p', { class: 'note ok' }, 'Qualifies: eligible HQ, B2B, active, early stage and total funding ≤ $3M — each with a source quote.'),
    c.sync_error ? h('p', { class: 'note bad' }, c.sync_error) : null,
    recordFacts(rec), rec.description ? h('p', {}, rec.description) : null,
    (rec.warnings || []).length ? h('div', { class: 'note' }, h('b', {}, 'Warnings'), h('ul', { class: 'reasons' }, rec.warnings.map((w) => h('li', {}, w)))) : null,
    evidenceBlock(rec), sourcesBlock(rec.sources),
    (data.sync_ops || []).length ? h('details', { class: 'section' }, h('summary', {}, `Sheet write history (${data.sync_ops.length})`), h('ul', { class: 'source-list' }, data.sync_ops.map((o) => h('li', {}, badge(o.status), `${fmtDate(o.updated_at)} ${o.sheet_range || ''} ${o.error || ''}`)))) : null);
  const d = openOverlay({ title: c.name, sub: `${c.partner} · saved ${fmtDate(c.created_at)} · sheet: ${STATUS[c.sync_status]?.[0] || c.sync_status}${c.sheet_row ? ' (row ' + c.sheet_row + ')' : ''}`, body, foot: [h('button', { class: 'btn', onclick: () => d.close() }, 'Close')] });
}

// ---------- partners ----------
async function renderPartners(main) {
  const list = h('section', { class: 'panel' });
  async function load() {
    const ps = await api('/api/partners');
    list.replaceChildren(h('div', { class: 'panel-head' }, h('div', {}, h('h2', {}, 'Rotation order'), h('p', {}, 'Least recently sourced first. Counts and last-visit state persist across restarts and are mirrored to the control tab when syncing.'))),
      ps.length ? h('div', { class: 'table-wrap' }, h('table', {}, h('thead', {}, h('tr', {}, h('th', {}, 'Partner'), h('th', { class: 'hide-mobile' }, 'Website'), h('th', { class: 'tight' }, 'Runs'), h('th', { class: 'tight hide-mobile' }, 'Last added'), h('th', { class: 'tight hide-mobile' }, 'Last visit'), h('th', { class: 'tight' }, 'In rotation'))),
        h('tbody', {}, ps.map((p) => { const rep = parse(p.last_report); return h('tr', { onclick: (e) => { if (!e.target.closest('input,button,a')) openPartner(p, load); } },
          h('td', {}, h('div', { class: 'cell-main' }, p.name), (rep.warnings || []).length ? h('div', { class: 'cell-sub' }, rep.warnings[0]) : null),
          h('td', { class: 'hide-mobile' }, p.website ? h('a', { href: demoLink(p.website), target: '_blank', rel: 'noopener noreferrer' }, p.website) : h('span', { class: 'badge warn' }, 'no website')),
          h('td', { class: 'tight' }, p.run_count), h('td', { class: 'tight hide-mobile' }, p.last_added), h('td', { class: 'tight hide-mobile' }, p.last_sourced ? fmtDate(p.last_sourced) : 'Never'),
          h('td', { class: 'tight' }, h('input', { type: 'checkbox', checked: p.relevant, 'aria-label': 'Include ' + p.name + ' in rotation', onchange: async (e) => { try { await api('/api/partners/' + p.id, { method: 'PATCH', body: { relevant: e.target.checked } }); toast(e.target.checked ? 'Included in rotation' : 'Excluded from rotation'); } catch (err) { toast(err.message, 'bad'); e.target.checked = !e.target.checked; } } }))); })))) : h('div', { class: 'empty' }, h('b', {}, 'No partners'), 'Add a partner with its official website, or connect the partner sheet.'));
  }
  const name = h('input', { type: 'text', required: true, minlength: 2, maxlength: 200 });
  const site = h('input', { type: 'url', placeholder: 'https://partner.example', maxlength: 500 });
  const form = h('form', { class: 'panel-body form', onsubmit: async (e) => { e.preventDefault(); try { await api('/api/partners', { method: 'POST', body: { name: name.value, website: site.value } }); name.value = site.value = ''; toast('Partner added'); load(); } catch (err) { toast(err.message, 'bad'); } } },
    h('div', { class: 'field-row' }, h('label', {}, 'Partner name', name), h('label', {}, 'Official website', site)), h('div', { class: 'actions' }, h('button', { class: 'btn primary', type: 'submit' }, icon('plus'), 'Add partner')));
  main.replaceChildren(h('div', { class: 'page-head' }, h('div', {}, h('h2', {}, 'Portfolio partners'), h('p', {}, 'The crawler starts from each partner’s official site, follows portfolio pages, sitemaps and feeds, and collects official company links.'))),
    list, h('section', { class: 'panel', style: 'margin-top:16px' }, h('div', { class: 'panel-head' }, h('h2', {}, 'Add a partner')), form));
  app.keys = null;
  await load();
}
function openPartner(p, after) {
  const rep = parse(p.last_report);
  const site = h('input', { type: 'url', value: p.website, maxlength: 500 });
  const d = openOverlay({ title: p.name, sub: `${p.run_count} runs · last visit ${p.last_sourced ? fmtDate(p.last_sourced) : 'never'}`, body: h('div', { class: 'drawer-body' },
    h('label', {}, 'Official website', site),
    rep.pages_visited !== undefined ? h('div', { class: 'facts' }, h('div', {}, h('small', {}, 'Pages visited'), h('span', {}, rep.pages_visited)), h('div', {}, h('small', {}, 'Company links'), h('span', {}, rep.candidates)), h('div', {}, h('small', {}, 'Added last time'), h('span', {}, rep.added)), h('div', {}, h('small', {}, 'Crawl complete'), h('span', {}, rep.complete ? 'Yes' : 'No — limits or blocks'))) : h('p', { class: 'hint' }, 'Not crawled yet.'),
    (rep.warnings || []).length ? h('div', { class: 'note warn' }, h('b', {}, 'Last crawl warnings'), h('ul', { class: 'reasons' }, rep.warnings.map((w) => h('li', {}, w)))) : null),
  foot: [h('button', { class: 'btn', onclick: () => d.close() }, 'Close'), h('button', { class: 'btn primary', onclick: async () => { try { await api('/api/partners/' + p.id, { method: 'PATCH', body: { website: site.value } }); toast('Saved'); d.close(); after(); } catch (e) { toast(e.message, 'bad'); } } }, 'Save')] });
}

// ---------- activity ----------
async function renderActivity(main, params) {
  const jobsPanel = h('section', { class: 'panel' });
  const kind = h('select', { 'aria-label': 'Job type', onchange: () => loadJobs() }, h('option', { value: '' }, 'All jobs'), Object.entries(JOB_LABEL).map(([k, v]) => h('option', { value: k }, v)));
  async function loadJobs() {
    const js = await api('/api/jobs?limit=100' + (kind.value ? '&kind=' + kind.value : ''));
    jobsPanel.replaceChildren(h('div', { class: 'panel-head' }, h('h2', {}, 'Job history'), kind),
      js.length ? h('div', { class: 'table-wrap' }, h('table', {}, h('thead', {}, h('tr', {}, h('th', {}, 'Job'), h('th', { class: 'tight' }, 'Status'), h('th', { class: 'tight hide-mobile' }, 'Started'), h('th', { class: 'tight hide-mobile' }, 'Finished'))),
        h('tbody', {}, js.map((j) => h('tr', { onclick: () => openJob(j.id) }, h('td', {}, h('div', { class: 'cell-main' }, `${JOB_LABEL[j.kind] || j.kind} #${j.id}`, j.legacy_ref ? h('span', { class: 'badge', style: 'margin-left:6px' }, 'imported from Python app') : null), h('div', { class: 'cell-sub' }, j.message)), h('td', { class: 'tight' }, badge(j.status)), h('td', { class: 'tight hide-mobile' }, fmtDate(j.started_at)), h('td', { class: 'tight hide-mobile' }, fmtDate(j.finished_at))))))) : h('div', { class: 'empty' }, 'No jobs yet.'));
  }
  const auditKind = h('select', { 'aria-label': 'Event type', onchange: () => loadAudit() }, h('option', { value: '' }, 'All events'), ['accepted', 'rejected', 'needs_research', 'duplicate', 'sync', 'sync_error', 'inbox_review', 'error', 'warning', 'partner', 'settings', 'google_connected', 'migration', 'interrupted'].map((k) => h('option', { value: k }, k.replace(/_/g, ' '))));
  const audit = h('section', { class: 'panel', style: 'margin-top:16px' });
  async function loadAudit() { const ev = await api('/api/events?limit=300' + (auditKind.value ? '&kind=' + auditKind.value : '')); audit.replaceChildren(h('div', { class: 'panel-head' }, h('h2', {}, 'Audit log'), auditKind), ev.length ? eventList(ev) : h('div', { class: 'empty' }, 'No events.')); }
  main.replaceChildren(h('div', { class: 'page-head' }, h('div', {}, h('h2', {}, 'Progress & history'), h('p', {}, 'Every job keeps its progress, counters and events. Interrupted jobs are marked honestly after a restart; saved work is never discarded.'))), jobsPanel, audit);
  app.keys = null;
  await Promise.all([loadJobs(), loadAudit()]);
  if (params.get('job')) openJob(Number(params.get('job')));
}
async function openJob(id) {
  const data = await api('/api/jobs/' + id);
  const j = data.job, c = j.counters || {};
  const body = h('div', { class: 'drawer-body' }, h('p', {}, j.message),
    h('div', { class: 'facts' }, Object.entries(c).filter(([, v]) => typeof v !== 'object').map(([k, v]) => h('div', {}, h('small', {}, k.replace(/_/g, ' ')), h('span', {}, String(v))))),
    h('div', { class: 'facts' }, h('div', {}, h('small', {}, 'Started'), h('span', {}, fmtDate(j.started_at))), h('div', {}, h('small', {}, 'Finished'), h('span', {}, fmtDate(j.finished_at))), h('div', {}, h('small', {}, 'Mode'), h('span', {}, j.mode))),
    data.events.length ? h('div', { class: 'section' }, h('h3', {}, 'Events'), eventList(data.events)) : null);
  const foot = [h('button', { class: 'btn', onclick: () => d.close() }, 'Close')];
  if (j.status === 'running') foot.unshift(h('button', { class: 'btn danger', onclick: async () => { await api(`/api/jobs/${id}/cancel`, { method: 'POST' }); toast('Cancellation requested'); d.close(); } }, 'Cancel job'));
  const d = openOverlay({ title: `${JOB_LABEL[j.kind] || j.kind} #${j.id}`, sub: STATUS[j.status]?.[0] || j.status, body, foot });
}

// ---------- settings ----------
async function renderSettings(main, params) {
  const s = await api('/api/settings');
  const r = app.state.readiness;
  const g = r.google;
  const msg = params.get('google');
  const field = (key, label, hint, type = 'text') => { const i = h('input', { type, value: s[key] ?? '', disabled: !s.editable, 'data-key': key, id: 'set-' + key }); return h('label', {}, label, i, hint ? h('span', { class: 'hint' }, hint) : null); };
  const syncOut = h('input', { type: 'checkbox', checked: s.outbound_sync, disabled: !s.editable, 'data-key': 'outbound_sync' });
  const saveDest = async () => {
    const body = {}; $$('[data-key]', destForm).forEach((el) => { body[el.dataset.key] = el.type === 'checkbox' ? el.checked : el.value; });
    try { await api('/api/settings', { method: 'POST', body }); toast('Settings saved'); route(); } catch (e) { toast(e.message, 'bad'); }
  };
  const destForm = h('form', { class: 'panel-body form', onsubmit: (e) => { e.preventDefault(); saveDest(); } },
    !s.editable ? h('p', { class: 'note' }, 'Demo mode uses fixed local fixture destinations. Set APP_MODE=live in .env and restart to configure real sheets.') : null,
    field('expected_account', 'Expected Gmail account', 'Imports refuse to run if a different mailbox is connected.', 'email'),
    h('div', { class: 'field-row' }, field('inbound_sheet', 'Inbound sheet ID', 'From the sheet URL: /spreadsheets/d/<ID>/'), field('inbound_tab', 'Inbound tab', 'Headers: Company Name, Description, HQ, Contact, Status, Notes')),
    h('div', { class: 'field-row' }, field('outbound_sheet', 'Outbound sheet ID', 'Legacy 38-column sourcing sheet'), field('outbound_tab', 'Outbound tab')),
    h('div', { class: 'field-row' }, field('partner_tab', 'Partner list tab'), field('control_tab', 'Partner control tab')),
    field('gmail_query', 'Gmail search query', 'Standard Gmail search syntax; spam, trash and sent are excluded.'),
    h('label', { class: 'check' }, syncOut, 'Allow writing sourced companies to the outbound sheet'),
    s.editable ? h('div', { class: 'actions' }, h('button', { class: 'btn primary', type: 'submit' }, 'Save destinations')) : null);
  const testOut = h('div');
  const runTest = async () => { testOut.replaceChildren(h('p', { class: 'hint' }, 'Running read-only checks…')); try { const res = await api('/api/google/test', { method: 'POST' }); testOut.replaceChildren(h('ul', { class: 'ready-list' }, res.map((c) => h('li', {}, h('span', { class: 'dot ' + (c.Status === 'ok' ? 'ok' : c.Status === 'missing' ? 'warn' : 'bad') }), h('div', {}, h('b', {}, c.Name), h('span', {}, c.Message)))))); } catch (e) { testOut.replaceChildren(h('p', { class: 'note bad' }, e.message)); } };
  const fileIn = h('input', { type: 'file', accept: 'application/json,.json', 'aria-label': 'OAuth client JSON file' });
  const upload = async () => { const f = fileIn.files[0]; if (!f) { toast('Choose the downloaded client JSON file', 'bad'); return; } try { await api('/api/google/client', { method: 'POST', body: { json: await f.text() } }); toast('Client stored locally'); route(); } catch (e) { toast(e.message, 'bad'); } };
  const connectBtn = h('a', { class: 'btn primary', href: '/oauth/start' }, g.connected ? 'Reconnect Google' : 'Connect Google');
  if (!g.client_configured) disabledReason(connectBtn, 'Upload the OAuth client JSON first.');
  const googlePanel = h('section', { class: 'panel' }, h('div', { class: 'panel-head' }, h('div', {}, h('h2', {}, 'Google connection'), h('p', {}, 'One local OAuth connection: Gmail read-only + Sheets. Tokens stay in your data folder with user-only permissions.'))),
    h('div', { class: 'panel-body form' },
      msg === 'connected' ? h('p', { class: 'note ok', role: 'status' }, 'Google connected.') : msg === 'error' ? h('p', { class: 'note bad', role: 'alert' }, params.get('message') || 'Connection failed') : null,
      h('ul', { class: 'ready-list' },
        h('li', {}, h('span', { class: 'dot ' + (g.client_configured ? 'ok' : 'bad') }), h('div', {}, h('b', {}, 'OAuth client'), h('span', {}, g.client_configured ? 'Configured (' + g.client_source + ')' : 'Missing'))),
        h('li', {}, h('span', { class: 'dot ' + (g.connected ? 'ok' : 'bad') }), h('div', {}, h('b', {}, 'Consent'), h('span', {}, g.connected ? `Connected (${g.token_source})${g.account ? ' as ' + g.account : ''}` : 'Not connected'))),
        g.service_account ? h('li', {}, h('span', { class: 'dot ok' }), h('div', {}, h('b', {}, 'Service account'), h('span', {}, 'Present in .env (Sheets only)'))) : null),
      r.mode === 'live' ? h('div', { class: 'form' }, h('label', {}, 'OAuth client JSON (Desktop app) from Google Cloud Console', fileIn), h('div', { class: 'actions' }, h('button', { class: 'btn', onclick: upload }, 'Store client file'), connectBtn,
        g.connected ? h('button', { class: 'btn ghost', onclick: async () => { if (await confirmDialog('Disconnect Google?', 'This removes the local token. To also revoke it at Google, visit your Google account permissions page.', 'Disconnect')) { await api('/api/google/disconnect', { method: 'POST' }); route(); } } }, 'Disconnect') : null))
        : h('div', { class: 'actions' }, h('a', { class: 'btn', href: '/oauth/start' }, 'Try the consent flow (demo fixture)')),
      h('div', { class: 'actions' }, h('button', { class: 'btn', onclick: runTest }, 'Test connection (read-only)')), testOut));
  const prefsPanel = h('section', { class: 'panel' }, h('div', { class: 'panel-head' }, h('h2', {}, 'Display')), h('div', { class: 'panel-body form' },
    h('label', {}, 'Theme', (() => { const sel = h('select', { onchange: () => { prefs.set('theme', sel.value); applyTheme(sel.value); } }, ['system', 'light', 'dark'].map((v) => h('option', { value: v }, v[0].toUpperCase() + v.slice(1)))); sel.value = prefs.get('theme', 'system'); return sel; })()),
    h('label', { class: 'check' }, h('input', { type: 'checkbox', checked: document.documentElement.classList.contains('compact'), onchange: toggleCompact }), 'Compact density'),
    h('button', { class: 'btn ghost', onclick: () => { for (const k of ['pitches', 'companies']) prefs.set('view.' + k, '{}'); toast('Saved table filters cleared'); } }, 'Clear saved filters')));
  const llmPanel = h('section', { class: 'panel' }, h('div', { class: 'panel-head' }, h('div', {}, h('h2', {}, 'Local model (optional)'), h('p', {}, 'Off by default. Suggestions are advisory only and never change qualification, statuses or sheets.'))), h('div', { class: 'panel-body form', id: 'llm-body' }, h('p', { class: 'hint' }, 'Checking for a local Ollama server…')));
  const other = h('section', { class: 'panel' }, h('div', { class: 'panel-head' }, h('h2', {}, 'Providers & data')), h('div', { class: 'panel-body form' },
    h('ul', { class: 'ready-list' },
      h('li', {}, h('span', { class: 'dot ok' }), h('div', {}, h('b', {}, 'Discovery'), h('span', {}, 'First-party crawler (no Tavily). Pages fetched: ' + r.discovery.counters.pages_fetched + ' · cache hits: ' + r.discovery.counters.cache_hits))),
      h('li', {}, h('span', { class: 'dot ok' }), h('div', {}, h('b', {}, 'Extraction'), h('span', {}, 'Deterministic local rules (no OpenRouter).'))),
      (r.legacy_adapters.tavily_key_present || r.legacy_adapters.openrouter_key_present) ? h('li', {}, h('span', { class: 'dot warn' }), h('div', {}, h('b', {}, 'Legacy paid keys present in .env'), h('span', {}, r.legacy_adapters.note))) : null,
      h('li', {}, h('span', { class: 'dot' }), h('div', {}, h('b', {}, 'Data folder'), h('span', {}, h('code', {}, r.data_dir))))),
    h('div', { class: 'actions' }, h('button', { class: 'btn', onclick: async () => { const x = await api('/api/cache/clear', { method: 'POST' }); toast(`Cleared ${x.cleared} cached pages`); } }, 'Clear page cache'),
      r.mode === 'demo' ? h('button', { class: 'btn danger', onclick: async () => { if (await confirmDialog('Reset demo data?', 'All demo pitches, companies and history are removed and the fixture sheets are emptied. A backup of the demo database is written first. Live data is not affected.', 'Reset demo')) { try { await api('/api/demo/reset', { method: 'POST' }); toast('Demo data reset'); route(); } catch (e) { toast(e.message, 'bad'); } } } }, 'Reset demo data') : null)));
  main.replaceChildren(h('div', { class: 'page-head' }, h('div', {}, h('h2', {}, 'Settings'), h('p', {}, `Mode: ${r.mode}. Configuration from ${s.env_file || 'environment'} plus the values saved here.`))),
    h('div', { class: 'grid cols-2' }, googlePanel, h('section', { class: 'panel' }, h('div', { class: 'panel-head' }, h('h2', {}, 'Destinations')), destForm)),
    h('div', { class: 'grid cols-3', style: 'margin-top:16px' }, prefsPanel, llmPanel, other));
  app.keys = null;
  api('/api/llm').then((st) => {
    const body = $('#llm-body'); if (!body) return;
    const enabled = h('input', { type: 'checkbox', checked: s.llm_enabled, disabled: !st.available && !s.llm_enabled });
    const model = h('select', { disabled: !st.available }, h('option', { value: '' }, 'Choose a local model'), st.models.filter((m) => m.local).map((m) => h('option', { value: m.name }, m.name)));
    model.value = s.llm_model || '';
    body.replaceChildren(st.available ? h('p', { class: 'note ok' }, `Ollama found at ${st.endpoint} with ${st.models.filter((m) => m.local).length} local model(s).`) : h('p', { class: 'note' }, st.reason || 'No local model available.'),
      st.models.filter((m) => !m.local).length ? h('p', { class: 'hint' }, 'Cloud-backed models are hidden and never used.') : null,
      h('label', { class: 'check' }, enabled, 'Enable “Suggest with local model” in pitch review'), h('label', {}, 'Model', model),
      h('button', { class: 'btn', disabled: !st.available && !s.llm_enabled, onclick: async () => { try { await api('/api/settings', { method: 'POST', body: { llm_enabled: enabled.checked, llm_model: model.value } }); toast('Saved'); loadState(); } catch (e) { toast(e.message, 'bad'); } } }, 'Save'));
  }).catch(() => {});
}

// ---------- command palette & shortcuts ----------
const commands = () => [
  ['Go to Overview', 'g d', () => (location.hash = '#/')], ['Go to Pitch inbox', 'g i', () => (location.hash = '#/inbox')], ['Go to Sourcing', 'g s', () => (location.hash = '#/sourcing')],
  ['Go to Companies', 'g c', () => (location.hash = '#/companies')], ['Go to Partners', 'g p', () => (location.hash = '#/partners')], ['Go to Activity', 'g a', () => (location.hash = '#/activity')],
  ['Go to Settings', 'g t', () => (location.hash = '#/settings')], ['Import pitches from Gmail', '', () => (location.hash = '#/inbox?run=1')], ['Start a sourcing run', '', () => (location.hash = '#/sourcing?run=1')],
  ['Toggle theme', 't', cycleTheme], ['Toggle compact density', 'c', toggleCompact], ['Show keyboard shortcuts', '?', showShortcuts],
  ['Export companies CSV', '', () => (location.href = '/api/companies/export.csv')], ['Export pitches CSV', '', () => (location.href = '/api/pitches/export.csv')],
];
function openPalette() {
  if (overlays.some((o) => o.panel.classList.contains('palette'))) return;
  const input = h('input', { type: 'text', placeholder: 'Type a command…', 'aria-label': 'Command', role: 'combobox', 'aria-expanded': 'true', 'aria-controls': 'pal-list', autocomplete: 'off' });
  const list = h('ul', { role: 'listbox', id: 'pal-list' });
  let sel = 0, items = [];
  const render = () => { const q = input.value.toLowerCase(); items = commands().filter(([n]) => n.toLowerCase().includes(q)); sel = Math.min(sel, Math.max(0, items.length - 1)); list.replaceChildren(...items.map(([n, k, fn], i) => h('li', { role: 'option', id: 'pal-' + i, 'aria-selected': String(i === sel), onclick: () => { d.close(); fn(); } }, n, k ? h('small', {}, k) : null))); input.setAttribute('aria-activedescendant', 'pal-' + sel); };
  input.addEventListener('input', () => { sel = 0; render(); });
  input.addEventListener('keydown', (e) => { if (e.key === 'ArrowDown') { sel = Math.min(items.length - 1, sel + 1); render(); e.preventDefault(); } else if (e.key === 'ArrowUp') { sel = Math.max(0, sel - 1); render(); e.preventDefault(); } else if (e.key === 'Enter' && items[sel]) { e.preventDefault(); d.close(); items[sel][2](); } });
  const d = openOverlay({ kind: 'dialog', label: 'Command palette', body: h('div', {}, input, list), className: 'palette' });
  render();
}
function showShortcuts() {
  const rows = [['Ctrl/⌘ K', 'Command palette'], ['?', 'This help'], ['/', 'Focus search'], ['g then d·i·s·c·p·a·t', 'Go to a page'], ['j / k', 'Next / previous row'], ['Enter', 'Open row'], ['x', 'Select row'], ['t', 'Cycle theme'], ['c', 'Compact density'], ['Ctrl/⌘ Enter', 'Approve pitch (in review)'], ['Esc', 'Close panel or dialog']];
  openOverlay({ kind: 'dialog', title: 'Keyboard shortcuts', body: h('div', { class: 'drawer-body' }, h('div', { class: 'shortcuts' }, rows.map(([k, v]) => [h('kbd', {}, k), h('span', {}, v)]))) });
}
let gPending = 0;
document.addEventListener('keydown', (e) => {
  const editing = e.target.closest && e.target.closest('input,textarea,select,[contenteditable]');
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); openPalette(); return; }
  if (e.key === 'Escape' && $('#sidebar').classList.contains('open')) { $('#sidebar').classList.remove('open'); $('#menu-btn').setAttribute('aria-expanded', 'false'); $('#menu-btn').focus(); return; }
  if (editing || e.ctrlKey || e.metaKey || e.altKey || overlays.length) return;
  if (gPending && Date.now() - gPending < 1200) {
    const map = { d: '#/', i: '#/inbox', s: '#/sourcing', c: '#/companies', p: '#/partners', a: '#/activity', t: '#/settings' };
    gPending = 0; if (map[e.key]) { e.preventDefault(); location.hash = map[e.key]; } return;
  }
  if (e.key === 'g') { gPending = Date.now(); return; }
  if (e.key === '?') { e.preventDefault(); showShortcuts(); return; }
  if (e.key === '/') { const s = $('[data-search]'); if (s) { e.preventDefault(); s.focus(); } return; }
  if (e.key === 't') { cycleTheme(); return; }
  if (e.key === 'c') { toggleCompact(); return; }
  if (e.key === 's') { const cur = $('tbody tr[aria-current] .star'); if (cur) { cur.click(); return; } }
  if (app.keys && app.keys(e)) e.preventDefault();
});

// ---------- boot ----------
$('#theme-btn').addEventListener('click', cycleTheme);
$('#help-btn').addEventListener('click', showShortcuts);
$('#palette-btn').addEventListener('click', openPalette);
$('#menu-btn').addEventListener('click', () => { const sb = $('#sidebar'); const open = !sb.classList.contains('open'); sb.classList.toggle('open', open); $('#menu-btn').setAttribute('aria-expanded', String(open)); if (open) $('nav a', sb).focus(); });
applyTheme(prefs.get('theme', 'system'));
app.focusMain = false;
route();
setInterval(() => { if (document.visibilityState === 'visible' && !app.watchers.size) loadState().catch(() => {}); }, 30000);
