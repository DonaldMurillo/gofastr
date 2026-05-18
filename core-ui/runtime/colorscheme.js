// GoFastr color-scheme bootstrap.
//
// Runs synchronously at the TOP of <head>, before any CSS parses, so
// dark-mode tokens take effect during the same first paint. Reads,
// in order:
//
//   1. localStorage["gofastr.colorScheme"] — explicit user choice
//      (auto | light | dark). Apps that ship a theme toggle write
//      here.
//   2. window.matchMedia('(prefers-color-scheme: dark)') — OS hint.
//
// Sets <html data-color-scheme="dark|light"> + the matching
// `<meta name="color-scheme">` so native UA controls (scrollbars,
// form inputs) follow suit.
//
// Listens for OS preference changes when the stored mode is "auto"
// or unset.
(function () {
  'use strict';
  try {
    var KEY = 'gofastr.colorScheme';
    var stored = '';
    try { stored = localStorage.getItem(KEY) || ''; } catch (_) {}
    var apply = function () {
      var mode = stored;
      if (mode !== 'light' && mode !== 'dark') {
        var mq = window.matchMedia('(prefers-color-scheme: dark)');
        mode = mq && mq.matches ? 'dark' : 'light';
      }
      document.documentElement.setAttribute('data-color-scheme', mode);
      // Also set the native color-scheme meta so UA-rendered controls
      // (scrollbars, native datepickers, form inputs) follow.
      var meta = document.querySelector('meta[name="color-scheme"]');
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
        var mq2 = window.matchMedia('(prefers-color-scheme: dark)');
        var handler = function () {
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
      get: function () {
        try { return localStorage.getItem(KEY) || 'auto'; }
        catch (_) { return 'auto'; }
      },
      set: function (mode) {
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
