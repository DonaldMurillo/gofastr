// Reveal runtime module, uses IntersectionObserver to animate elements
// into view when they scroll into the viewport. One-shot: once revealed,
// the element stays visible.
//
// Setup:
//   - Elements with [data-cui-reveal] get class "cui-hidden" immediately
//   - When the element intersects the viewport, "cui-hidden" is removed
//     and "cui-revealed" + "cui-reveal-<type>" are added
//   - The attribute value is the animation type: data-cui-reveal="fade-up"
//     adds class "cui-reveal-fade-up"
//
// Loaded on-demand when a [data-cui-reveal] element appears.
(() => {
  'use strict';

  const REVEAL_ATTR = 'data-cui-reveal';
  const HIDDEN_CLASS = 'cui-hidden';
  const REVEALED_CLASS = 'cui-revealed';

  const observer = new IntersectionObserver((entries) => {
    for (const entry of entries) {
      if (!entry.isIntersecting) continue;
      const el = entry.target;
      const type = el.getAttribute(REVEAL_ATTR) || 'fade-in';

      el.classList.remove(HIDDEN_CLASS);
      el.classList.add(REVEALED_CLASS);
      el.classList.add('cui-reveal-' + type);

      observer.unobserve(el);
    }
  }, { threshold: 0.1 });

  const setupOne = (el) => {
    if (el.classList.contains(REVEALED_CLASS)) return; // already revealed
    if (el.dataset._cuiRevealObserved) return;         // idempotent
    el.dataset._cuiRevealObserved = '1';

    el.classList.add(HIDDEN_CLASS);
    observer.observe(el);
  };

  const scan = (root) => {
    if (root.matches && root.matches('[' + REVEAL_ATTR + ']')) {
      setupOne(root);
    }
    const els = root.querySelectorAll('[' + REVEAL_ATTR + ']');
    for (const el of els) setupOne(el);
  };

  // Initial scan
  scan(document);

  // SPA re-wire
  window.addEventListener('gofastr:navigate', () => {
    scan(document);
  });

  // Register module
  window.__gofastr = window.__gofastr || {};
  (window.__gofastr.loadedModules = window.__gofastr.loadedModules || {}).reveal = true;
  (window.__gofastr._moduleScanners = window.__gofastr._moduleScanners || {}).reveal = scan;
})();
