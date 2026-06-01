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

    // Initial compute fills the (SSR-empty) computed node on boot.
    recompute();
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

  if (window.__gofastr) {
    window.__gofastr._moduleScanners = window.__gofastr._moduleScanners || {};
    window.__gofastr._moduleScanners['computed'] = (root) => scan(root);
  }
  (window.__gofastr.loadedModules = window.__gofastr.loadedModules || {}).computed = true;
})();
