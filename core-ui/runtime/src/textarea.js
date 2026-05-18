// TextArea runtime module — applies the data-fui-autogrow handler to
// any textarea on the page (not just inside widgets). Inside widgets
// the same handler runs from widgets.js on every widget mount; this
// module catches the rest (plain forms, standalone fields).
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
  document.addEventListener('gofastr:navigate', function () { scan(document); });

  // Expose for the runtime's per-module rescan loop on partial swaps.
  window.__gofastr = window.__gofastr || {};
  window.__gofastr.textarea = { rescan: scan };
})();
