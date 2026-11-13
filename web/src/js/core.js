// nVentures Sourcing UI — native JavaScript, no framework, no external assets.
// All untrusted text is inserted with textContent via h(); never innerHTML.
// replaceChildren/append stringify null; skip empty optional children instead.

for (const m of ['replaceChildren', 'append', 'prepend']) {
  const orig = Element.prototype[m];
  Element.prototype[m] = function (...kids) { return orig.apply(this, kids.flat(Infinity).filter((k) => k !== null && k !== undefined && k !== false)); };
}
export const $ = (s, el = document) => el.querySelector(s);
export const $$ = (s, el = document) => Array.from(el.querySelectorAll(s));

// act() prevents duplicate submissions: while a handler's promise is pending
// the button (or form) ignores further clicks and shows a busy state.
export function act(fn) {
  return async function (e) {
    const el = e && e.currentTarget;
    if (el && el.dataset.busy) { e.preventDefault(); return; }
    let r;
    try { r = fn.call(this, e); } catch (err) { throw err; }
    if (!(r instanceof Promise) || !el) return r;
    el.dataset.busy = '1'; el.setAttribute('aria-busy', 'true'); if (el.tagName === 'BUTTON') el.disabled = true;
    try { return await r; } finally { delete el.dataset.busy; el.removeAttribute('aria-busy'); if (el.tagName === 'BUTTON' && el.getAttribute('aria-disabled') !== 'true') el.disabled = false; }
  };
}
export function h(tag, attrs, ...kids) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v === undefined || v === null || v === false) continue;
    if (k === 'class') el.className = v;
    else if ((k === 'onclick' && tag === 'button') || k === 'onsubmit') el.addEventListener(k.slice(2), act(v));
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
export const icon = (name) => { const s = document.createElementNS('http://www.w3.org/2000/svg', 'svg'); s.setAttribute('aria-hidden', 'true'); const u = document.createElementNS('http://www.w3.org/2000/svg', 'use'); u.setAttribute('href', '#i-' + name); s.append(u); return s; };

export async function api(path, opts = {}) {
  const init = { method: opts.method || 'GET', headers: { 'X-NV-Request': '1' } };
  if (opts.body !== undefined) { init.headers['Content-Type'] = 'application/json'; init.body = JSON.stringify(opts.body); }
  let res;
  try { res = await fetch(path, init); } catch {
    // A single failed request (for example one cut off by navigation) is not
    // proof the app stopped: confirm with a health check before saying so.
    if (!(await reachable())) setOffline(true);
    throw new Error('The local nVentures server is not reachable. Is it still running? Start it again with start-go.sh.');
  }
  setOffline(false);
  const text = await res.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch { data = null; }
  if (res.status === 401 && !path.startsWith('/api/auth/') && app.signedOut) { app.signedOut(); throw new Error('Please sign in'); }
  if (!res.ok) throw new Error((data && data.error) || `Request failed (${res.status})`);
  return data;
}

let offline = false;
async function reachable() {
  try { const r = await fetch('/health', { cache: 'no-store', signal: AbortSignal.timeout(3000) }); return r.ok; } catch { return false; }
}
export function setOffline(on) {
  if (on === offline) return;
  offline = on;
  const el = $('#offline');
  if (!el) return;
  el.hidden = !on;
  if (on) pollOnline();
}
export async function pollOnline() {
  while (offline) {
    await new Promise((r) => setTimeout(r, 3000));
    try { const r = await fetch('/health', { cache: 'no-store' }); if (r.ok) { setOffline(false); toast('Reconnected to the local app'); app.route(); } } catch {}
  }
}
window.addEventListener('offline', () => setOffline(true));

// ---------- formatting ----------
export const fmtDate = (s) => { if (!s) return '—'; const d = new Date(s); return isNaN(d) ? s : d.toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' }); };
export const fmtTime = (s) => { const d = new Date(s); return isNaN(d) ? '' : d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' }); };
export const fmtUSD = (n) => (n === null || n === undefined || n === '') ? 'Unknown' : '$' + Number(n).toLocaleString(undefined, { maximumFractionDigits: 0 });
export const parse = (v) => { if (typeof v === 'string') { try { return JSON.parse(v); } catch { return {}; } } return v || {}; };
export const STATUS = {
  ready: ['Ready', 'info'], review: ['Needs review', 'warn'], skipped: ['Skipped', ''], duplicate: ['Duplicate', ''], verified: ['In sheet ✓', 'ok'],
  already_exists: ['Already in sheet', 'ok'], demo_verified: ['In sheet (legacy demo)', 'ok'], error: ['Error', 'bad'], uncertain: ['Write uncertain', 'bad'],
  conflict: ['Conflict', 'bad'], syncing: ['Syncing…', 'info'], pending: ['Not synced', ''], blocked: ['Blocked', 'bad'], demo: ['Demo', ''],
  running: ['Running', 'info'], completed: ['Completed', 'ok'], completed_with_errors: ['Completed with errors', 'warn'], partial: ['Partial', 'warn'],
  cancelled: ['Cancelled', ''], failed: ['Failed', 'bad'], interrupted: ['Interrupted', 'warn'],
  accepted: ['Accepted', 'ok'], rejected: ['Rejected', 'bad'], needs_research: ['Needs research', 'warn'],
};
export const badge = (s) => { const [t, c] = STATUS[s] || [s || '—', '']; return h('span', { class: 'badge ' + c }, t); };
export const JOB_LABEL = { inbound_import: 'Inbox import', inbound_sync: 'Inbound sync', outbound_run: 'Sourcing run', outbound_sync: 'Outbound sync', outbound_research: 'Company research' };

// ---------- preferences ----------
export const prefs = {
  get(k, d) { try { const v = localStorage.getItem('nv.' + k); return v === null ? d : v; } catch { return d; } },
  set(k, v) { try { localStorage.setItem('nv.' + k, v); } catch {} api('/api/preferences', { method: 'POST', body: { [k]: String(v) } }).catch(() => {}); },
  json(k, d) { try { return JSON.parse(this.get(k, '')) ?? d; } catch { return d; } },
};
export function applyTheme(pref) {
  const dark = pref === 'dark' || (pref === 'system' && matchMedia('(prefers-color-scheme: dark)').matches);
  document.documentElement.dataset.theme = dark ? 'dark' : 'light';
  document.documentElement.dataset.themePref = pref;
  const b = $('#theme-btn');
  if (b) { b.setAttribute('aria-label', `Theme: ${pref}${pref === 'system' ? ' (' + (dark ? 'dark' : 'light') + ')' : ''}. Click to change.`); const l = $('#theme-label'); if (l) l.textContent = pref[0].toUpperCase() + pref.slice(1); }
  const m = $('meta[name="theme-color"]'); if (m) m.setAttribute('content', dark ? '#0d1218' : '#f4f6f8');
}
export function cycleTheme() {
  const order = ['system', 'light', 'dark'];
  const next = order[(order.indexOf(prefs.get('theme', 'system')) + 1) % 3];
  prefs.set('theme', next); applyTheme(next); toast('Theme: ' + next);
}
export function toggleCompact() {
  const on = !document.documentElement.classList.contains('compact');
  document.documentElement.classList.toggle('compact', on); prefs.set('compact', on ? '1' : '0'); toast(on ? 'Compact density' : 'Comfortable density');
}
// Follow OS theme changes while "System" is selected. The change event is the
// primary signal; focus/visibility and a light 2 s check cover browsers or
// embedded views that do not deliver it.
const darkMQ = matchMedia('(prefers-color-scheme: dark)');
let lastSystemDark = darkMQ.matches;
export function syncSystemTheme() {
  const now = darkMQ.matches;
  if (now === lastSystemDark) return;
  lastSystemDark = now;
  if (prefs.get('theme', 'system') === 'system') applyTheme('system');
}
darkMQ.addEventListener('change', syncSystemTheme);
window.addEventListener('focus', syncSystemTheme);
document.addEventListener('visibilitychange', syncSystemTheme);
setInterval(syncSystemTheme, 2000);

// ---------- toasts ----------
export function toast(msg, kind = '') {
  const t = h('div', { class: 'toast ' + kind }, kind === 'bad' ? icon('alert') : icon('check'), h('span', {}, msg));
  $('#toasts').append(t); setTimeout(() => t.remove(), kind === 'bad' ? 9000 : 4000);
}

// ---------- overlays with focus management ----------
export const overlays = [];
export function focusables(el) { return $$('a[href],button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])', el).filter((x) => x.offsetParent !== null || x === document.activeElement); }
export function openOverlay({ kind = 'drawer', title, sub, body, foot, label, onClose, className = '', isDirty }) {
  const prev = document.activeElement;
  const id = 'ov-' + Math.random().toString(36).slice(2);
  const panel = h('div', { class: (kind === 'drawer' ? 'drawer' : 'dialog') + ' ' + className, role: 'dialog', 'aria-modal': 'true', 'aria-labelledby': title ? id : null, 'aria-label': title ? null : label, tabindex: '-1' },
    title ? h('div', { class: 'drawer-head' }, h('div', { style: 'flex:1;min-width:0' }, h('h2', { id }, title), sub ? h('p', { class: 'sub' }, sub) : null), h('button', { class: 'icon-btn', 'aria-label': 'Close', onclick: () => close() }, icon('close'))) : null,
    body, foot ? h('div', { class: 'drawer-foot' }, foot) : null);
  const ov = h('div', { class: 'overlay ' + (kind === 'drawer' ? '' : 'dialog-wrap') }, panel);
  ov.addEventListener('mousedown', (e) => { if (e.target === ov) close(); });
  const rec = { ov, panel, close, prev, isDirty };
  async function close(force) {
    const i = overlays.indexOf(rec); if (i < 0) return true;
    if (force !== true && isDirty && isDirty()) {
      if (!(await confirmDialog('Discard unsaved changes?', 'You edited this record but did not save it. Your changes will be lost.', 'Discard changes'))) return false;
    }
    overlays.splice(i, 1); ov.remove(); onClose && onClose();
    if (prev && document.contains(prev)) prev.focus();
    return true;
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
export const anyDirty = () => overlays.some((o) => o.isDirty && o.isDirty());
window.addEventListener('beforeunload', (e) => { if (anyDirty()) { e.preventDefault(); e.returnValue = ''; } });
export function confirmDialog(title, text, okLabel = 'Confirm') {
  return new Promise((resolve) => {
    let done = false;
    const ok = h('button', { class: 'btn primary', onclick: () => { done = true; d.close(); resolve(true); } }, okLabel);
    const d = openOverlay({ kind: 'dialog', title, body: h('div', { class: 'drawer-body' }, h('p', {}, text)), foot: [h('button', { class: 'btn', onclick: () => d.close() }, 'Cancel'), ok], onClose: () => { if (!done) resolve(false); } });
  });
}

// ---------- global state ----------
export const app = { state: null, page: null, watchers: new Map(), me: null };

// ---------- roles ----------
const ROLE_HINT = { 'inbox.import': 'importing pitches', 'inbox.review': 'reviewing pitches', 'inbox.sync': 'syncing pitches', 'pitch.work': 'working pitches', 'sourcing.run': 'running sourcing', 'sourcing.sync': 'syncing companies', 'partners.edit': 'editing partners', 'company.work': 'working companies', shortlist: 'the shortlist', 'jobs.control': 'controlling jobs', 'templates.manage': 'editing templates', 'settings.manage': 'changing settings', 'users.manage': 'managing the team', 'schedules.manage': 'managing schedules', 'demo.reset': 'resetting demo data' };
export const can = (perm) => !!(app.me && app.me.permissions && app.me.permissions.includes(perm));
export const roleReason = (perm) => `Your role (${app.me ? app.me.role_name : 'signed out'}) doesn't include ${ROLE_HINT[perm] || 'this action'}. Ask an admin if you need it.`;
// need() disables a control the person's role can't use, with an explanation.
export function need(el, perm) {
  if (!el || can(perm)) return el;
  el.setAttribute('aria-disabled', 'true');
  el.title = roleReason(perm);
  el.addEventListener('click', (e) => { e.preventDefault(); e.stopImmediatePropagation(); toast(roleReason(perm), 'bad'); }, true);
  return el;
}
export async function loadState() {
  app.state = await api('/api/state');
  // If browser storage was cleared, fall back to the preference saved on the server.
  const sp = app.state.preferences || {};
  try {
    if (!localStorage.getItem('nv.theme') && sp['pref.theme']) { localStorage.setItem('nv.theme', sp['pref.theme']); applyTheme(sp['pref.theme']); }
    if (!localStorage.getItem('nv.compact') && sp['pref.compact']) { localStorage.setItem('nv.compact', sp['pref.compact']); document.documentElement.classList.toggle('compact', sp['pref.compact'] === '1'); }
  } catch {}
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

// ---------- form validation ----------
export function fieldError(el, msg) {
  const id = el.id || (el.id = 'f-' + Math.random().toString(36).slice(2));
  el.setAttribute('aria-invalid', 'true');
  const m = h('span', { class: 'field-error', id: id + '-err', role: 'alert' }, msg);
  el.setAttribute('aria-describedby', m.id);
  el.insertAdjacentElement('afterend', m);
}
export function clearErrors(fields) {
  for (const el of Object.values(fields)) { el.removeAttribute('aria-invalid'); el.removeAttribute('aria-describedby'); const n = el.nextElementSibling; if (n && n.classList.contains('field-error')) n.remove(); }
}
