// GoFastr runtime module — Dropdown click-toggle + outside-dismiss
//
// Composes with the existing disclosure infrastructure. Each dropdown
// trigger has data-fui-dropdown; its panel sibling has
// data-fui-dropdown-panel. The module handles:
//
//   - Click on trigger → toggle aria-expanded + show/hide panel
//   - Click outside open panel → close
//   - Escape → close (delegates to document-level handler)
//   - SPA navigation → close all open dropdowns
//
// Loads on demand:
//   - core.js's marker scanner picks up [data-fui-dropdown] on a page
//     and idle-loads this module.
(() => {
  'use strict';
  window.__gofastr = window.__gofastr || {};
  const NS = window.__gofastr;

  const IS_OPEN = 'data-fui-dropdown-open';

  const open = (trigger, panel) => {
    trigger.setAttribute('aria-expanded', 'true');
    panel.removeAttribute('hidden');
    trigger.closest('[data-fui-dropdown-wrap]')?.setAttribute(IS_OPEN, '');
  };

  const close = (trigger, panel) => {
    trigger.setAttribute('aria-expanded', 'false');
    panel.setAttribute('hidden', '');
    trigger.closest('[data-fui-dropdown-wrap]')?.removeAttribute(IS_OPEN);
  };

  const isOpen = (trigger) =>
    trigger.getAttribute('aria-expanded') === 'true';

  const toggle = (trigger) => {
    const wrap = trigger.closest('[data-fui-dropdown-wrap]');
    if (!wrap) return;
    const panel = wrap.querySelector('[data-fui-dropdown-panel]');
    if (!panel) return;
    if (isOpen(trigger)) {
      close(trigger, panel);
    } else {
      // Close all other open dropdowns first (singleton-by-default).
      closeAll(wrap);
      open(trigger, panel);
    }
  };

  const closeAll = (except) => {
    const sel = '[data-fui-dropdown-wrap][' + IS_OPEN + ']';
    for (const w of document.querySelectorAll(sel)) {
      if (w === except) continue;
      const trig = w.querySelector('[data-fui-dropdown]');
      const panel = w.querySelector('[data-fui-dropdown-panel]');
      if (trig && panel) close(trig, panel);
    }
  };

  // Click on trigger → toggle.
  document.addEventListener('click', (e) => {
    const trigger = e.target.closest('[data-fui-dropdown]');
    if (trigger) {
      e.preventDefault();
      toggle(trigger);
      return;
    }
    // Click outside any open dropdown → close.
    const openWrap = e.target.closest('[data-fui-dropdown-wrap][' + IS_OPEN + ']');
    if (!openWrap) {
      closeAll(null);
    }
  });

  // Escape → close all.
  document.addEventListener('keydown', (e) => {
    if (e.key !== 'Escape') return;
    // Defer to modal stack if one is active.
    if (NS._modalStack && NS._modalStack.length > 0) return;
    // Defer to native disclosure handler for details-based disclosures.
    closeAll(null);
  });

  // SPA navigation → close all.
  document.addEventListener('gofastr:navigate', () => {
    closeAll(null);
  });

  // Scan: wire up initial state for SSR'd dropdowns.
  const scan = (root) => {
    for (const w of root.querySelectorAll('[data-fui-dropdown-wrap]')) {
      const trigger = w.querySelector('[data-fui-dropdown]');
      const panel = w.querySelector('[data-fui-dropdown-panel]');
      if (!trigger || !panel) continue;
      // Ensure panel starts hidden unless the wrapper says open.
      if (w.hasAttribute(IS_OPEN)) {
        open(trigger, panel);
      } else {
        close(trigger, panel);
      }
    }
  };

  requestAnimationFrame(() => scan(document));
  document.addEventListener('gofastr:navigate', () => {
    requestAnimationFrame(() => scan(document));
  });

  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules.dropdown = true;
  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners.dropdown = scan;
})();
