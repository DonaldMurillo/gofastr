// local-bridge: the four bridges between framework/local's browser
// store and Go screens: the seed (a signal filled from a record), the
// mirror (a record a render reads at first paint, through a cookie),
// the upload (records that ride an RPC request) and the download
// (records a response writes back). Requires local-store, which owns
// the store, the caps and the migrations; this file owns the markers
// data-local-seed and data-local-send, the cookies, the
// request/response hooks on rpc.js's data-fui-rpc-with seam, and
// nothing else.
//
// The line between the modules is a responsibility, not a byte count:
// local-store keeps records, local-migrate rewrites them between
// versions, local-bridge is every way they reach a Go handler. A page
// whose store keeps its records to itself never loads this file;
// local-store asks for it when a declaration mirrors a collection or a
// logout is pending.
(() => {
  'use strict';
  const NAME = 'local-bridge';
  const NS = window.__gofastr = window.__gofastr || {};
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  // Flag first (see local-store.js).
  (NS.loadedModules = NS.loadedModules || {})[NAME] = true;

  // The reserved-name guard is spelled here because this file needs it
  // BEFORE it has a store: an app id off a marker names the store it
  // would fetch. Everything else comes off the store object this file
  // already fetches, so there is no second global for a script on the
  // origin to replace.
  const RESERVED = /^(__proto__|constructor|prototype)$/;
  const own = (o, k) => Object.prototype.hasOwnProperty.call(o, k);
  const openStore = (app) => NS.localStore(app);
  // validKey is the one validator with real content, a byte length and
  // a reserved-name set that must agree with the Go side, so it comes
  // off the store rather than being spelled twice. encode and isObject
  // are two lines each and are spelled here.
  const validKeyOf = (store) => (store && store.helpers && store.helpers.validKey) || (() => false);
  const isObject = (v) => v !== null && typeof v === 'object' && !Array.isArray(v);
  const encode = (value) => {
    let text;
    try { text = JSON.stringify(value); } catch (_) { return null; }
    return typeof text === 'string' ? text : null;
  };
  // The bound on the trigger is bytes, the unit the server's
  // MaxBytesReader counts; String.length is UTF-16 units, and
  // JSON.stringify leaves non-ASCII unescaped, so a CJK draft is up to
  // three times longer on the wire than in units.
  const bytesOf = (text) => {
    try { return new TextEncoder().encode(text).length; } catch (_) { return text.length; }
  };

  const MARKER = '[data-local-seed]';

  // ─── the mirror bridge: a record a Go render reads at first paint ──

  // A mirrored collection keeps every record in a cookie too, so a Go
  // screen can read it at FIRST PAINT (the IndexedDB read lands after
  // hydration, by construction). The name is the framework's namespace
  // plus one component-encoded segment, the value the component-
  // encoded JSON text: neither can carry the cookie grammar. Same
  // shape as banner.js's dismissal cookie, which is the one channel a
  // browser-held value had to the server before this module.
  //
  // On https the cookie is Secure, like every cookie the Go side sets
  // (response.go follows the request scheme). Without it a single
  // plain-http request to the origin, a typo, a stripped link, a
  // captive portal, carries every mirrored record in clear text.
  // Spelled as whole literal writes per case rather than a
  // concatenated suffix: the cookie lint reads a document.cookie
  // assignment operand by operand and refuses any non-literal that is
  // not component-encoded, which is the rule that keeps a value from
  // planting its own cookie name and attributes.
  const SECURE = (() => {
    try { return window.location.protocol === 'https:'; } catch (_) { return false; }
  })();
  const cookieNamed = (name) => {
    try { return ('; ' + document.cookie).indexOf('; ' + name + '=') >= 0; } catch (_) { return false; }
  };

  // mirrorUsed is what the origin's OTHER mirror cookies already cost
  // the Cookie header, every store's, which is the number the budget is
  // about: the header is per origin, a record costs more encoded than
  // stored, and a header block past the 8-16 KiB most proxies allow is
  // a 431 the user can only clear by hand.
  const mirrorUsed = (app, seg) => {
    let all = '';
    try { all = document.cookie; } catch (_) { return 0; }
    let used = 0;
    for (const part of all.split('; ')) {
      const eq = part.indexOf('=');
      if (eq < 0) continue;
      const nm = part.slice(0, eq);
      if (nm.indexOf('gofastr.local.') !== 0 || nm.indexOf('gofastr.local.clear.') === 0 || nm === 'gofastr.local.' + seg) continue;
      used += part.length + 2;
    }
    return used;
  };

  // mirror writes the record's cookie, or clears it when text is ''
  // (a zero max-age is the deletion). Over budget the write is refused
  // and the page hears it; the record itself is kept, only its
  // shortcut to first paint is not.
  const mirror = (app, coll, key, text, budget) => {
    const seg = encodeURIComponent(app + '.' + coll + '.' + key);
    if (text) {
      const enc = encodeURIComponent(text);
      // The name, the '=', the encoded value and the '; ' a browser
      // puts between cookies.
      const cost = 'gofastr.local.'.length + seg.length + 1 + enc.length + 2;
      if (budget > 0 && mirrorUsed(app, seg) + cost > budget) {
        try {
          window.dispatchEvent(new CustomEvent('gofastr:local-error', {
            detail: { app: app, collection: coll, key: key, reason: 'mirror', size: cost, max: budget },
          }));
        } catch (_) { /* best-effort */ }
        return;
      }
    }
    try {
      if (text && SECURE) document.cookie = 'gofastr.local.' + encodeURIComponent(app + '.' + coll + '.' + key) + '=' + encodeURIComponent(text) + '; path=/; max-age=31536000; SameSite=Lax; Secure';
      else if (text) document.cookie = 'gofastr.local.' + encodeURIComponent(app + '.' + coll + '.' + key) + '=' + encodeURIComponent(text) + '; path=/; max-age=31536000; SameSite=Lax';
      else if (SECURE) document.cookie = 'gofastr.local.' + encodeURIComponent(app + '.' + coll + '.' + key) + '=; path=/; max-age=0; SameSite=Lax; Secure';
      else document.cookie = 'gofastr.local.' + encodeURIComponent(app + '.' + coll + '.' + key) + '=; path=/; max-age=0; SameSite=Lax';
    } catch (_) { /* best-effort */ }
  };
  // Take the seam over: local-store queued every mirror call made
  // before this file evaluated, and they run now, in order.
  NS._localMirror(mirror);

  // Clear-on-next-load: a full-navigation logout cannot ride an RPC
  // response header, so the Go side plants a bit in a cookie and this
  // module honours it once, then drops the cookie, but only once the
  // clear succeeded, or a logout that could not reach the
  // store would be forgotten.
  //
  // Honoured over every app the manifest declares, not only the ones a
  // marker on this page names: the page a logout redirects to need not
  // carry that app's marker at all.
  const honourClearBits = () => {
    const all = window.__gofastr_local;
    if (!all || typeof all !== 'object') return;
    for (const app of Object.keys(all)) {
      if (RESERVED.test(app) || !cookieNamed('gofastr.local.clear.' + encodeURIComponent(app))) continue;
      const store = openStore(app);
      if (!store) continue;
      store.clear().then((r) => {
        if (!r.ok) return;
        try {
          if (SECURE) document.cookie = 'gofastr.local.clear.' + encodeURIComponent(app) + '=; path=/; max-age=0; SameSite=Lax; Secure';
          else document.cookie = 'gofastr.local.clear.' + encodeURIComponent(app) + '=; path=/; max-age=0; SameSite=Lax';
        } catch (_) { /* best-effort */ }
      });
    }
  };

  // A mirrored collection re-stamps its cookies when this module loads,
  // so a cookie the browser dropped while the record survived comes
  // back.
  const restampMirrors = () => {
    const all = window.__gofastr_local;
    if (!all || typeof all !== 'object') return;
    for (const app of Object.keys(all)) {
      if (RESERVED.test(app)) continue;
      const store = openStore(app);
      if (!store) continue;
      const m = all[app];
      const cs = m && isObject(m.collections) ? m.collections : null;
      const budget = m && m.mirrorMax > 0 ? m.mirrorMax : 0;
      if (!cs) continue;
      for (const name of Object.keys(cs)) {
        if (!cs[name] || !cs[name].mirror || !store.collection(name)) continue;
        const prefix = 'local.' + app + '.' + name + ':';
        const coll = name;
        store.collection(name).count().then(() => NS.local.entries(prefix)).then((er) => {
          if (!er.ok) return;
          for (const e of er.entries) mirror(app, coll, e.key.slice(prefix.length), encode(e.value) || 'null', budget);
        });
      }
    }
  };

  // ─── the seed bridge: a signal filled from a record ─────────────

  // signal name -> { echoing, last }. Per NAME, not per element, and
  // never torn down: the seeded slice is app-global (Seed implies
  // Global in Go), so the listener outlives any one page.
  const seeds = new Map();

  const wireSeed = (el, store) => {
    const spec = el.getAttribute('data-local-seed') || '';
    const colon = spec.indexOf(':');
    const name = el.getAttribute('data-fui-signal');
    if (colon <= 0 || !name || RESERVED.test(name)) return;
    const coll = spec.slice(0, colon);
    const key = spec.slice(colon + 1);
    const validKey = validKeyOf(store);
    const c = store.collection(coll);
    if (!c || !validKey(key)) return;
    let entry = seeds.get(name);
    // One signal, one record. A second element binding the same signal
    // to another record would read one and write back to the other.
    if (entry && entry.spec !== spec) {
      console.warn('[gofastr] local seed: signal', name, 'is already seeded from', entry.spec, '- ignoring', spec);
      return;
    }
    const apply = (value) => {
      if (value === undefined) return;
      entry.last = encode(value);
      entry.echoing = true;
      try { NS.setSignal(name, value); } finally { entry.echoing = false; }
    };
    if (!entry) {
      // Spelled at the sink, and BEFORE the entry is inserted. wireSeed
      // already refused a reserved name above, so this is unreachable,
      // but core-ui/check's proto-key-write lint reads the guard
      // right before the write, on purpose: a guard a function
      // scope away is one the next edit moves out from under, and
      // data-fui-signal="__proto__" re-parents the whole signal store.
      // It sat AFTER seeds.set, so the one path that could reach it
      // left a half-wired entry behind; it no longer can.
      if (name === '__proto__' || name === 'constructor' || name === 'prototype') return;
      entry = { echoing: false, last: null, spec: spec };
      seeds.set(name, entry);
      // Own-property read, computed.js's idiom: a name like
      // "constructor" resolves through the prototype chain otherwise.
      if (!Object.prototype.hasOwnProperty.call(NS._signals, name) || !NS._signals[name]) NS._signals[name] = { value: undefined, listeners: [] };
      NS._signals[name].listeners.push((v) => {
        if (entry.echoing) return; // a restore or a mirror is not a new write
        const text = encode(v);
        if (text === null || text === entry.last) return;
        c.put(key, v).then((r) => { if (r.ok) entry.last = text; });
      });
      c.subscribe((ev) => {
        if (ev.key !== key || ev.source !== 'tab') return;
        c.get(key).then(apply);
      });
    }
    // On EVERY scan, including the one after a client navigation
    // whose DOM came back from the route cache: the record is what
    // this browser holds, so it wins over whatever the cached markup
    // painted. A restore never writes back (echoing). Unless the signal
    // moved while the read was in flight: then the user's value is the
    // newer one, it has already been written back, and the read is
    // stale.
    const current = () => (own(NS._signals, name) && NS._signals[name] ? encode(NS._signals[name].value) : null);
    const before = current();
    c.get(key).then((value) => {
      if (current() !== before) return;
      apply(value);
    });
  };

  // ─── the upload bridge: a request hook on data-fui-rpc ──────────

  // The trigger carries data-local-send="<coll>[:<key>][,<coll>…]" and
  // data-fui-rpc-with="local-bridge", so rpc.js has this module loaded
  // and runs the hook before the fetch. Only what the attribute names
  // travels, as the reserved field __local: {"<coll>": [{k, v}, …]} in
  // a JSON body, the form field __local in a FormData body, and a
  // fresh JSON body when the trigger had none. A GET carries nothing.
  const gather = (store, sendSpec) => {
    // A Map, never a plain object: the collection name arrives from
    // an attribute, and a bracket write keyed by one is how __proto__
    // re-parents a store. Serialised as an object at the end.
    const out = new Map();
    const jobs = [];
    for (const raw of sendSpec.split(',')) {
      const part = raw.trim();
      if (!part) continue;
      const colon = part.indexOf(':');
      const coll = colon < 0 ? part : part.slice(0, colon);
      const key = colon < 0 ? '' : part.slice(colon + 1);
      const c = store.collection(coll);
      if (!c || RESERVED.test(coll)) continue;
      if (!out.has(coll)) out.set(coll, []);
      const rows = out.get(coll);
      if (key) {
        jobs.push(c.get(key).then((v) => { if (v !== undefined) rows.push({ k: key, v: v }); }));
      } else {
        jobs.push(c.list().then((list) => { for (const r of list) rows.push({ k: r.key, v: r.value }); }));
      }
    }
    return Promise.all(jobs).then(() => Object.fromEntries(out));
  };

  // refuse marks the request fatal so rpc.js cancels the dispatch, and
  // tells the page why. The upload bridge FAILS CLOSED: a request whose
  // declared records could not be attached is not the request the
  // markup promised, and a handler that acts on it, saving a draft
  // that is not there or checking a team it never received, is worse
  // than no request at all.
  const refuse = (req, app, reason, size, max) => {
    req.fatal = 'local:' + reason;
    try {
      window.dispatchEvent(new CustomEvent('gofastr:local-error', {
        detail: { app: app, collection: '', key: '', reason: reason, size: size || 0, max: max || 0 },
      }));
    } catch (_) { /* best-effort */ }
  };

  const requestHook = (node, req) => {
    const sendSpec = node.getAttribute('data-local-send') || '';
    const app = node.getAttribute('data-local-store');
    if (!sendSpec || !app || RESERVED.test(app) || req.method === 'GET') return Promise.resolve();
    const store = openStore(app);
    if (!store) {
      // The trigger declares records the manifest never did. Nothing
      // can be gathered and nothing should be sent.
      refuse(req, app, 'store');
      return Promise.resolve();
    }
    // The declaration's own ceiling, carried on the trigger by
    // Upload.Attrs. Without it the browser posted whatever it had and
    // the server answered a bare 413 the page could not see, the one
    // failure a size-capped store exists to report.
    const max = parseInt(node.getAttribute('data-local-max'), 10);
    return gather(store, sendSpec).then((payload) => {
      const text = JSON.stringify(payload);
      const size = bytesOf(text);
      if (max > 0 && size > max) {
        refuse(req, app, 'size', size, max);
        return;
      }
      if (req.isFormData && req.body && typeof req.body.append === 'function') {
        req.body.append('__local', text);
        return;
      }
      let obj = {};
      if (typeof req.body === 'string' && req.body !== '') {
        try { obj = JSON.parse(req.body); } catch (_) { obj = null; }
        if (!isObject(obj)) {
          // A body the bridge cannot open is a body the records cannot
          // ride on, and sending it without them is the silent half-
          // request this bridge exists to prevent.
          refuse(req, app, 'body');
          return;
        }
      }
      obj.__local = payload;
      req.body = JSON.stringify(obj);
      req.headers['Content-Type'] = 'application/json';
    }, (err) => {
      console.warn('[gofastr] local upload: the records could not be read', err);
      refuse(req, app, 'gather');
    });
  };

  // ─── the download bridge: records written from a response ───────

  // X-Gofastr-Local: {"app":"<app>","ops":[{"c":"<coll>","k":"<key>","v":…}
  // | {"c":"<coll>","k":"<key>","d":true} | {"clear":true}]}. Every op
  // goes through the same put/delete as a page write, so the caps hold
  // and subscribers hear it; a refused op raises gofastr:local-error
  // like any other.
  //
  // The download bridge is ADVISORY and the doc says so: the response
  // has already been written when the browser reads the header, so the
  // server believes it wrote whatever the browser then refuses. What
  // the bridge owes the page is to be loud about it: every settlement
  // is read, a refusal is warned and raised, and a store the manifest
  // never declared is not silence.
  const responseHook = (node, r) => {
    let header = '';
    try { header = r.headers.get('X-Gofastr-Local') || ''; } catch (_) { return; }
    if (!header) return;
    let msg = null;
    try { msg = JSON.parse(header); } catch (_) { return; }
    if (!isObject(msg) || typeof msg.app !== 'string' || RESERVED.test(msg.app) || !Array.isArray(msg.ops)) return;
    const store = openStore(msg.app);
    if (!store) {
      console.warn('[gofastr] local download: no store declared for', msg.app, '- the response wrote nothing');
      return;
    }
    const validKey = validKeyOf(store);
    const settle = (coll, key, p) => p.then((r) => {
      if (r && r.ok) return;
      // The op carries the store's own gofastr:local-error too; this
      // one says the op came from a RESPONSE, so the server believes it
      // wrote what the browser refused. Advisory by construction, the
      // response is already sent, which is why it has to be
      // visible.
      const why = (r && r.reason) || 'unavailable';
      console.warn('[gofastr] local download refused:', msg.app, coll, key, why);
      try {
        window.dispatchEvent(new CustomEvent('gofastr:local-error', {
          detail: { app: msg.app, collection: coll, key: key, reason: 'download', cause: why, size: 0, max: 0 },
        }));
      } catch (_) { /* best-effort */ }
    });
    for (const op of msg.ops) {
      if (!isObject(op)) continue;
      if (op.clear === true) { settle('', '', store.clear()); continue; }
      const c = typeof op.c === 'string' ? store.collection(op.c) : null;
      if (!c || !validKey(op.k)) {
        console.warn('[gofastr] local download: no such collection or key', msg.app, op.c, op.k);
        continue;
      }
      if (op.d === true) settle(op.c, op.k, c.delete(op.k));
      else if (own(op, 'v')) settle(op.c, op.k, c.put(op.k, op.v));
    }
  };

  NS._rpcHooks = NS._rpcHooks || { request: [], response: [] };
  NS._rpcHooks.request.push(requestHook);
  NS._rpcHooks.response.push(responseHook);

  // ─── scan ───────────────────────────────────────────────────────

  const wire = (el) => {
    const app = el.getAttribute('data-local-store');
    if (!app || RESERVED.test(app)) return;
    const store = openStore(app);
    if (!store) return;
    if (el.hasAttribute('data-local-seed')) wireSeed(el, store);
  };
  const scan = (root) => {
    const scope = root && root.querySelectorAll ? root : document;
    if (scope.matches && scope.matches(MARKER)) wire(scope);
    scope.querySelectorAll(MARKER).forEach(wire);
  };

  honourClearBits();
  restampMirrors();
  scan(document);
  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;
})();
