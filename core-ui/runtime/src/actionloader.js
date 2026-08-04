// GoFastr runtime module — per-screen action-script loader.
//
// Loaded by boot only when the page manifest carries compiled-action
// hashes (window.__gofastr_actions, assigned by /__gofastr/manifest.js).
// SSR emits a content-addressed <script> per component id present on the
// initial page; this module covers what SSR can't see — client-side
// navigation to a screen whose action registry hasn't loaded yet — by
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

  // Re-fire the JUST-registered component's data-action-mount actions.
  // Boot's gofastr:navigate listener ran the mount pass before this
  // script's registrations existed, so those triggers were dropped —
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
      const params = {};
      for (const a of el.attributes) {
        if (a.name.startsWith('data-param-')) params[a.name.slice('data-param-'.length)] = a.value;
      }
      G.trigger(id, action, params);
    }
  };

  const scan = (root) => {
    const manifest = window.__gofastr_actions;
    if (!manifest || !root || !root.querySelectorAll) return;
    for (const el of root.querySelectorAll('[data-component],[data-widget]')) {
      const id = el.getAttribute('data-component') || el.getAttribute('data-widget');
      if (!id || !manifest[id] || loaded.has(id)) continue;
      loaded.add(id);
      const s = document.createElement('script');
      s.src = '/__gofastr/widget/' + id + '.js?v=' + manifest[id];
      s.onload = () => fireMounts(root, id);
      document.head.appendChild(s);
    }
  };

  window.addEventListener('gofastr:navigate', (e) => {
    scan((e.detail && e.detail.root) || document);
  });
  // Catch anything already on the page that SSR didn't tag (injected
  // islands, widget chrome mounted before this module loaded).
  scan(document);

  (G.loadedModules ||= {}).actionloader = true;
})();
