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

  // Lazily create a shared sentinel + observer on first wire().
  function ensureObserver() {
    if (_observer) return;

    _sentinel = document.createElement('div');
    _sentinel.setAttribute('aria-hidden', 'true');
    _sentinel.className = 'ui-btt-sentinel';
    document.body.appendChild(_sentinel);

    _observer = new IntersectionObserver(function (entries) {
      for (var j = 0; j < entries.length; j++) {
        var visible = !entries[j].isIntersecting;
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
      }
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

  function init(root) {
    ensureObserver();
    var btns = (root || document).querySelectorAll('[data-fui-back-to-top]');
    for (var i = 0; i < btns.length; i++) {
      wire(btns[i]);
    }
    // Re-size sentinel based on max threshold.
    var maxH = 0;
    for (var k = 0; k < _buttons.length; k++) {
      var t = parseInt(_buttons[k].getAttribute('data-fui-btt-threshold') || '400', 10);
      if (t > maxH) maxH = t;
    }
    if (_sentinel) _sentinel.style.height = maxH + 'px';
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', function () { init(document); });
  } else {
    init(document);
  }

  // Re-scan after SPA page swaps.
  if (window.__gofastr && window.__gofastr.onPageSwap) {
    window.__gofastr.onPageSwap(function (root) { init(root); });
  }

  (window.__gofastr.loadedModules = window.__gofastr.loadedModules || {}).backtotop = true;
})();
