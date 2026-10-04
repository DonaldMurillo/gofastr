// GoFastr IndexedDB adapter (localdb)
//
// The browser half of core-ui/localdb, registered there as an
// on-request behaviour (registry.OnRequest): it has no DOM marker and
// is not part of core-ui/runtime. Its callers (an application script,
// framework/localentity's behaviours through Requires) load it with
// __gofastr.loadModule('localdb') and call the API:
//
//   const db = await __gofastr.localdb.open('pokedex')
//   const id = await db.put('members', { name: 'Pikachu' })
//   const rows = await db.list('members', { index: 'by_created', direction: 'prev', limit: 6 })
//   const stop = db.watch('members', (e) => { ... e.origin is 'local' or 'remote' })
//
// The schema comes from the Go declarations, read once from the inert
// #gofastr-localdb manifest. Only declared databases, stores and
// indexes are reachable. The schema is applied additively: a missing
// store or index bumps the IndexedDB version and is created, nothing
// is ever deleted or rebuilt: a store whose key path changed, or an
// index whose definition changed, rejects "schema" (declare it under a
// new name). When another tab upgrades, this tab's connection steps aside
// (versionchange) and the next operation reopens it.
//
// Writes notify watchers in this tab when their transaction commits,
// and every other tab of the origin over a BroadcastChannel carrying
// store names, ops and keys, never record values: a receiving tab
// re-reads what it needs from the database it shares.
//
// Failures are LocalDBError with a stable `code`: unsupported,
// unknown-db, unknown-store, unknown-index, invalid, constraint,
// quota, schema, version, closed, aborted, failed. Nothing here logs;
// record contents never reach console output or an error message.
(() => {
  'use strict';
  window.__gofastr = window.__gofastr || {};
  const NS = window.__gofastr;
  const PREFIX = 'gofastr.';
  const has = (o, k) => o != null && Object.prototype.hasOwnProperty.call(o, k);
  const OPS = ['put', 'add', 'delete', 'clear'];

  const fail = (code, what) => Object.assign(new Error('localdb: ' + code + (what ? ' ' + what : '')),
    { name: 'LocalDBError', code: code });

  // wrap maps a DOMException to a LocalDBError by its name; the
  // browser's message is dropped, so nothing it echoes leaks out.
  const CODES = {
    QuotaExceededError: 'quota', ConstraintError: 'constraint', DataError: 'invalid',
    DataCloneError: 'invalid', InvalidStateError: 'closed', VersionError: 'version', AbortError: 'aborted',
  };
  const wrap = (e) => e && e.name === 'LocalDBError' ? e
    : fail(e && has(CODES, e.name) ? CODES[e.name] : 'failed', e && typeof e.name === 'string' ? e.name : '');

  let manifest;
  const specFor = (name) => {
    if (!manifest) {
      try {
        manifest = JSON.parse(document.getElementById('gofastr-localdb').textContent) || {};
      } catch (_) {
        manifest = {};
      }
    }
    const spec = has(manifest, name) && manifest[name];
    return spec && spec.stores && typeof spec.stores === 'object' ? spec : null;
  };

  const request = (r) => new Promise((resolve, reject) => {
    r.onsuccess = () => resolve(r.result);
    r.onerror = () => reject(wrap(r.error));
  });

  // newID mints a UUIDv7: 48-bit millisecond timestamp, version 7, a
  // 12-bit counter (RFC 9562 method 1), variant 10, the rest from
  // crypto.getRandomValues. The counter keeps keys minted in one
  // millisecond in mint order (seeded random below 2048 so it rarely
  // overflows; on overflow the timestamp steps forward), so an AutoKey
  // store's primary order is this tab's creation order.
  let lastMs = 0;
  let seq = 0;
  const newID = () => {
    const b = crypto.getRandomValues(new Uint8Array(16));
    let ms = Date.now();
    if (ms > lastMs) {
      seq = b[7] | ((b[6] & 7) << 8);
    } else if (++seq > 4095) {
      ms = lastMs + 1;
      seq = 0;
    } else {
      ms = lastMs;
    }
    lastMs = ms;
    for (let i = 5; i >= 0; i--, ms = Math.floor(ms / 256)) b[i] = ms % 256;
    b[6] = 112 | (seq >> 8);
    b[7] = seq & 255;
    b[8] = (b[8] & 63) | 128;
    const h = Array.from(b, (x) => (x < 16 ? '0' : '') + x.toString(16)).join('');
    return h.slice(0, 8) + '-' + h.slice(8, 12) + '-' + h.slice(12, 16) + '-' + h.slice(16, 20) + '-' + h.slice(20);
  };

  // sameIndex compares a live index to its declaration; ixPath is the
  // declaration's key path in IndexedDB's shape (a string for one
  // property, an array for a compound index).
  const ixPath = (w) => (w.keyPath.length === 1 ? w.keyPath[0] : w.keyPath);
  const sameIndex = (live, w) => JSON.stringify(live.keyPath) === JSON.stringify(ixPath(w)) &&
    live.unique === !!w.unique && live.multiEntry === !!w.multiEntry;

  // drift reports whether the open database lacks a declared store or
  // index. A store whose primary key path changed, or an index whose
  // definition changed, is a schema error naming it: rebuilding it in
  // place would undo the other deploy's version whenever tabs on two
  // deploys meet, each upgrade closing the other's connection. Declare
  // the new shape under a new name instead.
  const drift = (db, spec) => {
    const names = Object.keys(spec.stores);
    if (names.some((s) => !db.objectStoreNames.contains(s))) return true;
    if (!names.length) return false;
    const tx = db.transaction(names, 'readonly');
    return names.some((s) => {
      const store = tx.objectStore(s);
      const want = spec.stores[s];
      if (store.keyPath !== want.keyPath) throw fail('schema', s);
      return Object.keys(want.indexes || {}).some((ix) => {
        if (!store.indexNames.contains(ix)) return true;
        if (!sameIndex(store.index(ix), want.indexes[ix])) throw fail('schema', s + '.' + ix);
        return false;
      });
    });
  };

  const upgrade = (db, tx, spec) => {
    for (const s of Object.keys(spec.stores)) {
      const want = spec.stores[s];
      const store = db.objectStoreNames.contains(s) ? tx.objectStore(s) : db.createObjectStore(s, { keyPath: want.keyPath });
      for (const ix of Object.keys(want.indexes || {})) {
        const w = want.indexes[ix];
        if (!store.indexNames.contains(ix)) {
          store.createIndex(ix, ixPath(w), { unique: !!w.unique, multiEntry: !!w.multiEntry });
        }
      }
    }
  };

  const emitDoc = (type, name) => {
    try {
      document.dispatchEvent(new CustomEvent('gofastr:localdb', { detail: { type: type, db: name } }));
    } catch (_) { /* a listener's failure is its own */ }
  };

  // openRaw opens the database at version (undefined = current),
  // applying the declaration when the open upgrades. A tab that holds
  // the old version and is not ours to close keeps the request
  // pending; "blocked" is announced so a page can say so.
  const openRaw = (name, spec, version) => new Promise((resolve, reject) => {
    const r = indexedDB.open(PREFIX + name, version);
    r.onupgradeneeded = () => upgrade(r.result, r.transaction, spec);
    r.onblocked = () => emitDoc('blocked', name);
    r.onsuccess = () => resolve(r.result);
    r.onerror = () => reject(wrap(r.error));
  });

  const connect = async (name, spec) => {
    for (let attempt = 0; attempt < 4; attempt++) {
      const db = await openRaw(name, spec);
      try {
        if (!drift(db, spec)) return db;
      } catch (e) {
        db.close();
        throw wrap(e);
      }
      db.close();
      try {
        const up = await openRaw(name, spec, db.version + 1);
        if (!drift(up, spec)) return up;
        up.close();
      } catch (e) {
        // Another tab won the race to the same version: read the new
        // current version and try again.
        if (e.code !== 'version') throw e;
      }
    }
    throw fail('schema', name);
  };

  // A handle is one declared database, held for the page's life: its
  // connection reopens itself after another tab's upgrade closed it
  // (versionchange) or the browser dropped it.
  const handles = new Map();

  class Handle {
    constructor(name, spec) {
      this.name = name;
      this._sp = spec;
      this._db = null;
      this._op = null;
      this._w = new Map();
      this._ch = null;
    }

    _ready() {
      if (this._db) return Promise.resolve(this._db);
      return this._op || (this._op = connect(this.name, this._sp).then((db) => {
        this._op = null;
        this._db = db;
        db.onversionchange = db.onclose = (e) => {
          if (this._db === db) this._db = null;
          db.close();
          emitDoc(e.type, this.name);
        };
        return db;
      }, (e) => {
        this._op = null;
        throw wrap(e);
      }));
    }

    _store(s) {
      if (typeof s !== 'string' || !has(this._sp.stores, s)) throw fail('unknown-store', String(s));
      return this._sp.stores[s];
    }

    // _run executes body(api) inside one transaction and resolves with
    // its return value once the transaction COMMITS; watchers hear the
    // change only then. A connection closed under us reopens once.
    _run(stores, mode, body, retried) {
      const names = [].concat(stores);
      try {
        names.forEach((s) => this._store(s));
      } catch (e) {
        return Promise.reject(e);
      }
      return this._ready().then((db) => new Promise((resolve, reject) => {
        let tx;
        try {
          tx = db.transaction(names, mode);
        } catch (e) {
          if (retried || wrap(e).code !== 'closed') return reject(wrap(e));
          if (this._db === db) this._db = null;
          return resolve(this._run(stores, mode, body, true));
        }
        const changes = [];
        let result;
        let failure;
        tx.oncomplete = () => {
          resolve(result);
          if (changes.length) this._emit(changes, 'local');
        };
        tx.onabort = () => reject(failure || wrap(tx.error || { name: 'AbortError' }));
        Promise.resolve()
          .then(() => body(this._api(tx, changes)))
          .then((r) => { result = r; }, (e) => {
            failure = wrap(e);
            try { tx.abort(); } catch (_) {}
          });
      }));
    }

    _api(tx, changes) {
      const source = (s, q) => {
        const decl = this._store(s).indexes || {};
        const store = tx.objectStore(s);
        if (!q || q.index === undefined) return store;
        if (typeof q.index !== 'string' || !has(decl, q.index)) throw fail('unknown-index', String(q.index));
        return store.index(q.index);
      };
      const write = (op, s, v) => {
        const spec = this._store(s);
        if (!v || typeof v !== 'object' || Array.isArray(v)) throw fail('invalid', 'record');
        const k = v[spec.keyPath];
        if (spec.autoKey && (k === undefined || k === null || k === '')) {
          // Spread copies with define semantics, so an own
          // "__proto__" key on the caller's object stays data. The key
          // path is Go-validated: never a reserved name.
          v = { ...v };
          v[spec.keyPath] = newID();
        }
        const store = tx.objectStore(s);
        return request(op === 'add' ? store.add(v) : store.put(v)).then((key) => {
          changes.push({ store: s, op: op, key: key });
          return key;
        });
      };
      const drop = (op, s, key) => request(op === 'clear' ? source(s).clear() : source(s).delete(key))
        .then(() => { changes.push({ store: s, op: op, key: key === undefined ? null : key }); });
      return {
        get: (s, key) => request(source(s).get(key)),
        put: (s, v) => write('put', s, v),
        add: (s, v) => write('add', s, v),
        delete: (s, key) => drop('delete', s, key),
        clear: (s) => drop('clear', s),
        count: (s, q) => request(source(s, q).count(range(q))),
        // list reads records in key or index order. q: index, only |
        // lower/upper (+lowerOpen/upperOpen), direction, offset, limit,
        // keys (resolve primary keys instead of records).
        list: (s, q) => new Promise((resolve, reject) => {
          q = q || {};
          const dir = q.direction || 'next';
          const limit = q.limit || 0;
          let offset = q.offset || 0;
          if (!['next', 'prev', 'nextunique', 'prevunique'].includes(dir) ||
              !(Number.isInteger(limit) && limit >= 0 && Number.isInteger(offset) && offset >= 0)) {
            throw fail('invalid', 'query');
          }
          const src = source(s, q);
          const r = q.keys ? src.openKeyCursor(range(q), dir) : src.openCursor(range(q), dir);
          const out = [];
          r.onsuccess = () => {
            const c = r.result;
            if (!c) return resolve(out);
            if (offset) {
              const n = offset;
              offset = 0;
              return c.advance(n);
            }
            out.push(q.keys ? c.primaryKey : c.value);
            if (limit && out.length >= limit) return resolve(out);
            c.continue();
          };
          r.onerror = () => reject(wrap(r.error));
        }),
      };
    }

    _emit(changes, origin) {
      const byStore = new Map();
      for (const c of changes) {
        if (!byStore.has(c.store)) byStore.set(c.store, []);
        byStore.get(c.store).push({ op: c.op, key: c.key });
      }
      for (const [store, list] of byStore) {
        const event = { db: this.name, store: store, origin: origin, changes: list };
        for (const fn of Array.from(this._w.get(store) || [])) {
          try { fn(event); } catch (_) { /* one watcher never breaks the others */ }
        }
      }
      // A writing tab announces even when it watches nothing itself:
      // another tab's list must hear a save from a form-only page.
      if (origin === 'local') {
        this._listen();
        if (this._ch) {
          try { this._ch.postMessage({ v: 1, changes: changes }); } catch (_) {}
        }
      }
    }

    // _listen joins the origin-wide channel for this database. Any
    // same-origin script can post on a BroadcastChannel, so a message
    // naming an undeclared store or an unknown op is dropped whole.
    _listen() {
      if (this._ch || typeof BroadcastChannel !== 'function') return;
      this._ch = new BroadcastChannel(PREFIX + 'localdb.' + this.name);
      this._ch.onmessage = (e) => {
        const m = e.data;
        if (m && m.v === 1 && Array.isArray(m.changes) && m.changes.length <= 1000 &&
            m.changes.every((c) => c && has(this._sp.stores, c.store) && OPS.includes(c.op))) {
          this._emit(m.changes.map((c) => ({ store: c.store, op: c.op, key: c.key })), 'remote');
        }
      };
    }

    // tx runs fn(t) in one transaction over stores, t carrying the
    // methods above. Await only t's methods inside fn: IndexedDB
    // commits a transaction once it has no pending request, so
    // awaiting a fetch inside fn ends it.
    tx(stores, mode, fn) {
      if ((mode !== 'readonly' && mode !== 'readwrite') || typeof fn !== 'function') {
        return Promise.reject(fail('invalid', 'tx'));
      }
      return this._run(stores, mode, fn);
    }

    // watch calls fn({db, store, origin, changes}) after every
    // committed write to store, from this tab (origin "local") or any
    // other tab of the origin ("remote"). Returns the unsubscribe.
    watch(s, fn) {
      this._store(s);
      if (typeof fn !== 'function') throw fail('invalid', 'watch');
      this._listen();
      if (!this._w.has(s)) this._w.set(s, new Set());
      this._w.get(s).add(fn);
      return () => this._w.get(s).delete(fn);
    }
  }

  // The single-operation methods, each its own transaction: get(s,
  // key), put(s, record), add(s, record), delete(s, key), clear(s),
  // count(s, q), list(s, q).
  for (const m of ['get', 'put', 'add', 'delete', 'clear', 'count', 'list']) {
    const mode = OPS.includes(m) ? 'readwrite' : 'readonly';
    Handle.prototype[m] = function (s, a) { return this._run(s, mode, (t) => t[m](s, a)); };
  }

  // range builds the IDBKeyRange a query names: only, or lower/upper
  // with optional open ends. No bound means the whole store or index.
  function range(q) {
    q = q || {};
    try {
      const lo = q.lower !== undefined;
      const hi = q.upper !== undefined;
      return q.only !== undefined ? IDBKeyRange.only(q.only)
        : lo && hi ? IDBKeyRange.bound(q.lower, q.upper, !!q.lowerOpen, !!q.upperOpen)
          : lo ? IDBKeyRange.lowerBound(q.lower, !!q.lowerOpen)
            : hi ? IDBKeyRange.upperBound(q.upper, !!q.upperOpen) : null;
    } catch (e) {
      throw wrap(e);
    }
  }

  // storage calls a navigator.storage method, answering fallback where
  // the browser lacks it or refuses.
  const storage = (method, fallback) => {
    try {
      return navigator.storage[method]().catch(() => fallback);
    } catch (_) {
      return Promise.resolve(fallback);
    }
  };

  NS.localdb = {
    newID: newID,

    // open resolves the handle for a declared database, opening (and
    // if needed upgrading) its connection first.
    open(name) {
      const spec = typeof name === 'string' && specFor(name);
      if (!spec) return Promise.reject(fail('unknown-db', String(name)));
      if (typeof indexedDB === 'undefined' || !indexedDB) return Promise.reject(fail('unsupported'));
      if (!handles.has(name)) handles.set(name, new Handle(name, spec));
      const h = handles.get(name);
      return h._ready().then(() => h);
    },

    // persist asks the browser to exempt this origin's storage from
    // eviction. Browsers decide (Chromium by engagement, Firefox by
    // prompt, Safari by home-screen install), so false is an answer,
    // not an error.
    persist: () => storage('persist', false),
    persisted: () => storage('persisted', false),
    // estimate resolves {usage, quota} in bytes, or null.
    estimate: () => storage('estimate', null),
  };

  (NS.loadedModules = NS.loadedModules || {}).localdb = true;
})();
