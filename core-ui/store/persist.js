// core-ui/store's browser-persisted slices: the runtime half of
// store.Slice.Persist / PersistMax.
//
// This module is deliberately thin. The browser store itself is the
// kernel's `local` primitive (core-ui/runtime/src/local.js): IndexedDB
// with a tiny-value localStorage fallback, one namespace, best-effort
// by contract. All this file does is join that primitive to the signal
// bus, which is the only opinion core-ui takes about it. Registered by
// persist.go with Requires('local'), so the loader has the primitive
// registered before this file evaluates, and loaded when
// [data-fui-signal-persist] is in the page, which is exactly when a
// persisted slice rendered a binding.
//
// Per bound slice name, once:
//
//   1. read the key out of the browser store and push it into the
//      signal, replacing the value SSR painted;
//   2. subscribe to the signal's listener list and write every later
//      value back, refusing one over the slice's declared byte cap and
//      one the runtime marked untrusted;
//   3. mirror another tab's write of the same key into this tab.
//
// Nothing here is sent to the server: a Go render never sees these
// bytes. A screen that must know a browser-held value at FIRST PAINT
// wants the cookie mirror ui.Banner uses, not this: the restore lands
// after hydration, by construction.
(() => {
  'use strict';
  const NAME = 'signal-persist';
  const NS = window.__gofastr = window.__gofastr || {};
  // The kernel fetches a module once per page, but anything that
  // evaluates this file twice must subscribe nothing twice.
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;

  const MARKER = '[data-fui-signal-persist]';
  // Mirrors PersistDefaultMaxBytes in persist.go; used only when the
  // attribute is missing or unreadable.
  const DEFAULT_MAX_BYTES = 65536;

  // Slice name to { max, last, echoing }. A Map, never a plain object:
  // the key arrives from a DOM attribute, and a bracket write keyed by
  // one is how __proto__ re-parents a store.
  const slices = new Map();

  // bytesOf is the UTF-8 length of the JSON text, the unit PersistMax
  // is declared in and the unit the local primitive's entries() reports.
  // text.length counts UTF-16 units, so a CJK value would pass the cap
  // at a third of its stored size.
  const bytesOf = (text) => {
    try { return new TextEncoder().encode(text).length; } catch (_) { return text.length; }
  };

  // notify tells the page that a value could not be stored. The whole
  // point of a size-bounded, best-effort store is that the app can say
  // so; "you have run out of room" is a product decision, not a
  // framework one.
  const notify = (name, reason, size, max) => {
    try {
      window.dispatchEvent(new CustomEvent('gofastr:persist-overflow', {
        detail: { name: name, reason: reason, size: size, max: max },
      }));
    } catch (_) { /* best-effort */ }
  };

  const write = (name, value) => {
    const entry = slices.get(name);
    if (!entry || entry.echoing) return; // a cross-tab apply is not a new write
    // A signal the runtime marked untrusted holds a value some other
    // input supplied (widgets.js seeds one from location.search), and
    // runtime.js only keeps it out of innerHTML for as long as the flag
    // survives. Persisting it would launder it: the store outlives the
    // flag, the restore looks like any other value, and the next page
    // load writes attacker markup through an html-mode binding. A value
    // the browser was given is never a value the browser keeps.
    const sig = Object.prototype.hasOwnProperty.call(NS._signals, name) ? NS._signals[name] : null;
    if (sig && sig.untrusted) {
      notify(name, 'untrusted', 0, entry.max);
      return;
    }
    let text;
    // A value with a cycle in it (or a throwing toJSON) is not
    // storable; the signal keeps working with it in memory.
    try { text = JSON.stringify(value); } catch (_) { return; }
    if (typeof text !== 'string') return; // undefined: nothing to store
    if (text === entry.last) return;      // idempotent: no write amplification
    // The cap is this slice's, declared in Go and carried on the
    // marker; the primitive has its own engine limits underneath.
    const size = bytesOf(text);
    if (size > entry.max) {
      notify(name, 'size', size, entry.max);
      return;
    }
    NS.local.set(name, value).then((r) => {
      if (r.ok) { entry.last = text; return; }
      notify(name, r.reason, size, entry.max);
    });
  };

  const wire = (el) => {
    const name = el.getAttribute('data-fui-signal');
    if (!name || slices.has(name)) return;
    // The kernel's own reserved-key refusal, spelled here because this
    // module creates the signal slot: a planted
    // data-fui-signal="__proto__" would otherwise re-parent the shared
    // signal store through the __proto__ setter and kill every signal
    // on the page.
    if (name === '__proto__' || name === 'constructor' || name === 'prototype') return;
    const declared = parseInt(el.getAttribute('data-fui-signal-persist'), 10);
    const entry = { max: declared > 0 ? declared : DEFAULT_MAX_BYTES, last: null, echoing: false };
    slices.set(name, entry);

    const apply = (value) => {
      if (value === undefined) return;
      let text;
      try { text = JSON.stringify(value); } catch (_) { return; }
      entry.last = typeof text === 'string' ? text : null;
      entry.echoing = true;
      // { untrusted: true }: a value that came back out of the browser
      // store is not server-authored HTML, whatever it was when it went
      // in. runtime.js renders an untrusted value as text in html mode,
      // so a stored string can never become markup after a reload.
      try { NS.setSignal(name, value, { untrusted: true }); } finally { entry.echoing = false; }
    };

    // Seed from the browser. SSR painted the server's value; this is
    // the one this browser last held, so it wins. Asynchronous by
    // construction, since IndexedDB is, so the server's value is what
    // first paint shows. It wins over the SEED only: a setSignal that
    // lands while the read is in flight is newer than anything the
    // store holds, so the restore applies only while the signal still
    // holds what it held when the read was requested.
    const current = () => (Object.prototype.hasOwnProperty.call(NS._signals, name) && NS._signals[name] ? NS._signals[name].value : undefined);
    const seed = current();
    NS.local.get(name).then((value) => {
      if (current() !== seed) return;
      apply(value);
    });
    // And keep agreeing with the other tabs of this origin.
    NS.local.subscribe(name, apply);

    // Write back on every later change, through the store's own
    // listener list, the same one computed and animate subscribe to.
    // The subscription is per NAME, not per element, and a persisted
    // slice is app-global (Persist implies Global in Go), so it
    // deliberately outlives any one page: there is nothing to tear
    // down on gofastr:navigate and exactly one listener per slice.
    // Own-property read, computed.js's idiom: a name like "constructor"
    // resolves through the prototype chain and passes a truthiness gate
    // while holding no listener list at all.
    if (!Object.prototype.hasOwnProperty.call(NS._signals, name) || !NS._signals[name]) NS._signals[name] = { value: undefined, listeners: [] };
    NS._signals[name].listeners.push((v) => write(name, v));
  };

  const scan = (root) => {
    const scope = root && root.querySelectorAll ? root : document;
    if (scope.matches && scope.matches(MARKER)) wire(scope);
    scope.querySelectorAll(MARKER).forEach(wire);
  };

  scan(document);
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;
  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;
})();
