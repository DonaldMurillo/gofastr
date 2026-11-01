// GoFastr runtime module, per-screen action-script loader.
//
// Loaded by boot only when the page manifest carries compiled-action
// hashes (window.__gofastr_actions, assigned by /__gofastr/manifest.js).
// SSR emits a content-addressed <script> per component id present on the
// initial page; this module covers what SSR can't see, client-side
// navigation to a screen whose action registry hasn't loaded yet, by
// mirroring the CSS scanner: on every navigate, inject the script for
// any on-page component id in the manifest that isn't loaded.
(function () {
  'use strict';
  const G = window.__gofastr;

  const loaded = new Set();
  // Seed with what SSR already injected so navigation never double-loads.
  // Query-strip + suffix-match rather than a character class: component
  // ids may contain dots, which a [^.?]+ class truncated.
  for (const s of document.querySelectorAll('script[src^="/__gofastr/widget/"]')) {
    const m = s.getAttribute('src').split('?')[0].match(/\/__gofastr\/widget\/(.+)\.js$/);
    if (m) loaded.add(m[1]);
  }

  // A mount node's data-param-* attributes, as boot's collectParams reads them.
  const params = (el) => {
    const out = {};
    for (const a of el.attributes) {
      if (a.name.startsWith('data-param-')) out[a.name.slice('data-param-'.length)] = a.value;
    }
    return out;
  };
  // Re-fire the JUST-registered component's data-action-mount actions.
  // Boot's gofastr:navigate listener ran the mount pass before this
  // script's registrations existed, so those triggers were dropped,
  // an entity list navigated to for the first time never populated.
  // Scoped to one id so components whose registries were already live
  // (and whose mounts boot fired) don't fire twice.
  const fireMounts = (root, id) => {
    for (const el of (root || document).querySelectorAll('[data-action-mount]')) {
      const owner = el.closest('[data-component],[data-widget]');
      const cid = owner && (owner.getAttribute('data-component') || owner.getAttribute('data-widget'));
      if (cid !== id) continue;
      const action = el.getAttribute('data-action-mount');
      if (!action) continue;
      el._cuiM = 1;
      G.trigger(id, action, params(el));
    }
  };

  const scan = (root) => {
    const manifest = window.__gofastr_actions;
    if (!manifest || !root || !root.querySelectorAll) return;
    for (const el of [root, ...root.querySelectorAll('[data-component],[data-widget]')]) {
      const id = el.getAttribute && (el.getAttribute('data-component') || el.getAttribute('data-widget'));
      // Own entries only: the id is DOM-sourced, and "constructor" read
      // through the prototype is a function, not a hash.
      if (!id || !Object.hasOwn(manifest, id) || loaded.has(id)) continue;
      // Parity with loadModule's name gate (frag/boot.js): the id is
      // DOM-sourced and lands in a script src path, so an id failing the
      // anchored name shape (dots allowed — component ids carry them) is
      // refused before the URL is built; the browser must never be asked
      // to normalize a traversal-carrying src.
      if (!/^[\w.-]+$/.test(id)) continue;
      loaded.add(id);
      const s = document.createElement('script');
      s.src = '/__gofastr/widget/' + id + '.js?v=' + manifest[id];
      s.onload = () => fireMounts(document, id);
      // A failed fetch must not pin the id: the next time the component
      // appears (an overlay reopened, a swap) the script is asked again.
      s.onerror = () => { s.remove(); loaded.delete(id); };
      document.head.appendChild(s);
    }
  };

  window.addEventListener('gofastr:navigate', (e) => {
    scan((e.detail && e.detail.root) || document);
  });
  // Catch anything already on the page that SSR didn't tag (injected
  // islands, widget chrome mounted before this module loaded).
  scan(document);
  // And whatever is inserted later outside a navigation (an intercepted
  // route's overlay, an island swap, widget chrome): boot's mutation
  // observer hands each inserted subtree to the loaded modules' scanners
  // (a navigation hands them the document, whose mounts boot's own pass
  // fires). Such a subtree also fires its data-action-mount elements, each
  // once: boot marks the ones its pass fired (_cuiM), and a component whose
  // script is still loading is fired again by fireMounts when it lands.
  (G._moduleScanners ||= {}).actionloader = (root) => {
    scan(root);
    if (root === document || !root.querySelectorAll) return;
    for (const el of [root, ...root.querySelectorAll('[data-action-mount]')]) {
      const action = el.getAttribute('data-action-mount');
      if (!action || el._cuiM) continue;
      el._cuiM = 1;
      const owner = el.closest('[data-component],[data-widget]');
      const cid = owner && (owner.getAttribute('data-component') || owner.getAttribute('data-widget'));
      if (cid) G.trigger(cid, action, params(el));
    }
  };

  (G.loadedModules ||= {}).actionloader = true;
})();
