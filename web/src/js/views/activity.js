// ---------- activity ----------
import { RETRYABLE, eventList } from '../components.js';
import { $, JOB_LABEL, STATUS, api, app, badge, fmtDate, h, icon, openOverlay, toast } from '../core.js';

export async function renderActivity(main, params) {
  const jobsPanel = h('section', { class: 'panel' });
  const kind = h('select', { 'aria-label': 'Job type', onchange: () => loadJobs() }, h('option', { value: '' }, 'All jobs'), Object.entries(JOB_LABEL).map(([k, v]) => h('option', { value: k }, v)));
  async function loadJobs() {
    const js = await api('/api/jobs?limit=100' + (kind.value ? '&kind=' + kind.value : ''));
    jobsPanel.replaceChildren(h('div', { class: 'panel-head' }, h('h2', {}, 'Job history'), kind),
      js.length ? h('div', { class: 'table-wrap' }, h('table', {}, h('thead', {}, h('tr', {}, h('th', {}, 'Job'), h('th', { class: 'tight' }, 'Status'), h('th', { class: 'tight hide-mobile' }, 'Started'), h('th', { class: 'tight hide-mobile' }, 'Finished'))),
        h('tbody', {}, js.map((j) => h('tr', { onclick: () => openJob(j.id) }, h('td', {}, h('div', { class: 'cell-main' }, `${JOB_LABEL[j.kind] || j.kind} #${j.id}`, j.legacy_ref ? h('span', { class: 'badge', style: 'margin-left:6px' }, 'imported from Python app') : null), h('div', { class: 'cell-sub' }, j.message)), h('td', { class: 'tight' }, badge(j.status)), h('td', { class: 'tight hide-mobile' }, fmtDate(j.started_at)), h('td', { class: 'tight hide-mobile' }, fmtDate(j.finished_at))))))) : h('div', { class: 'empty' }, 'No jobs yet.'));
  }
  const auditKind = h('select', { 'aria-label': 'Event type', onchange: () => loadAudit() }, h('option', { value: '' }, 'All events'), ['accepted', 'rejected', 'needs_research', 'duplicate', 'sync', 'sync_error', 'inbox_review', 'error', 'warning', 'partner', 'settings', 'google_connected', 'migration', 'interrupted'].map((k) => h('option', { value: k }, k.replace(/_/g, ' '))));
  const audit = h('section', { class: 'panel', style: 'margin-top:16px' });
  async function loadAudit() { const ev = await api('/api/events?limit=300' + (auditKind.value ? '&kind=' + auditKind.value : '')); audit.replaceChildren(h('div', { class: 'panel-head' }, h('h2', {}, 'Audit log'), auditKind), ev.length ? eventList(ev) : h('div', { class: 'empty' }, 'No events.')); }
  main.replaceChildren(h('div', { class: 'page-head' }, h('div', {}, h('h2', {}, 'Activity'), h('p', {}, 'Every run and change, with who did it.'))), jobsPanel, audit);
  app.keys = null;
  await Promise.all([loadJobs(), loadAudit()]);
  if (params.get('job')) openJob(Number(params.get('job')));
}
export async function openJob(id) {
  const data = await api('/api/jobs/' + id);
  const j = data.job, c = j.counters || {};
  const body = h('div', { class: 'drawer-body' }, h('p', {}, j.message),
    h('div', { class: 'facts' }, Object.entries(c).filter(([, v]) => typeof v !== 'object').map(([k, v]) => h('div', {}, h('small', {}, k.replace(/_/g, ' ')), h('span', {}, String(v))))),
    h('div', { class: 'facts' }, h('div', {}, h('small', {}, 'Started'), h('span', {}, fmtDate(j.started_at))), h('div', {}, h('small', {}, 'Finished'), h('span', {}, fmtDate(j.finished_at))), h('div', {}, h('small', {}, 'Mode'), h('span', {}, j.mode))),
    data.events.length ? h('div', { class: 'section' }, h('h3', {}, 'Events'), eventList(data.events)) : null);
  const foot = [h('button', { class: 'btn', onclick: () => d.close() }, 'Close')];
  if (j.status === 'running') foot.unshift(h('button', { class: 'btn danger', onclick: async () => { await api(`/api/jobs/${id}/cancel`, { method: 'POST' }); toast('Cancellation requested'); d.close(); } }, 'Cancel job'));
  else if (RETRYABLE.has(j.status) && !(j.params && j.params.legacy)) foot.unshift(h('button', { class: 'btn', onclick: async () => { try { const n = await api(`/api/jobs/${id}/retry`, { method: 'POST' }); toast('Started again as job #' + n.id); d.close(true); location.hash = '#/activity?job=' + n.id; } catch (e) { toast(e.message, 'bad'); } } }, icon('sync'), 'Retry'));
  const d = openOverlay({ title: `${JOB_LABEL[j.kind] || j.kind} #${j.id}`, sub: STATUS[j.status]?.[0] || j.status, body, foot });
}

