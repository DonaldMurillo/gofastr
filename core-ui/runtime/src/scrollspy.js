// ScrollSpy runtime module — for every [data-fui-scrollspy] wrapper,
// walk the contained anchors, map href="#id" → target element under
// the observed region, and use IntersectionObserver to set
// aria-current + .is-active on the anchor whose target is in the
// upper half of the viewport.
//
// Loaded on-demand when a [data-fui-scrollspy] element appears.
(() => {
  'use strict';

  // Per-wrap IntersectionObserver. WeakMap keys by element so GC
  // collects naturally when a wrap is removed from the DOM AND we
  // no longer hold a strong reference; a separate Set keeps strong
  // refs for the navigate-time teardown sweep.
  const observers = new WeakMap();
  const activeWraps = new Set();

  const setupOne = (wrap) => {
    if (observers.has(wrap)) return; // idempotent
    const observeSel = wrap.getAttribute('data-fui-scrollspy');
    if (!observeSel) return;
    const root = document.querySelector(observeSel);
    if (!root) return;
    const targetSel = wrap.getAttribute('data-fui-scrollspy-target') || 'h2[id], h3[id]';

    // Build href → anchor lookup. Only anchors that point to in-page
    // ids inside the observed region participate.
    const anchors = wrap.querySelectorAll('a[href^="#"]');
    if (anchors.length === 0) return;
    const anchorByID = {};
    const targets = [];
    for (const a of anchors) {
      const id = a.getAttribute('href').slice(1);
      if (!id) continue;
      const t = root.querySelector('#' + cssEscape(id));
      if (!t) continue;
      anchorByID[id] = a;
      targets.push(t);
    }
    if (targets.length === 0) return;
    // Filter: also accept caller-supplied targetSel as an additional
    // source so non-heading sections work.
    if (targetSel !== 'h2[id], h3[id]') {
      for (const t of root.querySelectorAll(targetSel)) {
        if (!t.id) continue;
        if (!anchorByID[t.id]) continue;
        if (targets.indexOf(t) === -1) targets.push(t);
      }
    }

    const clearActive = () => {
      for (const a of wrap.querySelectorAll('a.is-active')) {
        a.classList.remove('is-active');
        a.removeAttribute('aria-current');
      }
    };
    const markActive = (id) => {
      clearActive();
      const a = anchorByID[id];
      if (a) {
        a.classList.add('is-active');
        a.setAttribute('aria-current', 'true');
      }
    };

    const observer = new IntersectionObserver((entries) => {
      // Among the entries currently intersecting, pick the one
      // whose top is closest to the rootMargin top (most "in view").
      const hits = entries.filter((e) => e.isIntersecting);
      if (hits.length === 0) return;
      hits.sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top);
      markActive(hits[0].target.id);
    }, { rootMargin: '0px 0px -70% 0px', threshold: 0 });

    // Sort targets by document order so the bootstrap's "first target
    // above midline" loop picks the DOM-topmost section, not whatever
    // the nav happened to list first. Without this, reversed-nav
    // layouts seed the wrong active anchor on page-land.
    targets.sort((a, b) => {
      const pos = a.compareDocumentPosition(b);
      if (pos & 4 /* Node.DOCUMENT_POSITION_FOLLOWING */) return -1;
      if (pos & 2 /* Node.DOCUMENT_POSITION_PRECEDING */) return 1;
      return 0;
    });
    for (const t of targets) observer.observe(t);
    observers.set(wrap, observer);
    activeWraps.add(wrap);

    // Bootstrap an initial active anchor — if the user lands on the
    // page before any target has crossed into the upper 30% (e.g. all
    // sections start below a tall page header), the IO never fires
    // and no anchor would be highlighted. Pick the first target whose
    // top is above the viewport midline; otherwise default to the
    // first target so something is always marked.
    requestAnimationFrame(() => {
      if (wrap.querySelector('a.is-active')) return; // IO already won
      const midY = window.innerHeight * 0.5;
      let picked = null;
      for (const t of targets) {
        const r = t.getBoundingClientRect();
        if (r.top <= midY) picked = t;
        else break;
      }
      if (!picked) picked = targets[0];
      markActive(picked.id);
    });
  };

  // Tiny CSS.escape polyfill — id may contain odd chars or start
  // with a digit. Per CSS spec, an identifier starting with a digit
  // must escape that digit as `\3<digit><space>` so the selector
  // parses. Modern browsers ship CSS.escape; the fallback covers
  // older or test-instrumented environments where it's missing.
  const cssEscape = (s) => {
    if (window.CSS && CSS.escape) return CSS.escape(s);
    let out = '';
    if (s.length > 0 && s.charCodeAt(0) >= 48 && s.charCodeAt(0) <= 57) {
      out = '\\3' + s.charAt(0) + ' ';
      s = s.slice(1);
    }
    return out + s.replace(/([!"#$%&'()*+,./:;<=>?@[\]^`{|}~])/g, '\\$1');
  };

  const scan = (root) => {
    const scope = root && root.querySelectorAll ? root : document;
    for (const wrap of scope.querySelectorAll('[data-fui-scrollspy]')) setupOne(wrap);
  };

  requestAnimationFrame(() => scan(document));
  document.addEventListener('gofastr:navigate', () => {
    // SPA navigation replaces the page DOM. Disconnect every active
    // observer before re-scanning so the old observers + their
    // strong references to (now-detached) target elements release.
    for (const w of activeWraps) {
      const ob = observers.get(w);
      if (ob) ob.disconnect();
      observers.delete(w);
    }
    activeWraps.clear();
    requestAnimationFrame(() => scan(document));
  });

  window.__gofastr = window.__gofastr || {};
  window.__gofastr.scrollspy = { rescan: scan };
})();
