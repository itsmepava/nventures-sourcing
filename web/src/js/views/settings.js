// ---------- settings ----------
import { disabledReason } from '../components.js';
import { $, $$, api, app, applyTheme, can, confirmDialog, h, loadState, need, prefs, toast, toggleCompact } from '../core.js';
import { schedulesPanel, templatesPanel } from './admin.js';

export async function renderSettings(main, params) {
  const s = await api('/api/settings');
  const r = app.state.readiness;
  const g = r.google;
  const msg = params.get('google');
  const editable = s.editable && can('settings.manage');
  const field = (key, label, hint, type = 'text') => { const i = h('input', { type, value: s[key] ?? '', disabled: !editable, 'data-key': key, id: 'set-' + key }); return h('label', {}, label, i, hint ? h('span', { class: 'hint' }, hint) : null); };
  const syncOut = h('input', { type: 'checkbox', checked: s.outbound_sync, disabled: !editable, 'data-key': 'outbound_sync' });
  const saveDest = async () => {
    const body = {}; $$('[data-key]', destForm).forEach((el) => { body[el.dataset.key] = el.type === 'checkbox' ? el.checked : el.value; });
    try { await api('/api/settings', { method: 'POST', body }); toast('Settings saved'); app.route(); } catch (e) { toast(e.message, 'bad'); }
  };
  const destForm = h('form', { class: 'panel-body form', onsubmit: (e) => { e.preventDefault(); saveDest(); } },
    !s.editable ? h('p', { class: 'note' }, 'Demo mode uses fixed local fixture destinations. Set APP_MODE=live in .env and restart to configure real sheets.') : !editable ? h('p', { class: 'note' }, 'Only admins can change destinations.') : null,
    field('expected_account', 'Expected Gmail account', 'Imports refuse to run if a different mailbox is connected.', 'email'),
    h('div', { class: 'field-row' }, field('inbound_sheet', 'Inbound sheet ID', 'From the sheet URL: /spreadsheets/d/<ID>/'), field('inbound_tab', 'Inbound tab', 'Headers: Company Name, Description, HQ, Contact, Status, Notes')),
    h('div', { class: 'field-row' }, field('outbound_sheet', 'Outbound sheet ID', 'Legacy 38-column sourcing sheet'), field('outbound_tab', 'Outbound tab')),
    h('div', { class: 'field-row' }, field('partner_tab', 'Partner list tab'), field('control_tab', 'Partner control tab')),
    field('gmail_query', 'Gmail search query', 'Standard Gmail search syntax; spam, trash and sent are excluded.'),
    h('label', { class: 'check' }, syncOut, 'Allow writing sourced companies to the outbound sheet'),
    editable ? h('div', { class: 'actions' }, h('button', { class: 'btn primary', type: 'submit' }, 'Save destinations')) : null);
  const testOut = h('div');
  const runTest = async () => { testOut.replaceChildren(h('p', { class: 'hint' }, 'Running read-only checks…')); try { const res = await api('/api/google/test', { method: 'POST' }); testOut.replaceChildren(h('ul', { class: 'ready-list' }, res.map((c) => h('li', {}, h('span', { class: 'dot ' + (c.Status === 'ok' ? 'ok' : c.Status === 'missing' ? 'warn' : 'bad') }), h('div', {}, h('b', {}, c.Name), h('span', {}, c.Message)))))); } catch (e) { testOut.replaceChildren(h('p', { class: 'note bad' }, e.message)); } };
  const fileIn = h('input', { type: 'file', accept: 'application/json,.json', 'aria-label': 'OAuth client JSON file' });
  const upload = async () => { const f = fileIn.files[0]; if (!f) { toast('Choose the downloaded client JSON file', 'bad'); return; } try { await api('/api/google/client', { method: 'POST', body: { json: await f.text() } }); toast('Client stored locally'); app.route(); } catch (e) { toast(e.message, 'bad'); } };
  const connectBtn = h('a', { class: 'btn primary', href: '/oauth/start' }, g.connected ? 'Reconnect Google' : 'Connect Google');
  if (!g.client_configured) disabledReason(connectBtn, 'Upload the OAuth client JSON first.');
  const googlePanel = h('section', { class: 'panel' }, h('div', { class: 'panel-head' }, h('div', {}, h('h2', {}, 'Google connection'), h('p', {}, 'One local OAuth connection: Gmail read-only + Sheets. Tokens stay in your data folder with user-only permissions.'))),
    h('div', { class: 'panel-body form' },
      msg === 'connected' ? h('p', { class: 'note ok', role: 'status' }, 'Google connected.') : msg === 'error' ? h('p', { class: 'note bad', role: 'alert' }, params.get('message') || 'Connection failed') : null,
      h('ul', { class: 'ready-list' },
        h('li', {}, h('span', { class: 'dot ' + (g.client_configured ? 'ok' : 'bad') }), h('div', {}, h('b', {}, 'OAuth client'), h('span', {}, g.client_configured ? 'Configured (' + g.client_source + ')' : 'Missing'))),
        h('li', {}, h('span', { class: 'dot ' + (g.connected ? 'ok' : 'bad') }), h('div', {}, h('b', {}, 'Consent'), h('span', {}, g.connected ? `Connected (${g.token_source})${g.account ? ' as ' + g.account : ''}` : 'Not connected'))),
        g.service_account ? h('li', {}, h('span', { class: 'dot ok' }), h('div', {}, h('b', {}, 'Service account'), h('span', {}, 'Present in .env (Sheets only)'))) : null),
      !can('settings.manage') ? h('p', { class: 'hint' }, 'Only admins can manage the Google connection.') : r.mode === 'live' ? h('div', { class: 'form' }, h('label', {}, 'OAuth client JSON (Desktop app) from Google Cloud Console', fileIn), h('div', { class: 'actions' }, h('button', { class: 'btn', onclick: upload }, 'Store client file'), connectBtn,
        g.connected ? h('button', { class: 'btn ghost', onclick: async () => { if (await confirmDialog('Disconnect Google?', 'This removes the local token. To also revoke it at Google, visit your Google account permissions page.', 'Disconnect')) { await api('/api/google/disconnect', { method: 'POST' }); app.route(); } } }, 'Disconnect') : null))
        : h('div', { class: 'actions' }, h('a', { class: 'btn', href: '/oauth/start' }, 'Try the consent flow (demo fixture)')),
      can('settings.manage') ? h('div', { class: 'actions' }, h('button', { class: 'btn', onclick: runTest }, 'Test connection (read-only)')) : null, testOut));
  const prefsPanel = h('section', { class: 'panel' }, h('div', { class: 'panel-head' }, h('h2', {}, 'Display')), h('div', { class: 'panel-body form' },
    h('label', {}, 'Theme', (() => { const sel = h('select', { onchange: () => { prefs.set('theme', sel.value); applyTheme(sel.value); } }, ['system', 'light', 'dark'].map((v) => h('option', { value: v }, v[0].toUpperCase() + v.slice(1)))); sel.value = prefs.get('theme', 'system'); return sel; })()),
    h('label', { class: 'check' }, h('input', { type: 'checkbox', checked: document.documentElement.classList.contains('compact'), onchange: toggleCompact }), 'Compact density'),
    h('button', { class: 'btn ghost', onclick: () => { for (const k of ['pitches', 'companies']) prefs.set('view.' + k, '{}'); toast('Saved table filters cleared'); } }, 'Clear saved filters')));
  const llmPanel = h('section', { class: 'panel' }, h('div', { class: 'panel-head' }, h('div', {}, h('h2', {}, 'Local model (optional)'), h('p', {}, 'Off by default. Suggestions are advisory only and never change qualification, statuses or sheets.'))), h('div', { class: 'panel-body form', id: 'llm-body' }, h('p', { class: 'hint' }, 'Checking for a local Ollama server…')));
  const other = h('section', { class: 'panel' }, h('div', { class: 'panel-head' }, h('h2', {}, 'Providers & data')), h('div', { class: 'panel-body form' },
    h('ul', { class: 'ready-list' },
      h('li', {}, h('span', { class: 'dot ok' }), h('div', {}, h('b', {}, 'Discovery'), h('span', {}, (r.discovery.paid_search ? 'First-party crawler; Tavily search on for missing partner websites only (' + r.discovery.paid_search_requests + ' requests since start). ' : 'First-party crawler (no Tavily). ') + 'Pages fetched: ' + r.discovery.counters.pages_fetched + ' · cache hits: ' + r.discovery.counters.cache_hits))),
      h('li', {}, h('span', { class: 'dot ok' }), h('div', {}, h('b', {}, 'Extraction'), h('span', {}, 'Deterministic local rules (no OpenRouter).'))),
      (r.legacy_adapters.tavily_key_present || r.legacy_adapters.openrouter_key_present) ? h('li', {}, h('span', { class: 'dot warn' }), h('div', {}, h('b', {}, 'Legacy paid keys present in .env'), h('span', {}, r.legacy_adapters.note))) : null,
      h('li', {}, h('span', { class: 'dot' }), h('div', {}, h('b', {}, 'Data folder'), h('span', {}, h('code', {}, r.data_dir))))),
    partnerSearchField(s, r),
    h('div', { class: 'actions' }, need(h('button', { class: 'btn', onclick: async () => { try { const x = await api('/api/cache/clear', { method: 'POST' }); toast(`Cleared ${x.cleared} cached pages`); } catch (e) { toast(e.message, 'bad'); } } }, 'Clear page cache'), 'settings.manage'),
      r.mode === 'demo' ? need(h('button', { class: 'btn danger', onclick: async () => { if (await confirmDialog('Reset demo data?', 'All demo pitches, companies and history are removed and the fixture sheets are emptied. A backup of the demo database is written first. Live data is not affected.', 'Reset demo')) { try { await api('/api/demo/reset', { method: 'POST' }); toast('Demo data reset'); app.route(); } catch (e) { toast(e.message, 'bad'); } } } }, 'Reset demo data'), 'demo.reset') : null)));
  main.replaceChildren(h('div', { class: 'page-head' }, h('div', {}, h('h2', {}, 'Settings'), h('p', {}, `Mode: ${r.mode}. Configuration from ${s.env_file || 'environment'} plus the values saved here.`))),
    h('div', { class: 'grid cols-2' }, googlePanel, h('section', { class: 'panel' }, h('div', { class: 'panel-head' }, h('h2', {}, 'Destinations')), destForm)),
    h('div', { class: 'grid cols-3', style: 'margin-top:16px' }, prefsPanel, llmPanel, other),
    can('templates.manage') ? templatesPanel() : null,
    can('schedules.manage') ? schedulesPanel() : null);
  app.keys = null;
  api('/api/llm').then((st) => {
    const body = $('#llm-body'); if (!body) return;
    const enabled = h('input', { type: 'checkbox', checked: s.llm_enabled, disabled: !st.available && !s.llm_enabled });
    const model = h('select', { disabled: !st.available }, h('option', { value: '' }, 'Choose a local model'), st.models.filter((m) => m.local).map((m) => h('option', { value: m.name }, m.name)));
    model.value = s.llm_model || '';
    body.replaceChildren(st.available ? h('p', { class: 'note ok' }, `Ollama found at ${st.endpoint} with ${st.models.filter((m) => m.local).length} local model(s).`) : h('p', { class: 'note' }, st.reason || 'No local model available.'),
      st.models.filter((m) => !m.local).length ? h('p', { class: 'hint' }, 'Cloud-backed models are hidden and never used.') : null,
      h('label', { class: 'check' }, enabled, 'Enable “Suggest with local model” in pitch review'), h('label', {}, 'Model', model),
      need(h('button', { class: 'btn', disabled: !st.available && !s.llm_enabled, onclick: async () => { try { await api('/api/settings', { method: 'POST', body: { llm_enabled: enabled.checked, llm_model: model.value } }); toast('Saved'); loadState(); } catch (e) { toast(e.message, 'bad'); } } }, 'Save'), 'settings.manage'));
  }).catch(() => {});
}


// Opt-in Tavily lookup for partners whose website is missing. Free methods
// (contact email domain, checked name-based addresses) always run first.
function partnerSearchField(s, r) {
  const box = h('input', { type: 'checkbox', checked: s.partner_search, disabled: !s.partner_search_available || !can('settings.manage') });
  box.addEventListener('change', async () => {
    try { await api('/api/settings', { method: 'POST', body: { partner_search: box.checked } }); toast(box.checked ? 'Web search on for missing partner websites' : 'Web search off'); loadState(); }
    catch (e) { box.checked = !box.checked; toast(e.message, 'bad'); }
  });
  const why = r.mode !== 'live' ? 'Live mode only.' : !s.partner_search_available ? 'Add TAVILY_API_KEY to .env and restart to make this available.' : 'Uses your Tavily plan (paid beyond its free allowance). Only partner names are sent.';
  return h('div', {}, h('label', { class: 'check' }, box, 'Search the web (Tavily) for missing partner websites'),
    h('p', { class: 'hint' }, 'When a partner has no Company Portfolio link, nVentures first tries the contact\'s work-email domain and addresses built from the partner\'s name, and accepts a site only if it names the partner. ' + why));
}
