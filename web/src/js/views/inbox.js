// ---------- inbox ----------
import { JobWatcher, STAGES, intakeStatus, dataTable, disabledReason, draftReply, memberName, notesSection, qs, relatedSection, rowLink, stageName, starButton, startJob, team, workflowSection } from '../components.js';
import { $, STATUS, api, app, badge, shortDate, can, clearErrors, fieldError, fmtDate, h, icon, loadState, need, openOverlay, parse, prefs, roleReason, toast } from '../core.js';

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
      { label: 'Company', sort: 'name', render: (p) => { const d = parse(p.data); return [h('div', { class: 'cell-main' }, p.name || h('span', { class: 'muted' }, 'Name not stated'), d.name_guessed ? h('span', { class: 'badge warn', style: 'margin-left:6px' }, 'name guessed') : null), h('div', { class: 'cell-sub' }, d.description || '')]; } },
      { label: 'Contact', cls: 'hide-mobile', render: (p) => { const d = parse(p.data); return h('span', { class: 'cell-clip', title: d.contact_source ? 'From ' + d.contact_source : null }, d.contact || '—'); } },
      { label: '', cls: 'tight hide-mobile', render: (p) => { const d = parse(p.data); const n = (d.attachments || []).length; return n || (d.deck_links || []).length ? h('span', { class: 'signal', title: n ? `${n} attachment(s) — decks can be read in the review panel` : 'Deck link' }, icon('paperclip'), n || '') : null; } },
      { label: 'Owner', cls: 'tight hide-mobile', render: (p) => p.assignee_id || p.stage !== 'new' ? [h('div', {}, p.assignee_id ? memberName(p.assignee_id) : '—'), p.stage !== 'new' ? h('div', { class: 'cell-sub' }, stageName('pitch', p.stage), p.follow_up_at ? ' · ' + p.follow_up_at : '') : null] : h('span', { class: 'muted' }, '—') },
      { label: 'Received', sort: 'received', cls: 'tight hide-mobile', render: (p) => h('span', { title: fmtDate(p.received_at) }, shortDate(p.received_at)) },
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
    h('div', { class: 'page-head' }, h('div', {}, h('h2', {}, 'Pitch inbox'), h('p', {}, 'Complete pitches that fit our focus go to the sheet automatically. Everything else waits here for review.'), intakeStatus()),
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
    deckSection(p, d, data.decks || [], f, locked),
    d.excerpt ? h('details', { class: 'section' }, h('summary', {}, 'Email text (quoted replies removed)'), h('pre', { class: 'excerpt' }, d.excerpt)) : null,
    (data.sync_ops || []).length ? h('details', { class: 'section' }, h('summary', {}, `Sheet write history (${data.sync_ops.length})`), h('ul', { class: 'source-list' }, data.sync_ops.map((o) => h('li', {}, badge(o.status), `${fmtDate(o.updated_at)} ${o.sheet_row ? '· row ' + o.sheet_row : ''} ${o.error || ''}`)))) : null,
    notes,
    can('templates.use') ? draftReply(p, d) : null,
    relatedSection({ name: d.name || p.name, website: d.website, pitch: p.id }));
  const foot = locked ? [h('button', { class: 'btn', onclick: () => dr.close() }, 'Close')] : [
    h('button', { class: 'btn ghost', onclick: () => save('skipped') }, 'Skip'), h('button', { class: 'btn', onclick: () => save('review') }, 'Save, keep in review'),
    h('button', { class: 'btn', onclick: () => save('ready'), title: 'Ctrl+Enter' }, icon('check'), 'Approve'), syncBtn];
  const dr = openOverlay({ title: d.name || p.name || 'Name not stated — from ' + (d.reply_to || d.sender || 'unknown sender'), sub: `Pitch #${p.id} · ${STATUS[p.status]?.[0] || p.status}${locked ? ' · read-only' : ''}`, body, foot, isDirty: dirty });
  dr.panel.addEventListener('keydown', (e) => { if ((e.ctrlKey || e.metaKey) && e.key === 'Enter' && !locked) { e.preventDefault(); save('ready'); } });
}


// ---------- pitch decks ----------
const DECK_STATUS = {
  extracted: ['Text read', 'ok'], no_text: ['No text layer', 'warn'], protected: ['Password-protected', 'warn'],
  too_large: ['Too large', 'warn'], unsupported: ['Not readable', 'warn'], error: ['Damaged / unreadable', 'bad'],
};
const FACT_LABEL = { description: 'Description', hq: 'HQ', website: 'Website', contact: 'Contact', stage: 'Stage', total_funding: 'Funding to date', raising: 'Raising', founded: 'Founded', founders: 'Founders', traction: 'Traction' };
const FACT_FIELD = { description: 'description', hq: 'hq', website: 'website', contact: 'contact' };
const isDeck = (a) => /\.(pdf|pptx)$/i.test(a.filename || '') || a.mime_type === 'application/pdf' || a.mime_type === 'application/vnd.openxmlformats-officedocument.presentationml.presentation';

// deckSection lists attachments and deck links. PDF/PPTX attachments can be
// read on request; the text and facts are shown with page numbers and exact
// quotes, and the reviewer chooses what to copy into the form. Nothing is
// applied automatically, and deck text is never treated as instructions.
function deckSection(p, d, decks, f, locked) {
  const atts = d.attachments || [], links = d.deck_links || [];
  if (!atts.length && !links.length) return null;
  const r = app.state.readiness;
  const list = h('div', { class: 'deck-list' });
  const reading = (a) => decks.find((x) => x.filename === a.filename && (x.part_id || '') === (a.part_id || ''));
  const renderOne = (a) => {
    const box = h('div', { class: 'deck' });
    const draw = (rd) => {
      const kb = Math.max(1, Math.round(((rd && rd.size) || a.size || 0) / 1024));
      const head = h('div', { class: 'deck-head' }, icon('paperclip'), h('b', {}, a.filename), h('span', { class: 'hint' }, `${kb} KB`),
        rd ? h('span', { class: 'badge ' + (DECK_STATUS[rd.status] || ['', ''])[1] }, (DECK_STATUS[rd.status] || [rd.status])[0]) : h('span', { class: 'badge warn' }, 'unreviewed'));
      const kids = [head];
      if (isDeck(a)) {
        const btn = h('button', { class: 'btn sm', type: 'button', onclick: async () => {
          try { const res = await api(`/api/pitches/${p.id}/decks`, { method: 'POST', body: { part_id: a.part_id || '', filename: a.filename } }); decks = decks.filter((x) => x.id !== res.id).concat(res); draw(res); toast(res.status === 'extracted' ? 'Deck text read — review the facts below' : (DECK_STATUS[res.status] || [res.status])[0]); }
          catch (e) { toast(e.message, 'bad'); }
        } }, rd ? 'Read again' : 'Read deck text');
        if (!can('inbox.review')) need(btn, 'inbox.review'); else if (!r.google.connected) disabledReason(btn, 'Connect Google in Settings first: the attachment is downloaded read-only from Gmail.');
        kids.push(h('div', { class: 'actions' }, btn, rd ? h('span', { class: 'hint' }, `${rd.page_count} ${rd.kind === 'pptx' ? 'slides' : 'pages'} · read ${fmtDate(rd.read_at)}${rd.read_by_name ? ' by ' + rd.read_by_name : ''}`) : h('span', { class: 'hint' }, 'Downloads this attachment read-only and extracts its text on this computer.')));
      } else {
        kids.push(h('p', { class: 'hint' }, 'Only PDF and PowerPoint (.pptx) decks can be read. Open the original email to view this file.'));
      }
      if (rd && rd.note) kids.push(h('p', { class: 'note ' + (rd.status === 'extracted' ? '' : 'warn') }, rd.note));
      if (rd && rd.status === 'extracted') kids.push(deckFacts(parse(rd.facts), f, locked), deckText(parse(rd.pages), rd));
      box.replaceChildren(...kids);
    };
    draw(reading(a));
    return box;
  };
  atts.forEach((a) => list.append(renderOne(a)));
  return h('div', { class: 'section' }, h('h3', {}, 'Decks & attachments'),
    h('p', { class: 'hint' }, 'Deck text is read only when you ask, entirely on this computer (no AI service). Facts are suggestions with their exact source line — check them before using.'),
    list,
    links.length ? h('div', { style: 'margin-top:10px' }, h('small', { class: 'hint' }, 'Deck links (DocSend, Drive, …) usually need the founder\'s sign-in, so they are not read:'),
      h('ul', { class: 'source-list' }, links.map((u) => h('li', {}, h('span', { class: 'badge warn' }, 'unreviewed'), h('a', { href: u, target: '_blank', rel: 'noopener noreferrer' }, u))))) : null);
}

function deckFacts(facts, f, locked) {
  if (!Array.isArray(facts) || !facts.length) return h('p', { class: 'hint' }, 'No company facts were recognised in the deck text. Read the text below.');
  const rows = facts.map((x) => {
    const field = FACT_FIELD[x.field];
    const use = !locked && field ? h('button', { class: 'btn sm', type: 'button', onclick: () => { f[field].value = x.value; f[field].dispatchEvent(new Event('input', { bubbles: true })); toast(`${FACT_LABEL[x.field]} filled from the deck — save to keep it`); } }, f[field].value.trim() ? 'Replace' : 'Use') : null;
    const note = !locked && !field ? h('button', { class: 'btn sm ghost', type: 'button', onclick: () => { const line = `Deck p.${x.page}: ${x.quote}`; if (!f.notes.value.includes(line)) f.notes.value = (f.notes.value.trim() ? f.notes.value.trim() + '\n' : '') + line; toast('Added to notes — save to keep it'); } }, 'Add to notes') : null;
    return h('div', { class: 'deck-fact' }, h('div', { class: 'deck-fact-head' }, h('small', {}, FACT_LABEL[x.field] || x.field), h('b', {}, x.value), h('span', { class: 'hint' }, 'p. ' + x.page), use, note),
      x.quote !== x.value ? h('blockquote', {}, '“' + x.quote + '”') : null);
  });
  return h('div', { class: 'deck-facts' }, h('small', { class: 'hint' }, 'Facts found in the deck'), rows);
}

function deckText(pages, rd) {
  if (!Array.isArray(pages) || !pages.length) return null;
  const unit = rd.kind === 'pptx' ? 'Slide' : 'Page';
  const find = h('input', { type: 'search', placeholder: 'Find in deck…', 'aria-label': 'Find in deck text' });
  const blocks = pages.map((t, i) => h('div', { class: 'deck-page', dataset: { text: (t || '').toLowerCase() } }, h('small', {}, `${unit} ${i + 1}`), h('pre', { class: 'excerpt' }, t || '(no text on this ' + unit.toLowerCase() + ')')));
  const count = h('span', { class: 'hint' });
  find.addEventListener('input', () => {
    const q = find.value.trim().toLowerCase(); let n = 0;
    blocks.forEach((b) => { const hit = !q || b.dataset.text.includes(q); b.hidden = !hit; if (hit) n++; });
    count.textContent = q ? `${n} of ${blocks.length}` : '';
  });
  return h('details', { class: 'deck-text' }, h('summary', {}, `Deck text (${pages.length} ${unit.toLowerCase()}s${rd.truncated ? ', shortened' : ''})`), h('div', { class: 'actions' }, find, count), blocks);
}
