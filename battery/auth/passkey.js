// passkey: signs in with a passkey, from a button or from the username
// field's autofill list, and holds what the passkey-manage module shares
// (the WebAuthn JSON helpers, the same-origin transport, the one-request
// lock). Buttons render hidden, so a page without script, or a browser
// without WebAuthn, never shows a control that cannot work.
//
//   data-passkey-login="<auth base>"     sign in, then go where the
//                                        server says
//   data-passkey-autofill="<auth base>"  on the username field: offer
//                                        the device's passkeys in its
//                                        autofill list (conditional UI)
//
// Failures surface as a toast; a ceremony the user cancelled says nothing.
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

  const KINDS = ['login', 'autofill'];
  const SEL = KINDS.map(function (k) { return '[data-passkey-' + k + ']'; }).join(', ');
  // An auth base is a same-origin path: "/auth", "/api/auth". Anything
  // else leaves the element inert, so injected markup cannot aim a
  // ceremony at another origin.
  const BASE = /^(?:\/[A-Za-z0-9._~-]+)+$/;
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
  // credJSON is an assertion's JSON (passkey-manage converts a
  // registration's).
  function credJSON(cred) {
    if (typeof cred.toJSON === 'function') {
      try { return cred.toJSON(); } catch (_) { /* hand conversion below */ }
    }
    const r = cred.response;
    return { id: toB64(cred.rawId), response: {
      clientDataJSON: toB64(r.clientDataJSON), authenticatorData: toB64(r.authenticatorData),
      signature: toB64(r.signature), userHandle: r.userHandle ? toB64(r.userHandle) : '',
    } };
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
    // redirect 'error': a same-origin path that redirects elsewhere must
    // not carry the body (an assertion, a proof) or the CSRF token there.
    const r = await hostFetch(url, Object.assign({ credentials: 'same-origin', redirect: 'error' }, init));
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

  // A browser runs one WebAuthn request at a time, and the autofill one
  // waits as long as the field is on the page. Ceremonies run one after
  // another (chain); each withdraws the autofill request first
  // (exclusive), which starts again only once none is running or queued.
  let waiting = null;
  let ceremonies = 0;
  let chain = Promise.resolve();
  // leaving: a sign-in succeeded and the page is going away, so nothing
  // restarts the autofill request and no queued ceremony runs. field: the
  // one autofill field that owns the conditional request.
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
  // hold keeps every other ceremony queued until p settles.
  function hold(p) {
    ceremonies++;
    if (waiting) { waiting.abort(); waiting = null; }
    const done = function () { ceremonies--; };
    const settled = p.then(done, done);
    chain = chain.then(function () { return settled; });
    return p;
  }
  function exclusive(fn) {
    return hold(chain.then(function () {
      if (leaving) throw withdrawn();
      return fn();
    }));
  }
  async function idle() {
    while (ceremonies) await chain;
  }

  async function signIn(base, conditional, input) {
    const opts = await post(base + '/passkey/login/options' + (conditional ? '?autofill=1' : ''), {});
    const pk = opts.publicKey;
    // The page may have moved on, or a ceremony started, while the
    // options were on their way.
    if (conditional && (ceremonies || leaving || field !== input || !input.isConnected)) throw withdrawn();
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
      // Until this answers, no other sign-in starts.
      const sent = post(base + '/passkey/login', body);
      out = await (conditional ? hold(sent) : sent);
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
  // autofill list while the field is on the page; one field on the page
  // owns it. A request that ends without signing in (it timed out, or
  // the passkey was refused) is started again, a few times at most, each
  // time later, and its error shows once.
  async function autofill(input, base) {
    if (!supported || (field && field !== input && field.isConnected)) return;
    field = input;
    const c = await caps();
    const ok = c.conditionalGet === true || (c.conditionalGet === undefined &&
      typeof PKC.isConditionalMediationAvailable === 'function' && await PKC.isConditionalMediationAvailable().catch(function () { return false; }));
    if (!ok) return;
    const ac = input.getAttribute('autocomplete') || '';
    if (!/\bwebauthn\b/.test(ac)) input.setAttribute('autocomplete', (ac ? ac + ' ' : 'username ') + 'webauthn');
    let told = false;
    for (let tries = 0; tries < 5; tries++) {
      await idle();
      if (leaving || !input.isConnected || field !== input) break;
      try {
        await signIn(base, true, input);
        return;
      } catch (err) {
        if (err && err.name === 'AbortError') {
          tries--;
          continue;
        }
        const msg = failure(err, 'login');
        if (msg && !told) toast(msg, 'error');
        told = told || !!msg;
        await new Promise(function (r) { setTimeout(r, 1000 << tries); });
      }
    }
    if (field === input) field = null;
  }

  function bind(el) {
    if (bound.has(el)) return;
    bound.add(el);
    const kind = el.hasAttribute('data-passkey-login') ? 'login' : 'autofill';
    const base = el.getAttribute('data-passkey-' + kind);
    if (!BASE.test(base || '') || !supported) return;
    if (kind === 'autofill') { autofill(el, base); return; }
    el.hidden = false;
    el.addEventListener('click', function (e) {
      e.preventDefault();
      run(el, kind, function () { return signIn(base, false); });
    });
  }

  // What passkey-manage shares, so the two halves keep one transport,
  // one set of JSON helpers and one WebAuthn request at a time.
  NS._passkey = {
    BASE: BASE, supported: supported, caps: caps, say: say, send: send, post: post, toast: toast,
    requestOptions: requestOptions, fromB64: fromB64, toB64: toB64, descriptors: descriptors,
    assertionBody: assertionBody, exclusive: exclusive, run: run,
  };

  function scan(root) {
    const r = root && root.querySelectorAll ? root : document;
    if (r.matches && r.matches(SEL)) bind(r);
    r.querySelectorAll(SEL).forEach(bind);
  }

  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;
  scan(document);
})();
