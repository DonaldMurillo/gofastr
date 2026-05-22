// ThemeToggle runtime module.
//
// Registers a delegated click handler on [data-fui-theme-toggle] that
// cycles the color scheme and persists the choice to localStorage.
// The colorscheme.js bootstrap picks up the change and swaps
// data-color-scheme on <html> immediately.
//
// Loaded on demand via __gofastr.loadModule("themeswitch").
(function () {
  'use strict';
  if (!window.__gofastr_colorScheme) return;

  var KEY = 'gofastr.colorScheme';
  var CYCLE = ['dark', 'light', 'auto'];

  function currentScheme() {
    try { var s = localStorage.getItem(KEY) || ''; }
    catch (_) { return 'auto'; }
    if (s === 'light' || s === 'dark') return s;
    return 'auto';
  }

  function syncPillState(root) {
    var scheme = currentScheme();
    var btns = root.querySelectorAll('[data-fui-theme-toggle-opt]');
    for (var i = 0; i < btns.length; i++) {
      var opt = btns[i].getAttribute('data-fui-theme-toggle-opt');
      btns[i].setAttribute('aria-checked', opt === scheme ? 'true' : 'false');
    }
  }

  // Sync all pill instances on load
  function syncAllPills() {
    var pills = document.querySelectorAll('[data-fui-theme-toggle="pill"]');
    for (var i = 0; i < pills.length; i++) syncPillState(pills[i]);
  }

  document.addEventListener('click', function (e) {
    // Pill option click
    var opt = e.target.closest('[data-fui-theme-toggle-opt]');
    if (opt) {
      var pill = opt.closest('[data-fui-theme-toggle="pill"]');
      if (pill) {
        e.preventDefault();
        var mode = opt.getAttribute('data-fui-theme-toggle-opt');
        window.__gofastr_colorScheme.set(mode);
        syncPillState(pill);
        return;
      }
    }

    // Single button click (icon/label) — cycle dark → light → auto
    var btn = e.target.closest('[data-fui-theme-toggle]');
    if (!btn || btn.getAttribute('data-fui-theme-toggle') === 'pill') return;
    e.preventDefault();
    var cur = currentScheme();
    var idx = CYCLE.indexOf(cur);
    var next = CYCLE[(idx + 1) % CYCLE.length];
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
