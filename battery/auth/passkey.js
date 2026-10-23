// passkey: runs the WebAuthn ceremonies behind the buttons the passkey
// plugin's markup helpers mark. The buttons render hidden, so a page
// without script, or a browser without WebAuthn, never shows a control
// that cannot work; this module reveals them when it can drive them.
//
//   data-passkey-login="<auth base>"     sign in, then go where the
//                                        server says
//   data-passkey-register="<auth base>"  add a passkey to the signed-in
//                                        account; the name comes from a
//                                        [data-passkey-name] field in
//                                        the same form, when there is one
//   data-passkey-delete="<auth base>"    remove the passkey named by
//                                        the button's data-passkey-id
//
// Registering or removing re-renders the current page, so a list of
// passkeys the server drew shows the change. Failures surface as a
// toast; a ceremony the user cancelled says nothing.
(function () {
  'use strict';
  const NAME = 'passkey';
  const NS = window.__gofastr = window.__gofastr || {};
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;

  // say reads the page's runtime strings (passkey phrases registered in
  // battery/auth), so a toast speaks the reader's language.
  const say = function (k, en, p) { return typeof NS.say === 'function' ? NS.say(k, en, p) : en; };

  const SEL = '[data-passkey-login], [data-passkey-register], [data-passkey-delete]';
  // An auth base is a same-origin path: "/auth", "/api/auth". Anything
  // else leaves the button inert, so injected markup cannot aim a
  // ceremony at another origin.
  const BASE = /^\/(?!\/)[A-Za-z0-9._~\/-]*$/;
  const ID = /^[A-Za-z0-9_-]+$/;
  const bound = new WeakSet();
  const supported = typeof window.PublicKeyCredential === 'function' && !!navigator.credentials;

  function toB64(buf) {
    const bytes = new Uint8Array(buf);
    let s = '';
    for (let i = 0; i < bytes.length; i++) s += String.fromCharCode(bytes[i]);
    return btoa(s).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
  }

  function fromB64(s) {
    let t = String(s).replace(/-/g, '+').replace(/_/g, '/');
    while (t.length % 4) t += '=';
    const bin = atob(t);
    const out = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
    return out;
  }

  async function post(url, body) {
    // Forward the page's CSRF token, so the routes work behind
    // auth.CSRF and WithBFFPosture.
    const headers = { 'Content-Type': 'application/json', 'Accept': 'application/json' };
    const token = document.querySelector('meta[name="csrf-token"]')?.content;
    if (token) headers['X-CSRF-Token'] = token;
    const r = await fetch(url, {
      method: 'POST',
      credentials: 'same-origin',
      headers,
      body: JSON.stringify(body),
    });
    if (r.status === 204) return null;
    let data = null;
    try { data = await r.json(); } catch (_) { data = null; }
    if (!r.ok) {
      const msg = data && typeof data.error === 'string' ? data.error : say('runtime.request_failed_status', 'Request failed ({{status}})', { status: r.status });
      throw new Error(msg);
    }
    return data;
  }

  function toast(title, variant) {
    if (typeof NS._toastOrFallback === 'function') NS._toastOrFallback({ title: title, variant: variant });
  }

  // refresh re-renders the current page so server-drawn state (the
  // passkey list) reflects the change.
  function refresh() {
    if (typeof NS.navigate === 'function') NS.navigate(location.pathname + location.search, { replace: true, force: true });
  }

  async function signIn(base) {
    const opts = await post(base + '/passkey/login/options', {});
    const pk = opts.publicKey;
    pk.challenge = fromB64(pk.challenge);
    pk.allowCredentials = [];
    const cred = await navigator.credentials.get({ publicKey: pk });
    const res = cred.response;
    const out = await post(base + '/passkey/login', {
      id: toB64(cred.rawId),
      clientDataJSON: toB64(res.clientDataJSON),
      authenticatorData: toB64(res.authenticatorData),
      signature: toB64(res.signature),
      userHandle: res.userHandle ? toB64(res.userHandle) : '',
    });
    // Every cached screen was drawn signed out.
    if (typeof NS.invalidate === 'function') NS.invalidate('*');
    const to = out && typeof out.redirect === 'string' ? out.redirect : '/';
    if (typeof NS.navigate === 'function') NS.navigate(to, { force: true });
  }

  async function register(btn, base) {
    const form = btn.closest('form');
    const field = form ? form.querySelector('[data-passkey-name]') : null;
    const name = field && typeof field.value === 'string' ? field.value : '';
    const opts = await post(base + '/passkey/register/options', {});
    const pk = opts.publicKey;
    pk.challenge = fromB64(pk.challenge);
    pk.user.id = fromB64(pk.user.id);
    pk.excludeCredentials = (pk.excludeCredentials || []).map(function (c) {
      return { type: c.type, id: fromB64(c.id) };
    });
    const cred = await navigator.credentials.create({ publicKey: pk });
    await post(base + '/passkey/register', {
      name: name,
      id: toB64(cred.rawId),
      clientDataJSON: toB64(cred.response.clientDataJSON),
      attestationObject: toB64(cred.response.attestationObject),
    });
    toast(say('auth.passkey_added', 'Passkey added'), 'success');
    refresh();
  }

  async function remove(btn, base) {
    const id = btn.getAttribute('data-passkey-id') || '';
    if (!ID.test(id)) return;
    await post(base + '/passkey/delete', { id: id });
    toast(say('auth.passkey_removed', 'Passkey removed'), 'success');
    refresh();
  }

  function run(btn, fn) {
    if (btn.getAttribute('aria-busy') === 'true') return;
    btn.setAttribute('aria-busy', 'true');
    btn.disabled = true;
    fn().catch(function (err) {
      // NotAllowedError is the browser's answer to a cancelled or
      // timed-out prompt: the user already knows.
      if (err && err.name === 'NotAllowedError') return;
      toast(err && err.message ? err.message : say('auth.passkey_failed', 'Passkey request failed'), 'error');
    }).finally(function () {
      btn.removeAttribute('aria-busy');
      btn.disabled = false;
    });
  }

  function bind(btn) {
    if (bound.has(btn)) return;
    bound.add(btn);
    let kind = '';
    let base = '';
    if (btn.hasAttribute('data-passkey-login')) { kind = 'login'; base = btn.getAttribute('data-passkey-login'); }
    else if (btn.hasAttribute('data-passkey-register')) { kind = 'register'; base = btn.getAttribute('data-passkey-register'); }
    else { kind = 'delete'; base = btn.getAttribute('data-passkey-delete'); }
    if (!BASE.test(base || '')) return;
    // Removing a passkey needs no authenticator; the ceremonies do.
    if (kind !== 'delete' && !supported) return;
    btn.hidden = false;
    btn.addEventListener('click', function (e) {
      e.preventDefault();
      if (kind === 'login') run(btn, function () { return signIn(base); });
      else if (kind === 'register') run(btn, function () { return register(btn, base); });
      else run(btn, function () { return remove(btn, base); });
    });
  }

  function scan(root) {
    const r = root && root.querySelectorAll ? root : document;
    if (r.matches && r.matches(SEL)) bind(r);
    r.querySelectorAll(SEL).forEach(bind);
  }

  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;
  scan(document);
})();
