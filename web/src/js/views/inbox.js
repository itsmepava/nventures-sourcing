// ---------- inbox ----------
import { JobWatcher, STAGES, dataTable, disabledReason, draftReply, memberName, notesSection, qs, relatedSection, rowLink, stageName, starButton, startJob, team, workflowSection } from '../components.js';
import { $, STATUS, api, app, badge, can, clearErrors, fieldError, fmtDate, h, icon, loadState, need, openOverlay, parse, prefs, roleReason, toast } from '../core.js';

export async function renderInbox(main, params) {
  const r = app.state.readiness;
  const jobsMount = h('div');
  const connected = r.google.connected;
  const importBtn = h('button', { class: 'btn primary', onclick: () => openImportDialog(jobsMount, () => table.reload()) }, icon('inbox'), 'Import from Gmail');
  const syncReady = h('button', { class: 'btn', onclick: () => startJob('/api/inbox/sync', {}, jobsMount, () => { table.reload(); loadState(); }) }, icon('sync'), 'Sync all approved');
  if (!can('inbox.import')) need(importBtn, 'inbox.import'); else if (!connected) disabledReason(importBtn, 'Connect Google in Settings first (read-only Gmail access).');
  if (!can('inbox.sync')) need(syncReady, 'inbox.sync'); else if (!r.inbound.sheet_set || !connected) disabledReason(syncReady, 'Set the inbound sheet and connect Google in Settings first.');
  const counts = app.state.pitch_counts;
  const table = dataTable({
    key: 'pitches', selectable: can('inbox.sync'),
    extraFilters: [{ key: 'pipeline', label: 'Stage', options: STAGES.pitch }, { key: 'assignee', label: 'Assigned', options: [['me', 'Assigned to me'], ['none', 'Unassigned']] }],
    tabs: [['attention', 'Needs attention', 'attention'], ['ready', 'Approved', 'ready'], ['synced', 'In sheet', 'synced'], ['skipped', 'Skipped', 'skipped'], ['duplicate', 'Duplicates', 'duplicate'], ['', 'All', 'all']],
    fetchPage: async (st) => { const res = await api('/api/pitches?' + qs(st)); const c = app.state.pitch_counts || {}; res.counts = (k) => k === 'all' ? Object.values(c).reduce((a, b) => a + b, 0) : k === 'attention' ? (c.review || 0) + (c.error || 0) + (c.uncertain || 0) + (c.conflict || 0) : k === 'synced' ? (c.verified || 0) + (c.already_exists || 0) + (c.demo_verified || 0) : c[k] || 0; return res; },
    columns: [
      { label: '', cls: 'tight', render: (p, reload) => starButton('pitches', p, reload) },
      { label: 'Company', sort: 'name', render: (p) => { const d = parse(p.data); return [h('div', { class: 'cell-main' }, p.name, d.name_guessed ? h('span', { class: 'badge warn', style: 'margin-left:6px' }, 'name guessed') : null), h('div', { class: 'cell-sub' }, d.description || '')]; } },
      { label: 'Contact', cls: 'hide-mobile', render: (p) => { const d = parse(p.data); return [h('div', {}, d.contact || '—'), d.contact_source ? h('div', { class: 'cell-sub' }, 'from ' + d.contact_source) : null]; } },
      { label: 'Signals', cls: 'tight hide-mobile', render: (p) => { const d = parse(p.data); return h('div', { class: 'actions' }, d.structured ? h('span', { class: 'badge' }, 'form') : h('span', { class: 'badge warn' }, 'direct'), (d.attachments || []).length ? h('span', { class: 'badge', title: 'Attachments are not reviewed automatically' }, icon('paperclip'), d.attachments.length) : null, (d.deck_links || []).length ? h('span', { class: 'badge', title: 'Deck links are not reviewed automatically' }, 'deck') : null); } },
      { label: 'Team', cls: 'tight hide-mobile', render: (p) => [h('div', {}, stageName('pitch', p.stage)), h('div', { class: 'cell-sub' }, p.assignee_id ? memberName(p.assignee_id) : 'Unassigned', p.follow_up_at ? ' · ' + p.follow_up_at : '')] },
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
      h('div', { class: 'actions' }, importBtn, syncReady, (() => { const a = h('a', { class: 'btn ghost', href: '/api/pitches/export.csv', download: '', title: 'Exports the pitches matching the current search and filters' }, icon('download'), 'CSV'); a.addEventListener('click', () => { a.href = '/api/pitches/export.csv?' + qs(table.state); }); return a; })())),
    jobsMount, table);
  app.keys = table.keys; app.jobsMount = jobsMount; app.reloadPage = () => table.reload();
  const running = app.state.running.inbound; if (running) new JobWatcher(running, jobsMount, () => { table.reload(); loadState(); });
  await team().catch(() => {});
  if (params.get('run') === '1' && connected && can('inbox.import')) openImportDialog(jobsMount, () => table.reload());
  if (params.get('pitch')) openPitch(Number(params.get('pitch')), () => table.reload());
}
export function openImportDialog(mount, after) {
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
export async function openPitch(id, after) {
  let data;
  try { data = await api('/api/pitches/' + id); } catch (e) { toast(e.message, 'bad'); return; }
  const p = data.pitch, d = parse(p.data);
  const synced = ['verified', 'already_exists', 'demo_verified', 'syncing'].includes(p.status);
  const locked = synced || !can('inbox.review');
  const f = {
    name: h('input', { type: 'text', value: d.name || p.name, maxlength: 300, required: true, disabled: locked }),
    website: h('input', { type: 'url', value: d.website || '', maxlength: 500, disabled: locked }),
    description: h('textarea', { rows: 4, maxlength: 12000, disabled: locked }, d.description || ''),
    hq: h('input', { type: 'text', value: d.hq || '', maxlength: 300, placeholder: 'Unknown', disabled: locked }),
    contact: h('input', { type: 'email', value: d.contact || '', maxlength: 500, disabled: locked }),
    notes: h('textarea', { rows: 5, maxlength: 12000, disabled: locked }, d.notes || ''),
  };
  const initial = JSON.stringify(Object.fromEntries(Object.entries(f).map(([k, el]) => [k, el.value])));
  const wf = workflowSection('pitch', p, () => after && after());
  const notes = notesSection('pitch', p.id);
  const dirty = () => (!locked && JSON.stringify(Object.fromEntries(Object.entries(f).map(([k, el]) => [k, el.value]))) !== initial) || (wf.isDirty && wf.isDirty()) || (notes.isDirty && notes.isDirty());
  const validate = (decision) => {
    clearErrors(f);
    let ok = true;
    const err = (el, msg) => { fieldError(el, msg); if (ok) el.focus(); ok = false; };
    if (!f.name.value.trim()) err(f.name, 'Company name is required.');
    if (f.website.value.trim() && !/^https?:\/\/[^\s.]+\.[^\s]+$/i.test(f.website.value.trim())) err(f.website, 'Use a full web address starting with https://');
    if (decision === 'ready') {
      if (f.description.value.trim().length < 10) err(f.description, 'Describe what the company does (at least 10 characters) before approving.');
      if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(f.contact.value.trim())) err(f.contact, 'A valid contact email is required before approving.');
    } else if (f.contact.value.trim() && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(f.contact.value.trim())) err(f.contact, 'This does not look like an email address.');
    return ok;
  };
  const save = async (decision) => {
    if (!validate(decision)) { toast('Please fix the highlighted fields', 'bad'); return false; }
    try {
      await api('/api/pitches/' + id, { method: 'POST', body: { name: f.name.value, website: f.website.value, description: f.description.value, hq: f.hq.value, contact: f.contact.value, notes: f.notes.value, decision } });
      toast(decision === 'ready' ? 'Approved — ready to sync' : decision === 'skipped' ? 'Skipped' : 'Saved for review'); dr.close(true); after && after();
      return true;
    } catch (e) { toast(e.message, 'bad'); return false; }
  };
  const syncNow = async () => { if (await save('ready')) { await startJob('/api/inbox/sync', { ids: [id] }, app.jobsMount || $('#main'), () => { after && after(); loadState(); }); } };
  const r = app.state.readiness;
  const syncBtn = h('button', { class: 'btn primary', onclick: syncNow }, icon('sync'), 'Approve & sync now');
  if (!can('inbox.sync')) need(syncBtn, 'inbox.sync'); else if (!r.inbound.sheet_set || !r.google.connected) disabledReason(syncBtn, 'Set the inbound sheet and connect Google in Settings first.');
  const suggestBox = h('div');
  const suggest = r.local_model.enabled ? h('button', { class: 'btn sm', onclick: async () => {
    suggestBox.replaceChildren(h('p', { class: 'hint' }, 'Asking the local model…'));
    try { const s = await api(`/api/pitches/${id}/suggest`, { method: 'POST' }); suggestBox.replaceChildren(h('div', { class: 'note' }, h('b', {}, `Local model suggestion (${s.model}) — not applied`), s.sector ? h('p', {}, 'Sector: ' + s.sector) : null, s.summary ? h('p', {}, 'Summary: ' + s.summary) : null, (s.rejected || []).length ? h('p', { class: 'hint' }, 'Discarded: ' + s.rejected.join('; ')) : null, s.summary && !locked ? h('button', { class: 'btn sm', onclick: () => { f.description.value = s.summary; } }, 'Use summary as description') : null)); }
    catch (e) { suggestBox.replaceChildren(h('p', { class: 'note bad' }, e.message)); }
  } }, 'Suggest with local model') : null;
  const body = h('div', { class: 'drawer-body' },
    (d.reasons || []).length || p.error ? h('div', { class: 'note ' + (p.status === 'error' || p.status === 'conflict' || p.status === 'uncertain' ? 'bad' : 'warn') }, p.error ? h('p', {}, p.error) : null, (d.reasons || []).length ? h('ul', { class: 'reasons' }, d.reasons.map((x) => h('li', {}, x))) : null) : null,
    synced ? h('div', { class: 'note ok' }, h('p', {}, `Read-only: this pitch is in the inbound sheet${d.sheet_row ? ' at row ' + d.sheet_row : ''}. Edit the sheet directly — nVentures never overwrites sheet rows.`), rowLink(p.sheet_url, d.sheet_row)) : null,
    !synced && locked ? h('p', { class: 'note' }, roleReason('inbox.review')) : null,
    wf,
    h('form', { class: 'form', novalidate: true, onsubmit: (e) => { e.preventDefault(); return save('ready'); } },
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
    (data.sync_ops || []).length ? h('details', { class: 'section' }, h('summary', {}, `Sheet write history (${data.sync_ops.length})`), h('ul', { class: 'source-list' }, data.sync_ops.map((o) => h('li', {}, badge(o.status), `${fmtDate(o.updated_at)} ${o.sheet_row ? '· row ' + o.sheet_row : ''} ${o.error || ''}`)))) : null,
    notes,
    can('templates.use') ? draftReply(p, d) : null,
    relatedSection({ name: d.name || p.name, website: d.website, pitch: p.id }));
  const foot = locked ? [h('button', { class: 'btn', onclick: () => dr.close() }, 'Close')] : [
    h('button', { class: 'btn ghost', onclick: () => save('skipped') }, 'Skip'), h('button', { class: 'btn', onclick: () => save('review') }, 'Save, keep in review'),
    h('button', { class: 'btn', onclick: () => save('ready'), title: 'Ctrl+Enter' }, icon('check'), 'Approve'), syncBtn];
  const dr = openOverlay({ title: d.name || p.name, sub: `Pitch #${p.id} · ${STATUS[p.status]?.[0] || p.status}${locked ? ' · read-only' : ''}`, body, foot, isDirty: dirty });
  dr.panel.addEventListener('keydown', (e) => { if ((e.ctrlKey || e.metaKey) && e.key === 'Enter' && !locked) { e.preventDefault(); save('ready'); } });
}

