// Computed runtime module — client-side derived signals (core-ui/store).
//
// An element carries:
//   data-fui-computed="<reducerName>"   — host-registered reducer fn
//   data-fui-computed-deps="a,b"        — dependency signal names
//   data-fui-signal="<name>"            — this computed's own signal name
//
// On wire, the module subscribes to each dependency signal. When any
// dependency changes, it runs the reducer over the current dep values
// and broadcasts the result via setSignal(ownName, …) — which fans out
// to every consumer of the computed signal. No eval: the reducer is a
// real JS function the host registers on window.__gofastr._reducers by
// name (CSP-safe).
//
// Loaded on-demand when a [data-fui-computed] element appears.
(() => {
  'use strict';

  const SEL = '[data-fui-computed]';

  // Track wired elements + their dependency subscriptions so we can splice the
  // recompute closures back out of each G._signals[dep].listeners once the
  // element is detached by SPA navigation. Without this the closures (and the
  // detached nodes they close over) leak across every page swap.
  const wired = new Set();

  const wire = (el) => {
    if (el.__fuiComputedWired) return;
    const G = window.__gofastr;
    if (!G) return;

    const reducerName = el.getAttribute('data-fui-computed');
    const ownName = el.getAttribute('data-fui-signal');
    if (!reducerName || !ownName) return;

    const deps = (el.getAttribute('data-fui-computed-deps') || '')
      .split(',').map((s) => s.trim()).filter(Boolean);

    el.__fuiComputedWired = true;
    G._reducers = G._reducers || {};
    G._signals = G._signals || {};

    const recompute = () => {
      // Own-property lookup only: a reducer name like "constructor" /
      // "toString" / "valueOf" would otherwise resolve to the inherited
      // Object.prototype method (typeof === 'function') and get invoked
      // as a reducer, breaking the "missing reducer → no-op" contract.
      const fn = Object.prototype.hasOwnProperty.call(G._reducers, reducerName)
        ? G._reducers[reducerName]
        : undefined;
      if (typeof fn !== 'function') return; // missing reducer → no-op
      const vals = {};
      for (const d of deps) vals[d] = G.getSignal(d);
      let out;
      try { out = fn(vals); } catch (_) { return; } // a throwing reducer never breaks the page
      G.setSignal(ownName, out);
    };

    // Subscribe to every dependency. Ensure the slot exists so a
    // dependency that hasn't been seeded/set yet still wires up.
    for (const d of deps) {
      if (!G._signals[d]) G._signals[d] = { value: undefined, listeners: [] };
      G._signals[d].listeners.push(recompute);
    }

    // Remember the subscription so we can tear it down on SPA navigation.
    el.__fuiComputedEntry = { deps: deps, recompute: recompute };
    wired.add(el);

    // Initial compute fills the (SSR-empty) computed node on boot.
    recompute();
  };

  // Remove subscriptions for computed elements that left the document. Called
  // on gofastr:navigate so per-page recompute listeners don't leak across swaps.
  const teardownDetached = () => {
    const G = window.__gofastr;
    if (!G || !G._signals) return;
    for (const el of Array.from(wired)) {
      if (el.isConnected) continue;
      const entry = el.__fuiComputedEntry;
      wired.delete(el);
      el.__fuiComputedWired = false;
      el.__fuiComputedEntry = null;
      if (!entry) continue;
      for (const d of entry.deps) {
        const slot = G._signals[d];
        if (!slot || !slot.listeners) continue;
        const i = slot.listeners.indexOf(entry.recompute);
        if (i !== -1) slot.listeners.splice(i, 1);
      }
    }
  };

  const scan = (root) => {
    if (!root || !root.querySelectorAll) return;
    root.querySelectorAll(SEL).forEach(wire);
    if (root.matches && root.matches(SEL)) wire(root);
  };

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', () => scan(document));
  } else {
    scan(document);
  }

  // On SPA navigation, tear down listeners for computed elements that left the
  // DOM BEFORE the new page's scanner re-wires the fresh markers.
  document.addEventListener('gofastr:navigate', teardownDetached);

  if (window.__gofastr) {
    window.__gofastr._moduleScanners = window.__gofastr._moduleScanners || {};
    window.__gofastr._moduleScanners['computed'] = (root) => scan(root);
  }
  (window.__gofastr.loadedModules = window.__gofastr.loadedModules || {}).computed = true;
})();
