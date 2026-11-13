// My work: everything assigned to the signed-in person, follow-ups first.
import { stageName, team } from '../components.js';
import { $, api, app, badge, can, fmtDate, h } from '../core.js';

export async function renderQueue(main) {
  const q = await api('/api/queue');
  const counts = app.state.pitch_counts || {};
  const attention = (counts.review || 0) + (counts.error || 0) + (counts.uncertain || 0) + (counts.conflict || 0);
  const overdue = (it) => it.follow_up_at && it.follow_up_at < q.today;
  const dueToday = (it) => it.follow_up_at === q.today;
  const row = (it) => h('tr', { onclick: () => { location.hash = it.entity === 'pitch' ? `#/inbox?pitch=${it.id}` : `#/companies?company=${it.id}`; } },
    h('td', {}, h('div', { class: 'cell-main' }, it.name), h('div', { class: 'cell-sub' }, it.entity === 'pitch' ? 'Inbound pitch' : 'Sourced company')),
    h('td', { class: 'tight' }, stageName(it.entity, it.stage)),
    h('td', { class: 'tight' }, it.follow_up_at ? h('span', { class: 'badge ' + (overdue(it) ? 'bad' : dueToday(it) ? 'warn' : '') }, overdue(it) ? `Overdue · ${it.follow_up_at}` : dueToday(it) ? 'Today' : it.follow_up_at) : h('span', { class: 'hint' }, '—')),
    h('td', { class: 'tight hide-mobile' }, badge(it.status)),
    h('td', { class: 'tight hide-mobile' }, fmtDate(it.updated_at)));
  const hello = new Date().getHours() < 12 ? 'Good morning' : new Date().getHours() < 18 ? 'Good afternoon' : 'Good evening';
  main.replaceChildren(
    h('div', { class: 'page-head' }, h('div', {}, h('h2', {}, `${hello}, ${app.me.name.split(/\s+/)[0]}`),
      h('p', {}, `${q.items.length} open item${q.items.length === 1 ? '' : 's'} assigned to you${q.due ? ` · ${q.due} follow-up${q.due === 1 ? '' : 's'} due` : ''}.`))),
    h('div', { class: 'grid cols-3' },
      h('a', { class: 'panel stat', href: '#/inbox' }, h('div', { class: 'label' }, 'Pitches needing review (team)'), h('div', { class: 'value' }, attention), h('div', { class: 'sub' }, 'Unassigned items can be picked up by any responder')),
      h('div', { class: 'panel stat' }, h('div', { class: 'label' }, 'Assigned to me'), h('div', { class: 'value' }, q.items.length), h('div', { class: 'sub' }, 'Open pitches and companies')),
      h('div', { class: 'panel stat' }, h('div', { class: 'label' }, 'Follow-ups due'), h('div', { class: 'value' }, q.due), h('div', { class: 'sub' }, 'Today or overdue'))),
    h('section', { class: 'panel', style: 'margin-top:16px' }, h('div', { class: 'panel-head' }, h('h2', {}, 'Assigned to me'), h('p', {}, 'Passed and progressed items drop off automatically.')),
      q.items.length ? h('div', { class: 'table-wrap' }, h('table', {}, h('caption', { class: 'sr-only' }, 'Items assigned to me'),
        h('thead', {}, h('tr', {}, h('th', { scope: 'col' }, 'Item'), h('th', { scope: 'col', class: 'tight' }, 'Stage'), h('th', { scope: 'col', class: 'tight' }, 'Follow up'), h('th', { scope: 'col', class: 'tight hide-mobile' }, 'Sheet'), h('th', { scope: 'col', class: 'tight hide-mobile' }, 'Updated'))),
        h('tbody', {}, q.items.map(row))))
        : h('div', { class: 'empty' }, h('b', {}, 'Nothing assigned to you'), 'When someone assigns you a pitch or company it appears here, and you get a notification.')));
}
