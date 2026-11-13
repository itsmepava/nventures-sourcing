// ---------- sourcing ----------
import { JobWatcher, disabledReason, evidenceBlock, recordFacts, sourcesBlock, startJob } from '../components.js';
import { $, STATUS, api, app, badge, can, fmtDate, h, icon, loadState, need, openOverlay, parse, prefs, toast } from '../core.js';

export async function renderSourcing(main, params) {
  const r = app.state.readiness;
  const jobsMount = h('div');
  const runBtn = h('button', { class: 'btn primary', onclick: () => openRunDialog(jobsMount, () => cands.reload()) }, icon('play'), 'Start sourcing run');
  const syncBtn = h('button', { class: 'btn', onclick: () => startJob('/api/sourcing/sync', {}, jobsMount, () => cands.reload()) }, icon('sync'), 'Sync pending companies');
  need(runBtn, 'sourcing.run');
  if (!can('sourcing.sync')) need(syncBtn, 'sourcing.sync'); else if (!r.outbound.ready) disabledReason(syncBtn, r.outbound.sheet_set ? 'Enable outbound sheet writing and connect Google in Settings.' : 'Set the outbound sheet in Settings first.');
  const urls = h('textarea', { rows: 3, placeholder: 'https://company.example\nhttps://another.example', 'aria-label': 'Company URLs, one per line' });
  const manual = h('section', { class: 'panel', style: 'margin-bottom:16px' }, h('div', { class: 'panel-head' }, h('div', {}, h('h2', {}, 'Research specific companies'), h('p', {}, 'Paste company websites to check them against the same rules.'))),
    h('div', { class: 'panel-body form' }, urls, h('div', { class: 'actions' }, need(h('button', { class: 'btn', onclick: async () => { const list = urls.value.split(/\s+/).map((x) => x.trim()).filter(Boolean); if (!list.length) { toast('Paste at least one URL', 'bad'); return; } if (await startJob('/api/sourcing/research', { urls: list }, jobsMount, () => cands.reload())) urls.value = ''; } }, 'Research URLs'), 'sourcing.run'))));
  const cands = h('section', { class: 'panel' });
  const outcomeTabs = h('div', { class: 'tabs', role: 'group', 'aria-label': 'Filter outcomes' });
  let outcome = prefs.get('cand_outcome', '');
  async function loadCands() {
    const items = await api('/api/candidates?limit=300' + (outcome ? '&outcome=' + outcome : ''));
    outcomeTabs.replaceChildren(...[['', 'All'], ['accepted', 'Accepted'], ['needs_research', 'Needs research'], ['rejected', 'Rejected'], ['duplicate', 'Duplicates']].map(([v, l]) => h('button', { 'aria-pressed': String(outcome === v), onclick: () => { outcome = v; prefs.set('cand_outcome', v); loadCands(); } }, l)));
    cands.replaceChildren(h('div', { class: 'panel-head' }, h('div', {}, h('h2', {}, 'Outcomes'), h('p', {}, 'Every company checked, and why.')), outcomeTabs),
      items.length ? h('div', { class: 'table-wrap' }, h('table', {}, h('thead', {}, h('tr', {}, h('th', {}, 'Company'), h('th', { class: 'hide-mobile' }, 'Partner'), h('th', {}, 'Outcome'), h('th', { class: 'hide-mobile' }, 'Reasons'), h('th', { class: 'tight hide-mobile' }, 'When'))),
        h('tbody', {}, items.map((c) => h('tr', { onclick: () => openCandidate(c) }, h('td', {}, h('div', { class: 'cell-main' }, c.name), h('div', { class: 'cell-sub' }, hostOf(c.url))), h('td', { class: 'hide-mobile' }, c.partner), h('td', { class: 'tight' }, badge(c.outcome)), h('td', { class: 'hide-mobile' }, h('div', { class: 'cell-sub' }, c.reasons.join('; ') || (c.outcome === 'accepted' ? 'All hard requirements evidenced' : ''))), h('td', { class: 'tight hide-mobile' }, fmtDate(c.created_at))))))) : h('div', { class: 'empty' }, h('b', {}, 'No research yet'), 'Start a sourcing run to crawl partner portfolios.'));
  }
  cands.reload = loadCands;
  main.replaceChildren(
    h('div', { class: 'page-head' }, h('div', {}, h('h2', {}, 'Sourcing'), h('p', {}, 'We read partner portfolios and each company\'s own website. A company is accepted only when its pages show every requirement:'),
        h('div', { class: 'chips' }, ['HQ in South Asia or Singapore', 'B2B', 'Bootstrapped to seed', 'Total funding ≤ $3M'].map((t) => h('span', { class: 'chip' }, t)))),
      h('div', { class: 'actions' }, runBtn, syncBtn)),
    jobsMount, manual, cands);
  await loadCands();
  app.keys = null; app.jobsMount = jobsMount; app.reloadPage = loadCands;
  const running = app.state.running.outbound; if (running) new JobWatcher(running, jobsMount, () => loadCands());
  if (params.get('run') === '1' && can('sourcing.run')) openRunDialog(jobsMount, () => loadCands());
}
export function openRunDialog(mount, after) {
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
export function openCandidate(c) {
  const rec = parse(c.record);
  const body = h('div', { class: 'drawer-body' },
    (c.reasons || []).length ? h('div', { class: 'note ' + (c.outcome === 'rejected' ? 'bad' : 'warn') }, h('b', {}, c.outcome === 'rejected' ? 'Rejected — evidence contradicts the mandate' : c.outcome === 'needs_research' ? 'Needs research — evidence missing (not assumed)' : 'Not saved'), h('ul', { class: 'reasons' }, c.reasons.map((x) => h('li', {}, x)))) : h('p', { class: 'note ok' }, 'Every hard requirement is evidenced.'),
    (rec.warnings || []).length ? h('div', { class: 'note' }, h('b', {}, 'Warnings'), h('ul', { class: 'reasons' }, rec.warnings.map((w) => h('li', {}, w)))) : null,
    rec.name ? recordFacts(rec) : null, rec.description ? h('p', {}, rec.description) : null, rec.evidence ? evidenceBlock(rec) : null, sourcesBlock(c.sources));
  const foot = [h('button', { class: 'btn', onclick: () => d.close() }, 'Close')];
  if (c.company_id) foot.unshift(h('a', { class: 'btn', href: '#/companies?company=' + c.company_id }, 'Open company'));
  if (c.outcome !== 'accepted' && c.outcome !== 'duplicate' && can('sourcing.run')) foot.unshift(h('button', { class: 'btn', onclick: async () => { d.close(); await startJob('/api/sourcing/research', { urls: [c.url], partner: c.partner }, app.jobsMount || $('#main'), () => app.reloadPage && app.reloadPage()); } }, 'Research again'));
  const d = openOverlay({ title: c.name, sub: `${c.partner} · ${STATUS[c.outcome]?.[0] || c.outcome}`, body, foot });
}


const hostOf = (u) => { try { return new URL(u).hostname.replace(/^www\./, ''); } catch { return u || ''; } };
