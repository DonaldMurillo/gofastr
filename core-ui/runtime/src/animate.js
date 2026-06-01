// Animate runtime module — signal-driven CSS transitions.
//
// Loaded on-demand when any [data-fui-animate-signal] marker is on the
// page (or arrives via SPA-nav). Subscribes to signal changes via
// __gofastr._signals[name].listeners and toggles a CSS class on the
// element when the signal value changes.
//
// Attributes:
//   data-fui-animate-signal="<name>"  — signal to watch
//   data-fui-animate-class="<class>"  — CSS class to toggle
//
// Truthy signals ("true", non-empty, non-"0", non-"false") add the
// class; falsy signals remove it. Initial state is applied on setup.

(() => {
  'use strict';

  const ANIMATE_SEL = '[data-fui-animate-signal]';

  // Returns true for values that should activate the class.
  // Mirrors the runtime's toggle semantics: "false", "0", "",
  // null, undefined, and false are falsy; everything else is truthy.
  const isTruthy = (v) => {
    if (v == null || v === false) return false;
    if (typeof v === 'string') return v !== '' && v !== 'false' && v !== '0';
    if (typeof v === 'number') return v !== 0;
    if (typeof v === 'boolean') return v;
    // Objects (error objects, etc.) count as truthy.
    return true;
  };

  // Wire a single animate element to its signal.
  const wire = (el) => {
    const name = el.getAttribute('data-fui-animate-signal');
    const cls = el.getAttribute('data-fui-animate-class');
    if (!name || !cls) return;

    const G = window.__gofastr;
    if (!G) return;

    // Ensure the signal slot exists so we can attach a listener.
    if (!G._signals[name]) {
      G._signals[name] = { value: undefined, listeners: [] };
    }

    // Avoid double-wiring on SPA re-scan.
    if (el.__fuiAnimateWired) return;
    el.__fuiAnimateWired = true;

    const apply = (value) => {
      if (isTruthy(value)) {
        el.classList.add(cls);
      } else {
        el.classList.remove(cls);
      }
    };

    // Subscribe to future changes.
    G._signals[name].listeners.push(apply);

    // Apply current value immediately.
    apply(G._signals[name].value);
  };

  // Scan a root for all animate elements and wire them.
  const scan = (root) => {
    if (!root || !root.querySelectorAll) return;
    root.querySelectorAll(ANIMATE_SEL).forEach(wire);
    // Also check root itself.
    if (root.matches && root.matches(ANIMATE_SEL)) wire(root);
  };

  // Initial scan.
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', () => scan(document));
  } else {
    scan(document);
  }

  // Register SPA rescan handler.
  if (window.__gofastr) {
    window.__gofastr._moduleScanners = window.__gofastr._moduleScanners || {};
    window.__gofastr._moduleScanners['animate'] = (root) => scan(root);
  }

  (window.__gofastr.loadedModules = window.__gofastr.loadedModules || {}).animate = true;
})();
