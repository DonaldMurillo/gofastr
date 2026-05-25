// BackToTop runtime module.
//
// Uses a single IntersectionObserver on a shared sentinel element
// to toggle button visibility when the user scrolls past the
// configured threshold. On click, scrolls to top (or to
// data-fui-btt-target).
//
// Loaded on demand via __gofastr.loadModule("backtotop").
(() => {
  'use strict';

  let _observer = null;
  let _sentinel = null;
  let _buttons = [];
  let _rafPending = false;
  let _scrolling = false;

  // Remove buttons that are no longer in the DOM.
  const purgeStale = () => {
    _buttons = _buttons.filter((b) => b.isConnected);
  };

  // Debounced visibility toggle — coalesces rapid IntersectionObserver
  // callbacks (e.g. during smooth scroll) into a single paint.
  const scheduleToggle = (visible) => {
    if (_rafPending) return;
    _rafPending = true;
    requestAnimationFrame(() => {
      _rafPending = false;
      purgeStale();
      for (const btn of _buttons) {
        if (visible) {
          btn.setAttribute('data-fui-btt-visible', '');
          btn.removeAttribute('inert');
        } else {
          btn.removeAttribute('data-fui-btt-visible');
          btn.setAttribute('inert', '');
        }
      }
    });
  };

  // Lazily create a shared sentinel + observer on first wire().
  const ensureObserver = () => {
    if (_observer) return;

    _sentinel = document.createElement('div');
    _sentinel.setAttribute('aria-hidden', 'true');
    _sentinel.className = 'ui-btt-sentinel';
    document.body.appendChild(_sentinel);

    _observer = new IntersectionObserver((entries) => {
      // All entries refer to the same sentinel; use the first.
      const visible = entries.length > 0 && !entries[0].isIntersecting;
      scheduleToggle(visible);
    }, { rootMargin: '0px', threshold: 0 });
    _observer.observe(_sentinel);
  };

  const wire = (btn) => {
    if (btn.__bttWired) return;
    btn.__bttWired = true;

    const scrollBehavior = btn.getAttribute('data-fui-btt-scroll') === 'instant' ? 'instant' : 'smooth';
    const scrollTarget = btn.getAttribute('data-fui-btt-target') || '';
    const threshold = parseInt(btn.getAttribute('data-fui-btt-threshold') || '400', 10);

    _buttons.push(btn);

    // Update sentinel height to the max threshold across all buttons.
    if (_sentinel && threshold > _sentinel.offsetHeight) {
      _sentinel.style.height = threshold + 'px';
    }

    btn.addEventListener('click', () => {
      // Prevent double-trigger during ongoing smooth scroll.
      if (_scrolling) return;
      _scrolling = true;

      const onDone = () => { _scrolling = false; };

      if (scrollTarget) {
        const el = document.querySelector(scrollTarget);
        if (el) {
          el.scrollIntoView({ behavior: scrollBehavior, block: 'start' });
        }
      } else {
        window.scrollTo({ top: 0, behavior: scrollBehavior });
      }

      // For smooth scroll, allow re-click after animation completes.
      // Use a timeout as a fallback; scrollend event is the preferred
      // signal but isn't available in all browsers.
      if (scrollBehavior === 'smooth') {
        if ('onscrollend' in window) {
          const handler = () => {
            window.removeEventListener('scrollend', handler);
            onDone();
          };
          window.addEventListener('scrollend', handler);
        } else {
          // Fallback: detect when scroll position stops changing.
          let lastY = window.scrollY;
          let checks = 0;
          const poll = () => {
            const y = window.scrollY;
            if (y === lastY || y <= 0 || checks > 60) {
              onDone();
              return;
            }
            lastY = y;
            checks++;
            requestAnimationFrame(poll);
          };
          requestAnimationFrame(poll);
        }
      } else {
        onDone();
      }
    });
  };

  const rescaleSentinel = () => {
    let maxH = 0;
    for (const btn of _buttons) {
      const t = parseInt(btn.getAttribute('data-fui-btt-threshold') || '400', 10);
      if (t > maxH) maxH = t;
    }
    if (_sentinel) _sentinel.style.height = maxH + 'px';
  };

  const init = (root) => {
    ensureObserver();
    const btns = (root || document).querySelectorAll('[data-fui-back-to-top]');
    for (const btn of btns) {
      wire(btn);
    }
    rescaleSentinel();
  };

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', () => init(document));
  } else {
    init(document);
  }

  // Register SPA rescan handler so newly swapped content gets wired.
  if (window.__gofastr) {
    window.__gofastr._moduleScanners = window.__gofastr._moduleScanners || {};
    window.__gofastr._moduleScanners['backtotop'] = (root) => {
      purgeStale();
      init(root);
    };
  }

  (window.__gofastr.loadedModules = window.__gofastr.loadedModules || {}).backtotop = true;
})();
