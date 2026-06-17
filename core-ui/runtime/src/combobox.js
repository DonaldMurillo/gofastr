// GoFastr runtime module — Combobox
//
// Wires WAI-ARIA Combobox 1.2 keyboard + pointer behavior for any
// [role="combobox"] input paired with a [role="listbox"] via
// aria-controls. The server populates options via data-fui-rpc
// (debounced) + data-fui-signal swap; this module owns the
// interactive layer.
//
// Keyboard:
//   ArrowDown — open + highlight first option (or move highlight down)
//   ArrowUp   — open + highlight last option (or move highlight up)
//   Home / End — first / last option
//   Enter     — pick highlighted (sets input.value to data-value or
//               textContent, closes listbox)
//   Escape    — close listbox; on a second Esc with text, clears input
//   Tab       — close listbox + let Tab move focus naturally
//
// Pointer:
//   Click an option to pick it. Click outside to close.
//   Focus auto-opens the listbox if it already holds options.
//
// All listeners are installed at document level so RPC-injected
// options (the server returns <li role="option"> fragments swapped
// in via signal binding) are picked up without re-wiring.
//
// Loads on demand: core.js's _moduleMarkers entry triggers fetch
// when a [role="combobox"] is detected in the DOM.
(() => {
  'use strict';

  const highlight = (lb, opt) => {
    lb.querySelectorAll('[role="option"].is-active').forEach((o) => {
      o.classList.remove('is-active');
    });
    if (opt) {
      opt.classList.add('is-active');
      const input = document.querySelector(
        '[role="combobox"][aria-controls="' + lb.id + '"]'
      );
      if (input) input.setAttribute('aria-activedescendant', opt.id || '');
    }
  };
  const closeListbox = (input, lb) => {
    input.setAttribute('aria-expanded', 'false');
    input.setAttribute('aria-activedescendant', '');
    if (lb) {
      lb.setAttribute('hidden', '');
      lb.querySelectorAll('[role="option"].is-active').forEach((o) =>
        o.classList.remove('is-active')
      );
    }
  };
  const openListbox = (input, lb) => {
    if (!lb) return;
    input.setAttribute('aria-expanded', 'true');
    lb.removeAttribute('hidden');
  };
  const pickOption = (input, lb, opt) => {
    if (!opt) return;
    const val = opt.getAttribute('data-value') || (opt.textContent || '').trim();
    input.value = val;
    input.dispatchEvent(new Event('change', { bubbles: true }));
    closeListbox(input, lb);
    // Honor data-fui-push-state on the option — selecting a result
    // in a search/palette pattern is almost always meant to navigate.
    // The server side hands the option a path via this attribute;
    // we route it through the SPA navigator (or fall back to a hard
    // load if the navigator hasn't booted yet).
    const dest = opt.getAttribute('data-fui-push-state');
    if (dest) {
      // Close any enclosing widget (e.g. a command palette modal) so
      // the nav doesn't happen behind a backdrop.
      const widget = opt.closest('[data-fui-widget]');
      if (widget && window.__gofastr && window.__gofastr.closeWidget) {
        try { window.__gofastr.closeWidget(widget.getAttribute('data-fui-widget')); } catch (_) {}
      }
      if (window.__gofastr && window.__gofastr.navigate) {
        window.__gofastr.navigate(dest);
      } else {
        location.href = dest;
      }
    }
  };

  // Idempotent global listener installation. Multiple module loads
  // (rare but possible during dev rebuilds) won't double-wire.
  if (window.__fuiComboboxWired) return;
  window.__fuiComboboxWired = true;

  document.addEventListener('keydown', (e) => {
    const input = e.target && e.target.closest && e.target.closest('[role="combobox"]');
    if (!input) return;
    const lbId = input.getAttribute('aria-controls');
    if (!lbId) return;
    const lb = document.getElementById(lbId);
    if (!lb) return;
    const options = Array.from(
      lb.querySelectorAll('[role="option"]:not([aria-disabled="true"])')
    );
    const activeId = input.getAttribute('aria-activedescendant');
    const activeIdx = options.findIndex((o) => o.id === activeId);
    const isOpen = input.getAttribute('aria-expanded') === 'true';

    switch (e.key) {
      case 'ArrowDown': {
        if (options.length === 0) return;
        e.preventDefault();
        if (!isOpen) { openListbox(input, lb); highlight(lb, options[0]); return; }
        const next = options[(activeIdx + 1 + options.length) % options.length];
        highlight(lb, next);
        return;
      }
      case 'ArrowUp': {
        if (options.length === 0) return;
        e.preventDefault();
        if (!isOpen) {
          openListbox(input, lb);
          highlight(lb, options[options.length - 1]);
          return;
        }
        const prev = options[(activeIdx - 1 + options.length) % options.length];
        highlight(lb, prev);
        return;
      }
      case 'Home': {
        if (!isOpen || options.length === 0) return;
        e.preventDefault();
        highlight(lb, options[0]);
        return;
      }
      case 'End': {
        if (!isOpen || options.length === 0) return;
        e.preventDefault();
        highlight(lb, options[options.length - 1]);
        return;
      }
      case 'Enter': {
        if (!isOpen || activeIdx < 0) return;
        e.preventDefault();
        pickOption(input, lb, options[activeIdx]);
        return;
      }
      case 'Escape': {
        if (isOpen) { e.preventDefault(); closeListbox(input, lb); return; }
        if (input.value) {
          e.preventDefault();
          input.value = '';
          input.dispatchEvent(new Event('input', { bubbles: true }));
          return;
        }
        return;
      }
      case 'Tab': {
        if (isOpen) closeListbox(input, lb);
        return;
      }
    }
  });

  // Click-to-pick on options. Delegated so RPC-injected options work.
  document.addEventListener('click', (e) => {
    const opt = e.target && e.target.closest && e.target.closest('[role="option"]');
    if (!opt || opt.getAttribute('aria-disabled') === 'true') return;
    const lb = opt.closest('[role="listbox"]');
    if (!lb || !lb.id) return;
    const input = document.querySelector(
      '[role="combobox"][aria-controls="' + lb.id + '"]'
    );
    if (!input) return;
    e.preventDefault();
    pickOption(input, lb, opt);
  });

  // Focus auto-open: when an input gets focus and the linked listbox
  // already has options (e.g. user clicked back into a half-filled
  // search), reopen it so they can continue.
  document.addEventListener('focusin', (e) => {
    const input = e.target && e.target.closest && e.target.closest('[role="combobox"]');
    if (!input) return;
    const lbId = input.getAttribute('aria-controls');
    const lb = lbId ? document.getElementById(lbId) : null;
    if (!lb) return;
    if (lb.querySelector('[role="option"]')) openListbox(input, lb);
  });

  // Outside-click closes any open combobox.
  document.addEventListener('click', (e) => {
    for (const input of document.querySelectorAll(
      '[role="combobox"][aria-expanded="true"]'
    )) {
      const lbId = input.getAttribute('aria-controls');
      const lb = lbId ? document.getElementById(lbId) : null;
      if (input.contains(e.target) || (lb && lb.contains(e.target))) continue;
      closeListbox(input, lb);
    }
  });

  // Static-option filtering: a combobox whose listbox carries
  // data-fui-static-options renders every option inline (no RPC round-trip).
  // Filter them client-side on input — hide non-matches, show all when the
  // query clears. This is how a docs/nav palette works on a serverless
  // export where no search endpoint exists.
  document.addEventListener('input', (e) => {
    const input = e.target && e.target.closest && e.target.closest('[role="combobox"]');
    if (!input) return;
    const lbId = input.getAttribute('aria-controls');
    const lb = lbId ? document.getElementById(lbId) : null;
    if (!lb || !lb.hasAttribute('data-fui-static-options')) return;
    const q = (input.value || '').toLowerCase().trim();
    let firstVisible = null;
    let anyVisible = false;
    lb.querySelectorAll('[role="option"]').forEach((opt) => {
      const match = !q || (opt.textContent || '').toLowerCase().indexOf(q) !== -1;
      opt.hidden = !match;
      if (match) { anyVisible = true; if (!firstVisible) firstVisible = opt; }
    });
    if (anyVisible) { openListbox(input, lb); highlight(lb, firstVisible); }
    else { input.setAttribute('aria-activedescendant', ''); }
  });

  // Self-registration with the core runtime.
  window.__gofastr = window.__gofastr || {};
  (window.__gofastr.loadedModules ||= {}).combobox = true;
})();
