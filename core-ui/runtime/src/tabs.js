// tabs.js: opt-in contract behaviors for framework/ui.Tabs strips.
//
// Loaded by the kernel's data-fui-prefetch bridge, which the component
// arms with data-fui-prefetch="tabs" whenever StateAttrs or VacateHidden
// is on. Both are interaction-time behaviors, so a pointerover/focusin
// load has the module in place by the first click without costing the
// core bundle a scanner entry (core gzip headroom is ~13 bytes at its
// binding level-1 budget line; see
// core-ui/runtime/budget_test.go).
//
// Owns two markers, both on the strip's wrapper div:
//
//   - data-fui-tabs-state: mirror data-active into data-state=
//     "active"/"inactive" on every [role=tab] button. Core already
//     mirrors aria-selected the same way; this adds the attribute
//     Radix-style ports pin their test locators to.
//
//   - data-fui-tabs-vacate: keep hidden panels EMPTY. SSR ships their
//     content in an adjacent <script type="application/json"
//     data-fui-tabs-stash> (index → HTML). On first show the module
//     restores from that stash (innerHTML + scanAndLoadCSS, the same
//     insertion pipeline html-mode signal regions use); from then on it
//     moves the panel's LIVE nodes into a detached DocumentFragment on
//     hide and back on show, so anything the runtime swapped into the
//     panel (island updates, form state) survives re-show intact.
//
// Trade-offs by design: while a panel is vacated its nodes are detached,
// so document-scoped updates targeting them (SSE island pushes, RPC
// responses for their controls) are dropped permanently — nothing is
// queued for replay; re-show resurrects the panel's pre-vacate nodes,
// and only updates that arrive after re-show land. Focus inside a
// vacated panel escapes to <body>. Panel visibility itself is
// unchanged: CSS still keys off data-active, this module only owns the
// content.
(() => {
  'use strict';
  const G = window.__gofastr;
  const wired = new WeakSet();       // strips with an observer installed
  const live = new WeakMap();        // panel -> DocumentFragment of its vacated live nodes
  const parsed = new WeakMap();      // stash script -> decoded {index: html}

  const stashMap = (wrapper) => {
    const s = wrapper.querySelector('script[data-fui-tabs-stash]');
    if (!s) return {};
    if (!parsed.has(s)) {
      let m = {};
      try { m = JSON.parse(s.textContent || '{}'); } catch (_) {}
      parsed.set(s, m);
    }
    return parsed.get(s);
  };

  const apply = (wrapper) => {
    const active = wrapper.getAttribute('data-active');
    if (wrapper.hasAttribute('data-fui-tabs-state')) {
      wrapper.querySelectorAll('[role="tab"][data-fui-tab-index]').forEach((b) => {
        b.setAttribute('data-state',
          b.getAttribute('data-fui-tab-index') === active ? 'active' : 'inactive');
      });
    }
    if (!wrapper.hasAttribute('data-fui-tabs-vacate')) return;
    const map = stashMap(wrapper);
    wrapper.querySelectorAll('[role="tabpanel"][data-fui-tab-index]').forEach((p) => {
      const idx = p.getAttribute('data-fui-tab-index');
      if (idx === active) {
        // firstChild, not firstElementChild: text-only panels are content too.
        if (p.firstChild) return;
        const frag = live.get(p);
        if (frag) { p.appendChild(frag); return; }
        const html = map[idx];
        if (html) {
          p.innerHTML = html;
          if (G.scanAndLoadCSS) G.scanAndLoadCSS(p);
        }
      } else if (p.firstChild) {
        // Stash the live nodes themselves: listeners, island DOM the
        // runtime swapped in, and form state all move with them.
        const frag = document.createDocumentFragment();
        while (p.firstChild) frag.appendChild(p.firstChild);
        live.set(p, frag);
      }
    });
  };

  const wire = (w) => {
    if (wired.has(w)) return;
    wired.add(w);
    // Late module load (slow fetch, or a signal write before any
    // pointer interaction): reconcile against data-active right away.
    apply(w);
    new MutationObserver(() => apply(w))
      .observe(w, { attributes: true, attributeFilter: ['data-active'] });
  };

  const scan = (scope) => {
    const root = scope?.querySelectorAll ? scope : document;
    // Match the scope node ITSELF, not just its descendants: core's
    // DOM-insertion observer hands scanners the added node, and the
    // added node can be a strip — one nested inside a restored vacate
    // panel, or the root of an island/RPC/html-signal region swap
    // response. Same contract _scanForModules already follows
    // (module_root_marker_e2e_test.go).
    if (root.matches && root.matches('[data-fui-tabs-state],[data-fui-tabs-vacate]')) wire(root);
    root.querySelectorAll('[data-fui-tabs-state],[data-fui-tabs-vacate]').forEach(wire);
  };

  scan(document);
  // Core's DOM-insertion observer and the gofastr:navigate loop both
  // iterate _moduleScanners for loaded modules, so SPA-swapped strips
  // and strips nested inside restored content get wired.
  (G._moduleScanners ||= {}).tabs = scan;
  (G.loadedModules ||= {}).tabs = true;
})();
