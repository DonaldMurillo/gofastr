// AnimatedCounter runtime module — ticks the displayed value from
// data-fui-animated-counter-from to data-fui-animated-counter over
// data-fui-animated-counter-ms. Fires once per element on first
// IntersectionObserver hit.
//
// Respects prefers-reduced-motion: when set, the module is a no-op —
// the SSR-rendered target value is what the user sees.
(function () {
  'use strict';

  const prefersReduced = window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  if (prefersReduced) return;

  function easeOutCubic(t) { return 1 - Math.pow(1 - t, 3); }

  function animate(el) {
    if (el.dataset.fuiAnimatedCounterDone === '1') return;
    el.dataset.fuiAnimatedCounterDone = '1';
    const to = parseInt(el.getAttribute('data-fui-animated-counter') || '0', 10);
    const from = parseInt(el.getAttribute('data-fui-animated-counter-from') || '0', 10);
    const dur = parseInt(el.getAttribute('data-fui-animated-counter-ms') || '1200', 10);
    if (!Number.isFinite(to) || dur <= 0) return;
    const valueEl = el.querySelector('.ui-animated-counter__value');
    if (!valueEl) return;
    const start = performance.now();
    function tick(now) {
      const t = Math.min(1, (now - start) / dur);
      const cur = Math.round(from + (to - from) * easeOutCubic(t));
      valueEl.textContent = String(cur);
      if (t < 1) requestAnimationFrame(tick);
      else valueEl.textContent = String(to); // ensure exact final
    }
    requestAnimationFrame(tick);
  }

  // IntersectionObserver — animate when scrolled into view.
  let observer = null;
  function ensureObserver() {
    if (observer) return observer;
    observer = new IntersectionObserver(function (entries) {
      entries.forEach(function (entry) {
        if (entry.isIntersecting) {
          animate(entry.target);
          observer.unobserve(entry.target);
        }
      });
    }, { rootMargin: '0px', threshold: 0.4 });
    return observer;
  }

  function scan(root) {
    const scope = root && root.querySelectorAll ? root : document;
    const obs = ensureObserver();
    scope.querySelectorAll('[data-fui-animated-counter]').forEach(function (el) {
      if (el.dataset.fuiAnimatedCounterDone === '1') return;
      obs.observe(el);
    });
  }

  scan(document);
  document.addEventListener('gofastr:navigate', function () { scan(document); });

  window.__gofastr = window.__gofastr || {};
  window.__gofastr.animatedCounter = { rescan: scan };
})();
