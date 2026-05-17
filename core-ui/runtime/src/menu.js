// GoFastr runtime module — Menu keyboard navigation
//
// Document-level keydown handler for [role="menuitem"] elements
// inside a [role="menu"] panel. Handles:
//   ArrowDown / ArrowUp   — roving focus (wraps at edges)
//   Home / End            — jump to first / last enabled item
//   Tab                   — close the surrounding
//                           <details data-fui-disclosure> and let
//                           Tab fall through naturally
//   Printable single key  — type-ahead jump to next item whose
//                           label starts with the accumulated prefix
//                           (800ms inactivity resets the buffer)
//
// Loads on demand:
//   - core's marker scanner watches for [data-fui-menu] and idle-loads
//     this module.
//   - hover/focus prefetch via data-fui-prefetch="menu" warms it.
//
// The "focus first menuitem on disclosure open" behaviour lives in
// core's existing disclosure-toggle listener — it's a 4-line block
// integrated with the aria-expanded mirror, not worth duplicating
// across modules.
(() => {
  'use strict';
  window.__gofastr = window.__gofastr || {};
  const NS = window.__gofastr;

  let _menuTypeBuf = '', _menuTypeAt = 0;
  document.addEventListener('keydown', (e) => {
    const item = e.target && e.target.closest && e.target.closest('[role="menuitem"]');
    if (!item) return;
    const panel = item.closest('[role="menu"]');
    if (!panel) return;
    const items = Array.from(panel.querySelectorAll('[role="menuitem"]:not([aria-disabled="true"])'));
    if (items.length === 0) return;
    const idx = items.indexOf(item);
    const move = (to) => {
      e.preventDefault();
      items[(to + items.length) % items.length].focus();
    };
    if (e.key === 'ArrowDown') return move(idx + 1);
    if (e.key === 'ArrowUp')   return move(idx - 1);
    if (e.key === 'Home')      return move(0);
    if (e.key === 'End')       return move(items.length - 1);
    if (e.key === 'Tab') {
      // Close the surrounding disclosure so focus escapes naturally.
      const d = panel.closest('details[data-fui-disclosure]');
      if (d) d.removeAttribute('open');
      return; // do NOT preventDefault — let Tab move focus
    }
    // Type-ahead: a printable single-character key jumps to the
    // next item whose label starts with the accumulated prefix.
    if (e.key.length === 1 && !e.ctrlKey && !e.metaKey && !e.altKey) {
      const now = Date.now();
      if (now - _menuTypeAt > 800) _menuTypeBuf = '';
      _menuTypeAt = now;
      _menuTypeBuf += e.key.toLowerCase();
      for (let i = 1; i <= items.length; i++) {
        const cand = items[(idx + i) % items.length];
        const label = (cand.textContent || '').trim().toLowerCase();
        if (label.startsWith(_menuTypeBuf)) {
          e.preventDefault();
          cand.focus();
          return;
        }
      }
    }
  });

  (NS.loadedModules ||= {}).menu = true;
})();
