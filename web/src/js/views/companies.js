// ---------- companies ----------
import { STAGES, dataTable, disabledReason, evidenceBlock, memberName, notesSection, qs, recordFacts, relatedSection, rowLink, sourcesBlock, stageName, starButton, startJob, team, workflowSection } from '../components.js';
import { $, STATUS, api, app, badge, can, fmtDate, fmtUSD, h, icon, need, openOverlay, parse, toast } from '../core.js';

export async function renderCompanies(main, params) {
  const r = app.state.readiness;
  const jobsMount = h('div');
  const table = dataTable({
    key: 'companies', selectable: can('sourcing.sync'),
    extraFilters: [{ key: 'country', label: 'Country', options: app.state.countries }, { key: 'stage', label: 'Stage', options: ['bootstrapped', 'angel', 'pre-seed', 'seed'] }, { key: 'status', label: 'Sheet', options: ['pending', 'verified', 'already_exists', 'error', 'uncertain', 'conflict', 'blocked'] }, { key: 'pipeline', label: 'Outreach', options: STAGES.company }, { key: 'assignee', label: 'Assigned', options: [['me', 'Assigned to me'], ['none', 'Unassigned']] }],
    fetchPage: async (st) => api('/api/companies?' + qs({ ...st, status: st.filters.status || '' }, {})),
    columns: [
      { label: '', cls: 'tight', render: (c, reload) => starButton('companies', c, reload) },
      { label: 'Company', sort: 'name', render: (c) => { const d = parse(c.data); return [h('div', { class: 'cell-main' }, c.name), h('div', { class: 'cell-sub clamp' }, d.description || d.website || '')]; } },
      { label: 'HQ', sort: 'country', cls: 'tight', render: (c) => parse(c.data).hq_country || '—' },
      { label: 'Stage', cls: 'tight hide-mobile', render: (c) => parse(c.data).stage || '—' },
      { label: 'Funding', sort: 'funding', cls: 'tight hide-mobile', render: (c) => fmtUSD(parse(c.data).total_funding_usd) },
      { label: 'Partner', sort: 'partner', cls: 'hide-mobile', render: (c) => c.partner },
      { label: 'Owner', cls: 'tight hide-mobile', render: (c) => !c.assignee_id && c.stage === 'not_contacted' ? h('span', { class: 'muted' }, '—') : [h('div', {}, c.assignee_id ? memberName(c.assignee_id) : '—'), h('div', { class: 'cell-sub' }, stageName('company', c.stage), c.follow_up_at ? ' · ' + c.follow_up_at : '')] },
      { label: 'Sheet', sort: 'sync', cls: 'tight', render: (c) => [badge(c.sync_status), c.sheet_row ? (c.sheet_url ? h('a', { class: 'cell-sub row-link', href: c.sheet_url, target: '_blank', rel: 'noopener noreferrer', title: 'Open the sheet row' }, 'row ' + c.sheet_row) : h('div', { class: 'cell-sub' }, 'row ' + c.sheet_row)) : null] },
    ],
    onOpen: (c, reload) => openCompany(c.id, reload),
    bulk: (ids, done) => { const b = h('button', { class: 'btn sm primary', onclick: () => startJob('/api/sourcing/sync', { ids }, jobsMount, done) }, icon('sync'), 'Sync selected now'); if (!can('sourcing.sync')) need(b, 'sourcing.sync'); else if (!r.outbound.ready) disabledReason(b, 'Set and enable the outbound sheet in Settings first.'); return b; },
    emptyText: 'Accepted companies from sourcing runs appear here.',
  });
  const exportLink = h('a', { class: 'btn ghost', href: '/api/companies/export.csv', download: '' }, icon('download'), 'CSV');
  exportLink.title = 'Exports the companies matching the current search and filters';
  exportLink.addEventListener('click', () => { exportLink.href = '/api/companies/export.csv?' + qs({ ...table.state, status: table.state.filters.status || '' }); });
  main.replaceChildren(h('div', { class: 'page-head' }, h('div', {}, h('h2', {}, 'Companies'), h('p', {}, 'Every company accepted by sourcing, with the evidence behind it.')), h('div', { class: 'actions' }, exportLink)), jobsMount, table);
  app.keys = table.keys; app.jobsMount = jobsMount; app.reloadPage = () => table.reload();
  await team().catch(() => {});
  if (params.get('company')) openCompany(Number(params.get('company')), () => table.reload());
}
export async function openCompany(id, after) {
  let data;
  try { data = await api('/api/companies/' + id); } catch (e) { toast(e.message, 'bad'); return; }
  const c = data.company, rec = parse(c.data);
  const body = h('div', { class: 'drawer-body' },
    (data.qualification || []).length ? h('div', { class: 'note bad' }, h('b', {}, 'No longer qualifies'), h('ul', { class: 'reasons' }, data.qualification.map((x) => h('li', {}, x)))) : h('p', { class: 'note ok' }, 'Qualifies: eligible HQ, B2B, active, early stage and total funding ≤ $3M — each with a source quote.'),
    c.sync_error ? h('p', { class: 'note bad' }, c.sync_error) : null,
    c.sheet_row ? h('div', { class: 'note' }, h('p', {}, `Verified in the outbound sheet at row ${c.sheet_row}. Existing sheet rows are never overwritten.`), rowLink(c.sheet_url, c.sheet_row)) : null,
    recordFacts(rec), rec.description ? h('p', {}, rec.description) : null,
    (rec.warnings || []).length ? h('div', { class: 'note' }, h('b', {}, 'Warnings'), h('ul', { class: 'reasons' }, rec.warnings.map((w) => h('li', {}, w)))) : null,
    evidenceBlock(rec), sourcesBlock(rec.sources),
    (data.sync_ops || []).length ? h('details', { class: 'section' }, h('summary', {}, `Sheet write history (${data.sync_ops.length})`), h('ul', { class: 'source-list' }, data.sync_ops.map((o) => h('li', {}, badge(o.status), `${fmtDate(o.updated_at)} ${o.sheet_range || ''} ${o.error || ''}`)))) : null,
    workflowSection('company', c, () => after && after()),
    notesSection('company', c.id),
    relatedSection({ name: c.name, website: rec.website, company: c.id }));
  const sections = [...body.querySelectorAll('.section')];
  const d = openOverlay({ isDirty: () => sections.some((x) => x.isDirty && x.isDirty()), title: c.name, sub: `${c.partner} · saved ${fmtDate(c.created_at)} · sheet: ${STATUS[c.sync_status]?.[0] || c.sync_status}${c.sheet_row ? ' (row ' + c.sheet_row + ')' : ''}`, body, foot: [h('button', { class: 'btn', onclick: () => d.close() }, 'Close')] });
}

