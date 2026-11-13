// Admin panels shown in Settings: reply templates and automation schedules.
import { $, api, app, can, clearErrors, confirmDialog, fieldError, fmtDate, h, icon, openOverlay, toast } from '../core.js';

export function templatesPanel() {
  const list = h('div', { class: 'table-wrap' });
  const load = async () => {
    const ts = await api('/api/templates');
    list.replaceChildren(ts.length ? h('table', {}, h('caption', { class: 'sr-only' }, 'Reply templates'), h('tbody', {}, ts.map((t) => h('tr', { onclick: () => edit(t) },
      h('td', {}, h('div', { class: 'cell-main' }, t.name), h('div', { class: 'cell-sub' }, t.subject)), h('td', { class: 'tight hide-mobile' }, fmtDate(t.updated_at))))))
      : h('div', { class: 'empty' }, h('b', {}, 'No templates'), 'Add one so responders can draft replies quickly.'));
  };
  const edit = (t) => {
    const name = h('input', { type: 'text', value: t ? t.name : '', maxlength: 100 });
    const subject = h('input', { type: 'text', value: t ? t.subject : 'Re: {{company}}', maxlength: 200 });
    const body = h('textarea', { rows: 10, maxlength: 5000 }, t ? t.body : 'Hi {{founder}},\n\n\n\nBest regards,\n{{me}}\nnVentures');
    const start = JSON.stringify([name.value, subject.value, body.value]);
    const save = async () => {
      clearErrors({ name, subject, body });
      if (name.value.trim().length < 2) return fieldError(name, 'Give the template a name.'), name.focus();
      if (!subject.value.trim()) return fieldError(subject, 'Add a subject.'), subject.focus();
      if (!body.value.trim()) return fieldError(body, 'Write the message.'), body.focus();
      try { await api(t ? `/api/templates/${t.id}` : '/api/templates', { method: t ? 'PUT' : 'POST', body: { name: name.value, subject: subject.value, body: body.value } }); toast('Template saved'); d.close(true); load(); }
      catch (e) { toast(e.message, 'bad'); }
    };
    const foot = [h('button', { class: 'btn', onclick: () => d.close() }, 'Cancel'), h('button', { class: 'btn primary', onclick: save }, 'Save template')];
    if (t) foot.unshift(h('button', { class: 'btn danger', onclick: async () => { if (!(await confirmDialog(`Delete “${t.name}”?`, 'Responders will no longer see this template.', 'Delete'))) return; await api(`/api/templates/${t.id}`, { method: 'DELETE' }); toast('Template deleted'); d.close(true); load(); } }, 'Delete'));
    const d = openOverlay({ kind: 'dialog', title: t ? 'Edit reply template' : 'New reply template', isDirty: () => JSON.stringify([name.value, subject.value, body.value]) !== start,
      body: h('form', { class: 'drawer-body form', novalidate: true, onsubmit: (e) => { e.preventDefault(); return save(); } },
        h('label', {}, 'Name', name), h('label', {}, 'Subject', subject), h('label', {}, 'Message', body),
        h('p', { class: 'hint' }, 'Placeholders: {{company}}, {{founder}} (first name), {{me}} (the responder). Drafts open in the responder’s own email program; nVentures never sends email.')),
      foot });
  };
  load().catch((e) => list.replaceChildren(h('p', { class: 'note bad' }, e.message)));
  return h('section', { class: 'panel', style: 'margin-top:16px' }, h('div', { class: 'panel-head' }, h('div', {}, h('h2', {}, 'Reply templates'), h('p', {}, 'Used by pitch responders to draft replies to founders.')),
    h('button', { class: 'btn sm', onclick: () => edit(null) }, icon('plus'), 'New template')), list);
}

const KIND = { inbound_import: 'Inbox import', outbound_run: 'Sourcing run' };
const every = (m) => ({ 60: 'every hour', 1440: 'every day', 10080: 'every week' }[m] || (m % 1440 === 0 ? `every ${m / 1440} days` : m % 60 === 0 ? `every ${m / 60} hours` : `every ${m} minutes`));

export function schedulesPanel() {
  const list = h('div', { class: 'table-wrap' });
  const r = app.state.readiness;
  const load = async () => {
    const xs = await api('/api/schedules');
    list.replaceChildren(xs.length ? h('table', {}, h('caption', { class: 'sr-only' }, 'Schedules'),
      h('thead', {}, h('tr', {}, h('th', { scope: 'col' }, 'Job'), h('th', { scope: 'col', class: 'tight' }, 'Next run'), h('th', { scope: 'col', class: 'tight hide-mobile' }, 'Last result'), h('th', { scope: 'col', class: 'tight' }, 'On'), h('th', { scope: 'col', class: 'tight' }, h('span', { class: 'sr-only' }, 'Actions')))),
      h('tbody', {}, xs.map((x) => h('tr', {},
        h('td', {}, h('div', { class: 'cell-main' }, `${KIND[x.kind]} ${every(x.every_minutes)}`), h('div', { class: 'cell-sub' }, x.kind === 'inbound_import' ? `Up to ${x.params.limit} messages${x.params.sync ? ', then sync approved pitches' : ''}` : `Target ${x.params.target}, ${x.params.max_partners} partners${x.params.sync ? ', write verified rows' : ''}`)),
        h('td', { class: 'tight' }, x.enabled ? fmtDate(x.next_run_at) : '—'),
        h('td', { class: 'tight hide-mobile' }, x.last_job_id ? h('a', { href: `#/activity?job=${x.last_job_id}` }, x.last_status || 'View') : h('span', { class: 'hint' }, x.last_status || 'Not run yet')),
        h('td', { class: 'tight' }, h('input', { type: 'checkbox', checked: x.enabled, 'aria-label': `Enable schedule ${x.id}`, onchange: async (e) => { try { await api(`/api/schedules/${x.id}`, { method: 'PUT', body: { every_minutes: x.every_minutes, enabled: e.target.checked, sync: !!x.params.sync, limit: x.params.limit, target: x.params.target, max_partners: x.params.max_partners } }); toast(e.target.checked ? 'Schedule on' : 'Schedule paused'); load(); } catch (err) { toast(err.message, 'bad'); e.target.checked = !e.target.checked; } } })),
        h('td', { class: 'tight' }, h('div', { class: 'actions' },
          h('button', { class: 'btn sm', onclick: async () => { try { const j = await api(`/api/schedules/${x.id}/run`, { method: 'POST' }); toast(`Started job #${j.id}`); load(); } catch (e) { toast(e.message, 'bad'); } } }, icon('play'), 'Run now'),
          h('button', { class: 'btn sm danger', onclick: async () => { if (!(await confirmDialog('Delete this schedule?', 'Past runs stay in Activity.', 'Delete'))) return; await api(`/api/schedules/${x.id}`, { method: 'DELETE' }); toast('Schedule deleted'); load(); } }, 'Delete')))))))
      : h('div', { class: 'empty' }, h('b', {}, 'No schedules'), 'Automate inbox imports and sourcing runs, for example every morning.'));
  };
  const kind = h('select', { 'aria-label': 'Job' }, Object.entries(KIND).map(([k, v]) => h('option', { value: k }, v)));
  const freq = h('select', { 'aria-label': 'Frequency' }, [[60, 'Every hour'], [240, 'Every 4 hours'], [720, 'Every 12 hours'], [1440, 'Every day'], [10080, 'Every week']].map(([v, l]) => h('option', { value: v }, l)));
  freq.value = '1440';
  const sync = h('input', { type: 'checkbox' });
  const syncHint = h('span', { class: 'hint' });
  const refreshSync = () => {
    const ok = kind.value === 'inbound_import' ? r.inbound.sheet_set : r.outbound.ready;
    sync.disabled = !ok; if (!ok) sync.checked = false;
    syncHint.textContent = ok ? '' : kind.value === 'inbound_import' ? 'Set the inbound sheet in Settings to enable.' : 'Set and enable the outbound sheet in Settings to enable.';
  };
  kind.addEventListener('change', refreshSync); refreshSync();
  const form = h('form', { class: 'panel-body form', novalidate: true, onsubmit: async (e) => {
    e.preventDefault();
    try { await api('/api/schedules', { method: 'POST', body: { kind: kind.value, every_minutes: Number(freq.value), enabled: true, sync: sync.checked } }); toast('Schedule added'); load(); }
    catch (err) { toast(err.message, 'bad'); }
  } }, h('div', { class: 'field-row' }, h('label', {}, 'Job', kind), h('label', {}, 'How often', freq)),
  h('label', { class: 'check' }, sync, 'Also write verified rows to the sheet'), syncHint,
  h('p', { class: 'hint' }, r.google.connected ? 'Scheduled runs use the same rules as manual ones and never relax the mandate.' : 'Inbox imports are skipped until Google is connected; sourcing runs work now.'),
  h('div', { class: 'actions' }, h('button', { class: 'btn primary', type: 'submit' }, icon('plus'), 'Add schedule')));
  load().catch((e) => list.replaceChildren(h('p', { class: 'note bad' }, e.message)));
  return h('section', { class: 'panel', style: 'margin-top:16px' }, h('div', { class: 'panel-head' }, h('div', {}, h('h2', {}, 'Automation'), h('p', {}, 'Scheduled runs while the app is open.'))),
    list, h('div', { class: 'subpanel' }, h('h3', {}, 'Add a schedule'), form));
}
