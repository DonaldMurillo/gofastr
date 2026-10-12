// headless-navigation: the behaviour module for the page-level
// controls — the back-to-top link, the theme (colour-scheme) control
// group and the page-theme picker. Loaded by the kernel on one of its
// markers.
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
    sentinel = NS.doc.singleton('cui-backtotop-sentinel', function () {
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

  // markRadios checks the option of one radio group whose attr equals
  // value, or the option whose attr is '' when none does (a stored
  // ThemePicker class that no option names any more draws the app's
  // own theme, so Default is the honest answer). The checked option is
  // the group's one Tab stop; with nothing checked it is the first.
  const RADIO_SEL = {
    'data-hui-theme-pick': '[data-hui-theme-pick]',
    'data-hui-theme-option': '[data-hui-theme-option]',
  };

  function markRadios(group, attr, value) {
    const opts = Array.prototype.slice.call(group.querySelectorAll(RADIO_SEL[attr]));
    let hit = opts.find(function (o) { return o.getAttribute(attr) === value; });
    if (!hit) hit = opts.find(function (o) { return o.getAttribute(attr) === ''; });
    for (const o of opts) {
      o.setAttribute('aria-checked', o === hit ? 'true' : 'false');
      o.setAttribute('tabindex', o === hit || (!hit && o === opts[0]) ? '0' : '-1');
    }
  }

  function currentScheme() {
    let s = '';
    try { s = localStorage.getItem(THEME_KEY) || ''; } catch (_) { return 'auto'; }
    if (s === 'light' || s === 'dark') return s;
    return 'auto';
  }

  // resolveScheme: the scheme a page in this mode shows — auto is the
  // OS preference, never a value of its own.
  function resolveScheme(scheme) {
    if (scheme === 'light' || scheme === 'dark') return scheme;
    const mq = window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)');
    return mq && mq.matches ? 'dark' : 'light';
  }

  // applyScheme hands the choice to the colour-scheme bootstrap
  // (core-ui/runtime/colorscheme.js), which persists it, resolves auto
  // and writes data-color-scheme AND the color-scheme meta the native
  // controls follow. A page without the bootstrap gets the same
  // storage and a resolved attribute written here.
  function applyScheme(scheme, root) {
    const api = window.__gofastr_colorScheme;
    let done = false;
    if (api && typeof api.set === 'function') {
      try { api.set(scheme); done = true; } catch (_) {}
    }
    if (!done) {
      try {
        if (scheme === 'auto') localStorage.removeItem(THEME_KEY);
        else localStorage.setItem(THEME_KEY, scheme);
      } catch (_) {}
      document.documentElement.setAttribute('data-color-scheme', resolveScheme(scheme));
    }
    const scope = root && root.querySelectorAll ? root : document;
    for (const group of within(scope, '[data-hui-theme-toggle]')) markRadios(group, 'data-hui-theme-option', scheme);
  }

  // ─── page theme ──────────────────────────────────────────────────
  //
  // A ThemePicker option names one registered override class (or ''
  // for the app's own theme). The bootstrap's window.__gofastr_theme
  // owns the <html> class and the storage key it re-applies before
  // first paint; a page without the bootstrap gets the same writes
  // here.

  const PAGE_THEME_KEY = 'gofastr.theme';
  const PAGE_THEME_RE = /^cui-theme-[0-9a-f]{1,64}$/;

  function currentPageTheme() {
    const api = window.__gofastr_theme;
    if (api && typeof api.get === 'function') {
      try { return api.get(); } catch (_) { return ''; }
    }
    let v = '';
    try { v = localStorage.getItem(PAGE_THEME_KEY) || ''; } catch (_) { return ''; }
    return PAGE_THEME_RE.test(v) ? v : '';
  }

  function markPageTheme(scope, cls) {
    for (const group of within(scope, '[data-hui-theme-picker]')) markRadios(group, 'data-hui-theme-pick', cls);
  }

  function applyPageTheme(cls) {
    if (cls !== '' && !PAGE_THEME_RE.test(cls)) return;
    const api = window.__gofastr_theme;
    let done = false;
    if (api && typeof api.set === 'function') {
      try { api.set(cls); done = true; } catch (_) {}
    }
    if (!done) {
      const root = document.documentElement;
      for (const c of Array.prototype.slice.call(root.classList)) {
        if (c.indexOf('cui-theme-') === 0) root.classList.remove(c);
      }
      if (cls) root.classList.add(cls);
      try {
        if (cls) localStorage.setItem(PAGE_THEME_KEY, cls);
        else localStorage.removeItem(PAGE_THEME_KEY);
      } catch (_) {}
    }
    // Every picker on the page shows the same choice.
    markPageTheme(document, cls);
  }

  document.addEventListener('click', function (e) {
    const t = e.target;
    const pick = t && t.closest && t.closest('[data-hui-theme-pick]');
    if (pick && pick.closest('[data-hui-theme-picker]')) {
      applyPageTheme(pick.getAttribute('data-hui-theme-pick') || '');
      return;
    }
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
      // dark → light → auto, skipping a step that would leave the page
      // looking the same: on a dark OS, auto's next step (dark) shows
      // what auto already shows, so the click goes on to light. Every
      // click changes the scheme the reader sees.
      const order = ['dark', 'light', 'auto'];
      const cur = currentScheme();
      const shown = resolveScheme(cur);
      let i = order.indexOf(cur);
      let next = cur;
      for (let n = 0; n < order.length; n++) {
        i = (i + 1) % order.length;
        next = order[i];
        if (resolveScheme(next) !== shown) break;
      }
      applyScheme(next, cycle.closest('[data-hui-theme-toggle]') || document);
    }
  });

  // Arrow keys move through a theme radio group the way a native radio
  // set does: the next option takes focus and is chosen, wrapping at
  // either end; Home and End jump to the first and last.
  document.addEventListener('keydown', function (e) {
    if (e.isComposing || e.altKey || e.ctrlKey || e.metaKey) return;
    const t = e.target;
    const opt = t && t.closest && t.closest('[data-hui-theme-pick],[data-hui-theme-option]');
    const group = opt && opt.closest('[data-hui-theme-picker],[data-hui-theme-toggle][role="radiogroup"]');
    if (!group) return;
    const attr = opt.hasAttribute('data-hui-theme-pick') ? 'data-hui-theme-pick' : 'data-hui-theme-option';
    const opts = Array.prototype.slice.call(group.querySelectorAll(RADIO_SEL[attr]));
    const i = opts.indexOf(opt);
    let j;
    switch (e.key) {
      case 'ArrowRight': case 'ArrowDown': j = (i + 1) % opts.length; break;
      case 'ArrowLeft': case 'ArrowUp': j = (i - 1 + opts.length) % opts.length; break;
      case 'Home': j = 0; break;
      case 'End': j = opts.length - 1; break;
      default: return;
    }
    e.preventDefault();
    opts[j].focus();
    opts[j].click();
  });

  // ─── shortcuts ───────────────────────────────────────────────────
  //
  // One document-level keydown for every chord on the page, the
  // retired core-ui shortcut module's contract: no per-element
  // listeners (remounted widgets need no rebinding, detached elements
  // can never fire), composing events ignored (an IME confirmation is
  // not a hotkey), and the first CONNECTED match wins so a stale SSR
  // duplicate cannot steal the chord, and an element inside an inert
  // subtree never matches. data-hui-shortcut-target lets a
  // non-focusable wrapper carry the chord while focus lands on (or the
  // click lands in) the element its selector names — the styled search
  // bar wrapping the input is the shape it exists for.
  function parseCombo(combo) {
    const parts = combo.split('+').map(function (s) { return s.trim().toLowerCase(); });
    let key = '';
    let mod = false, shift = false, alt = false;
    parts.forEach(function (p) {
      if (p === 'mod' || p === 'meta' || p === 'ctrl' || p === 'cmd') mod = true;
      else if (p === 'shift') shift = true;
      else if (p === 'alt' || p === 'option') alt = true;
      else key = p;
    });
    return { key: key, mod: mod, shift: shift, alt: alt };
  }

  function chordMatches(e, combo) {
    const m = parseCombo(combo);
    if (!m.key) return false;
    if (e.key.toLowerCase() !== m.key) return false;
    if (m.mod && !(e.metaKey || e.ctrlKey)) return false;
    if (m.shift && !e.shiftKey) return false;
    if (m.alt && !e.altKey) return false;
    // Don't intercept while typing into a text-like input, except
    // when the chord includes a modifier (then it's an intentional
    // hotkey, not a typed character).
    const inField = document.activeElement && /^(INPUT|TEXTAREA|SELECT)$/.test(document.activeElement.tagName);
    if (inField && !m.mod && !m.alt) return false;
    return true;
  }

  function shortcutTarget(el) {
    const sel = el.getAttribute('data-hui-shortcut-target');
    if (sel) {
      // The selector is markup-borne input: a malformed value degrades
      // to the wrapper instead of throwing out of the document
      // keydown listener.
      try {
        const t = el.querySelector(sel) || document.querySelector(sel);
        if (t && t.isConnected) return t;
      } catch (_) {}
    }
    return el;
  }

  document.addEventListener('keydown', function (e) {
    if (e.isComposing) return;
    const els = document.querySelectorAll('[data-hui-shortcut-focus],[data-hui-shortcut-click]');
    for (const el of els) {
      // An inert subtree takes no input: a drawer under the top layer
      // keeps its own Save chord, and the top layer's must win.
      if (!el.isConnected || el.closest('[inert]')) continue;
      const focusCombo = el.getAttribute('data-hui-shortcut-focus');
      if (focusCombo && chordMatches(e, focusCombo)) {
        e.preventDefault();
        const target = shortcutTarget(el);
        try { target.focus(); if (target.select) target.select(); } catch (_) {}
        return;
      }
      const clickCombo = el.getAttribute('data-hui-shortcut-click');
      if (clickCombo && chordMatches(e, clickCombo)) {
        e.preventDefault();
        shortcutTarget(el).click();
        return;
      }
    }
  });
  // ─── the arrival pass ────────────────────────────────────────────

  function scan(root) {
    const scope = root && root.querySelectorAll ? root : document;
    for (const link of within(scope, '[data-hui-back-to-top]')) armLink(link);
    // A ShortcutHint with a BindTarget names its target's selector:
    // nothing to arm (the chord itself rides -focus/-click on the
    // target or its wrapper), but reading it here keeps the marker an
    // owned contract rather than an attribute nothing binds.
    for (const hint of within(scope, '[data-hui-shortcut-hint]')) void hint.getAttribute('data-hui-shortcut-hint');
    // within(), not scope.querySelector: the kernel hands scan() one
    // inserted subtree, and a subtree whose root IS the toggle group
    // is missed by a descendants-only lookup.
    for (const group of within(scope, '[data-hui-theme-toggle]')) markRadios(group, 'data-hui-theme-option', currentScheme());
    markPageTheme(scope, currentPageTheme());
  }

  scan(document);
  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;
})();
