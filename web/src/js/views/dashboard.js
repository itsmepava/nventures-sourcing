// ---------- dashboard ----------
import { eventList } from '../components.js';
import { $, JOB_LABEL, api, app, badge, can, fmtDate, h, icon } from '../core.js';

export async function renderDashboard(main) {
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
      h('div', { class: 'actions' }, can('inbox.import') ? h('a', { class: 'btn primary', href: '#/inbox?run=1' }, icon('inbox'), 'Import pitches') : null, can('sourcing.run') ? h('a', { class: 'btn', href: '#/sourcing?run=1' }, icon('radar'), 'Start sourcing') : null, h('a', { class: 'btn ghost', href: '#/queue' }, icon('check'), 'My work'))),
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

