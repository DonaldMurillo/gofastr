// passkey: runs the WebAuthn ceremonies behind the elements the passkey
// plugin's markup helpers mark. Buttons render hidden, so a page without
// script, or a browser without WebAuthn, never shows a control that
// cannot work; this module reveals them when it can drive them.
//
//   data-passkey-login="<auth base>"     sign in, then go where the
//                                        server says
//   data-passkey-autofill="<auth base>"  on the username field: offer
//                                        the device's passkeys in its
//                                        autofill list (conditional UI)
//   data-passkey-register="<auth base>"  add a passkey to the signed-in
//                                        account; the name comes from a
//                                        [data-passkey-name] field in
//                                        the same form, when there is one
//   data-passkey-upgrade="<auth base>"   after a password sign-in, ask
//                                        the password manager to save a
//                                        passkey without a prompt
//   data-passkey-rename="<auth base>"    rename the passkey named by
//                                        data-passkey-id to the form's
//                                        [data-passkey-name] value
//   data-passkey-delete="<auth base>"    remove the passkey named by
//                                        the button's data-passkey-id
//   data-passkey-reauth="<auth base>"    confirm with a passkey, put the
//                                        proof in the form's
//                                        passkey_reauth field, submit
//   data-passkey-list="<auth base>"      on a passkey list: tell the
//                                        password manager which passkeys
//                                        the account still has
//
// Adding, renaming or removing re-renders the current page, so a list of
// passkeys the server drew shows the change. Failures surface as a
// toast; a ceremony the user cancelled says nothing.
(function () {
  'use strict';
  const NAME = 'passkey';
  const NS = window.__gofastr = window.__gofastr || {};
  // The runtime's host transport when a native host installed one
  // (core-ui ARCHITECTURE.md "Host transport"); read at call time, since
  // the kernel may replace the namespace after this script ran.
  const hostFetch = (u, i) => ((window.__gofastr && window.__gofastr.fetch) || fetch)(u, i);
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;

  // say reads the page's runtime strings (passkey phrases registered in
  // battery/auth), so a toast speaks the reader's language.
  const say = function (k, en, p) { return typeof NS.say === 'function' ? NS.say(k, en, p) : en; };

  const KINDS = ['login', 'autofill', 'register', 'upgrade', 'rename', 'delete', 'reauth', 'list'];
  const SEL = KINDS.map(function (k) { return '[data-passkey-' + k + ']'; }).join(', ');
  // An auth base is a same-origin path: "/auth", "/api/auth". Anything
  // else leaves the element inert, so injected markup cannot aim a
  // ceremony at another origin.
  const BASE = /^(?:\/[A-Za-z0-9._~-]+)+$/;
  const ID = /^[A-Za-z0-9_-]+$/;
  const PROOF_FIELD = 'passkey_reauth';
  const bound = new WeakSet();
  const PKC = window.PublicKeyCredential;
  const supported = typeof PKC === 'function' && !!navigator.credentials;

  // caps resolves the browser's WebAuthn capabilities once; {} where
  // getClientCapabilities is missing, so every check falls back.
  let capsP = null;
  function caps() {
    if (!capsP) {
      capsP = supported && typeof PKC.getClientCapabilities === 'function'
        ? PKC.getClientCapabilities().catch(function () { return {}; })
        : Promise.resolve({});
    }
    return capsP;
  }

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

  // The JSON helpers where the browser has them, the same conversion by
  // hand where it does not.
  function descriptors(list) {
    return (list || []).map(function (c) {
      const d = { type: c.type, id: fromB64(c.id) };
      if (Array.isArray(c.transports)) d.transports = c.transports;
      return d;
    });
  }
  function requestOptions(pk) {
    if (typeof PKC.parseRequestOptionsFromJSON === 'function') return PKC.parseRequestOptionsFromJSON(pk);
    return Object.assign({}, pk, { challenge: fromB64(pk.challenge), allowCredentials: descriptors(pk.allowCredentials) });
  }
  function creationOptions(pk) {
    if (typeof PKC.parseCreationOptionsFromJSON === 'function') return PKC.parseCreationOptionsFromJSON(pk);
    return Object.assign({}, pk, {
      challenge: fromB64(pk.challenge),
      user: Object.assign({}, pk.user, { id: fromB64(pk.user.id) }),
      excludeCredentials: descriptors(pk.excludeCredentials),
    });
  }
  function credJSON(cred) {
    if (typeof cred.toJSON === 'function') {
      try { return cred.toJSON(); } catch (_) { /* hand conversion below */ }
    }
    const r = cred.response;
    const out = { id: toB64(cred.rawId), response: { clientDataJSON: toB64(r.clientDataJSON) } };
    if (r.attestationObject) {
      out.response.attestationObject = toB64(r.attestationObject);
      out.response.transports = typeof r.getTransports === 'function' ? r.getTransports() : [];
    } else {
      out.response.authenticatorData = toB64(r.authenticatorData);
      out.response.signature = toB64(r.signature);
      out.response.userHandle = r.userHandle ? toB64(r.userHandle) : '';
    }
    return out;
  }
  function assertionBody(cred) {
    const j = credJSON(cred), r = j.response;
    return {
      id: j.id, clientDataJSON: r.clientDataJSON, authenticatorData: r.authenticatorData,
      signature: r.signature, userHandle: r.userHandle || '',
    };
  }

  function csrfHeaders(headers) {
    // Forward the page's CSRF token, so the routes work behind
    // auth.CSRF and WithBFFPosture.
    const token = document.querySelector('meta[name="csrf-token"]')?.content;
    if (token) headers['X-CSRF-Token'] = token;
    return headers;
  }

  async function send(url, init) {
    // Belt to BASE's braces: nothing leaves the page's origin.
    if (new URL(url, location.href).origin !== location.origin) throw new Error(say('auth.passkey_failed', 'Passkey request failed'));
    const r = await hostFetch(url, Object.assign({ credentials: 'same-origin' }, init));
    if (r.status === 204) return null;
    let data = null;
    try { data = await r.json(); } catch (_) { data = null; }
    if (!r.ok) {
      const msg = data && typeof data.error === 'string' ? data.error : say('runtime.request_failed_status', 'Request failed ({{status}})', { status: r.status });
      const err = new Error(msg);
      err.status = r.status;
      err.reason = data && typeof data.reason === 'string' ? data.reason : '';
      throw err;
    }
    return data;
  }

  function post(url, body) {
    return send(url, {
      method: 'POST',
      headers: csrfHeaders({ 'Content-Type': 'application/json', 'Accept': 'application/json' }),
      body: JSON.stringify(body),
    });
  }

  function toast(title, variant) {
    if (typeof NS._toastOrFallback === 'function') NS._toastOrFallback({ title: title, variant: variant });
  }

  // refresh re-renders the current page so server-drawn state (the
  // passkey list) reflects the change.
  function refresh() {
    if (typeof NS.navigate === 'function') NS.navigate(location.pathname + location.search, { replace: true, force: true });
  }

  // signalList tells the password manager which of the account's
  // passkeys the server still accepts and what the account is called,
  // so a removed passkey stops being offered. Browsers without the
  // Signal API skip it; it never surfaces an error.
  async function signalList(base) {
    if (!supported || (typeof PKC.signalAllAcceptedCredentials !== 'function' && typeof PKC.signalCurrentUserDetails !== 'function')) return;
    try {
      const list = await send(base + '/passkey/list', { method: 'GET', headers: { 'Accept': 'application/json' } });
      if (!list || typeof list.rpId !== 'string' || typeof list.userId !== 'string') return;
      if (typeof PKC.signalAllAcceptedCredentials === 'function') {
        await PKC.signalAllAcceptedCredentials({
          rpId: list.rpId, userId: list.userId,
          allAcceptedCredentialIds: (list.passkeys || []).map(function (p) { return p.id; }),
        });
      }
      if (typeof PKC.signalCurrentUserDetails === 'function' && typeof list.userName === 'string') {
        await PKC.signalCurrentUserDetails({ rpId: list.rpId, userId: list.userId, name: list.userName, displayName: list.userName });
      }
    } catch (_) { /* advisory only */ }
  }

  // A browser runs one WebAuthn request at a time, and the autofill one
  // waits as long as the field is on the page. Every other ceremony
  // withdraws it first (exclusive), and it starts again only once no
  // ceremony is running.
  let waiting = null;
  let ceremonies = 0;
  let resumers = [];
  // leaving: a sign-in succeeded and the page is going away, so nothing
  // restarts the autofill request. field: the autofill field the
  // waiting request belongs to.
  let leaving = false;
  let field = null;
  // A navigation that took the field away withdraws its request.
  window.addEventListener('gofastr:navigate', function () {
    leaving = false;
    if (waiting && !(field && field.isConnected)) { waiting.abort(); waiting = null; }
  });
  function withdrawn() {
    const e = new Error('withdrawn');
    e.name = 'AbortError';
    return e;
  }
  async function exclusive(fn) {
    ceremonies++;
    if (waiting) { waiting.abort(); waiting = null; }
    try {
      return await fn();
    } finally {
      if (--ceremonies === 0) resumers.splice(0).forEach(function (r) { r(); });
    }
  }
  function idle() {
    return ceremonies ? new Promise(function (r) { resumers.push(r); }) : Promise.resolve();
  }

  async function signIn(base, conditional) {
    const opts = await post(base + '/passkey/login/options', conditional ? { autofill: true } : {});
    const pk = opts.publicKey;
    if (conditional && ceremonies) throw withdrawn();
    const req = { publicKey: requestOptions(pk) };
    let renew = 0;
    if (conditional) {
      const ac = waiting = new AbortController();
      req.signal = ac.signal;
      req.mediation = 'conditional';
      // A conditional request has no timer of its own, but its challenge
      // expires: withdraw it shortly before, so autofill asks for a fresh
      // one instead of offering a passkey the server will refuse.
      const ttl = typeof pk.timeout === 'number' && pk.timeout > 0 ? pk.timeout : 300000;
      renew = setTimeout(function () { if (waiting === ac) { waiting = null; ac.abort(); } }, Math.max(ttl - 30000, ttl / 2));
    }
    let cred;
    try {
      cred = await navigator.credentials.get(req);
    } finally {
      clearTimeout(renew);
    }
    if (conditional) waiting = null;
    const body = assertionBody(cred);
    let out;
    try {
      out = await post(base + '/passkey/login', body);
    } catch (err) {
      // The server holds no such passkey: tell the password manager, so
      // it stops offering it. The reason code, not the status alone,
      // says so; a 404 from a proxy or a deploy must not drop a passkey.
      if (err.status === 404 && err.reason === 'passkey_unknown_credential' && typeof PKC.signalUnknownCredential === 'function') {
        PKC.signalUnknownCredential({ rpId: pk.rpId, credentialId: body.id }).catch(function () {});
      }
      throw err;
    }
    // Every cached screen was drawn signed out.
    if (typeof NS.invalidate === 'function') NS.invalidate('*');
    const to = out && typeof out.redirect === 'string' ? out.redirect : '/';
    leaving = true;
    if (typeof NS.navigate === 'function') NS.navigate(to, { force: true });
  }

  // stepUp runs the step-up ceremony and answers the proof.
  async function stepUp(base) {
    const opts = await post(base + '/passkey/reauth/options', {});
    const cred = await navigator.credentials.get({ publicKey: requestOptions(opts.publicKey) });
    const out = await post(base + '/passkey/reauth', assertionBody(cred));
    return out && typeof out.proof === 'string' ? out.proof : '';
  }

  // guarded posts body, and when the server answers that the account's
  // existing passkey must confirm first, runs the step-up and posts it
  // again with the proof. A silent ceremony (quiet) never prompts: it
  // gives up instead.
  async function guarded(url, base, body, quiet) {
    try {
      return await post(url, body);
    } catch (err) {
      if (quiet || err.status !== 403 || err.reason !== 'passkey_reauth_required') throw err;
    }
    return post(url, Object.assign({}, body, { proof: await stepUp(base) }));
  }

  async function create(base, name, conditional) {
    const opts = await guarded(base + '/passkey/register/options', base, conditional ? { conditional: true } : {}, conditional);
    const req = { publicKey: creationOptions(opts.publicKey) };
    if (conditional) req.mediation = 'conditional';
    const cred = await navigator.credentials.create(req);
    const j = credJSON(cred), r = j.response;
    await post(base + '/passkey/register', {
      name: name, id: j.id, clientDataJSON: r.clientDataJSON, attestationObject: r.attestationObject,
      transports: Array.isArray(r.transports) ? r.transports : [],
    });
  }

  function nameIn(btn) {
    const form = btn.closest('form');
    const field = form ? form.querySelector('[data-passkey-name]') : null;
    return field && typeof field.value === 'string' ? field.value : '';
  }

  async function register(btn, base) {
    await create(base, nameIn(btn), false);
    toast(say('auth.passkey_added', 'Passkey added'), 'success');
    await signalList(base);
    refresh();
  }

  async function rename(btn, base) {
    const id = btn.getAttribute('data-passkey-id') || '';
    if (!ID.test(id)) return;
    await post(base + '/passkey/rename', { id: id, name: nameIn(btn) });
    toast(say('auth.passkey_renamed', 'Passkey renamed'), 'success');
    refresh();
  }

  async function remove(btn, base) {
    const id = btn.getAttribute('data-passkey-id') || '';
    if (!ID.test(id)) return;
    await guarded(base + '/passkey/delete', base, { id: id }, false);
    toast(say('auth.passkey_removed', 'Passkey removed'), 'success');
    await signalList(base);
    refresh();
  }

  async function reauth(btn, base) {
    const form = btn.closest('form');
    if (!form) return;
    const value = await stepUp(base);
    let proof = form.querySelector('input[name="' + PROOF_FIELD + '"]');
    if (!proof) {
      proof = document.createElement('input');
      proof.type = 'hidden';
      proof.name = PROOF_FIELD;
      form.appendChild(proof);
    }
    proof.value = value;
    form.requestSubmit();
  }

  function failure(err, kind) {
    // NotAllowedError is the browser's answer to a cancelled or
    // timed-out prompt, AbortError to one this module withdrew: the user
    // already knows, or never saw it.
    if (!err || err.name === 'NotAllowedError' || err.name === 'AbortError') return '';
    // Only a create answers InvalidStateError for an excluded credential.
    if (err.name === 'InvalidStateError' && kind === 'register') return say('auth.passkey_exists_here', 'This device already holds a passkey for this account');
    if (err.name === 'SecurityError') return say('auth.passkey_wrong_site', 'Passkeys do not work on this site address');
    return err.message || say('auth.passkey_failed', 'Passkey request failed');
  }

  function run(btn, kind, fn) {
    if (btn.getAttribute('aria-busy') === 'true') return;
    btn.setAttribute('aria-busy', 'true');
    btn.disabled = true;
    exclusive(fn).catch(function (err) {
      const msg = failure(err, kind);
      if (msg) toast(msg, 'error');
    }).finally(function () {
      btn.removeAttribute('aria-busy');
      btn.disabled = false;
    });
  }

  // autofill keeps one conditional sign-in waiting behind the field's
  // autofill list while the field is on the page. A request that ends
  // without signing in (it timed out, or the passkey was refused) is
  // started again, a few times at most.
  async function autofill(input, base) {
    if (!supported) return;
    const c = await caps();
    const ok = c.conditionalGet === true || (c.conditionalGet === undefined &&
      typeof PKC.isConditionalMediationAvailable === 'function' && await PKC.isConditionalMediationAvailable().catch(function () { return false; }));
    if (!ok) return;
    const ac = input.getAttribute('autocomplete') || '';
    if (!/\bwebauthn\b/.test(ac)) input.setAttribute('autocomplete', (ac ? ac + ' ' : 'username ') + 'webauthn');
    for (let tries = 0; tries < 5; tries++) {
      await idle();
      if (leaving || !input.isConnected) return;
      field = input;
      try {
        await signIn(base, true);
        return;
      } catch (err) {
        if (err && err.name === 'AbortError') {
          tries--;
          continue;
        }
        const msg = failure(err, 'login');
        if (msg) toast(msg, 'error');
      }
    }
  }

  // upgrade asks for a passkey without a prompt after a password
  // sign-in, where the browser can (conditionalCreate). It is silent: the
  // password manager tells the user when it saved one.
  async function upgrade(base) {
    if (!supported) return;
    const c = await caps();
    if (c.conditionalCreate !== true) return;
    // Once per tab session: every page load asking again would spend
    // the rate limit and a challenge for an offer already made.
    const key = 'gofastr.passkey.upgrade:' + base;
    try {
      if (sessionStorage.getItem(key)) return;
      sessionStorage.setItem(key, '1');
    } catch (_) { /* no storage: ask this once anyway */ }
    try { await exclusive(function () { return create(base, '', true); }); } catch (_) { /* nothing to tell */ }
  }

  function bind(el) {
    if (bound.has(el)) return;
    bound.add(el);
    let kind = '';
    for (const k of KINDS) {
      if (el.hasAttribute('data-passkey-' + k)) { kind = k; break; }
    }
    const base = el.getAttribute('data-passkey-' + kind);
    if (!BASE.test(base || '')) return;
    if (kind === 'autofill') { autofill(el, base); return; }
    if (kind === 'upgrade') { upgrade(base); return; }
    if (kind === 'list') { signalList(base); return; }
    // Renaming or removing a passkey needs no authenticator; the
    // ceremonies do. Adding one is offered wherever WebAuthn is: a USB
    // or NFC security key can hold a passkey on a device that reports no
    // platform authenticator and no hybrid transport.
    if (kind !== 'delete' && kind !== 'rename' && !supported) return;
    el.hidden = false;
    el.addEventListener('click', function (e) {
      e.preventDefault();
      if (kind === 'login') run(el, kind, function () { return signIn(base, false); });
      else if (kind === 'register') run(el, kind, function () { return register(el, base); });
      else if (kind === 'rename') run(el, kind, function () { return rename(el, base); });
      else if (kind === 'reauth') run(el, kind, function () { return reauth(el, base); });
      else run(el, kind, function () { return remove(el, base); });
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
