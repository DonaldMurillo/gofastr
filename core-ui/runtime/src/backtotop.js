// BackToTop runtime module.
//
// Uses a single IntersectionObserver on a shared sentinel element
// to toggle button visibility when the user scrolls past the
// configured threshold. On click, scrolls to top (or to
// data-fui-btt-target).
//
// Loaded on demand via __gofastr.loadModule("backtotop").
(function () {
  'use strict';

  var _observer = null;
  var _sentinel = null;
  var _buttons = [];
  var _rafPending = false;

  // Remove buttons that are no longer in the DOM.
  function purgeStale() {
    _buttons = _buttons.filter(function (b) { return b.isConnected; });
  }

  // Debounced visibility toggle — coalesces rapid IntersectionObserver
  // callbacks (e.g. during smooth scroll) into a single paint.
  function scheduleToggle(visible) {
    if (_rafPending) return;
    _rafPending = true;
    requestAnimationFrame(function () {
      _rafPending = false;
      purgeStale();
      for (var k = 0; k < _buttons.length; k++) {
        var btn = _buttons[k];
        if (visible) {
          btn.setAttribute('data-fui-btt-visible', '');
          btn.setAttribute('aria-hidden', 'false');
        } else {
          btn.removeAttribute('data-fui-btt-visible');
          btn.setAttribute('aria-hidden', 'true');
        }
      }
    });
  }

  // Lazily create a shared sentinel + observer on first wire().
  function ensureObserver() {
    if (_observer) return;

    _sentinel = document.createElement('div');
    _sentinel.setAttribute('aria-hidden', 'true');
    _sentinel.className = 'ui-btt-sentinel';
    document.body.appendChild(_sentinel);

    _observer = new IntersectionObserver(function (entries) {
      // All entries refer to the same sentinel; use the first.
      var visible = entries.length > 0 && !entries[0].isIntersecting;
      scheduleToggle(visible);
    }, { rootMargin: '0px', threshold: 0 });
    _observer.observe(_sentinel);
  }

  function wire(btn) {
    if (btn.__bttWired) return;
    btn.__bttWired = true;

    var scrollBehavior = btn.getAttribute('data-fui-btt-scroll') === 'instant' ? 'instant' : 'smooth';
    var scrollTarget = btn.getAttribute('data-fui-btt-target') || '';
    var threshold = parseInt(btn.getAttribute('data-fui-btt-threshold') || '400', 10);

    _buttons.push(btn);

    // Update sentinel height to the max threshold across all buttons.
    if (_sentinel && threshold > _sentinel.offsetHeight) {
      _sentinel.style.height = threshold + 'px';
    }

    btn.addEventListener('click', function () {
      if (scrollTarget) {
        var el = document.querySelector(scrollTarget);
        if (el) {
          el.scrollIntoView({ behavior: scrollBehavior, block: 'start' });
        }
      } else {
        window.scrollTo({ top: 0, behavior: scrollBehavior });
      }
    });
  }

  function rescaleSentinel() {
    var maxH = 0;
    for (var k = 0; k < _buttons.length; k++) {
      var t = parseInt(_buttons[k].getAttribute('data-fui-btt-threshold') || '400', 10);
      if (t > maxH) maxH = t;
    }
    if (_sentinel) _sentinel.style.height = maxH + 'px';
  }

  function init(root) {
    ensureObserver();
    var btns = (root || document).querySelectorAll('[data-fui-back-to-top]');
    for (var i = 0; i < btns.length; i++) {
      wire(btns[i]);
    }
    rescaleSentinel();
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', function () { init(document); });
  } else {
    init(document);
  }

  // Register SPA rescan handler so newly swapped content gets wired.
  if (window.__gofastr) {
    window.__gofastr._moduleScanners = window.__gofastr._moduleScanners || {};
    window.__gofastr._moduleScanners['backtotop'] = function (root) {
      purgeStale();
      init(root);
    };
  }

  (window.__gofastr.loadedModules = window.__gofastr.loadedModules || {}).backtotop = true;
})();
