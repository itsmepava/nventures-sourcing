// ---------- partners ----------
import { demoLink } from '../components.js';
import { $, api, app, badge, can, clearErrors, fieldError, fmtDate, h, icon, need, openOverlay, parse, roleReason, toast } from '../core.js';

export async function renderPartners(main) {
  const list = h('section', { class: 'panel' });
  async function load() {
    const ps = await api('/api/partners');
    list.replaceChildren(h('div', { class: 'panel-head' }, h('div', {}, h('h2', {}, 'Rotation order'), h('p', {}, 'Least recently sourced first. Counts and last-visit state persist across restarts and are mirrored to the control tab when syncing.'))),
      ps.length ? h('div', { class: 'table-wrap' }, h('table', {}, h('thead', {}, h('tr', {}, h('th', {}, 'Partner'), h('th', { class: 'hide-mobile' }, 'Website'), h('th', { class: 'tight' }, 'Runs'), h('th', { class: 'tight hide-mobile' }, 'Last added'), h('th', { class: 'tight hide-mobile' }, 'Last visit'), h('th', { class: 'tight' }, 'In rotation'))),
        h('tbody', {}, ps.map((p) => { const rep = parse(p.last_report); return h('tr', { onclick: (e) => { if (!e.target.closest('input,button,a')) openPartner(p, load); } },
          h('td', {}, h('div', { class: 'cell-main' }, p.name), (rep.warnings || []).length ? h('div', { class: 'cell-sub' }, rep.warnings[0]) : null),
          h('td', { class: 'hide-mobile' }, p.website ? h('a', { href: demoLink(p.website), target: '_blank', rel: 'noopener noreferrer' }, p.website) : h('span', { class: 'badge warn' }, 'no website')),
          h('td', { class: 'tight' }, p.run_count), h('td', { class: 'tight hide-mobile' }, p.last_added), h('td', { class: 'tight hide-mobile' }, p.last_sourced ? fmtDate(p.last_sourced) : 'Never'),
          h('td', { class: 'tight' }, h('input', { type: 'checkbox', checked: p.relevant, disabled: !can('partners.edit'), title: can('partners.edit') ? null : roleReason('partners.edit'), 'aria-label': 'Include ' + p.name + ' in rotation', onchange: async (e) => { try { await api('/api/partners/' + p.id, { method: 'PATCH', body: { relevant: e.target.checked } }); toast(e.target.checked ? 'Included in rotation' : 'Excluded from rotation'); } catch (err) { toast(err.message, 'bad'); e.target.checked = !e.target.checked; } } }))); })))) : h('div', { class: 'empty' }, h('b', {}, 'No partners'), 'Add a partner with its official website, or connect the partner sheet.'));
  }
  const name = h('input', { type: 'text', required: true, minlength: 2, maxlength: 200 });
  const site = h('input', { type: 'url', placeholder: 'https://partner.example', maxlength: 500 });
  const form = h('form', { class: 'panel-body form', novalidate: true, onsubmit: async (e) => { e.preventDefault(); clearErrors({ name, site }); if (name.value.trim().length < 2) { fieldError(name, 'Enter the partner name (at least 2 characters).'); name.focus(); return; } if (site.value.trim() && !/^(https?:\/\/)?[^\s.]+\.[^\s]+$/i.test(site.value.trim())) { fieldError(site, 'Enter a web address such as https://partner.example'); site.focus(); return; } try { await api('/api/partners', { method: 'POST', body: { name: name.value, website: site.value } }); name.value = site.value = ''; toast('Partner added'); load(); } catch (err) { toast(err.message, 'bad'); } } },
    h('div', { class: 'field-row' }, h('label', {}, 'Partner name', name), h('label', {}, 'Official website', site)), h('div', { class: 'actions' }, need(h('button', { class: 'btn primary', type: 'submit' }, icon('plus'), 'Add partner'), 'partners.edit')));
  main.replaceChildren(h('div', { class: 'page-head' }, h('div', {}, h('h2', {}, 'Portfolio partners'), h('p', {}, 'The crawler starts from each partner’s official site, follows portfolio pages, sitemaps and feeds, and collects official company links.'))),
    list, h('section', { class: 'panel', style: 'margin-top:16px' }, h('div', { class: 'panel-head' }, h('h2', {}, 'Add a partner')), form));
  app.keys = null;
  await load();
}
export function openPartner(p, after) {
  const rep = parse(p.last_report);
  const site = h('input', { type: 'url', value: p.website, maxlength: 500, disabled: !can('partners.edit') });
  const d = openOverlay({ isDirty: () => site.value !== p.website, title: p.name, sub: `${p.run_count} runs · last visit ${p.last_sourced ? fmtDate(p.last_sourced) : 'never'}`, body: h('div', { class: 'drawer-body' },
    h('label', {}, 'Official website', site),
    rep.pages_visited !== undefined ? h('div', { class: 'facts' }, h('div', {}, h('small', {}, 'Pages visited'), h('span', {}, rep.pages_visited)), h('div', {}, h('small', {}, 'Company links'), h('span', {}, rep.candidates)), h('div', {}, h('small', {}, 'Added last time'), h('span', {}, rep.added)), h('div', {}, h('small', {}, 'Crawl complete'), h('span', {}, rep.complete ? 'Yes' : 'No — limits or blocks'))) : h('p', { class: 'hint' }, 'Not crawled yet.'),
    (rep.warnings || []).length ? h('div', { class: 'note warn' }, h('b', {}, 'Last crawl warnings'), h('ul', { class: 'reasons' }, rep.warnings.map((w) => h('li', {}, w)))) : null),
  foot: [h('button', { class: 'btn', onclick: () => d.close() }, 'Close'), can('partners.edit') ? h('button', { class: 'btn primary', onclick: async () => { try { await api('/api/partners/' + p.id, { method: 'PATCH', body: { website: site.value } }); toast('Saved'); d.close(true); after(); } catch (e) { toast(e.message, 'bad'); } } }, 'Save') : null] });
}

