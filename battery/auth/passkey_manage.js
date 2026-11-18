// passkey-manage: adds, renames and removes the signed-in account's
// passkeys, runs a step-up for a sensitive form, and keeps the password
// manager in step. It runs on the passkey module's helpers
// (__gofastr._passkey), which the loader brings first (Requires).
//
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
// passkeys the server drew shows the change.
(function () {
  'use strict';
  const NAME = 'passkey-manage';
  const NS = window.__gofastr = window.__gofastr || {};
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  const P = NS._passkey;
  if (!P) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;
  const say = P.say, post = P.post, toast = P.toast;
  const PKC = window.PublicKeyCredential;
  const KINDS = ['register', 'upgrade', 'rename', 'delete', 'reauth', 'list'];
  const SEL = KINDS.map(function (k) { return '[data-passkey-' + k + ']'; }).join(', ');
  const ID = /^[A-Za-z0-9_-]+$/;
  const PROOF_FIELD = 'passkey_reauth';
  const bound = new WeakSet();

  // refresh re-renders the current page so server-drawn state (the
  // passkey list) reflects the change.
  function refresh() {
    if (typeof NS.navigate === 'function') NS.navigate(location.pathname + location.search, { replace: true, force: true });
  }

  // signalList tells the password manager which of the account's
  // passkeys the server still accepts and what the account is called,
  // so a removed passkey stops being offered. Browsers without the
  // Signal API skip it; it never surfaces an error.
  // reads counts list reads (a newer one supersedes an older one);
  // changes counts the starts and ends of adds and removes. A read that
  // overlapped a change never signals: it is read again once no change
  // runs, as is base after a change that succeeded.
  let reads = 0, changes = 0, changing = 0, missed = [];
  async function change(fn, base) {
    changing++;
    changes++;
    let ok = false;
    try {
      const r = await fn();
      ok = true;
      return r;
    } finally {
      changing--;
      changes++;
      if (!changing) {
        const again = new Set(missed);
        missed = [];
        if (ok) again.add(base);
        again.forEach(signalList);
      }
    }
  }
  async function signalList(base) {
    if (!P.supported || (typeof PKC.signalAllAcceptedCredentials !== 'function' && typeof PKC.signalCurrentUserDetails !== 'function')) return;
    const mine = ++reads, seen = changes;
    try {
      const list = await P.send(base + '/passkey/list', { method: 'GET', headers: { 'Accept': 'application/json' } });
      if (mine !== reads) return;
      if (seen !== changes || changing) {
        if (changing) missed.push(base);
        else setTimeout(function () { signalList(base); }, 0);
        return;
      }
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

  function creationOptions(pk) {
    if (typeof PKC.parseCreationOptionsFromJSON === 'function') return PKC.parseCreationOptionsFromJSON(pk);
    return Object.assign({}, pk, {
      challenge: P.fromB64(pk.challenge),
      user: Object.assign({}, pk.user, { id: P.fromB64(pk.user.id) }),
      excludeCredentials: P.descriptors(pk.excludeCredentials),
    });
  }

  function createdJSON(cred) {
    if (typeof cred.toJSON === 'function') {
      try { return cred.toJSON(); } catch (_) { /* hand conversion below */ }
    }
    const r = cred.response;
    return { id: P.toB64(cred.rawId), response: {
      clientDataJSON: P.toB64(r.clientDataJSON), attestationObject: P.toB64(r.attestationObject),
      transports: typeof r.getTransports === 'function' ? r.getTransports() : [],
    } };
  }

  // stepUp runs the step-up ceremony and answers the proof.
  async function stepUp(base) {
    const opts = await post(base + '/passkey/reauth/options', {});
    const cred = await navigator.credentials.get({ publicKey: P.requestOptions(opts.publicKey) });
    const out = await post(base + '/passkey/reauth', P.assertionBody(cred));
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

  function create(base, name, conditional) {
    return change(async function () {
      const opts = await guarded(base + '/passkey/register/options', base, conditional ? { conditional: true } : {}, conditional);
      const req = { publicKey: creationOptions(opts.publicKey) };
      if (conditional) req.mediation = 'conditional';
      const cred = await navigator.credentials.create(req);
      const j = createdJSON(cred), r = j.response;
      await post(base + '/passkey/register', {
        name: name, id: j.id, clientDataJSON: r.clientDataJSON, attestationObject: r.attestationObject,
        transports: Array.isArray(r.transports) ? r.transports : [], binding: opts.binding,
      });
    }, base);
  }

  function nameIn(btn) {
    const form = btn.closest('form');
    const field = form ? form.querySelector('[data-passkey-name]') : null;
    return field && typeof field.value === 'string' ? field.value : '';
  }

  async function register(btn, base) {
    await create(base, nameIn(btn), false);
    toast(say('auth.passkey_added', 'Passkey added'), 'success');
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
    await change(function () { return guarded(base + '/passkey/delete', base, { id: id }, false); }, base);
    toast(say('auth.passkey_removed', 'Passkey removed'), 'success');
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
    // Submitted as the button would have: its formaction and its own
    // name and value go with the proof.
    if (btn.type === 'submit' && btn.form === form) {
      btn.disabled = false;
      form.requestSubmit(btn);
    } else {
      form.requestSubmit();
    }
  }

  // upgrade asks for a passkey without a prompt after a password
  // sign-in, where the browser can (conditionalCreate). It is silent: the
  // password manager tells the user when it saved one.
  async function upgrade(base) {
    if (!P.supported) return;
    const c = await P.caps();
    if (c.conditionalCreate !== true) return;
    // Once per tab session: every page load asking again would spend
    // the rate limit and a challenge for an offer already made.
    const key = 'gofastr.passkey.upgrade:' + base;
    try {
      if (sessionStorage.getItem(key)) return;
      sessionStorage.setItem(key, '1');
    } catch (_) {
      return; // no storage: every load would ask again, so never ask
    }
    try { await P.exclusive(function () { return create(base, '', true); }); } catch (_) { /* nothing to tell */ }
  }

  function bind(el) {
    if (bound.has(el)) return;
    bound.add(el);
    let kind = '';
    for (const k of KINDS) {
      if (el.hasAttribute('data-passkey-' + k)) { kind = k; break; }
    }
    const base = el.getAttribute('data-passkey-' + kind);
    if (!P.BASE.test(base || '')) return;
    if (kind === 'upgrade') { upgrade(base); return; }
    if (kind === 'list') { signalList(base); return; }
    // Renaming or removing a passkey needs no authenticator; the
    // ceremonies do. Adding one is offered wherever WebAuthn is: a USB
    // or NFC security key can hold a passkey on a device that reports no
    // platform authenticator and no hybrid transport.
    if (kind !== 'delete' && kind !== 'rename' && !P.supported) return;
    el.hidden = false;
    el.addEventListener('click', function (e) {
      e.preventDefault();
      if (kind === 'register') P.run(el, kind, function () { return register(el, base); });
      else if (kind === 'rename') P.run(el, kind, function () { return rename(el, base); });
      else if (kind === 'reauth') P.run(el, kind, function () { return reauth(el, base); });
      else P.run(el, kind, function () { return remove(el, base); });
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
