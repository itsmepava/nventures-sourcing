// Sign-in, first-run setup and password change screens.
import { $, api, app, can, clearErrors, fieldError, h, icon, openOverlay, toast } from '../core.js';

const brand = () => h('div', { class: 'auth-brand' },
  h('img', { class: 'logo logo-light', src: '/assets/brand/logo.png', alt: 'nVentures', width: 200, height: 61 }),
  h('img', { class: 'logo logo-dark', src: '/assets/brand/logo-dark.png', alt: '', 'aria-hidden': 'true', width: 200, height: 61 }));

const emailOK = (v) => /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(v.trim());

function field(label, input, hint) {
  return h('label', {}, label, input, hint ? h('span', { class: 'hint' }, hint) : null);
}

export function renderAuth(root, { setup, mustChange, mode, error, onDone }) {
  const card = h('main', { class: 'auth-card', id: 'main', tabindex: '-1' }, brand());
  const formError = h('p', { class: 'note bad', role: 'alert', hidden: true });
  const showError = (msg) => { formError.textContent = msg; formError.hidden = false; };
  if (error) showError(error);

  if (setup) {
    const name = h('input', { type: 'text', autocomplete: 'name', required: true, maxlength: 100 });
    const email = h('input', { type: 'email', autocomplete: 'email', required: true, maxlength: 254 });
    const pw = h('input', { type: 'password', autocomplete: 'new-password', required: true, minlength: 10 });
    const pw2 = h('input', { type: 'password', autocomplete: 'new-password', required: true });
    const f = { name, email, pw, pw2 };
    card.append(h('h1', {}, 'Set up your workspace'), h('p', { class: 'muted' }, 'Create the first admin account. You can add sourcers, pitch responders and viewers afterwards.'), formError,
      h('form', { class: 'form', novalidate: true, onsubmit: async (e) => {
        e.preventDefault(); clearErrors(f); formError.hidden = true;
        if (name.value.trim().length < 2) return fieldError(name, 'Enter your name.'), name.focus();
        if (!emailOK(email.value)) return fieldError(email, 'Enter a valid email address.'), email.focus();
        if (pw.value.length < 10) return fieldError(pw, 'Use at least 10 characters.'), pw.focus();
        if (pw.value !== pw2.value) return fieldError(pw2, 'Passwords do not match.'), pw2.focus();
        try { await api('/api/auth/setup', { method: 'POST', body: { name: name.value, email: email.value, password: pw.value } }); toast('Workspace ready — welcome, ' + name.value.trim()); onDone(); }
        catch (err) { showError(err.message); }
      } }, field('Your name', name), field('Work email', email), field('Password', pw, 'At least 10 characters.'), field('Confirm password', pw2),
      h('button', { class: 'btn primary block', type: 'submit' }, 'Create admin account')));
  } else if (mustChange) {
    const cur = h('input', { type: 'password', autocomplete: 'current-password', required: true });
    const pw = h('input', { type: 'password', autocomplete: 'new-password', required: true });
    const pw2 = h('input', { type: 'password', autocomplete: 'new-password', required: true });
    const f = { cur, pw, pw2 };
    card.append(h('h1', {}, 'Choose your password'), h('p', { class: 'muted' }, 'You signed in with a temporary password. Choose your own to continue.'), formError,
      h('form', { class: 'form', novalidate: true, onsubmit: async (e) => {
        e.preventDefault(); clearErrors(f); formError.hidden = true;
        if (!cur.value) return fieldError(cur, 'Enter the temporary password.'), cur.focus();
        if (pw.value.length < 10) return fieldError(pw, 'Use at least 10 characters.'), pw.focus();
        if (pw.value !== pw2.value) return fieldError(pw2, 'Passwords do not match.'), pw2.focus();
        try { await api('/api/me/password', { method: 'POST', body: { current: cur.value, new: pw.value } }); toast('Password updated'); onDone(); }
        catch (err) { showError(err.message); }
      } }, field('Temporary password', cur), field('New password', pw, 'At least 10 characters.'), field('Confirm new password', pw2),
      h('button', { class: 'btn primary block', type: 'submit' }, 'Save and continue'),
      h('button', { class: 'btn ghost block', type: 'button', onclick: async () => { await api('/api/auth/logout', { method: 'POST' }); location.reload(); } }, icon('logout'), 'Sign out')));
  } else {
    const email = h('input', { type: 'email', autocomplete: 'username', required: true });
    const pw = h('input', { type: 'password', autocomplete: 'current-password', required: true });
    const f = { email, pw };
    card.append(h('h1', {}, 'Sign in'), h('p', { class: 'muted' }, 'nVentures sourcing and pitch workspace'), formError,
      h('form', { class: 'form', novalidate: true, onsubmit: async (e) => {
        e.preventDefault(); clearErrors(f); formError.hidden = true;
        if (!emailOK(email.value)) return fieldError(email, 'Enter your work email.'), email.focus();
        if (!pw.value) return fieldError(pw, 'Enter your password.'), pw.focus();
        try { await api('/api/auth/login', { method: 'POST', body: { email: email.value, password: pw.value } }); onDone(); }
        catch (err) { showError(err.message); pw.value = ''; pw.focus(); }
      } }, field('Email', email), field('Password', pw), h('button', { class: 'btn primary block', type: 'submit' }, 'Sign in')),
      h('p', { class: 'hint center' }, 'Forgot your password? Ask an admin to reset it.'));
  }
  if (mode === 'demo') card.append(h('p', { class: 'hint center' }, 'Demo mode: Gmail, Sheets and websites are local fixtures.'));
  root.replaceChildren(card);
  requestAnimationFrame(() => { const i = card.querySelector('input'); if (i) i.focus(); });
}

export function openChangePassword() {
  const cur = h('input', { type: 'password', autocomplete: 'current-password' });
  const pw = h('input', { type: 'password', autocomplete: 'new-password' });
  const pw2 = h('input', { type: 'password', autocomplete: 'new-password' });
  const f = { cur, pw, pw2 };
  const dirty = () => !!(cur.value || pw.value || pw2.value);
  const save = async () => {
    clearErrors(f);
    if (!cur.value) return fieldError(cur, 'Enter your current password.'), cur.focus();
    if (pw.value.length < 10) return fieldError(pw, 'Use at least 10 characters.'), pw.focus();
    if (pw.value !== pw2.value) return fieldError(pw2, 'Passwords do not match.'), pw2.focus();
    try { await api('/api/me/password', { method: 'POST', body: { current: cur.value, new: pw.value } }); toast('Password changed. Other sessions were signed out.'); d.close(true); }
    catch (e) { toast(e.message, 'bad'); }
  };
  const d = openOverlay({ kind: 'dialog', title: 'Change password', sub: app.me ? app.me.email : '', isDirty: dirty,
    body: h('form', { class: 'drawer-body form', novalidate: true, onsubmit: (e) => { e.preventDefault(); return save(); } }, field('Current password', cur), field('New password', pw, 'At least 10 characters.'), field('Confirm new password', pw2)),
    foot: [h('button', { class: 'btn', onclick: () => d.close() }, 'Cancel'), h('button', { class: 'btn primary', onclick: save }, 'Change password')] });
}
