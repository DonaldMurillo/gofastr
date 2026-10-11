// GoFastr color-scheme and page-theme bootstrap.
//
// Runs synchronously at the TOP of <head>, before any CSS parses, so
// dark-mode tokens take effect during the same first paint. Reads,
// in order:
//
//   1. localStorage["gofastr.colorScheme"]: explicit user choice
//      (auto | light | dark). Apps that ship a theme toggle write
//      here.
//   2. window.matchMedia('(prefers-color-scheme: dark)'): OS hint.
//
// Sets <html data-color-scheme="dark|light"> + the matching
// `<meta name="color-scheme">` so native UA controls (scrollbars,
// form inputs) follow suit.
//
// Listens for OS preference changes when the stored mode is "auto"
// or unset.
//
// Also applies the page theme a ui.ThemePicker chose:
// localStorage["gofastr.theme"] names one registered override class
// (cui-theme-<hex>), added to <html> here so a themed page never paints
// in the default first. Anything else stored there is ignored.
(() => {
  'use strict';
  try {
    const KEY = 'gofastr.theme';
    const VALID = /^cui-theme-[0-9a-f]{1,64}$/;
    const root = document.documentElement;
    let stored = '';
    try { stored = localStorage.getItem(KEY) || ''; } catch (_) {}
    if (VALID.test(stored)) root.classList.add(stored);
    // Public API: window.__gofastr_theme.set(cls) puts one override
    // class on <html> and persists it; set('') returns to the app's
    // own theme. A value that is not one override class is refused.
    window.__gofastr_theme = {
      get: () => {
        try {
          const v = localStorage.getItem(KEY) || '';
          return VALID.test(v) ? v : '';
        } catch (_) { return ''; }
      },
      set: (cls) => {
        if (cls !== '' && !VALID.test(cls)) return;
        for (const c of Array.prototype.slice.call(root.classList)) {
          if (c.indexOf('cui-theme-') === 0) root.classList.remove(c);
        }
        if (cls) root.classList.add(cls);
        try {
          if (cls) localStorage.setItem(KEY, cls);
          else localStorage.removeItem(KEY);
        } catch (_) {}
      },
    };
  } catch (_) { /* SSR / non-browser */ }
})();
(() => {
  'use strict';
  try {
    const KEY = 'gofastr.colorScheme';
    let stored = '';
    try { stored = localStorage.getItem(KEY) || ''; } catch (_) {}
    const apply = () => {
      let mode = stored;
      if (mode !== 'light' && mode !== 'dark') {
        const mq = window.matchMedia('(prefers-color-scheme: dark)');
        mode = mq && mq.matches ? 'dark' : 'light';
      }
      document.documentElement.setAttribute('data-color-scheme', mode);
      // Also set the native color-scheme meta so UA-rendered controls
      // (scrollbars, native datepickers, form inputs) follow.
      let meta = document.querySelector('meta[name="color-scheme"]');
      if (!meta) {
        meta = document.createElement('meta');
        meta.setAttribute('name', 'color-scheme');
        document.head.appendChild(meta);
      }
      meta.setAttribute('content', mode);
    };
    apply();
    // Re-apply on OS preference change when there's no explicit
    // override. If the user picks an explicit mode via a toggle, the
    // change is ignored until they switch back to 'auto'.
    if (stored !== 'light' && stored !== 'dark') {
      try {
        const mq2 = window.matchMedia('(prefers-color-scheme: dark)');
        const handler = () => {
          // Re-read storage in case the toggle wrote since boot.
          try { stored = localStorage.getItem(KEY) || ''; } catch (_) {}
          apply();
        };
        if (mq2.addEventListener) mq2.addEventListener('change', handler);
        else if (mq2.addListener) mq2.addListener(handler); // Safari < 14
      } catch (_) { /* no media query */ }
    }
    // Public API: window.__gofastr_colorScheme.set('auto' | 'light' | 'dark').
    // Apps wire their theme toggle here.
    window.__gofastr_colorScheme = {
      get: () => {
        try { return localStorage.getItem(KEY) || 'auto'; }
        catch (_) { return 'auto'; }
      },
      set: (mode) => {
        if (mode !== 'auto' && mode !== 'light' && mode !== 'dark') return;
        try {
          if (mode === 'auto') localStorage.removeItem(KEY);
          else localStorage.setItem(KEY, mode);
          stored = mode === 'auto' ? '' : mode;
        } catch (_) {}
        apply();
      },
    };
  } catch (_) { /* SSR / non-browser */ }
})();
