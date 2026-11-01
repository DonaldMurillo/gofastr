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

  // painted: the element has already been on screen. The module loads
  // after first paint, so at the initial scan an element inside the
  // viewport was painted visible; hiding it to fade it back in is a
  // blink, so it is left as it is. Content scanned later (a navigation
  // swap, an inserted island) is hidden before it paints and keeps the
  // entrance.
  const onScreen = (el) => {
    const r = el.getBoundingClientRect();
    return r.bottom > 0 && r.right > 0 && r.top < innerHeight && r.left < innerWidth;
  };
  // The observed set is in memory, not an attribute: a cached screen
  // replays a copy of the markup, and a copy carrying an "observed"
  // attribute would never be observed again and stay hidden.
  const observed = new WeakSet();
  const setupOne = (el, painted) => {
    if (el.classList.contains(REVEALED_CLASS)) return; // already revealed
    if (observed.has(el)) return;                      // idempotent
    observed.add(el);
    if (painted && onScreen(el)) {
      el.classList.add(REVEALED_CLASS);
      return;
    }

    el.classList.add(HIDDEN_CLASS);
    observer.observe(el);
  };

  const scan = (root, painted) => {
    if (root.matches && root.matches('[' + REVEAL_ATTR + ']')) {
      setupOne(root, painted);
    }
    const els = root.querySelectorAll('[' + REVEAL_ATTR + ']');
    for (const el of els) setupOne(el, painted);
  };

  // Initial scan: the document has painted by now.
  scan(document, true);

  // SPA re-wire
  window.addEventListener('gofastr:navigate', () => {
    scan(document);
  });

  // Register module
  window.__gofastr = window.__gofastr || {};
  (window.__gofastr.loadedModules = window.__gofastr.loadedModules || {}).reveal = true;
  (window.__gofastr._moduleScanners = window.__gofastr._moduleScanners || {}).reveal = (root) => scan(root, false);
})();
