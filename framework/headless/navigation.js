// headless-navigation: the behaviour module for the page-level
// controls — the back-to-top link and the theme (colour-scheme)
// control group. Loaded by the kernel on one of its markers.
//
// The back-to-top link is a real anchor whose href is the no-script
// destination; the module adds the threshold, the scroll, and the
// focus return, and owns the visibility mark no component renders.
// The theme control group persists through the same storage key the
// bootstrap reads, so the scheme never flashes.
(function () {
  'use strict';
  const NAME = 'headless-navigation';
  const NS = window.__gofastr = window.__gofastr || {};
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;

  const THEME_KEY = 'gofastr.colorScheme';
  const REDUCED = window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)');

  // within(root, sel): root itself when it matches, plus everything
  // matching inside it — the kernel hands scan() one inserted subtree,
  // and a subtree whose root IS the marker is missed by
  // querySelectorAll alone.
  function within(root, sel) {
    const out = [];
    if (root.matches && root.matches(sel)) out.push(root);
    if (root.querySelectorAll) out.push.apply(out, root.querySelectorAll(sel));
    return out;
  }

  // ─── back to top ─────────────────────────────────────────────────

  // One sentinel for the whole document (the singleton the retired
  // core module kept): the link shows past the threshold and hides
  // before it, through the runtime-owned data-hui-back-to-top-visible
  // mark a stylesheet keys off.
  let sentinel = null;
  let observer = null;
  const links = new Set();

  function applyVisible(visible) {
    for (const link of Array.from(links)) {
      if (!link.isConnected) { links.delete(link); continue; }
      if (visible) link.setAttribute('data-hui-back-to-top-visible', '');
      else link.removeAttribute('data-hui-back-to-top-visible');
    }
  }

  // The sentinel is a document-top strip whose HEIGHT is the max
  // threshold across all links (the retired core module's recipe): the
  // observer reports not-intersecting exactly when the reader has
  // scrolled past that strip, which is the threshold, without a
  // scroll listener.
  function ensureSentinel() {
    if (sentinel && sentinel.isConnected) return;
    sentinel = NS.doc.singleton('fui-backtotop-sentinel', function () {
      const s = document.createElement('div');
      s.setAttribute('aria-hidden', 'true');
      // Pinned to the document's top and taken out of flow: an
      // in-flow sentinel appended at the end of a tall body sits
      // BELOW the fold and reports "scrolled" on every page at rest.
      s.style.cssText = 'position:absolute;top:0;left:0;width:1px;pointer-events:none;';
      return s;
    });
    if (observer) observer.disconnect();
    observer = new IntersectionObserver(function (entries) {
      applyVisible(!entries[0].isIntersecting);
    }, { rootMargin: '0px', threshold: 0 });
    observer.observe(sentinel);
  }

  function armLink(link) {
    if (link.dataset.huiBttArmed === '1') return;
    link.dataset.huiBttArmed = '1';
    links.add(link);
    ensureSentinel();
    const threshold = parseInt(link.getAttribute('data-hui-back-to-top-threshold') || '0', 10);
    if (threshold > sentinel.offsetHeight) {
      sentinel.style.height = threshold + 'px';
    }
  }

  document.addEventListener('click', function (e) {
    const t = e.target;
    const link = t && t.closest && t.closest('[data-hui-back-to-top]');
    if (!link) return;
    // The href already goes there without script; with script the
    // jump is in-page and focus comes back to the link, so the next
    // Tab is where the reader left off.
    e.preventDefault();
    const targetID = link.getAttribute('data-hui-back-to-top-target');
    let dest = null;
    if (targetID) dest = document.getElementById(targetID);
    const behavior = link.hasAttribute('data-hui-back-to-top-smooth') && !(REDUCED && REDUCED.matches) ? 'smooth' : 'auto';
    if (dest) {
      dest.scrollIntoView({ behavior: behavior, block: 'start' });
    } else {
      window.scrollTo({ top: 0, behavior: behavior });
    }
    link.focus({ preventScroll: true });
  });

  // ─── theme ───────────────────────────────────────────────────────

  function currentScheme() {
    let s = '';
    try { s = localStorage.getItem(THEME_KEY) || ''; } catch (_) { return 'auto'; }
    if (s === 'light' || s === 'dark') return s;
    return 'auto';
  }

  function applyScheme(scheme, root) {
    try { localStorage.setItem(THEME_KEY, scheme); } catch (_) {}
    const html = document.documentElement;
    html.setAttribute('data-color-scheme', scheme);
    if (window.__gofastr_colorScheme) {
      try { window.__gofastr_colorScheme.apply(scheme); } catch (_) {}
    }
    const scope = root && root.querySelectorAll ? root : document;
    for (const opt of scope.querySelectorAll('[data-hui-theme-option]')) {
      opt.setAttribute('aria-checked', opt.getAttribute('data-hui-theme-option') === scheme ? 'true' : 'false');
    }
  }

  document.addEventListener('click', function (e) {
    const t = e.target;
    const opt = t && t.closest && t.closest('[data-hui-theme-option]');
    if (opt) {
      const scheme = opt.getAttribute('data-hui-theme-option');
      if (scheme === 'light' || scheme === 'dark' || scheme === 'auto') {
        applyScheme(scheme, opt.closest('[data-hui-theme-toggle]') || document);
      }
      return;
    }
    const cycle = t && t.closest && t.closest('[data-hui-theme-cycle]');
    if (cycle) {
      const order = ['dark', 'light', 'auto'];
      const cur = currentScheme();
      applyScheme(order[(order.indexOf(cur) + 1) % order.length], cycle.closest('[data-hui-theme-toggle]') || document);
    }
  });

  // ─── the arrival pass ────────────────────────────────────────────

  function scan(root) {
    const scope = root && root.querySelectorAll ? root : document;
    for (const link of within(scope, '[data-hui-back-to-top]')) armLink(link);
    // within(), not scope.querySelector: the kernel hands scan() one
    // inserted subtree, and a subtree whose root IS the toggle group
    // is missed by a descendants-only lookup.
    for (const group of within(scope, '[data-hui-theme-toggle]')) {
      const scheme = currentScheme();
      for (const opt of group.querySelectorAll('[data-hui-theme-option]')) {
        opt.setAttribute('aria-checked', opt.getAttribute('data-hui-theme-option') === scheme ? 'true' : 'false');
      }
    }
  }

  scan(document);
  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;
})();
