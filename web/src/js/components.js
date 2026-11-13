// ---------- job progress ----------
import { $, $$, JOB_LABEL, STATUS, ago, api, app, badge, can, clearErrors, fieldError, fmtDate, fmtTime, fmtUSD, h, icon, need, parse, prefs, toast } from './core.js';

export class JobWatcher {
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
        running ? need(h('button', { class: 'btn sm', onclick: async () => { try { await api(`/api/jobs/${j.id}/cancel`, { method: 'POST' }); toast('Cancellation requested'); } catch (e) { toast(e.message, 'bad'); } } }, 'Cancel'), 'jobs.control') : null,
        j.created_by_name ? h('span', { class: 'hint' }, 'by ' + j.created_by_name) : null,
        !running && RETRYABLE.has(j.status) ? retryButton(j, this.mount, this.onDone) : null,
        !running ? h('a', { class: 'btn sm ghost', href: `#/activity?job=${j.id}` }, 'Details') : null),
      h('div', { class: 'progress', role: 'progressbar', 'aria-valuemin': 0, 'aria-valuemax': 100, 'aria-valuenow': j.progress, 'aria-label': 'Progress' }, h('div', { style: `width:${j.progress}%` })),
      h('div', { class: 'counters' }, keys.filter((k) => c[k]).map((k) => h('span', {}, k.replace(/_/g, ' ') + ': ', h('b', {}, c[k])))),
      c.more_available ? h('p', { class: 'hint' }, 'More matching messages remain — run the import again to continue.') : null,
      this.events.length ? h('details', { open: running }, h('summary', {}, `Events (${this.events.length})`), eventList(this.events.slice(-80).reverse())) : null);
  }
}
export const RETRYABLE = new Set(['failed', 'cancelled', 'interrupted', 'partial', 'completed_with_errors']);
export function retryButton(j, mount, onDone) {
  if ((j.params && j.params.legacy) || !can('jobs.control')) return null;
  return h('button', { class: 'btn sm', title: 'Start the same job again with the same settings', onclick: async () => {
    try { const n = await api(`/api/jobs/${j.id}/retry`, { method: 'POST' }); toast('Started again as job #' + n.id); if (mount) new JobWatcher(n.id, mount, onDone); } catch (e) { toast(e.message, 'bad'); }
  } }, icon('sync'), 'Retry');
}
export function eventList(events) {
  return h('ul', { class: 'events' }, events.map((e) => h('li', {}, h('time', { datetime: e.created_at, title: fmtDate(e.created_at) }, fmtTime(e.created_at)), h('span', { class: 'kind' }, e.kind.replace(/_/g, ' ')), h('p', {}, e.message, e.actor ? h('span', { class: 'actor' }, ' · ' + e.actor) : null))));
}
export async function startJob(path, body, mount, onDone) {
  try { const j = await api(path, { method: 'POST', body }); new JobWatcher(j.id, mount, onDone); return j; }
  catch (e) { toast(e.message, 'bad'); return null; }
}

// ---------- generic table ----------
export function dataTable({ key, columns, fetchPage, onOpen, selectable, tabs, extraFilters, bulk, emptyText, rowClass }) {
  const saved = prefs.json('view.' + key, {});
  const st = { q: saved.q || '', status: saved.status ?? (tabs ? tabs[0][0] : ''), sort: saved.sort || '', desc: saved.desc ?? false, page: 1, per: Number(prefs.get('per_page', '25')), starred: saved.starred || false, filters: saved.filters || {}, selected: new Set(), cursor: -1, items: [] };
  const root = h('section', { class: 'panel' });
  const search = h('input', { type: 'search', placeholder: 'Search…', 'aria-label': 'Search', value: st.q, 'data-search': '1' });
  let debounce;
  search.addEventListener('input', () => { clearTimeout(debounce); debounce = setTimeout(() => { st.q = search.value; st.page = 1; save(); load(); }, 220); });
  const tabBar = tabs ? h('div', { class: 'tabs', role: 'group', 'aria-label': 'Filter by status' }) : null;
  const starBtn = h('button', { class: 'btn sm', 'aria-pressed': String(st.starred), onclick: () => { st.starred = !st.starred; starBtn.setAttribute('aria-pressed', String(st.starred)); st.page = 1; save(); load(); } }, icon('star'), 'Shortlist');
  const filterEls = (extraFilters || []).map((f) => { const s = h('select', { 'aria-label': f.label, onchange: () => { st.filters[f.key] = s.value; st.page = 1; save(); load(); } }, h('option', { value: '' }, f.label + ': all'), f.options.map((o) => Array.isArray(o) ? h('option', { value: o[0] }, o[1]) : h('option', { value: o }, o))); s.value = st.filters[f.key] || ''; return s; });
  const defaultStatus = tabs ? tabs[0][0] : '';
  const active = () => !!(st.q || st.starred || Object.values(st.filters).some(Boolean) || st.status !== defaultStatus);
  const clearBtn = h('button', { class: 'btn sm ghost', onclick: () => clearAll() }, icon('close'), 'Clear filters');
  function clearAll() {
    st.q = ''; search.value = ''; st.starred = false; starBtn.setAttribute('aria-pressed', 'false'); st.filters = {}; filterEls.forEach((el) => { el.value = ''; }); st.status = defaultStatus; st.page = 1; st.sort = ''; st.desc = false; save(); load();
  }
  search.setAttribute('aria-label', 'Search ' + key);
  const toolbar = h('div', { class: 'toolbar' }, h('label', { class: 'search' }, icon('search'), search), tabBar, filterEls, starBtn, clearBtn);
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
    clearBtn.hidden = !active();
    tableWrap.setAttribute('aria-busy', 'true');
    if (!tableWrap.firstChild) tableWrap.append(h('div', { class: 'loading', role: 'status' }, h('span', { class: 'spinner', 'aria-hidden': 'true' }), 'Loading…'));
    let res;
    try { res = await fetchPage(st); } catch (e) { tableWrap.replaceChildren(h('div', { class: 'note bad error-state', role: 'alert' }, h('p', {}, e.message), h('button', { class: 'btn sm', onclick: () => load() }, icon('sync'), 'Retry'))); return; }
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
    tableWrap.replaceChildren(res.items.length ? h('table', {}, h('caption', { class: 'sr-only' }, `${key}: ${res.total} results`), h('thead', {}, head), h('tbody', {}, rows))
      : active() ? h('div', { class: 'empty' }, h('b', {}, 'No matches'), h('p', {}, 'Nothing matches the current search and filters.'), h('button', { class: 'btn sm', onclick: () => clearAll() }, 'Clear filters'))
      : h('div', { class: 'empty' }, h('b', {}, 'Nothing here yet'), emptyText || ''));
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
  root.reload = load; root.state = st; root.exportQuery = () => qs({ ...st, status: st.filters.status ?? st.status });
  load();
  return root;
}
export function starButton(table, item, reload) {
  const b = h('button', { class: 'icon-btn star', 'aria-pressed': String(!!item.starred), 'aria-label': (item.starred ? 'Remove from' : 'Add to') + ' shortlist', title: 'Shortlist (s)', onclick: async () => {
    try { await api(`/api/${table}/${item.id}/star`, { method: 'POST', body: { on: !item.starred } }); item.starred = !item.starred; b.setAttribute('aria-pressed', String(item.starred)); b.setAttribute('aria-label', (item.starred ? 'Remove from' : 'Add to') + ' shortlist'); } catch (e) { toast(e.message, 'bad'); }
  } }, icon('star'));
  return need(b, 'shortlist');
}
export function qs(st, extra = {}) {
  const p = new URLSearchParams({ q: st.q, status: st.status, sort: st.sort, desc: st.desc ? '1' : '0', page: st.page, per_page: st.per, starred: st.starred ? '1' : '0', ...st.filters, ...extra });
  for (const [k, v] of [...p]) if (v === '' || v === '0' && k === 'starred') p.delete(k);
  return p.toString();
}
export const disabledReason = (btn, reason) => { if (reason) { btn.setAttribute('aria-disabled', 'true'); btn.title = reason; btn.addEventListener('click', (e) => { e.preventDefault(); e.stopImmediatePropagation(); toast(reason, 'bad'); }, true); } return btn; };

export function rowLink(url, row) {
  if (!url) return null;
  return h('p', {}, h('a', { href: url, target: '_blank', rel: 'noopener noreferrer' }, icon('ext'), ` Open sheet row ${row || ''}`.trimEnd()));
}
export function relatedSection({ name, website, company, pitch }) {
  const box = h('div', { class: 'section related', 'aria-live': 'polite' }, h('h3', {}, 'Across nVentures'), h('p', { class: 'hint' }, 'Loading related records…'));
  const q = new URLSearchParams({ name: name || '', website: website || '', company: company || 0, pitch: pitch || 0 });
  api('/api/related?' + q).then((rel) => {
    const items = [];
    for (const c of rel.companies) items.push(h('li', {}, h('span', { class: 'badge info' }, 'Sourced company'), h('a', { href: '#/companies?company=' + c.id }, c.name), ' · ', badge(c.sync_status)));
    for (const p of rel.pitches) items.push(h('li', {}, h('span', { class: 'badge' }, 'Pitch'), h('a', { href: '#/inbox?pitch=' + p.id }, p.name), ' · ', badge(p.status), ' · ', fmtDate(p.received_at)));
    for (const c of rel.candidates) items.push(h('li', {}, h('span', { class: 'badge' }, 'Research'), `${c.partner}: `, badge(c.outcome), ' · ', fmtDate(c.created_at)));
    box.replaceChildren(h('h3', {}, 'Across nVentures'),
      items.length ? h('ul', { class: 'source-list' }, items) : h('p', { class: 'hint' }, 'No other records share this company name or website.'),
      rel.events.length ? h('details', {}, h('summary', {}, `Activity (${rel.events.length})`), eventList(rel.events)) : null);
  }).catch((e) => box.replaceChildren(h('h3', {}, 'Across nVentures'), h('p', { class: 'note bad' }, e.message)));
  return box;
}


// ---------- record presentation (shared by sourcing and companies) ----------
export function evidenceBlock(rec) {
  const ev = rec.evidence || {};
  const fields = ['headquarters', 'b2b', 'active', 'stage', 'funding'];
  const names = { headquarters: 'Headquarters', b2b: 'Business model', active: 'Active operations', stage: 'Stage', funding: 'Total funding' };
  return h('div', { class: 'section' }, h('h3', {}, 'Evidence (exact quotes from fetched pages)'), fields.map((f) => h('div', {}, h('b', {}, names[f]),
    (ev[f] || []).length ? ev[f].map((e) => h('div', { class: 'evidence' }, h('blockquote', {}, '“' + e.quote + '”'), h('a', { href: demoLink(e.url), target: '_blank', rel: 'noopener noreferrer' }, e.url))) : h('p', { class: 'hint' }, 'No evidence found'))));
}
export function demoLink(u) { try { const x = new URL(u); if (x.hostname.endsWith('.nventures-demo.test')) return `/demo-site/${x.hostname}${x.pathname}${x.search}`; } catch {} return u; }
export function recordFacts(rec) {
  return h('div', { class: 'facts' },
    h('div', {}, h('small', {}, 'HQ country'), h('span', {}, rec.hq_country || 'Unknown')), h('div', {}, h('small', {}, 'Stage'), h('span', {}, rec.stage || 'Unknown')),
    h('div', {}, h('small', {}, 'Total funding (USD)'), h('span', {}, fmtUSD(rec.total_funding_usd))), h('div', {}, h('small', {}, 'B2B'), h('span', {}, rec.b2b === true ? 'Yes' : rec.b2b === false ? 'No' : 'Unknown')),
    h('div', {}, h('small', {}, 'Founded'), h('span', {}, rec.founded_year || '—')), h('div', {}, h('small', {}, 'Founders'), h('span', {}, rec.founders || '—')),
    h('div', {}, h('small', {}, 'Website'), rec.website ? h('a', { href: demoLink(rec.website), target: '_blank', rel: 'noopener noreferrer' }, rec.website) : '—'),
    h('div', {}, h('small', {}, 'LinkedIn'), rec.linkedin ? h('a', { href: rec.linkedin, target: '_blank', rel: 'noopener noreferrer' }, 'Company page') : '—'));
}
export function sourcesBlock(sources) {
  const list = parse(sources);
  if (!Array.isArray(list) || !list.length) return null;
  return h('details', { class: 'section' }, h('summary', {}, `Sources consulted (${list.length})`), h('ul', { class: 'source-list' }, list.map((s) => h('li', {}, badge(s.status === 'ok' || s.status === 'cached' ? 'completed' : s.status === 'js_only' || s.status === 'truncated' ? 'partial' : 'failed'), h('span', {}, `${s.url} ${s.status !== 'ok' ? '(' + s.status + (s.note ? ': ' + s.note : '') + ')' : ''}`)))));
}

// ---------- team: assignment, stage, follow-up, notes, reply drafts ----------
export const STAGES = {
  pitch: [['new', 'New'], ['reviewing', 'Reviewing'], ['contacted', 'Contacted'], ['meeting', 'Meeting'], ['passed', 'Passed'], ['progressed', 'Progressed']],
  company: [['not_contacted', 'Not contacted'], ['contacted', 'Contacted'], ['meeting', 'Meeting'], ['passed', 'Passed'], ['progressed', 'Progressed']],
};
export const stageName = (entity, s) => (STAGES[entity].find(([k]) => k === s) || [s, s || '—'])[1];
export async function team() {
  if (!app.team || Date.now() - app.team.at > 60000) app.team = { at: Date.now(), ...(await api('/api/team')) };
  return app.team;
}
export const memberName = (id) => { const m = app.team && app.team.members.find((x) => x.id === id); return m ? m.name : id ? 'Former member' : 'Unassigned'; };

// workflowSection lets people with the right role assign, stage and schedule a follow-up.
export function workflowSection(entity, rec, onSaved) {
  const perm = entity === 'pitch' ? 'pitch.work' : 'company.work';
  const editable = can(perm);
  const box = h('div', { class: 'section workflow' }, h('h3', {}, 'Team workflow'), h('p', { class: 'hint' }, 'Loading team…'));
  team().then((t) => {
    const who = h('select', { 'aria-label': 'Assigned to', disabled: !editable }, h('option', { value: '' }, 'Unassigned'), t.members.map((m) => h('option', { value: m.id }, `${m.name} · ${m.role_name}`)));
    who.value = rec.assignee_id || '';
    const stage = h('select', { 'aria-label': 'Stage', disabled: !editable }, STAGES[entity].map(([k, v]) => h('option', { value: k }, v)));
    stage.value = rec.stage || STAGES[entity][0][0];
    const fu = h('input', { type: 'date', value: rec.follow_up_at || '', 'aria-label': 'Follow up on', disabled: !editable });
    const save = h('button', { class: 'btn sm', onclick: async () => {
      try {
        await api(`/api/${entity === 'pitch' ? 'pitches' : 'companies'}/${rec.id}/workflow`, { method: 'POST', body: { assignee_id: who.value ? Number(who.value) : null, stage: stage.value, follow_up_at: fu.value } });
        rec.assignee_id = who.value ? Number(who.value) : null; rec.stage = stage.value; rec.follow_up_at = fu.value;
        toast('Workflow saved'); onSaved && onSaved();
      } catch (e) { toast(e.message, 'bad'); }
    } }, 'Save workflow');
    if (!editable) need(save, perm);
    box.replaceChildren(h('h3', {}, 'Team workflow'),
      h('div', { class: 'field-row' }, h('label', {}, 'Assigned to', who), h('label', {}, 'Stage', stage), h('label', {}, 'Follow up on', fu)),
      h('div', { class: 'actions' }, save, h('span', { class: 'hint' }, 'Kept in nVentures only; never written to the sheets.')));
    box.isDirty = () => editable && (String(who.value) !== String(rec.assignee_id || '') || stage.value !== (rec.stage || STAGES[entity][0][0]) || fu.value !== (rec.follow_up_at || ''));
  }).catch((e) => box.replaceChildren(h('h3', {}, 'Team workflow'), h('p', { class: 'note bad' }, e.message)));
  return box;
}

export function notesSection(entity, id) {
  const perm = entity === 'pitch' ? 'pitch.work' : 'company.work';
  const list = h('ul', { class: 'notes' });
  const input = h('textarea', { rows: 3, maxlength: 5000, placeholder: 'Add an internal note (visible to the team, never sent to founders)', 'aria-label': 'New note', disabled: !can(perm) });
  const add = h('button', { class: 'btn sm', onclick: async () => {
    const body = input.value.trim();
    if (!body) { fieldError(input, 'Write something first.'); input.focus(); return; }
    clearErrors({ input });
    try { await api('/api/notes', { method: 'POST', body: { entity, id, body } }); input.value = ''; await load(); toast('Note added'); } catch (e) { toast(e.message, 'bad'); }
  } }, 'Add note');
  need(add, perm);
  async function load() {
    const ns = await api(`/api/notes?entity=${entity}&id=${id}`);
    list.replaceChildren(...(ns.length ? ns.map((n) => h('li', {}, h('div', { class: 'note-head' }, h('b', {}, n.author || 'Someone'), h('time', { datetime: n.created_at }, fmtDate(n.created_at))), h('p', {}, n.body))) : [h('li', { class: 'hint' }, 'No notes yet.')]));
  }
  load().catch((e) => list.replaceChildren(h('li', { class: 'note bad' }, e.message)));
  const box = h('div', { class: 'section' }, h('h3', {}, 'Notes'), list, input, h('div', { class: 'actions' }, add));
  box.isDirty = () => input.value.trim() !== '';
  return box;
}

// draftReply fills a reply template and opens it in the person's own email
// program. nVentures itself never sends email (Gmail access is read-only).
export function draftReply(pitch, data) {
  const box = h('div', { class: 'section' }, h('h3', {}, 'Reply to founder'), h('p', { class: 'hint' }, 'Loading templates…'));
  api('/api/templates').then((ts) => {
    if (!ts.length) { box.replaceChildren(h('h3', {}, 'Reply to founder'), h('p', { class: 'hint' }, 'No reply templates yet. Admins can add them in Settings.')); return; }
    const pick = h('select', { 'aria-label': 'Template' }, ts.map((t) => h('option', { value: t.id }, t.name)));
    const fill = (s) => s.replaceAll('{{company}}', data.name || pitch.name).replaceAll('{{founder}}', (data.founder || '').split(/\s+/)[0] || 'there').replaceAll('{{me}}', app.me ? app.me.name : '');
    const subject = h('input', { type: 'text', 'aria-label': 'Subject' });
    const body = h('textarea', { rows: 7, 'aria-label': 'Message' });
    const apply = () => { const t = ts.find((x) => String(x.id) === pick.value); subject.value = fill(t.subject); body.value = fill(t.body); };
    pick.addEventListener('change', apply); apply();
    const to = data.contact || '';
    const open = h('a', { class: 'btn sm', href: '#', onclick: (e) => { e.preventDefault(); if (!to.includes('@')) { toast('This pitch has no contact email', 'bad'); return; } location.href = `mailto:${encodeURIComponent(to)}?subject=${encodeURIComponent(subject.value)}&body=${encodeURIComponent(body.value)}`; } }, icon('mail'), 'Open in my email');
    const copy = h('button', { class: 'btn sm', onclick: async () => { try { await navigator.clipboard.writeText(`Subject: ${subject.value}\n\n${body.value}`); toast('Draft copied'); } catch { toast('Copy failed — select the text instead', 'bad'); } } }, 'Copy draft');
    box.replaceChildren(h('h3', {}, 'Reply to founder'), h('p', { class: 'hint' }, `Opens a draft to ${to || 'the founder'} in your own email program. nVentures never sends email for you.`),
      h('label', {}, 'Template', pick), h('label', {}, 'Subject', subject), h('label', {}, 'Message', body), h('div', { class: 'actions' }, open, copy));
  }).catch((e) => box.replaceChildren(h('h3', {}, 'Reply to founder'), h('p', { class: 'note bad' }, e.message)));
  return box;
}

// intakeStatus is the one-line automatic Gmail intake status.
export function intakeStatus() {
  const a = (app.state.readiness || {}).auto_intake;
  if (!a) return null;
  if (!a.enabled) return h('p', { class: 'status-line' }, h('span', { class: 'dot' }), 'Automatic intake is off', can('settings.manage') ? h('a', { href: '#/settings' }, 'Turn on') : null);
  return h('p', { class: 'status-line' }, h('span', { class: 'dot ' + (a.problem ? 'warn' : 'ok') }),
    `Checking Gmail every ${a.every_minutes} min`, h('span', { class: 'muted status-detail' }, a.last_at ? `${ago(a.last_at)}: ${a.last_result}` : 'first check shortly'));
}
