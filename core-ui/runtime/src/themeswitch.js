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
    // Flip the EFFECTIVE displayed scheme so a click always visibly toggles
    // light<->dark. (Cycling the stored preference dark→light→auto made the
    // first click a no-op when the stored value was 'auto' and the OS already
    // showed the next step.)
    const effective = document.documentElement.getAttribute('data-color-scheme') === 'dark' ? 'dark' : 'light';
    window.__gofastr_colorScheme.set(effective === 'dark' ? 'light' : 'dark');
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
