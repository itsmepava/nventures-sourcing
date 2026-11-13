// Team (admins): add members, change roles, deactivate, reset passwords.
import { $, api, app, badge, can, clearErrors, confirmDialog, fieldError, fmtDate, h, icon, openOverlay, toast } from '../core.js';

function showTempPassword(title, who, pw) {
  const code = h('code', { class: 'temp-pw' }, pw);
  const d = openOverlay({ kind: 'dialog', title, body: h('div', { class: 'drawer-body' },
    h('p', {}, `Give this one-time password to ${who} privately. They must choose their own password when they first sign in. It will not be shown again.`),
    h('p', { class: 'center' }, code)),
  foot: [h('button', { class: 'btn', onclick: async () => { try { await navigator.clipboard.writeText(pw); toast('Copied'); } catch { toast('Copy failed — select the text instead', 'bad'); } } }, 'Copy'), h('button', { class: 'btn primary', onclick: () => d.close(true) }, 'Done')] });
}

export async function renderTeam(main) {
  const data = await api('/api/users');
  const roles = data.roles;
  const roleSelect = (value, label) => { const s = h('select', { 'aria-label': label }, roles.map((r) => h('option', { value: r.ID }, r.Name))); s.value = value; return s; };
  const load = async () => { const d = await api('/api/users'); table.replaceChildren(rowsFor(d.users)); app.team = null; };
  const rowsFor = (users) => h('table', {}, h('caption', { class: 'sr-only' }, 'Team members'),
    h('thead', {}, h('tr', {}, h('th', { scope: 'col' }, 'Member'), h('th', { scope: 'col', class: 'tight' }, 'Role'), h('th', { scope: 'col', class: 'tight hide-mobile' }, 'Last sign-in'), h('th', { scope: 'col', class: 'tight' }, 'Status'), h('th', { scope: 'col', class: 'tight' }, h('span', { class: 'sr-only' }, 'Actions')))),
    h('tbody', {}, users.map((u) => {
      const me = u.id === app.me.id;
      const role = roleSelect(u.role, `Role for ${u.name}`);
      role.addEventListener('change', async () => {
        try { await api(`/api/users/${u.id}`, { method: 'PATCH', body: { role: role.value } }); toast(`${u.name} is now ${role.selectedOptions[0].textContent}`); await load(); }
        catch (e) { toast(e.message, 'bad'); role.value = u.role; }
      });
      return h('tr', { class: u.active ? '' : 'muted-row' },
        h('td', {}, h('div', { class: 'cell-main' }, u.name, me ? h('span', { class: 'badge info', style: 'margin-left:6px' }, 'You') : null), h('div', { class: 'cell-sub' }, u.email)),
        h('td', { class: 'tight' }, role),
        h('td', { class: 'tight hide-mobile' }, u.last_login_at ? fmtDate(u.last_login_at) : 'Never'),
        h('td', { class: 'tight' }, u.active ? (u.must_change_password ? h('span', { class: 'badge warn' }, 'Invited') : h('span', { class: 'badge ok' }, 'Active')) : h('span', { class: 'badge' }, 'Deactivated')),
        h('td', { class: 'tight' }, h('div', { class: 'actions' },
          h('button', { class: 'btn sm', onclick: async () => {
            if (!(await confirmDialog(`Reset ${u.name}'s password?`, 'They will be signed out everywhere and must sign in with a new one-time password.', 'Reset password'))) return;
            try { const r = await api(`/api/users/${u.id}/reset-password`, { method: 'POST' }); showTempPassword('Password reset', u.name, r.temporary_password); await load(); } catch (e) { toast(e.message, 'bad'); }
          } }, 'Reset password'),
          me ? null : h('button', { class: 'btn sm ' + (u.active ? 'danger' : ''), onclick: async () => {
            if (u.active && !(await confirmDialog(`Deactivate ${u.name}?`, 'They are signed out immediately and can no longer sign in. Their history stays. You can reactivate them later.', 'Deactivate'))) return;
            try { await api(`/api/users/${u.id}`, { method: 'PATCH', body: { active: !u.active } }); toast(u.active ? `${u.name} deactivated` : `${u.name} reactivated`); await load(); } catch (e) { toast(e.message, 'bad'); }
          } }, u.active ? 'Deactivate' : 'Reactivate'))));
    })));
  const table = h('div', { class: 'table-wrap' }, rowsFor(data.users));

  const name = h('input', { type: 'text', maxlength: 100, autocomplete: 'off' });
  const email = h('input', { type: 'email', maxlength: 254, autocomplete: 'off' });
  const role = roleSelect('responder', 'Role');
  const form = h('form', { class: 'panel-body form', novalidate: true, onsubmit: async (e) => {
    e.preventDefault(); clearErrors({ name, email });
    if (name.value.trim().length < 2) { fieldError(name, 'Enter their name.'); name.focus(); return; }
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.value.trim())) { fieldError(email, 'Enter a valid email address.'); email.focus(); return; }
    try {
      const u = await api('/api/users', { method: 'POST', body: { name: name.value, email: email.value, role: role.value } });
      showTempPassword('Member added', u.name, u.temporary_password);
      name.value = ''; email.value = ''; await load();
    } catch (err) { toast(err.message, 'bad'); }
  } }, h('div', { class: 'field-row' }, h('label', {}, 'Name', name), h('label', {}, 'Email', email), h('label', {}, 'Role', role)),
  h('div', { class: 'actions' }, h('button', { class: 'btn primary', type: 'submit' }, icon('plus'), 'Add member')));

  main.replaceChildren(
    h('div', { class: 'page-head' }, h('div', {}, h('h2', {}, 'Team'), h('p', {}, 'Everyone signs in with their own account. Roles decide what each person can do; every action is recorded in Activity with their name.'))),
    h('section', { class: 'panel' }, h('div', { class: 'panel-head' }, h('h2', {}, 'Members')), table),
    h('div', { class: 'grid cols-2', style: 'margin-top:16px' },
      h('section', { class: 'panel' }, h('div', { class: 'panel-head' }, h('h2', {}, 'Add a member')), form),
      h('section', { class: 'panel' }, h('div', { class: 'panel-head' }, h('h2', {}, 'Roles')), h('div', { class: 'panel-body' }, h('ul', { class: 'ready-list' }, roles.map((r) => h('li', {}, h('span', { class: 'dot ok' }), h('div', {}, h('b', {}, r.Name), h('span', {}, r.Summary)))))))));
}
