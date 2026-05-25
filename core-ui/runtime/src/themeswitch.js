// ThemeToggle runtime module.
//
// Registers a delegated click handler on [data-fui-theme-toggle] that
// cycles the color scheme and persists the choice to localStorage.
// The colorscheme.js bootstrap picks up the change and swaps
// data-color-scheme on <html> immediately.
//
// Loaded on demand via __gofastr.loadModule("themeswitch").
(() => {
  'use strict';
  if (!window.__gofastr_colorScheme) return;

  const KEY = 'gofastr.colorScheme';
  const CYCLE = ['dark', 'light', 'auto'];

  const currentScheme = () => {
    let s = '';
    try { s = localStorage.getItem(KEY) || ''; }
    catch (_) { return 'auto'; }
    if (s === 'light' || s === 'dark') return s;
    return 'auto';
  };

  const syncPillState = (root) => {
    const scheme = currentScheme();
    for (const btn of root.querySelectorAll('[data-fui-theme-toggle-opt]')) {
      const opt = btn.getAttribute('data-fui-theme-toggle-opt');
      btn.setAttribute('aria-checked', opt === scheme ? 'true' : 'false');
    }
  };

  // Sync all pill instances on load
  const syncAllPills = () => {
    for (const pill of document.querySelectorAll('[data-fui-theme-toggle="pill"]')) {
      syncPillState(pill);
    }
  };

  document.addEventListener('click', (e) => {
    // Pill option click
    const opt = e.target.closest('[data-fui-theme-toggle-opt]');
    if (opt) {
      const pill = opt.closest('[data-fui-theme-toggle="pill"]');
      if (pill) {
        e.preventDefault();
        const mode = opt.getAttribute('data-fui-theme-toggle-opt');
        window.__gofastr_colorScheme.set(mode);
        syncPillState(pill);
        return;
      }
    }

    // Single button click (icon/label) — cycle dark → light → auto
    const btn = e.target.closest('[data-fui-theme-toggle]');
    if (!btn || btn.getAttribute('data-fui-theme-toggle') === 'pill') return;
    e.preventDefault();
    const cur = currentScheme();
    const idx = CYCLE.indexOf(cur);
    const next = CYCLE[(idx + 1) % CYCLE.length];
    window.__gofastr_colorScheme.set(next);
    syncAllPills();
  });

  // Initial sync for pills already in the DOM
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', syncAllPills);
  } else {
    syncAllPills();
  }

  // Signal to the module loader that this module is loaded.
  (window.__gofastr.loadedModules = window.__gofastr.loadedModules || {}).themeswitch = true;
})();
