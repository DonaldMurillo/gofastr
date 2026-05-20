// ScrollSpy runtime module — for every [data-fui-scrollspy] wrapper,
// walk the contained anchors, map href="#id" → target element under
// the observed region, and use IntersectionObserver to set
// aria-current + .is-active on the anchor whose target is in the
// upper half of the viewport.
//
// Loaded on-demand when a [data-fui-scrollspy] element appears.
(function () {
  'use strict';

  // Per-wrap IntersectionObserver. WeakMap keys by element so GC
  // collects naturally when a wrap is removed from the DOM AND we
  // no longer hold a strong reference; a separate Set keeps strong
  // refs for the navigate-time teardown sweep.
  var observers = new WeakMap();
  var activeWraps = new Set();

  function setupOne(wrap) {
    if (observers.has(wrap)) return; // idempotent
    var observeSel = wrap.getAttribute('data-fui-scrollspy');
    if (!observeSel) return;
    var root = document.querySelector(observeSel);
    if (!root) return;
    var targetSel = wrap.getAttribute('data-fui-scrollspy-target') || 'h2[id], h3[id]';

    // Build href → anchor lookup. Only anchors that point to in-page
    // ids inside the observed region participate.
    var anchors = wrap.querySelectorAll('a[href^="#"]');
    if (anchors.length === 0) return;
    var anchorByID = {};
    var targets = [];
    anchors.forEach(function (a) {
      var id = a.getAttribute('href').slice(1);
      if (!id) return;
      var t = root.querySelector('#' + cssEscape(id));
      if (!t) return;
      anchorByID[id] = a;
      targets.push(t);
    });
    if (targets.length === 0) return;
    // Filter: also accept caller-supplied targetSel as an additional
    // source so non-heading sections work.
    if (targetSel !== 'h2[id], h3[id]') {
      root.querySelectorAll(targetSel).forEach(function (t) {
        if (!t.id) return;
        if (!anchorByID[t.id]) return;
        if (targets.indexOf(t) === -1) targets.push(t);
      });
    }

    function clearActive() {
      wrap.querySelectorAll('a.is-active').forEach(function (a) {
        a.classList.remove('is-active');
        a.removeAttribute('aria-current');
      });
    }
    function markActive(id) {
      clearActive();
      var a = anchorByID[id];
      if (a) {
        a.classList.add('is-active');
        a.setAttribute('aria-current', 'true');
      }
    }

    var observer = new IntersectionObserver(function (entries) {
      // Among the entries currently intersecting, pick the one
      // whose top is closest to the rootMargin top (most "in view").
      var hits = entries.filter(function (e) { return e.isIntersecting; });
      if (hits.length === 0) return;
      hits.sort(function (a, b) {
        return a.boundingClientRect.top - b.boundingClientRect.top;
      });
      markActive(hits[0].target.id);
    }, { rootMargin: '0px 0px -70% 0px', threshold: 0 });

    // Sort targets by document order so the bootstrap's "first target
    // above midline" loop picks the DOM-topmost section, not whatever
    // the nav happened to list first. Without this, reversed-nav
    // layouts seed the wrong active anchor on page-land.
    targets.sort(function (a, b) {
      var pos = a.compareDocumentPosition(b);
      if (pos & 4 /* Node.DOCUMENT_POSITION_FOLLOWING */) return -1;
      if (pos & 2 /* Node.DOCUMENT_POSITION_PRECEDING */) return 1;
      return 0;
    });
    targets.forEach(function (t) { observer.observe(t); });
    observers.set(wrap, observer);
    activeWraps.add(wrap);

    // Bootstrap an initial active anchor — if the user lands on the
    // page before any target has crossed into the upper 30% (e.g. all
    // sections start below a tall page header), the IO never fires
    // and no anchor would be highlighted. Pick the first target whose
    // top is above the viewport midline; otherwise default to the
    // first target so something is always marked.
    requestAnimationFrame(function () {
      if (wrap.querySelector('a.is-active')) return; // IO already won
      var midY = window.innerHeight * 0.5;
      var picked = null;
      for (var i = 0; i < targets.length; i++) {
        var r = targets[i].getBoundingClientRect();
        if (r.top <= midY) picked = targets[i];
        else break;
      }
      if (!picked) picked = targets[0];
      markActive(picked.id);
    });
  }

  // Tiny CSS.escape polyfill — id may contain odd chars or start
  // with a digit. Per CSS spec, an identifier starting with a digit
  // must escape that digit as `\3<digit><space>` so the selector
  // parses. Modern browsers ship CSS.escape; the fallback covers
  // older or test-instrumented environments where it's missing.
  function cssEscape(s) {
    if (window.CSS && CSS.escape) return CSS.escape(s);
    var out = '';
    if (s.length > 0 && s.charCodeAt(0) >= 48 && s.charCodeAt(0) <= 57) {
      out = '\\3' + s.charAt(0) + ' ';
      s = s.slice(1);
    }
    return out + s.replace(/([!"#$%&'()*+,./:;<=>?@[\]^`{|}~])/g, '\\$1');
  }

  function scan(root) {
    var scope = root && root.querySelectorAll ? root : document;
    scope.querySelectorAll('[data-fui-scrollspy]').forEach(setupOne);
  }

  requestAnimationFrame(function () { scan(document); });
  document.addEventListener('gofastr:navigate', function () {
    // SPA navigation replaces the page DOM. Disconnect every active
    // observer before re-scanning so the old observers + their
    // strong references to (now-detached) target elements release.
    activeWraps.forEach(function (w) {
      var ob = observers.get(w);
      if (ob) ob.disconnect();
      observers.delete(w);
    });
    activeWraps.clear();
    requestAnimationFrame(function () { scan(document); });
  });

  window.__gofastr = window.__gofastr || {};
  window.__gofastr.scrollspy = { rescan: scan };
})();
