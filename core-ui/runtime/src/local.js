// local.js: the browser's own store, as one primitive.
//
// The runtime had five hard-coded Web-storage keys and no way for
// anything else to keep a value in the browser: a sidebar boolean, a
// banner bit, a colour scheme, a form draft, a scroll map. Each was
// written where it was needed, each re-derived its own namespace, and
// an app that wanted to remember anything of its own had to leave the
// framework and write a document script. This is that capability with
// one owner.
//
// Engine: IndexedDB, because a saved value is not a preference. It is
// asynchronous, it is not capped at the ~5 MB localStorage shares
// across a whole origin, and it does not block the main thread on a
// large read. localStorage is the FALLBACK, for tiny values only
// (LS_MAX_BYTES), in a browser that has no usable IndexedDB. No
// external dependency: both are browser APIs.
//
// The contract is BEST-EFFORT and that is the design, not a caveat.
// Private mode, a blocked origin, a full quota, a hand-cleared store
// and a browser that refuses to open a database are all normal. Every
// call settles, none throws, and set() says why it refused instead of
// pretending. Never keep something here whose loss is a bug: the
// server is still where truth lives.
//
// Keys are namespaced: the stored key is always the literal
// 'gofastr.state.' plus the component-encoded application key, spelled
// at every sink, in BOTH engines. An application key can therefore
// never name another feature's storage, and core-ui/check's
// storage-key lint holds the spelling.
//
// It has no marker, so nothing loads it on its own: a module that
// needs it declares registry.Requires('local') and the loader has it
// registered before the dependent evaluates, and an application can
// reach it with __gofastr.loadModule('local'). window.__gofastr.local
// is the public API: get, set, remove, keys, entries, subscribe,
// watch, available.
//
// Every call settles and none throws. set, remove, keys and entries
// settle to { ok, reason, … }: an enumeration that failed is not an
// empty store, and a caller that deletes what it enumerated has to be
// able to tell the two apart.
//
// keys and entries take an optional PREFIX and enumerate only the
// application keys under it, as one IndexedDB key range rather than a
// scan of the whole store: a layer above that groups its records under
// a common prefix (a collection) can list one collection without
// reading every other. The component encoding is per character, so the
// encoded prefix is a prefix of every encoded key beneath it, and the
// range is [PREFIX + enc(prefix), PREFIX + enc(prefix) + '\uffff'].
//
// Schemas, migrations, an upload channel, a sync lane: opinionated
// storage of that kind is a layer ABOVE this one and does not belong
// here.
(() => {
  'use strict';
  const NS = window.__gofastr = window.__gofastr || {};

  // The one namespace, shared with every other gofastr.* storage key on
  // the origin and spelled at each sink.
  const PREFIX = 'gofastr.state.';
  const DB_NAME = 'gofastr.state';
  const DB_STORE = 'kv';
  // The localStorage fallback is for tiny values only: it is synchronous
  // and its quota is the whole origin's.
  const LS_MAX_BYTES = 8192;

  // key to Set<fn>. Maps, never plain objects: an application key is
  // often attribute-borne, and a bracket write keyed by one is how
  // __proto__ re-parents a store.
  const subs = new Map();
  // Prefix watchers: [{ prefix, fn }]. A watcher hears every key under
  // its prefix that another tab changed, key only; it re-reads what it
  // needs, the same rule subscribe keeps.
  const watchers = [];

  let dbPromise = null;

  // openDB resolves the database, or null when this browser will not
  // give us one (no IndexedDB, private mode, a blocked upgrade). Cached:
  // the answer does not change within a document.
  const openDB = () => {
    if (dbPromise) return dbPromise;
    dbPromise = new Promise((resolve) => {
      let req;
      try { req = window.indexedDB.open(DB_NAME, 1); } catch (_) { resolve(null); return; }
      if (!req) { resolve(null); return; }
      req.onupgradeneeded = () => {
        try { req.result.createObjectStore(DB_STORE); } catch (_) { /* already there */ }
      };
      req.onsuccess = () => resolve(req.result || null);
      req.onerror = () => resolve(null);
      req.onblocked = () => resolve(null);
    });
    // eslint-disable-next-line no-use-before-define
    dbPromise = dbPromise.then((db) => (db ? adopt(db).then(() => db) : db));
    return dbPromise;
  };

  // adopt closes the split between the two engines. A session that
  // could not open IndexedDB wrote its entries to the fallback; every
  // later IndexedDB session was blind to them, so they were records
  // nothing could read, nothing could overwrite and no clear() could
  // ever remove, a ghost that outlives a logout. The first session
  // that does open the database moves them across (a key IndexedDB
  // already holds is dropped) and empties the fallback of this
  // namespace. One way only: the fallback never receives from
  // IndexedDB, so the two cannot diverge a second time.
  const adopt = (db) => new Promise((resolve) => {
    const found = [];
    try {
      for (let i = 0; i < window.localStorage.length; i++) {
        // eslint-disable-next-line no-use-before-define
        const app = strip(window.localStorage.key(i));
        if (app !== null) found.push(app);
      }
    } catch (_) { resolve(); return; }
    if (found.length === 0) { resolve(); return; }
    let tx;
    try {
      tx = db.transaction(DB_STORE, 'readwrite');
      const os = tx.objectStore(DB_STORE);
      for (const app of found) {
        // Guard spelled at the sink: literal namespace + component
        // encoding, the same spelling every other sink in this file uses.
        const stored = PREFIX + encodeURIComponent(app);
        const req = os.getKey(stored);
        req.onsuccess = () => {
          if (req.result !== undefined) return;
          let text = null;
          try { text = window.localStorage.getItem(PREFIX + encodeURIComponent(app)); } catch (_) { text = null; }
          if (typeof text === 'string') os.put(text, stored);
        };
      }
    } catch (_) { resolve(); return; }
    const drop = () => {
      for (const app of found) {
        try { window.localStorage.removeItem(PREFIX + encodeURIComponent(app)); } catch (_) { /* best-effort */ }
      }
      resolve();
    };
    tx.oncomplete = drop;
    tx.onabort = () => resolve();
    tx.onerror = () => resolve();
  });

  // idbRun runs one transaction and settles to { ok, value, reason }.
  // Never rejects: the caller's settlement is always one of the two
  // outcomes, the same rule src/action.js's request keeps. fn returns
  // the request whose result is the value, or an array of requests
  // whose results are collected in order (entries reads keys and
  // values in one transaction so the two cannot drift apart).
  const idbRun = (mode, fn) => openDB().then((db) => {
    if (!db) return { ok: false, reason: 'unavailable' };
    return new Promise((resolve) => {
      let tx;
      let req;
      try {
        tx = db.transaction(DB_STORE, mode);
        req = fn(tx.objectStore(DB_STORE));
      } catch (_) { resolve({ ok: false, reason: 'unavailable' }); return; }
      tx.oncomplete = () => resolve({
        ok: true,
        value: Array.isArray(req) ? req.map((r) => r.result) : (req ? req.result : undefined),
      });
      const fail = () => {
        const first = Array.isArray(req) ? req[0] : req;
        const name = (tx.error && tx.error.name) || (first && first.error && first.error.name) || '';
        resolve({ ok: false, reason: name === 'QuotaExceededError' ? 'quota' : 'unavailable' });
      };
      tx.onabort = fail;
      tx.onerror = fail;
    });
  });

  // lsAvailable probes the fallback the only way that is honest: by
  // writing. Safari's private mode exposes localStorage and throws on
  // every setItem.
  const lsAvailable = () => {
    try {
      window.localStorage.setItem(PREFIX + encodeURIComponent('__probe'), '1');
      window.localStorage.removeItem(PREFIX + encodeURIComponent('__probe'));
      return true;
    } catch (_) { return false; }
  };

  // Values travel as JSON text in both engines, so size accounting is
  // one number and a structured-clone surprise (a DOM node, a function)
  // cannot reach the database.
  const encode = (value) => {
    let text;
    try { text = JSON.stringify(value); } catch (_) { return null; }
    return typeof text === 'string' ? text : null;
  };
  const decode = (text) => {
    if (typeof text !== 'string') return undefined;
    try { return JSON.parse(text); } catch (_) { return undefined; }
  };
  // bytesOf is the stored size of one entry: the UTF-8 length of its
  // JSON text, the unit a layer above budgets in. (LS_MAX_BYTES above
  // compares code units, which is the fallback engine's own unit.)
  const bytesOf = (text) => {
    try { return new TextEncoder().encode(text).length; } catch (_) { return text.length; }
  };

  // storedPrefix is the stored form of an application-key prefix; a
  // missing prefix names the whole namespace. rangeFor is the key range
  // that holds exactly the stored keys under it: every stored key is
  // ASCII (the namespace literal plus a component encoding), so
  // '\uffff' bounds the range from above.
  const storedPrefix = (prefix) => PREFIX + (typeof prefix === 'string' ? encodeURIComponent(prefix) : '');
  const rangeFor = (prefix) => {
    const lo = storedPrefix(prefix);
    try { return window.IDBKeyRange.bound(lo, lo + '\uffff'); } catch (_) { return null; }
  };
  // strip turns a stored key back into the application key, or null
  // for a key outside the namespace (or the prefix) and for a malformed
  // escape, which must not take an enumeration down.
  const strip = (stored, prefix) => {
    if (typeof stored !== 'string' || stored.indexOf(storedPrefix(prefix)) !== 0) return null;
    try { return decodeURIComponent(stored.slice(PREFIX.length)); } catch (_) { return null; }
  };

  let channel = null;
  try { channel = new BroadcastChannel(DB_NAME); } catch (_) { channel = null; }

  // announce tells the other tabs of this origin that a key moved. The
  // key travels, never the value: a subscriber re-reads, so a large
  // entry is not copied into every tab and a listener always sees what
  // the store holds.
  const announce = (key) => {
    if (!channel) return;
    try { channel.postMessage({ k: key }); } catch (_) { /* best-effort */ }
  };

  const notify = (key) => {
    for (const w of watchers) {
      if (key.indexOf(w.prefix) !== 0) continue;
      try { w.fn(key); } catch (_) { /* a throwing watcher is its own problem */ }
    }
    const fns = subs.get(key);
    if (!fns || fns.size === 0) return;
    // eslint-disable-next-line no-use-before-define
    api.get(key).then((value) => {
      for (const fn of fns) {
        try { fn(value, key); } catch (_) { /* a throwing subscriber is its own problem */ }
      }
    });
  };

  if (channel) {
    channel.onmessage = (e) => {
      if (e && e.data && typeof e.data.k === 'string') notify(e.data.k);
    };
  }
  // The storage event covers the fallback engine and any writer that is
  // not this module. BroadcastChannel and storage both skip the writing
  // context, so a tab never hears its own write back.
  window.addEventListener('storage', (e) => {
    if (!e || typeof e.key !== 'string' || e.storageArea !== window.localStorage) return;
    if (e.key.indexOf(PREFIX) !== 0) return;
    let key;
    // decodeURIComponent throws URIError on a malformed escape; a
    // foreign key in our namespace must not take the listener down.
    try { key = decodeURIComponent(e.key.slice(PREFIX.length)); } catch (_) { return; }
    notify(key);
  });

  const api = {
    // available reports what this browser gives us, probed
    // rather than feature-detected: { idb, ls, engine }, where engine
    // is the one that will answer a get or a set: 'idb',
    // 'ls' or 'none'. Two booleans could not say that, and "which
    // engine am I on" is the first question a bug report needs.
    available() {
      return openDB().then((db) => {
        const ls = lsAvailable();
        return { idb: !!db, ls: ls, engine: db ? 'idb' : (ls ? 'ls' : 'none') };
      });
    },

    // get resolves the stored value, or undefined when the key is
    // absent, the entry is unreadable, or no engine is available.
    get(key) {
      if (typeof key !== 'string' || key === '') return Promise.resolve(undefined);
      return openDB().then((db) => {
        if (db) {
          return idbRun('readonly', (s) => s.get(PREFIX + encodeURIComponent(key)))
            .then((r) => (r.ok ? decode(r.value) : undefined));
        }
        let text = null;
        // Guard spelled at the sink: literal namespace + component
        // encoding, so an application key names nothing outside it.
        try { text = window.localStorage.getItem(PREFIX + encodeURIComponent(key)); } catch (_) { text = null; }
        return text === null ? undefined : decode(text);
      });
    },

    // set stores the value and resolves { ok, reason }. reason is
    // 'encode' (the value is not JSON), 'size' (over the fallback's
    // tiny-value cap), 'quota' (the browser said no) or 'unavailable'
    // (no engine). It never throws and never rejects.
    set(key, value) {
      if (typeof key !== 'string' || key === '') return Promise.resolve({ ok: false, reason: 'unavailable' });
      const text = encode(value);
      if (text === null) return Promise.resolve({ ok: false, reason: 'encode' });
      return openDB().then((db) => {
        if (db) {
          return idbRun('readwrite', (s) => s.put(text, PREFIX + encodeURIComponent(key))).then((r) => {
            if (r.ok) announce(key);
            return { ok: r.ok, reason: r.ok ? '' : r.reason };
          });
        }
        if (text.length > LS_MAX_BYTES) return { ok: false, reason: 'size' };
        try {
          // Guard spelled at the sink (see get above).
          window.localStorage.setItem(PREFIX + encodeURIComponent(key), text);
        } catch (err) {
          // Only a quota error is 'quota'. A SecurityError (storage
          // blocked for the origin) is the engine being gone, which is
          // 'unavailable', the same answer a blocked IndexedDB gives.
          const n = (err && err.name) || '';
          return { ok: false, reason: n === 'QuotaExceededError' || n === 'NS_ERROR_DOM_QUOTA_REACHED' ? 'quota' : 'unavailable' };
        }
        // No announce: a localStorage write already reaches the other
        // tabs of this origin through the native storage event, which
        // this module listens for. Announcing too would deliver the
        // same change twice and fire every subscriber twice.
        return { ok: true, reason: '' };
      });
    },

    // remove drops the entry from BOTH engines. Adoption runs once, at
    // open, so a fallback entry written after it (or by a script this
    // module did not run) would otherwise survive a delete and a
    // logout. Removing a key localStorage does not hold changes
    // nothing and fires no storage event, so the common path is free.
    remove(key) {
      if (typeof key !== 'string' || key === '') return Promise.resolve({ ok: false, reason: 'unavailable' });
      return openDB().then((db) => {
        let lsOK = true;
        // held: the fallback had the key, so its removal fires the
        // native storage event in the other tabs and set()'s rule
        // applies: announcing as well would deliver the change twice.
        let held = false;
        try {
          // Guard spelled at the sink (see get above).
          held = window.localStorage.getItem(PREFIX + encodeURIComponent(key)) !== null;
          window.localStorage.removeItem(PREFIX + encodeURIComponent(key));
        } catch (_) {
          lsOK = false;
        }
        if (db) {
          return idbRun('readwrite', (s) => s.delete(PREFIX + encodeURIComponent(key))).then((r) => {
            if (r.ok && !held) announce(key);
            return { ok: r.ok, reason: r.ok ? '' : r.reason };
          });
        }
        // No announce on the fallback: the storage event carries it
        // (see set above).
        return lsOK ? { ok: true, reason: '' } : { ok: false, reason: 'unavailable' };
      });
    },

    // keys resolves { ok, reason, keys }: the application keys this
    // origin holds, namespace stripped and decoded, or only those under
    // prefix when one is given, sorted so a caller can diff two reads.
    //
    // The settlement shape is the same one set and remove keep, and it
    // is load-bearing: an aborted transaction and an empty store used
    // to be the same empty array, so a clear() built on it reported
    // success while every record survived. An enumeration that failed
    // has to be able to say so.
    keys(prefix) {
      return openDB().then((db) => {
        if (db) {
          return idbRun('readonly', (s) => s.getAllKeys(rangeFor(prefix))).then((r) => {
            if (!r.ok) return { ok: false, reason: r.reason, keys: [] };
            const out = [];
            for (const k of r.value || []) {
              const app = strip(k, prefix);
              if (app !== null) out.push(app);
            }
            return { ok: true, reason: '', keys: out.sort() };
          });
        }
        const out = [];
        try {
          for (let i = 0; i < window.localStorage.length; i++) {
            const app = strip(window.localStorage.key(i), prefix);
            if (app !== null) out.push(app);
          }
        } catch (_) { return { ok: false, reason: 'unavailable', keys: [] }; }
        return { ok: true, reason: '', keys: out.sort() };
      });
    },

    // entries resolves { ok, reason, entries }, where entries is
    // [{ key, value, size }] for every entry under prefix (or the whole
    // namespace), sorted by key; size is the stored UTF-8 length of the
    // entry's JSON text. One transaction, so keys and values are one
    // snapshot. An unreadable entry is skipped; a read that FAILED says
    // so rather than looking like an empty collection (see keys).
    entries(prefix) {
      const collect = (pairs) => {
        const out = [];
        for (const [k, text] of pairs) {
          const app = strip(k, prefix);
          if (app === null || typeof text !== 'string') continue;
          const value = decode(text);
          if (value === undefined) continue;
          out.push({ key: app, value: value, size: bytesOf(text) });
        }
        return out.sort((a, b) => (a.key < b.key ? -1 : a.key > b.key ? 1 : 0));
      };
      return openDB().then((db) => {
        if (db) {
          const range = rangeFor(prefix);
          return idbRun('readonly', (s) => [s.getAllKeys(range), s.getAll(range)]).then((r) => {
            if (!r.ok || !r.value) return { ok: false, reason: r.reason || 'unavailable', entries: [] };
            const ks = r.value[0] || [];
            const vs = r.value[1] || [];
            const pairs = [];
            for (let i = 0; i < ks.length && i < vs.length; i++) pairs.push([ks[i], vs[i]]);
            return { ok: true, reason: '', entries: collect(pairs) };
          });
        }
        const pairs = [];
        try {
          for (let i = 0; i < window.localStorage.length; i++) {
            const k = window.localStorage.key(i);
            if (strip(k, prefix) === null) continue;
            pairs.push([k, window.localStorage.getItem(k)]);
          }
        } catch (_) { return { ok: false, reason: 'unavailable', entries: [] }; }
        return { ok: true, reason: '', entries: collect(pairs) };
      });
    },

    // subscribe calls fn(value, key) when ANOTHER tab of this origin
    // changes the key. Returns the unsubscribe function. A tab never
    // hears its own writes: both transports skip the writing context,
    // so a subscriber is a cross-tab channel, not a change feed.
    subscribe(key, fn) {
      if (typeof key !== 'string' || typeof fn !== 'function') return () => {};
      let fns = subs.get(key);
      if (!fns) {
        fns = new Set();
        subs.set(key, fns);
      }
      fns.add(fn);
      return () => {
        const cur = subs.get(key);
        if (!cur) return;
        cur.delete(fn);
        if (cur.size === 0) subs.delete(key);
      };
    },

    // watch calls fn(key) when ANOTHER tab of this origin changes any
    // key under prefix. The key travels, never the value: a watcher
    // re-reads, so a layer that groups records under a prefix can
    // mirror a sibling tab's write into one collection without a
    // subscription per record. Returns the unwatch function; a tab
    // never hears its own writes, as with subscribe.
    watch(prefix, fn) {
      if (typeof prefix !== 'string' || typeof fn !== 'function') return () => {};
      const w = { prefix: prefix, fn: fn };
      watchers.push(w);
      return () => {
        const i = watchers.indexOf(w);
        if (i >= 0) watchers.splice(i, 1);
      };
    },
  };

  NS.local = api;
  (NS.loadedModules = NS.loadedModules || {}).local = true;
})();
