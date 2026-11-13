// Demo sheet viewer: shows the local fixture sheet so demo row links can be
// checked. In live mode, row links open Google Sheets instead.
import { $, api, h } from '../core.js';

export async function renderDemoSheet(main, params) {
  const id = params.get('id') || '', tab = params.get('tab') || '';
  const row = Number(params.get('row') || 0);
  if (!id) { // opened without a sheet (bookmark or typed URL): offer both demo sheets
    const st = await api('/api/settings');
    const link = (label, sid, stab) => sid ? h('li', {}, h('a', { href: `#/demo-sheet?id=${encodeURIComponent(sid)}&tab=${encodeURIComponent(stab)}` }, `${label} · ${stab}`)) : null;
    main.replaceChildren(h('div', { class: 'page-head' }, h('div', {}, h('h2', {}, 'Demo sheets'), h('p', {}, 'Local fixture copies of the inbound and outbound sheets (demo mode only). In live mode, row links open Google Sheets.'))),
      h('section', { class: 'panel' }, h('ul', { class: 'source-list', style: 'padding:16px' }, link('Inbound pitches', st.inbound_sheet, st.inbound_tab || 'Pitches'), link('Outbound sourcing', st.outbound_sheet, st.outbound_tab || 'Sourcing'))));
    return;
  }
  const data = await api(`/api/demo/sheet?id=${encodeURIComponent(id)}&tab=${encodeURIComponent(tab)}`);
  const table = h('table', { class: 'sheet-table' }, h('caption', { class: 'sr-only' }, `Demo sheet ${tab}`), h('tbody', {}, data.rows.map((r, i) =>
    h('tr', { id: `row-${i + 1}`, class: i + 1 === row ? 'target' : '' }, h('th', { scope: 'row' }, String(i + 1)), r.map((c) => h('td', {}, h('div', { class: 'cell' }, c)))))));
  main.replaceChildren(
    h('div', { class: 'page-head' }, h('div', {}, h('h2', {}, `Demo sheet · ${tab}`), h('p', {}, `Local fixture copy of “${id}”. Every row was written with RAW input and verified by reading it back.${row ? ` Row ${row} is highlighted.` : ''}`)),
      h('div', { class: 'actions' }, h('button', { class: 'btn', onclick: () => history.back() }, 'Back'))),
    h('section', { class: 'panel' }, h('div', { class: 'table-wrap sheet-wrap' }, table)));
  if (row) requestAnimationFrame(() => { const el = document.getElementById(`row-${row}`); if (el) el.scrollIntoView({ block: 'center' }); });
}
