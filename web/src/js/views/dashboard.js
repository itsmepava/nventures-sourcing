// ---------- home ----------
// A calm overview: a greeting, four numbers, what needs a person, how the
// two pipelines are flowing, and a short activity list. Setup steps appear
// only while something isn't set up.
import { intakeStatus } from '../components.js';
import { api, app, can, h, icon, shortDate } from '../core.js';

const greeting = () => { const hr = new Date().getHours(); return hr < 12 ? 'Good morning' : hr < 18 ? 'Good afternoon' : 'Good evening'; };
const firstName = () => ((app.me && app.me.name) || '').trim().split(/\s+/)[0];
const trim = (s, n) => (s && s.length > n ? s.slice(0, n - 1).trimEnd() + '…' : s || '');

export async function renderDashboard(main) {
  const d = await api('/api/dashboard');
  const r = app.state.readiness;
  const pc = d.pitches || {};
  const inSheet = (pc.verified || 0) + (pc.already_exists || 0) + (pc.demo_verified || 0);
  const waiting = (pc.review || 0) + (pc.error || 0) + (pc.uncertain || 0) + (pc.conflict || 0);
  const totalPitches = Object.values(pc).reduce((a, b) => a + b, 0);
  const cand = d.candidates || {};
  const checked = Object.values(cand).reduce((a, b) => a + b, 0);
  const countries = Object.entries(d.by_country || {}).filter(([, n]) => n > 0).sort((a, b) => b[1] - a[1]);

  const kpi = (value, label, href, sub) => h('a', { class: 'kpi', href }, h('span', { class: 'kpi-value' }, String(value)), h('span', { class: 'kpi-label' }, label), sub ? h('span', { class: 'kpi-sub' }, sub) : null);
  const flow = (label, steps) => h('div', { class: 'flow' }, h('div', { class: 'flow-label' }, label),
    h('ol', { class: 'flow-steps' }, steps.map(([n, t], i) => h('li', {}, h('b', {}, String(n)), h('span', {}, t), i < steps.length - 1 ? h('span', { class: 'flow-arrow', 'aria-hidden': 'true' }, '→') : null))));

  // Setup only while something isn't ready (live mode).
  const setup = [];
  if (r.mode === 'live') {
    if (!r.google.connected) setup.push(['Connect Google', 'Read-only Gmail and your Sheets', '#/settings']);
    if (!r.inbound.sheet_set) setup.push(['Choose the pitch sheet', 'Where promising pitches are added', '#/settings']);
  }

  const actions = h('div', { class: 'actions' },
    h('a', { class: 'btn primary', href: '#/inbox' }, icon('inbox'), waiting ? `Review ${waiting} pitch${waiting === 1 ? '' : 'es'}` : 'Pitch inbox'),
    can('sourcing.run') ? h('a', { class: 'btn', href: '#/sourcing?run=1' }, icon('radar'), 'Start sourcing') : null);

  main.replaceChildren(
    h('section', { class: 'hero' },
      h('div', {}, h('p', { class: 'hero-kicker' }, new Date().toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'long' })),
        h('h2', { class: 'hero-title' }, `${greeting()}${firstName() ? ', ' + firstName() : ''}`),
        h('p', { class: 'hero-sub' }, 'Pitches arrive on their own; promising ones go straight to the sheet, and partners are scouted for new companies.'),
        intakeStatus()),
      actions),
    setup.length ? h('section', { class: 'panel setup' }, h('div', { class: 'panel-head' }, h('h2', {}, 'Finish setting up')),
      h('ol', { class: 'setup-list' }, setup.map(([t, s, href]) => h('li', {}, h('a', { href }, h('b', {}, t), h('span', {}, s)))))) : null,
    h('div', { class: 'kpis' },
      kpi(d.pitches_received || 0, 'Pitches received', '#/inbox', `${d.pitches_week || 0} this week`),
      kpi(waiting, 'Waiting for review', '#/inbox', waiting ? 'needs a person' : 'all clear'),
      kpi(inSheet, 'Pitches in the sheet', '#/inbox'),
      kpi(d.companies.total, 'Companies sourced', '#/companies', countries.length ? `in ${countries.length} countr${countries.length === 1 ? 'y' : 'ies'}` : null)),
    h('div', { class: 'home-grid' },
      h('section', { class: 'panel' }, h('div', { class: 'panel-head' }, h('h2', {}, 'Needs your attention'), waiting > (d.attention || []).length ? h('a', { href: '#/inbox' }, 'See all') : null),
        (d.attention || []).length ? h('ul', { class: 'attention' }, d.attention.map((x) => h('li', {}, h('a', { href: '#/inbox?pitch=' + x.id },
          h('div', { class: 'att-main' }, h('b', {}, x.name || 'Name not stated'), h('span', { class: 'muted' }, shortDate(x.received_at))),
          h('div', { class: 'att-sub' }, trim(x.reason || x.from, 110))))))
          : h('div', { class: 'empty calm' }, icon('check'), h('p', {}, 'Nothing waiting. New pitches appear here when they need a person.'))),
      h('section', { class: 'panel' }, h('div', { class: 'panel-head' }, h('h2', {}, 'Pipeline')),
        h('div', { class: 'panel-body' },
          flow('Inbound pitches', [[totalPitches, 'received'], [waiting, 'in review'], [inSheet, 'in sheet']]),
          flow('Sourcing', [[checked, 'checked'], [cand.accepted || 0, 'accepted'], [d.companies.verified, 'in sheet']]),
          countries.length ? h('div', { class: 'flow' }, h('div', { class: 'flow-label' }, 'Where our companies are based'),
            h('div', { class: 'chips' }, countries.map(([c, n]) => h('span', { class: 'chip' }, `${c} `, h('b', {}, String(n)))))) : null))),
    h('section', { class: 'panel', style: 'margin-top:16px' }, h('div', { class: 'panel-head' }, h('h2', {}, 'Recent activity'), h('a', { href: '#/activity' }, 'All activity')),
      (d.events || []).length ? h('ul', { class: 'recent' }, d.events.slice(0, 6).map((e) => h('li', {}, h('span', { class: 'muted recent-time' }, shortDate(e.created_at)), h('span', { class: 'recent-msg' }, trim(e.message, 140)), e.actor ? h('span', { class: 'muted' }, e.actor) : null)))
        : h('div', { class: 'empty calm' }, h('p', {}, 'No activity yet.'))));
}

