// BackToTop runtime module.
//
// Uses IntersectionObserver on a sentinel element to toggle the
// button's visibility when the user scrolls past the configured
// threshold. On click, scrolls to top (or to data-fui-btt-target).
//
// Loaded on demand via __gofastr.loadModule("backtotop").
(function () {
  'use strict';

  function init(root) {
    var btns = (root || document).querySelectorAll('[data-fui-back-to-top]');
    for (var i = 0; i < btns.length; i++) {
      wire(btns[i]);
    }
  }

  function wire(btn) {
    if (btn.__bttWired) return;
    btn.__bttWired = true;

    var threshold = parseInt(btn.getAttribute('data-fui-btt-threshold') || '400', 10);
    var scrollBehavior = btn.getAttribute('data-fui-btt-scroll') === 'instant' ? 'instant' : 'smooth';
    var scrollTarget = btn.getAttribute('data-fui-btt-target') || '';

    // Sentinel element at the top of the document. When it leaves
    // the viewport (user scrolled past threshold), button appears.
    var sentinel = document.createElement('div');
    sentinel.setAttribute('aria-hidden', 'true');
    sentinel.style.cssText = 'position:absolute;top:0;left:0;width:0;height:' + threshold + 'px;pointer-events:none;';
    document.body.appendChild(sentinel);

    var observer = new IntersectionObserver(function (entries) {
      for (var j = 0; j < entries.length; j++) {
        if (!entries[j].isIntersecting) {
          btn.setAttribute('data-fui-btt-visible', '');
          btn.setAttribute('aria-hidden', 'false');
        } else {
          btn.removeAttribute('data-fui-btt-visible');
          btn.setAttribute('aria-hidden', 'true');
        }
      }
    }, { rootMargin: '0px', threshold: 0 });
    observer.observe(sentinel);

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
