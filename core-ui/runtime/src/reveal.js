// Reveal runtime module — uses IntersectionObserver to animate elements
// into view when they scroll into the viewport. One-shot: once revealed,
// the element stays visible.
//
// Setup:
//   - Elements with [data-fui-reveal] get class "fui-hidden" immediately
//   - When the element intersects the viewport, "fui-hidden" is removed
//     and "fui-revealed" + "fui-reveal-<type>" are added
//   - The attribute value is the animation type: data-fui-reveal="fade-up"
//     adds class "fui-reveal-fade-up"
//
// Loaded on-demand when a [data-fui-reveal] element appears.
(() => {
  'use strict';

  const REVEAL_ATTR = 'data-fui-reveal';
  const HIDDEN_CLASS = 'fui-hidden';
  const REVEALED_CLASS = 'fui-revealed';

  const observer = new IntersectionObserver((entries) => {
    for (const entry of entries) {
      if (!entry.isIntersecting) continue;
      const el = entry.target;
      const type = el.getAttribute(REVEAL_ATTR) || 'fade-in';

      el.classList.remove(HIDDEN_CLASS);
      el.classList.add(REVEALED_CLASS);
      el.classList.add('fui-reveal-' + type);

      observer.unobserve(el);
    }
  }, { threshold: 0.1 });

  const setupOne = (el) => {
    if (el.classList.contains(REVEALED_CLASS)) return; // already revealed
    if (el.dataset._fuiRevealObserved) return;         // idempotent
    el.dataset._fuiRevealObserved = '1';

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
  requestAnimationFrame(() => scan(document));

  // SPA re-wire
  document.addEventListener('gofastr:navigate', () => {
    requestAnimationFrame(() => scan(document));
  });

  // Register module
  window.__gofastr = window.__gofastr || {};
  (window.__gofastr.loadedModules = window.__gofastr.loadedModules || {}).reveal = true;
  (window.__gofastr._moduleScanners = window.__gofastr._moduleScanners || {}).reveal = scan;
})();
