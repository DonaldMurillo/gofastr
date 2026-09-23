// headless-rail: the behaviour module for this package's rails and
// tables of contents — the one IntersectionObserver that marks the
// anchor whose target is currently in view. Loaded by the kernel on
// one of its markers, at boot, on insertion, or after a client
// navigation, which hands every inserted subtree and the
// post-navigation document to scan() below.
//
// The active state is aria-current and a class on the link, never a
// style: the stylesheet owns what active looks like, the module owns
// which link is active, and a reader without script keeps every link
// working with nothing marked. A malformed or unmatched selector is a
// safe no-op — the observer simply never arms — because the primitive
// refused the values that could not even reach the browser and the
// browser owns everything past that.
//
// headless-toc declares Requires("headless-rail") and arms its navs
// through the exported NS._huiRailWatch, so there is one observer
// implementation, not two drifting implementations of the same
// scroll-spy.
(function () {
  'use strict';
  const NAME = 'headless-rail';
  const NS = window.__gofastr = window.__gofastr || {};
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;

  // One observer per nav element. WeakMap keys by the element, so a
  // nav removed from the document takes its observer with it when the
  // tree is collected, and a nav handed to scan() twice is armed once.
  const observers = new WeakMap();

  function within(root, sel) {
    const out = [];
    if (root.matches && root.matches(sel)) out.push(root);
    if (root.querySelectorAll) out.push.apply(out, root.querySelectorAll(sel));
    return out;
  }

  // Tiny CSS.escape: an id may start with a digit or carry punctuation,
  // and '#' + id must parse as a selector. Modern browsers ship
  // CSS.escape; the fallback covers instrumented environments.
  function cssEscape(s) {
    if (window.CSS && CSS.escape) return CSS.escape(s);
    let out = '';
    if (s.length > 0 && s.charCodeAt(0) >= 48 && s.charCodeAt(0) <= 57) {
      out = '\\3' + s.charAt(0) + ' ';
      s = s.slice(1);
    }
    return out + s.replace(/([!"#$%&'()*+,./:;<=>?@[\]^`{|}~])/g, '\\$1');
  }

  // watch arms one nav: resolve the anchors it lists, map each to the
  // element its fragment names inside the observed region, and mark
  // the anchor whose target holds the top of the view. Exported for
  // headless-toc, which passes its own selector pair.
  function watch(nav, observeSel, targetSel) {
    if (!nav || observers.has(nav)) return;
    if (!observeSel) return; // a static rail: links only
    let root = null;
    try { root = document.querySelector(observeSel); } catch (_) { return; }
    if (!root) return;
    targetSel = targetSel || 'h2[id], h3[id], h4[id]';

    const anchors = nav.querySelectorAll('a[href^="#"]');
    if (anchors.length === 0) return;
    const anchorByID = {};
    const targets = [];
    for (const a of anchors) {
      const id = a.getAttribute('href').slice(1);
      if (!id || id === '__proto__' || id === 'constructor' || id === 'prototype') continue;
      let t = null;
      try { t = root.querySelector('#' + cssEscape(id)); } catch (_) { t = null; }
      if (!t) continue;
      anchorByID[id] = a;
      targets.push(t);
    }
    if (targets.length === 0) return;
    if (targetSel) {
      let extra = [];
      try { extra = Array.from(root.querySelectorAll(targetSel)); } catch (_) { extra = []; }
      for (const t of extra) {
        if (!t.id || !anchorByID[t.id]) continue;
        if (targets.indexOf(t) === -1) targets.push(t);
      }
    }

    const clearActive = () => {
      for (const a of nav.querySelectorAll('a[aria-current], a.is-active')) {
        a.classList.remove('is-active');
        a.removeAttribute('aria-current');
      }
    };
    const markActive = (id) => {
      clearActive();
      const a = Object.prototype.hasOwnProperty.call(anchorByID, id) ? anchorByID[id] : undefined;
      if (a) {
        a.classList.add('is-active');
        a.setAttribute('aria-current', 'true');
      }
    };

    const observer = new IntersectionObserver((entries) => {
      const hits = entries.filter((e) => e.isIntersecting);
      if (hits.length === 0) return;
      hits.sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top);
      markActive(hits[0].target.id);
    }, { rootMargin: '0px 0px -70% 0px', threshold: 0 });

    // Document order, so the bootstrap below picks the DOM-topmost
    // section, not whatever the nav happened to list first.
    targets.sort((a, b) => {
      const pos = a.compareDocumentPosition(b);
      if (pos & 4) return -1;
      if (pos & 2) return 1;
      return 0;
    });
    for (const t of targets) observer.observe(t);
    observers.set(nav, observer);

    // Bootstrap an initial active anchor: on a page whose sections all
    // start below a tall header the observer never fires, and no anchor
    // would be marked. Pick the last target above the midline, else the
    // first, so something is always marked once the module is here.
    requestAnimationFrame(() => {
      if (nav.querySelector('a.is-active')) return;
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
  }
  NS._huiRailWatch = watch;

  function scan(root) {
    const scope = root && root.querySelectorAll ? root : document;
    for (const nav of within(scope, '[data-hui-rail]')) {
      watch(
        nav,
        nav.getAttribute('data-hui-rail-observe'),
        nav.getAttribute('data-hui-rail-target')
      );
    }
  }

  scan(document);
  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;
})();
