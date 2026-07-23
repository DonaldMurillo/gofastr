// TextArea runtime module — applies the data-fui-autogrow handler to
// any textarea on the page, inside widgets and out. This module is the
// sole owner of autogrow wiring (widgets.js demand-loads it and relies
// on the rescan loops for widget-mounted textareas).
//
// Loaded on-demand when a [data-fui-autogrow] textarea is on the page.
(function () {
  'use strict';

  // Track wired textareas to avoid double-binding when this module
  // runs alongside the widget-scope handler.
  const wired = new WeakSet();

  function wire(ta) {
    if (!ta || wired.has(ta)) return;
    wired.add(ta);
    const grow = () => {
      // Reset to auto first so shrinking works; then read scrollHeight.
      ta.style.height = 'auto';
      ta.style.height = ta.scrollHeight + 'px';
    };
    ta.addEventListener('input', grow);
    const form = ta.form;
    if (form) form.addEventListener('reset', function () { requestAnimationFrame(grow); });
    // Initial pass after layout so the SSR-rendered value picks up
    // the correct row count.
    Promise.resolve().then(grow);
  }

  function scan(root) {
    const scope = root && root.querySelectorAll ? root : document;
    scope.querySelectorAll('textarea[data-fui-autogrow]').forEach(wire);
  }
  scan(document);

  // Standard module self-registration: the runtime's MutationObserver
  // and gofastr:navigate loops call the scanner for inserted/swapped
  // DOM, but only for modules marked loaded.
  window.__gofastr = window.__gofastr || {};
  (window.__gofastr._moduleScanners ||= {}).textarea = scan;
  (window.__gofastr.loadedModules ||= {}).textarea = true;
  // Legacy hook preserved for external callers.
  window.__gofastr.textarea = { rescan: scan };
})();
