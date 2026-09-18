// local-store: the runtime half of framework/local, a local-first
// state API for GoFastr apps: declared in Go, persisted in the
// browser, with a documented contract.
//
// This module is the OPINION; the storage is not here. Records live in
// the kernel's `local` primitive (core-ui/runtime/src/local.js:
// IndexedDB, a tiny-value localStorage fallback, one namespace, every
// call best-effort), which this file's registration Requires. What
// this file adds is what a store needs and a key-value primitive must
// not carry: named collections with a schema version and migrations,
// a size cap per record and per collection, a key field, an ordered
// list, and a change feed that includes this tab's own writes. The
// bridges to Go screens are local-bridge.js, which Requires this one:
// a signal seeded from a record, a mirror cookie, an upload declared
// per request, and records written from a response. The version steps
// are local-migrate.js, loaded at idle. A page that only reads records
// pays for neither.
//
// The declaration arrives from Go, not from the DOM: Store.Script
// serves `window.__gofastr_local[<app>]` on the host's extra-script
// rail (uihost.WithExtraScripts), the same rail computed reducers use,
// so a marker planted in an island response cannot redefine a
// collection's caps or migrations. A marker names a collection; the
// manifest says what it is.
//
// Keys. A record is the primitive entry 'local.<app>.<collection>:<key>'
// and a collection's version is the entry 'local.<app>.<collection>'
// (no colon, so it never collides with a record: app and collection
// names are [a-z0-9-] by the Go validator). The primitive stores every
// entry under 'gofastr.state.' plus the component encoding, so nothing
// here can name storage outside that namespace, and one prefix
// enumerates one collection.
//
// Contract, the same promises the primitive keeps: every call settles,
// nothing throws, a refusal says why ({ok:false, reason}), and the page
// hears gofastr:local-error for the refusals an app should tell the
// user about (key, size, full, quota, encode, unavailable, migration,
// version). A collection whose migration did not complete is GATED:
// every method refuses with reason 'migration' rather than answering
// from records on a schema this build cannot read.
// Truth still lives on the server. This is not offline sync: no queue,
// no conflict resolution, no reconciliation.
(() => {
  'use strict';
  const NAME = 'local-store';
  const NS = window.__gofastr = window.__gofastr || {};
  // The kernel fetches a module once per page, but anything that
  // evaluates this file twice must bind nothing twice.
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  // Flag first: the loader resolves on registration, and a file that
  // failed halfway with its flag unset would be re-executed by the
  // next loadModule and install everything twice.
  (NS.loadedModules = NS.loadedModules || {})[NAME] = true;

  const MARKER = '[data-local-store]';
  const RESERVED = /^(__proto__|constructor|prototype)$/;

  // app -> store API. A Map: the app id arrives from a DOM attribute on
  // the seed and send markers, and a bracket write keyed by one is how
  // __proto__ re-parents a registry.
  const stores = new Map();

  const emit = (type, detail) => {
    try { window.dispatchEvent(new CustomEvent(type, { detail })); } catch (_) { /* best-effort */ }
  };
  const fail = (app, collection, key, reason, size, max) => {
    emit('gofastr:local-error', { app, collection, key, reason, size: size || 0, max: max || 0 });
    return { ok: false, reason };
  };
  const encode = (value) => {
    let text;
    try { text = JSON.stringify(value); } catch (_) { return null; }
    return typeof text === 'string' ? text : null;
  };
  const bytesOf = (text) => {
    try { return new TextEncoder().encode(text).length; } catch (_) { return text.length; }
  };
  // KeyMaxLen in local.go, in the same unit: BYTES. String.length is
  // UTF-16 code units, so 200 accented characters passed here and were
  // refused by the Go validator: a key the browser wrote happily and
  // the server would not read, on a record that then only exists on one
  // side. The two validators mirror each other or they do not.
  const validKey = (key) => typeof key === 'string' && key !== '' && bytesOf(key) <= 256 && !RESERVED.test(key);
  const isObject = (v) => v !== null && typeof v === 'object' && !Array.isArray(v);
  const own = (o, k) => Object.prototype.hasOwnProperty.call(o, k);

  // The manifest the Go declaration served. Read at open time, never
  // cached before the rail ran: extra scripts are parser-blocking and
  // the kernel's module scan runs after them, but a module reached
  // through loadModule from a script that itself sits on the rail
  // could evaluate first.
  const manifestFor = (app) => {
    const all = window.__gofastr_local;
    if (!all || typeof all !== 'object' || !own(all, app)) return null;
    const m = all[app];
    return m && typeof m === 'object' && isObject(m.collections) ? m : null;
  };

  // ─── the mirror seam ────────────────────────────────────────────

  // The mirror is a BRIDGE to a Go screen, a cookie a render reads at
  // first paint, not part of keeping records, so it lives with the
  // other three in local-bridge.js and this file holds one seam to it.
  // Calls made before the bridge installs itself are queued, never
  // dropped: a cookie is read on the NEXT request, so the only thing
  // that matters is that it is written, not when.
  //
  // Splitting here rather than at a byte count: local-store is the
  // store, local-bridge is every way the store reaches a Go handler.
  const seam = { fn: null, queue: [] };
  const mirror = (app, coll, key, text, budget) => {
    if (seam.fn) seam.fn(app, coll, key, text, budget);
    else seam.queue.push([app, coll, key, text, budget]);
  };
  // installMirror is how local-bridge takes the seam over.
  NS._localMirror = (fn) => {
    seam.fn = fn;
    for (const a of seam.queue.splice(0)) fn.apply(null, a);
  };
  const cookieNamed = (name) => {
    try { return ('; ' + document.cookie).indexOf('; ' + name + '=') >= 0; } catch (_) { return false; }
  };
  // The bridges are wanted when a declaration mirrors a collection or a
  // logout is pending; a store that keeps its records to itself never
  // pays for them.
  const wantsBridge = () => {
    const all = window.__gofastr_local;
    for (const app of Object.keys(all && typeof all === 'object' ? all : {})) {
      if (cookieNamed('gofastr.local.clear.' + encodeURIComponent(app))) return true;
      const cs = (all[app] || {}).collections;
      for (const n of Object.keys(isObject(cs) ? cs : {})) if (cs[n] && cs[n].mirror) return true;
    }
    return false;
  };

  // ─── one store ──────────────────────────────────────────────────

  const openStore = (app) => {
    if (stores.has(app)) return stores.get(app);
    const manifest = manifestFor(app);
    if (!manifest) return null;
    const P = NS.local;
    const budget = manifest.mirrorMax > 0 ? manifest.mirrorMax : 0;
    const prefixOf = (coll) => 'local.' + app + '.' + coll + ':';
    const metaKey = (coll) => 'local.' + app + '.' + coll;
    const recordKey = (coll, key) => prefixOf(coll) + key;
    // collection -> Set<fn>, this tab's subscribers.
    const subs = new Map();
    // collection -> Promise, the once-per-page readiness (migration).
    // Dropped when another tab writes the collection's version entry:
    // a tab left open across a deploy would otherwise keep answering
    // from the readiness it cached at load and write old-schema records
    // into a collection a newer tab has already migrated and stamped,
    // which nothing would ever migrate again.
    const ready = new Map();
    const collections = Object.create(null);

    const notify = (coll, key, source) => {
      const fns = subs.get(coll);
      if (!fns) return;
      for (const fn of fns) {
        try { fn({ app, collection: coll, key, source }); } catch (_) { /* a throwing subscriber is its own problem */ }
      }
    };

    // Another tab's write of anything under the store: route it to the
    // collection's subscribers with source 'tab'. One watcher per store.
    P.watch('local.' + app + '.', (k) => {
      const rest = k.slice(('local.' + app + '.').length);
      const colon = rest.indexOf(':');
      if (colon < 0) { ready.delete(rest); return; } // a version entry: re-check on the next call
      notify(rest.slice(0, colon), rest.slice(colon + 1), 'tab');
    });

    const specOf = (coll) => (own(manifest.collections, coll) && isObject(manifest.collections[coll]) ? manifest.collections[coll] : null);

    // migrate brings one collection to its declared version, once per
    // page, under the Web Locks API when the browser has it so two
    // tabs do not race the same rewrite. A collection nobody wrote yet
    // is stamped at the declared version and nothing runs. A func step
    // whose function is not registered leaves the records and the
    // version untouched and raises gofastr:local-error with reason
    // 'migration': the data is worth more than the schema.
    //
    // The rewrite itself is local-migrate.js, a third module this one
    // asks for by name. Schema evolution is not the same job as keeping
    // records: different failure mode, different blast radius, and a
    // collection that declares no version step never runs a line of it.
    // So it is a module of its own, registered LoadIdle so it is
    // never on the critical path yet always there before the first read
    // of a versioned collection.
    const migrate = (coll, spec) => {
      const target = spec.v > 0 ? spec.v : 1;
      const run = () => P.get(metaKey(coll)).then((meta) => {
        const have = meta && typeof meta.v === 'number' ? meta.v : 0;
        if (have === target) return true;
        // A stored version ABOVE the declared one is a rollback: this
        // build's steps cannot undo what a newer build wrote, and
        // reading those records as if they were this schema is how a
        // deploy that gets rolled back corrupts the browsers that
        // already moved on. Refuse the collection and say so.
        if (have > target) {
          fail(app, coll, '', 'version', have, target);
          return false;
        }
        const from = have === 0 ? 1 : have;
        const steps = [];
        for (const m of spec.migrations || []) {
          if (m && m.v > from && m.v <= target) steps.push(m);
        }
        if (steps.length === 0) {
          // Nothing to rewrite: stamp the version. A refused stamp is a
          // failed migration like any other, or the steps would run
          // again over records that already moved.
          return P.set(metaKey(coll), { v: target }).then((r) => r.ok || fail(app, coll, '', 'migration').ok);
        }
        steps.sort((a, b) => a.v - b.v);
        return NS.loadModule('local-migrate').then(() => NS._localMigrate({
          P,
          app,
          coll,
          prefix: prefixOf(coll),
          meta: metaKey(coll),
          steps,
          from,
          to: target,
          mirror: spec.mirror ? (key, text) => mirror(app, coll, key, text, budget) : null,
          emit,
          fail,
        }), () => fail(app, coll, '', 'migration').ok);
      }).catch(() => {
        fail(app, coll, '', 'migration');
        return false;
      });
      const locks = window.navigator && window.navigator.locks;
      if (locks && typeof locks.request === 'function') {
        try { return locks.request('gofastr.local.' + app + '.' + coll, run); } catch (_) { return run(); }
      }
      return run();
    };
    const whenReady = (coll) => {
      const spec = specOf(coll);
      if (!spec) return Promise.resolve(false);
      if (!ready.has(coll)) ready.set(coll, migrate(coll, spec));
      return ready.get(coll);
    };
    // gate is whenReady as a refusal: null when the collection is
    // usable, {ok:false, reason:'migration'} when it is not. EVERY
    // method goes through it. A collection whose migration failed holds
    // records on a schema this build cannot read, and serving them
    // anyway, which is what discarding whenReady's answer did, turns
    // one failed rewrite into corrupt data everywhere the records go.
    const gate = (coll) => whenReady(coll).then((ok) => (ok ? null : fail(app, coll, '', 'migration')));

    const collectionAPI = (coll) => {
      const spec = specOf(coll);
      // The caps come from the declaration, always: Define resolves
      // every default and the manifest always carries the three
      // numbers. Mirroring the Go defaults here as a fallback meant a
      // manifest that lost a cap silently got a GENEROUS one; an entry
      // without them is refused instead, which is the direction a cap
      // should fail in.
      const maxRecord = spec.maxRecord;
      const maxRecords = spec.maxRecords;
      const maxBytes = spec.maxBytes;

      // Every write to this collection queues behind the one before
      // it. Cap enforcement is read-then-write across two transactions,
      // so two puts racing each other both read the same total, both
      // decide they fit and both land past the cap; and a put racing a
      // clear or a delete commits after the enumeration that never saw
      // it, so the record survives a logout that reported success. One
      // chain per collection covers all three; reads stay parallel, and
      // two collections never wait on one another.
      // Across tabs the chain is a Web Lock on the collection, where the
      // browser has one (Safari lacks it: there the cap holds per tab).
      let chain = Promise.resolve();
      const locks = window.navigator && window.navigator.locks;
      const locked = (fn) => {
        if (!locks || typeof locks.request !== 'function') return fn();
        try { return locks.request('gofastr.local.' + app + '.' + coll + '.write', fn); } catch (_) { return fn(); }
      };
      const serial = (fn) => {
        const mine = chain.then(() => locked(fn), () => locked(fn));
        chain = mine.then(() => {}, () => {});
        return mine;
      };

      const api = {
        name: coll,
        // get resolves undefined on a gated collection, like a missing
        // key; the refusal itself rides gofastr:local-error.
        get(key) {
          if (!validKey(key)) return Promise.resolve(undefined);
          return gate(coll).then((no) => (no ? undefined : P.get(recordKey(coll, key))));
        },
        // put(key, value), or put(value) when the collection declares a
        // key field: the key is then value[keyField], a non-empty string.
        // Resolves {ok, reason}: 'key', 'encode', 'size' (over the
        // per-record cap), 'full' (over the collection's record or byte
        // cap), 'quota', 'unavailable'.
        put(key, value) {
          if (arguments.length === 1) {
            value = key;
            key = spec.key && isObject(value) ? value[spec.key] : undefined;
          }
          if (!validKey(key)) return Promise.resolve(fail(app, coll, String(key), 'key'));
          const text = encode(value);
          if (text === null) return Promise.resolve(fail(app, coll, key, 'encode'));
          const size = bytesOf(text);
          if (size > maxRecord) return Promise.resolve(fail(app, coll, key, 'size', size, maxRecord));
          return gate(coll).then((no) => no || serial(() => P.entries(prefixOf(coll)).then((er) => {
            if (!er.ok) return fail(app, coll, key, er.reason || 'unavailable');
            const entries = er.entries;
            let total = size;
            let n = 1;
            const mine = recordKey(coll, key);
            for (const e of entries) {
              if (e.key === mine) continue;
              total += e.size;
              n++;
            }
            if (n > maxRecords) return fail(app, coll, key, 'full', n, maxRecords);
            if (total > maxBytes) return fail(app, coll, key, 'full', total, maxBytes);
            return P.set(mine, value).then((r) => {
              if (!r.ok) return fail(app, coll, key, r.reason, size, maxRecord);
              if (spec.mirror) mirror(app, coll, key, text, budget);
              notify(coll, key, 'local');
              return { ok: true, reason: '' };
            });
          })));
        },
        delete(key) {
          if (!validKey(key)) return Promise.resolve(fail(app, coll, String(key), 'key'));
          return gate(coll).then((no) => no || serial(() => P.remove(recordKey(coll, key)).then((r) => {
            if (!r.ok) return fail(app, coll, key, r.reason);
            if (spec.mirror) mirror(app, coll, key, '', budget);
            notify(coll, key, 'local');
            return { ok: true, reason: '' };
          })));
        },
        // list resolves [{key, value}] sorted by key, or by opts.orderBy
        // (a top-level field; opts.desc reverses). The array is the
        // page's to filter and slice: one collection this browser owns,
        // and nothing re-implemented that the server does for server
        // data.
        list(opts) {
          const o = opts && typeof opts === 'object' ? opts : {};
          // An enumeration that failed is not an empty collection: the
          // page hears it, the way clear() and put() already say so.
          return gate(coll).then((no) => (no ? [] : P.entries(prefixOf(coll)).then((er) => {
            if (!er.ok) { fail(app, coll, '', er.reason || 'unavailable'); return []; }
            const out = [];
            for (const e of er.entries) out.push({ key: e.key.slice(prefixOf(coll).length), value: e.value });
            if (typeof o.orderBy === 'string' && o.orderBy !== '') {
              const pick = (r) => (isObject(r.value) ? r.value[o.orderBy] : undefined);
              // Entries arrive sorted by key, and the sort is stable, so
              // equal fields keep key order; undefined sorts last.
              out.sort((a, b) => {
                const x = pick(a);
                const y = pick(b);
                if (x === y) return 0;
                if (x === undefined) return 1;
                if (y === undefined) return -1;
                return x < y ? -1 : 1;
              });
            }
            if (o.desc) out.reverse();
            return out;
          })));
        },
        count() {
          return gate(coll).then((no) => (no ? 0 : P.keys(prefixOf(coll)).then((r) => {
            if (!r.ok) { fail(app, coll, '', r.reason || 'unavailable'); return 0; }
            return r.keys.length;
          })));
        },
        // subscribe calls fn({app, collection, key, source}) after every
        // write to this collection: source 'local' for this tab's own
        // put/delete (including one a response wrote), 'tab' for another
        // tab's. Returns the unsubscribe function.
        subscribe(fn) {
          if (typeof fn !== 'function') return () => {};
          let fns = subs.get(coll);
          if (!fns) { fns = new Set(); subs.set(coll, fns); }
          fns.add(fn);
          return () => { fns.delete(fn); };
        },
        // clear removes every record of the collection, and says so
        // only when it did. An enumeration that aborted used to look
        // the same as an empty collection, so the logout path reported
        // {ok:true} over records, and mirror cookies, that all
        // survived. A removal that failed is the same lie one record
        // deep, so both are checked.
        clear() {
          return gate(coll).then((no) => no || serial(() => P.keys(prefixOf(coll)).then((r) => {
            if (!r.ok) return fail(app, coll, '', r.reason || 'unavailable');
            const ks = r.keys;
            return Promise.all(ks.map((k) => P.remove(k))).then((rs) => {
              let bad = '';
              for (let i = 0; i < ks.length; i++) {
                if (!rs[i] || !rs[i].ok) { bad = (rs[i] && rs[i].reason) || 'unavailable'; continue; }
                const key = ks[i].slice(prefixOf(coll).length);
                if (spec.mirror) mirror(app, coll, key, '', budget);
                notify(coll, key, 'local');
              }
              if (bad) return fail(app, coll, '', bad);
              return { ok: true, reason: '' };
            });
          })));
        },
      };
      return api;
    };

    for (const name of Object.keys(manifest.collections)) {
      if (RESERVED.test(name) || !specOf(name)) continue;
      collections[name] = collectionAPI(name);
    }

    const store = {
      app,
      collections,
      // The one validator local-bridge needs, on the object it already
      // fetches: a byte length and a reserved-name set that must agree
      // with the Go side. It was an undocumented global bag, __gofastr
      // ._localHelpers, a second public object anything on the origin
      // could replace, and replacing validKey is how a key escapes the
      // namespace.
      helpers: { validKey },
      collection(name) {
        return typeof name === 'string' && own(collections, name) ? collections[name] : null;
      },
      // clear drops every record of every collection: logout. It
      // reports the first collection that could not be cleared, because
      // "the previous user's records are gone" is the only thing a
      // logout is for.
      clear() {
        return Promise.all(Object.keys(collections).map((c) => collections[c].clear())).then((rs) => {
          for (const r of rs) if (!r || !r.ok) return { ok: false, reason: (r && r.reason) || 'unavailable' };
          return { ok: true, reason: '' };
        });
      },
    };
    stores.set(app, store);

    return store;
  };

  // ─── scan ───────────────────────────────────────────────────────

  // Opening a store on its marker is all the core module does with
  // the DOM: it honours the clear bit and re-stamps mirror cookies.
  // The seed and send markers are the bridge module's (local-bridge.js,
  // Requires this one).
  const wire = (el) => {
    const app = el.getAttribute('data-local-store');
    if (app && !RESERVED.test(app)) openStore(app);
  };
  const scan = (root) => {
    const scope = root && root.querySelectorAll ? root : document;
    if (scope.matches && scope.matches(MARKER)) wire(scope);
    scope.querySelectorAll(MARKER).forEach(wire);
  };

  // localStore(app) is the API an application script reaches after
  // __gofastr.loadModule('local-store'): the store for that app id, or
  // null when no manifest declared it.
  NS.localStore = (app) => {
    if (typeof app !== 'string' || RESERVED.test(app)) return null;
    const s = openStore(app);
    if (!s) console.warn('[gofastr] local: no manifest for app', app, '- is /__gofastr/local/' + app + '.js served?');
    return s;
  };

  if (wantsBridge()) NS.loadModule('local-bridge').catch(() => {});
  scan(document);
  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;
})();
