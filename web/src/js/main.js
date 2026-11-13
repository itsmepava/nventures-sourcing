// Entry module: sign-in gate, workspace shell, router, palette and shortcuts.
// ---------- router ----------
import { $, $$, anyDirty, api, app, applyTheme, badge, can, cycleTheme, fmtDate, h, icon, loadState, openOverlay, overlays, prefs, setOffline, toast, toggleCompact } from './core.js';
import { renderActivity } from './views/activity.js';
import { openChangePassword, renderAuth } from './views/auth.js';
import { renderCompanies } from './views/companies.js';
import { renderDashboard } from './views/dashboard.js';
import { renderDemoSheet } from './views/demosheet.js';
import { renderInbox } from './views/inbox.js';
import { renderPartners } from './views/partners.js';
import { renderQueue } from './views/queue.js';
import { renderSettings } from './views/settings.js';
import { renderSourcing } from './views/sourcing.js';
import { renderTeam } from './views/team.js';

const routes = {
  '': ['Overview', renderDashboard], queue: ['My work', renderQueue], inbox: ['Pitch inbox', renderInbox], sourcing: ['Sourcing', renderSourcing],
  companies: ['Companies', renderCompanies], partners: ['Partners', renderPartners], activity: ['Activity', renderActivity],
  team: ['Team', renderTeam, 'users.manage'], settings: ['Settings', renderSettings], 'demo-sheet': ['Demo sheet', renderDemoSheet],
};
export function currentRoute() { const [path, qs] = location.hash.replace(/^#\/?/, '').split('?'); return { name: path.split('/')[0] || '', params: new URLSearchParams(qs || '') }; }
export async function route() {
  if (!app.me) return boot();
  const { name, params } = currentRoute();
  if (name === 'signin' || name === 'setup') { location.hash = '#/'; return; }
  let [title, render, perm] = routes[name] || routes[''];
  if (perm && !can(perm)) { [title, render] = routes['']; toast('That page is for admins only', 'bad'); }
  $('#page-title').textContent = title; document.title = title + ' · nVentures';
  $$('.sidebar nav a').forEach((a) => a.toggleAttribute('aria-current', false));
  const nav = $(`.sidebar nav a[data-nav="${name || 'dashboard'}"]`); if (nav) nav.setAttribute('aria-current', 'page');
  $('#sidebar').classList.remove('open'); $('#menu-btn').setAttribute('aria-expanded', 'false');
  while (overlays.length) await overlays[overlays.length - 1].close(true);
  app.jobsMount = null; app.reloadPage = null; app.keys = null;
  for (const w of app.watchers.values()) w.detach();
  const main = $('#main');
  main.replaceChildren(h('div', { class: 'loading', role: 'status' }, h('span', { class: 'spinner', 'aria-hidden': 'true' }), 'Loading…'));
  if (name !== app.page) window.scrollTo(0, 0);
  try { await loadState(); app.page = name; await render(main, params); }
  catch (e) { main.replaceChildren(h('div', { class: 'note bad error-state', role: 'alert' }, h('p', {}, e.message), h('button', { class: 'btn', onclick: () => route() }, icon('sync'), 'Try again'))); }
  if (app.focusMain) { main.focus({ preventScroll: true }); }
  app.focusMain = true;
  refreshBell();
}
app.route = route;
let lastHash = location.hash, revertingHash = false;
window.addEventListener('hashchange', async () => {
  if (revertingHash) { revertingHash = false; return; }
  if (anyDirty()) {
    const target = location.hash;
    revertingHash = true; location.hash = lastHash;
    const top = overlays.filter((o) => o.isDirty && o.isDirty()).pop();
    if (!(await top.close())) return;
    location.hash = target; return;
  }
  lastHash = location.hash;
  route();
});

// ---------- sign-in gate ----------
async function boot() {
  let st;
  try { st = await api('/api/auth/state'); } catch (e) { showAuth({ error: e.message }); return; }
  $('#version').textContent = `v${st.version} · localhost`;
  const mb = $('#mode-badge'); mb.hidden = false; mb.textContent = st.mode === 'demo' ? 'Demo' : 'Live'; mb.classList.toggle('live', st.mode === 'live');
  if (st.setup_needed) return showAuth({ setup: true, mode: st.mode });
  if (!st.user) return showAuth({ mode: st.mode });
  await enterWorkspace();
}
function showAuth(opts) {
  app.me = null;
  document.body.classList.remove('booting');
  $('#shell').hidden = true;
  const root = $('#auth-root'); root.hidden = false;
  renderAuth(root, { ...opts, onDone: () => enterWorkspace() });
}
async function enterWorkspace() {
  app.me = await api('/api/me');
  if (app.me.must_change_password) { showAuth({ mustChange: true }); return; }
  document.body.classList.remove('booting');
  $('#auth-root').hidden = true; $('#auth-root').replaceChildren();
  $('#shell').hidden = false;
  $$('[data-perm]').forEach((el) => { el.hidden = !can(el.dataset.perm); });
  $('#account-name').textContent = app.me.name;
  $('#avatar').textContent = app.me.name.split(/\s+/).map((x) => x[0]).slice(0, 2).join('').toUpperCase();
  app.focusMain = false;
  await route();
}
app.signedOut = () => { if (app.me) { app.me = null; toast('Your session ended. Please sign in again.', 'bad'); } boot(); };

// ---------- account menu ----------
function openAccount() {
  const me = app.me;
  const d = openOverlay({ kind: 'dialog', title: me.name, sub: `${me.email} · ${me.role_name}`, className: 'account-dialog', body: h('div', { class: 'drawer-body' },
    h('p', { class: 'hint' }, `Signed in as ${me.role_name}. ${me.last_login_at ? 'Last sign-in ' + fmtDate(me.last_login_at) + '.' : ''}`),
    h('div', { class: 'actions' },
      h('button', { class: 'btn', onclick: () => { d.close(true); openChangePassword(); } }, icon('key'), 'Change password'),
      h('button', { class: 'btn', onclick: () => { d.close(true); location.hash = '#/queue'; } }, icon('check'), 'My work'),
      h('button', { class: 'btn danger', onclick: async () => { await api('/api/auth/logout', { method: 'POST' }); d.close(true); app.me = null; location.hash = '#/'; boot(); } }, icon('logout'), 'Sign out'))) });
}

// ---------- notifications ----------
async function refreshBell() {
  if (!app.me) return;
  try {
    const n = await api('/api/notifications');
    const b = $('#bell-count'); b.hidden = !n.unread; b.textContent = n.unread > 9 ? '9+' : n.unread;
    $('#bell-btn').setAttribute('aria-label', n.unread ? `Notifications: ${n.unread} unread` : 'Notifications');
    app.notifications = n;
  } catch {}
}
async function openBell() {
  await refreshBell();
  const n = app.notifications || { items: [] };
  const list = n.items.length ? h('ul', { class: 'notif-list' }, n.items.map((x) => h('li', { class: x.read_at ? '' : 'unread' },
    h('a', { href: x.link || '#/', onclick: () => { api('/api/notifications/read', { method: 'POST', body: { ids: [x.id] } }).then(refreshBell); d.close(true); } }, x.message), h('small', {}, fmtDate(x.created_at)))))
    : h('div', { class: 'empty' }, h('b', {}, 'All caught up'), 'Assignments and scheduled job results appear here.');
  const d = openOverlay({ kind: 'dialog', title: 'Notifications', className: 'notif-dialog', body: h('div', { class: 'drawer-body' }, list),
    foot: n.unread ? [h('button', { class: 'btn', onclick: async () => { await api('/api/notifications/read', { method: 'POST', body: {} }); await refreshBell(); d.close(true); toast('All marked as read'); } }, 'Mark all read')] : null });
}

// ---------- command palette & shortcuts ----------
export const commands = () => [
  ['Go to Overview', 'g d', () => (location.hash = '#/')], ['Go to My work', 'g w', () => (location.hash = '#/queue')], ['Go to Pitch inbox', 'g i', () => (location.hash = '#/inbox')],
  ['Go to Sourcing', 'g s', () => (location.hash = '#/sourcing')], ['Go to Companies', 'g c', () => (location.hash = '#/companies')], ['Go to Partners', 'g p', () => (location.hash = '#/partners')],
  ['Go to Activity', 'g a', () => (location.hash = '#/activity')], can('users.manage') ? ['Go to Team', 'g m', () => (location.hash = '#/team')] : null, ['Go to Settings', 'g t', () => (location.hash = '#/settings')],
  can('inbox.import') ? ['Import pitches from Gmail', '', () => (location.hash = '#/inbox?run=1')] : null, can('sourcing.run') ? ['Start a sourcing run', '', () => (location.hash = '#/sourcing?run=1')] : null,
  ['Toggle theme', 't', cycleTheme], ['Toggle compact density', 'c', toggleCompact], ['Show keyboard shortcuts', '?', showShortcuts], ['Notifications', 'n', openBell], ['Change my password', '', openChangePassword],
  ['Export companies CSV', '', () => (location.href = '/api/companies/export.csv')], ['Export pitches CSV', '', () => (location.href = '/api/pitches/export.csv')],
].filter(Boolean);
export function openPalette() {
  if (!app.me || overlays.some((o) => o.panel.classList.contains('palette'))) return;
  const input = h('input', { type: 'text', placeholder: 'Type a command…', 'aria-label': 'Command', role: 'combobox', 'aria-expanded': 'true', 'aria-controls': 'pal-list', autocomplete: 'off' });
  const list = h('ul', { role: 'listbox', id: 'pal-list' });
  let sel = 0, items = [];
  const render = () => { const q = input.value.toLowerCase(); items = commands().filter(([n]) => n.toLowerCase().includes(q)); sel = Math.min(sel, Math.max(0, items.length - 1)); list.replaceChildren(...items.map(([n, k, fn], i) => h('li', { role: 'option', id: 'pal-' + i, 'aria-selected': String(i === sel), onclick: () => { d.close(true); fn(); } }, n, k ? h('small', {}, k) : null))); input.setAttribute('aria-activedescendant', 'pal-' + sel); };
  input.addEventListener('input', () => { sel = 0; render(); });
  input.addEventListener('keydown', (e) => { if (e.key === 'ArrowDown') { sel = Math.min(items.length - 1, sel + 1); render(); e.preventDefault(); } else if (e.key === 'ArrowUp') { sel = Math.max(0, sel - 1); render(); e.preventDefault(); } else if (e.key === 'Enter' && items[sel]) { e.preventDefault(); d.close(true); items[sel][2](); } });
  const d = openOverlay({ kind: 'dialog', label: 'Command palette', body: h('div', {}, input, list), className: 'palette' });
  render();
}
export function showShortcuts() {
  const rows = [['Ctrl/⌘ K', 'Command palette'], ['?', 'This help'], ['/', 'Focus search'], ['g then d·w·i·s·c·p·a·m·t', 'Go to a page'], ['j / k', 'Next / previous row'], ['Enter', 'Open row'], ['x', 'Select row'], ['s', 'Shortlist row'], ['n', 'Notifications'], ['t', 'Cycle theme'], ['c', 'Compact density'], ['Ctrl/⌘ Enter', 'Approve pitch (in review)'], ['Esc', 'Close panel or dialog']];
  openOverlay({ kind: 'dialog', title: 'Keyboard shortcuts', body: h('div', { class: 'drawer-body' }, h('div', { class: 'shortcuts' }, rows.map(([k, v]) => [h('kbd', {}, k), h('span', {}, v)]))) });
}
let gPending = 0;
document.addEventListener('keydown', (e) => {
  if (!app.me) return;
  const editing = e.target.closest && e.target.closest('input,textarea,select,[contenteditable]');
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); openPalette(); return; }
  if (e.key === 'Escape' && $('#sidebar').classList.contains('open')) { $('#sidebar').classList.remove('open'); $('#menu-btn').setAttribute('aria-expanded', 'false'); $('#menu-btn').focus(); return; }
  if (editing || e.ctrlKey || e.metaKey || e.altKey || overlays.length) return;
  if (gPending && Date.now() - gPending < 1200) {
    const map = { d: '#/', w: '#/queue', i: '#/inbox', s: '#/sourcing', c: '#/companies', p: '#/partners', a: '#/activity', m: '#/team', t: '#/settings' };
    gPending = 0; if (map[e.key]) { e.preventDefault(); location.hash = map[e.key]; } return;
  }
  if (e.key === 'g') { gPending = Date.now(); return; }
  if (e.key === '?') { e.preventDefault(); showShortcuts(); return; }
  if (e.key === '/') { const s = $('[data-search]'); if (s) { e.preventDefault(); s.focus(); } return; }
  if (e.key === 't') { cycleTheme(); return; }
  if (e.key === 'c') { toggleCompact(); return; }
  if (e.key === 'n') { openBell(); return; }
  if (e.key === 's') { const cur = $('tbody tr[aria-current] .star'); if (cur) { cur.click(); return; } }
  if (app.keys && app.keys(e)) e.preventDefault();
});

// ---------- boot ----------
$('#theme-btn').addEventListener('click', cycleTheme);
$('#offline-retry').addEventListener('click', async () => { try { const r = await fetch('/health', { cache: 'no-store' }); if (r.ok) { setOffline(false); route(); return; } } catch {} toast('Still not reachable. Start the app again with start-go.sh.', 'bad'); });
$('#palette-btn').addEventListener('click', openPalette);
$('#bell-btn').addEventListener('click', openBell);
$('#account-btn').addEventListener('click', openAccount);
$('#menu-btn').addEventListener('click', () => { const sb = $('#sidebar'); const open = !sb.classList.contains('open'); sb.classList.toggle('open', open); $('#menu-btn').setAttribute('aria-expanded', String(open)); if (open) $('nav a', sb).focus(); });
applyTheme(prefs.get('theme', 'system'));
app.focusMain = false;
boot();
setInterval(() => { if (app.me && document.visibilityState === 'visible' && !app.watchers.size) { loadState().catch(() => {}); refreshBell(); } }, 30000);
