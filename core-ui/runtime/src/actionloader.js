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
  for (const s of document.querySelectorAll('script[src^="/__gofastr/widget/"]')) {
    const m = s.getAttribute('src').match(/\/__gofastr\/widget\/([^.?]+)\.js/);
    if (m) loaded.add(m[1]);
  }

  const scan = (root) => {
    const manifest = window.__gofastr_actions;
    if (!manifest || !root || !root.querySelectorAll) return;
    for (const el of root.querySelectorAll('[data-component],[data-widget]')) {
      const id = el.getAttribute('data-component') || el.getAttribute('data-widget');
      if (!id || !manifest[id] || loaded.has(id)) continue;
      loaded.add(id);
      const s = document.createElement('script');
      s.src = '/__gofastr/widget/' + id + '.js?v=' + manifest[id];
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
