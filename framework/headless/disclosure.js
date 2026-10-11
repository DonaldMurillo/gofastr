// headless-disclosure: the behaviour module for this package's
// disclosures — native <details> elements carrying the
// data-hui-disclosure hook. The open state is the browser's; what the
// module owns is what the platform leaves missing:
//
//   - the aria-expanded mirror on the controller (a native <summary>
//     reports as a button with no expanded state),
//   - Escape closing the deepest open disclosure that contains focus,
//     one level per press, with focus returned to that disclosure's
//     controller (native <details> only handles Escape while the
//     summary itself has focus),
//   - the close-on-navigation for disclosures that did not ask to
//     persist, and the restore-from-store for the ones that did,
//   - light dismiss for the popups that ask for it
//     (data-hui-disclosure-dismiss, every menu among them): a press
//     outside an open one closes it,
//   - the optional trap posture: Tab containment over the kernel's
//     shared focus selector (the widget runtime's own technique — see
//     widgetfocus.js), armed while the disclosure is open and released
//     on close and on detach.
//
// A menu disclosure's focus-on-open, lazy inflation and keyboard
// contract live in headless-menu, which declares this module as a
// requirement: the menu IS a disclosure, and this module holds the
// disclosure half.
(function () {
  'use strict';
  const NAME = 'headless-disclosure';
  const NS = window.__gofastr = window.__gofastr || {};
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;

  const HOOK = 'details[data-hui-disclosure]';

  // ─── persistence ─────────────────────────────────────────────────
  //
  // The store is the session's: an open section a navigation kept is a
  // this-tab fact. The key is namespaced AND component-encoded at the
  // sink, so a caller key carrying '.' or '[' can neither walk into
  // another component's namespace nor reshape the storage key.
  function persistKey(key) {
    return 'gofastr.disclosure.' + encodeURIComponent(key);
  }
  function restore(d) {
    const key = d.getAttribute('data-hui-disclosure-persist');
    if (!key) return;
    let saved = null;
    try { saved = sessionStorage.getItem(persistKey(key)); } catch (_) { return; }
    if (saved === '1') d.setAttribute('open', '');
    else if (saved === '0') d.removeAttribute('open');
  }
  function record(d) {
    const key = d.getAttribute('data-hui-disclosure-persist');
    if (!key) return;
    try { sessionStorage.setItem(persistKey(key), d.open ? '1' : '0'); } catch (_) {}
  }

  // ─── the aria mirror ─────────────────────────────────────────────

  // The controller of a disclosure: its <summary>, or — for a menu
  // whose trigger is a caller-owned element — that element, resolved
  // through headless-menu's exported resolver (guarded: that module
  // declares this one as a requirement, but a trigger menu that was
  // never opened cannot be open, so the fallback is unreachable).
  function controllerOf(d) {
    const s = d.querySelector(':scope > summary');
    if (s) return s;
    return NS._huiMenuTriggerOf ? NS._huiMenuTriggerOf(d) : null;
  }

  function mirror(d) {
    const t = controllerOf(d);
    if (t) t.setAttribute('aria-expanded', d.open ? 'true' : 'false');
  }

  // ─── the trap posture ────────────────────────────────────────────
  //
  // Containment is `inert` first: set inert on every body child that
  // is NOT the trapped disclosure's ancestor chain. inert removes
  // elements from both the focus order AND the accessibility tree, so
  // a screen reader's virtual cursor cannot walk out of the drawer the
  // way a Tab cycle alone would let it. inertNeighbors records exactly
  // what was toggled — "remove inert from everything" would clobber a
  // host's own inert state — and a sibling that was already inert is
  // never touched. The Tab cycle below stays as the fallback for the
  // engines where inert is unavailable.
  const inertNeighbors = new WeakMap();
  const activeTraps = new Set();
  const applyTrap = (d, engage) => {
    if (engage) {
      let bodyChild = d;
      while (bodyChild.parentElement && bodyChild.parentElement !== document.body) {
        bodyChild = bodyChild.parentElement;
      }
      if (bodyChild.parentElement !== document.body) return; // not in body
      const made = [];
      for (const sib of document.body.children) {
        if (sib === bodyChild) continue;
        if (sib.hasAttribute('inert')) continue; // don't touch existing
        sib.setAttribute('inert', '');
        made.push(sib);
      }
      inertNeighbors.set(d, made);
      activeTraps.add(d);
    } else {
      activeTraps.delete(d);
      const made = inertNeighbors.get(d);
      if (!made) return;
      for (const sib of made) sib.removeAttribute('inert');
      inertNeighbors.delete(d);
    }
    syncTrapWatcher();
  };

  // Releasing the trap hangs off the element's own `toggle` event, and
  // a DETACHED <details> never fires one: a navigation that replaces
  // the shell would leave the inert stuck to every other body child,
  // gone from the focus order and the accessibility tree for the life
  // of the tab. Watch for the detach directly, and only while a trap
  // is actually engaged.
  let trapWatcher = null;
  const releaseStaleTraps = () => {
    for (const d of Array.from(activeTraps)) {
      if (!d.isConnected || !d.open) applyTrap(d, false);
    }
  };
  const syncTrapWatcher = () => {
    if (activeTraps.size > 0 && !trapWatcher) {
      // childList only: releaseStaleTraps mutates attributes, so an
      // attribute-observing watcher would re-enter itself.
      trapWatcher = new MutationObserver(releaseStaleTraps);
      trapWatcher.observe(document.body, { childList: true, subtree: true });
    } else if (activeTraps.size === 0 && trapWatcher) {
      trapWatcher.disconnect();
      trapWatcher = null;
    }
  };

  function openTraps() {
    const out = [];
    for (const d of document.querySelectorAll(HOOK + '[data-hui-disclosure-trap][open]')) {
      if (d.isConnected) out.push(d);
    }
    return out;
  }

  // ─── keeping a popup on screen ───────────────────────────────────
  //
  // A light-dismiss popup's panel hangs from one edge of its trigger,
  // chosen at render; only a measure after open knows whether that edge
  // has room. A panel that would cross the viewport's inline edge is
  // shifted back inside it through --hui-panel-shift, which the kit's
  // panel rules read as a translate (the open animation is a
  // transform, so the two never fight). The shift is cleared first, so
  // each open measures where the panel really hangs.
  const GUTTER = 8;
  function keepInView(d) {
    const p = d.querySelector(':scope > :not(summary)');
    if (!p) return;
    p.style.removeProperty('--hui-panel-shift');
    land(d, p);
    if (!d.open) return;
    const r = p.getBoundingClientRect();
    if (clipped(d, r)) {
      float(d, p, r);
      return;
    }
    const w = document.documentElement.clientWidth;
    let dx = 0;
    if (r.right > w - GUTTER) dx = w - GUTTER - r.right;
    if (r.left + dx < GUTTER) dx = GUTTER - r.left;
    if (dx) p.style.setProperty('--hui-panel-shift', Math.round(dx) + 'px');
  }

  // ─── floating out of a clipping box ──────────────────────────────
  //
  // A box that scrolls (a table wider than its column scrolls sideways,
  // and any overflow but visible clips both axes) cuts a panel off at
  // its edge, and opening the panel scrolls the box instead of showing
  // it: a table's last-row menu opened half hidden. A panel that the
  // measure finds past such a box floats instead: position fixed,
  // which takes it out of every ancestor's overflow, hung from the
  // same edge of its trigger, flipped above the trigger when the
  // viewport has no room below, and moved with the trigger while
  // anything scrolls. The written styles are the panel's own inline
  // ones, cleared on close and before every measure.
  const FLOAT_PROPS = ['position', 'top', 'left', 'right', 'bottom'];
  const floating = new Map(); // details -> {end}

  function clipped(d, r) {
    const root = document.documentElement;
    for (let el = d.parentElement; el && el !== document.body && el !== root; el = el.parentElement) {
      const cs = getComputedStyle(el);
      if (cs.overflowX === 'visible' && cs.overflowY === 'visible') continue;
      const b = el.getBoundingClientRect();
      const top = b.top + el.clientTop;
      const left = b.left + el.clientLeft;
      if (r.top < top - 1 || r.left < left - 1 ||
          r.bottom > top + el.clientHeight + 1 || r.right > left + el.clientWidth + 1) return true;
    }
    return false;
  }

  function float(d, p, r) {
    const t = (controllerOf(d) || d).getBoundingClientRect();
    // The panel hangs from the trigger edge it was laid out against.
    floating.set(d, { end: Math.abs(r.right - t.right) < Math.abs(r.left - t.left) });
    syncFloatWatch();
    place(d, p);
  }

  function place(d, p) {
    const vw = document.documentElement.clientWidth;
    const vh = document.documentElement.clientHeight;
    const gap = 4;
    // Fixed first: the panel's width depends on its containing block.
    p.style.setProperty('position', 'fixed');
    p.style.setProperty('right', 'auto');
    p.style.setProperty('bottom', 'auto');
    const w = p.offsetWidth;
    const h = p.offsetHeight;
    // And the trigger after: a panel laid out inside the box could give
    // the box a scrollbar, which moved the trigger.
    const t = (controllerOf(d) || d).getBoundingClientRect();
    let top = t.bottom + gap;
    if (top + h > vh - GUTTER && t.top - gap - h >= GUTTER) top = t.top - gap - h;
    let left = floating.get(d).end ? t.right - w : t.left;
    left = Math.max(GUTTER, Math.min(left, vw - GUTTER - w));
    const o = fixedOrigin(p);
    p.style.setProperty('top', (top - o.y) + 'px');
    p.style.setProperty('left', (left - o.x) + 'px');
  }

  // A transformed, filtered or contained ancestor is a fixed panel's
  // containing block in place of the viewport; the panel's coordinates
  // are then relative to that ancestor's padding box.
  function fixedOrigin(p) {
    const root = document.documentElement;
    for (let el = p.parentElement; el && el !== root; el = el.parentElement) {
      const cs = getComputedStyle(el);
      if (cs.transform !== 'none' || cs.translate !== 'none' || cs.scale !== 'none' ||
          cs.rotate !== 'none' || cs.perspective !== 'none' || cs.filter !== 'none' ||
          (cs.backdropFilter && cs.backdropFilter !== 'none') ||
          /paint|layout|strict|content/.test(cs.contain) ||
          /transform|perspective|filter/.test(cs.willChange)) {
        const b = el.getBoundingClientRect();
        return { x: b.left + el.clientLeft, y: b.top + el.clientTop };
      }
    }
    return { x: 0, y: 0 };
  }

  function land(d, p) {
    if (!floating.delete(d)) return;
    for (const prop of FLOAT_PROPS) p.style.removeProperty(prop);
    syncFloatWatch();
  }

  let floatFrame = 0;
  const follow = () => {
    if (floatFrame) return;
    floatFrame = requestAnimationFrame(() => {
      floatFrame = 0;
      for (const d of Array.from(floating.keys())) {
        const p = d.querySelector(':scope > :not(summary)');
        if (!p) { floating.delete(d); continue; }
        if (!d.isConnected || !d.open) land(d, p);
        else place(d, p);
      }
    });
  };
  let watching = false;
  const syncFloatWatch = () => {
    if (floating.size > 0 && !watching) {
      window.addEventListener('scroll', follow, { capture: true, passive: true });
      window.addEventListener('resize', follow, { passive: true });
      watching = true;
    } else if (floating.size === 0 && watching) {
      window.removeEventListener('scroll', follow, { capture: true });
      window.removeEventListener('resize', follow);
      watching = false;
    }
  };

  // ─── listeners, installed once ───────────────────────────────────

  document.addEventListener('toggle', (e) => {
    const d = e.target;
    if (!d || d.tagName !== 'DETAILS' || !d.hasAttribute('data-hui-disclosure')) return;
    mirror(d);
    record(d);
    if (d.hasAttribute('data-hui-disclosure-trap')) applyTrap(d, d.open);
    if (d.hasAttribute('data-hui-disclosure-dismiss')) keepInView(d);
  }, true);

  document.addEventListener('click', (e) => {
    if (e.defaultPrevented) return;
    // An interactive descendant of the summary (an icon button trigger)
    // swallows the UA's summary activation in some engines; toggle it
    // here and preventDefault so engines that do toggle for nested
    // controls cannot double-toggle. A defaultPrevented earlier
    // listener — the caller wired the element itself — wins.
    const t = e.target;
    if (!t || !t.closest) return;
    const interactive = t.closest('button, a, input, select, textarea, [role="button"]');
    if (!interactive) return;
    const summary = interactive.closest('summary');
    if (!summary) return;
    const d = summary.closest(HOOK);
    if (!d) return;
    e.preventDefault();
    d.toggleAttribute('open');
    mirror(d);
  });

  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
      // An open modal widget's own CloseOnEscape handler runs; one
      // Escape must not close both.
      if (NS._modalStack && NS._modalStack.length > 0) return;
      const open = document.querySelectorAll(HOOK + '[open]');
      let deepest = null;
      for (const d of open) {
        if (d.contains(document.activeElement) && (!deepest || deepest.contains(d))) deepest = d;
      }
      if (deepest) {
        deepest.removeAttribute('open');
        const c = controllerOf(deepest);
        if (c) c.focus({ preventScroll: true });
        return;
      }
      for (const d of open) d.removeAttribute('open');
      return;
    }
    if (e.key !== 'Tab') return;
    // The Tab-cycle fallback: the topmost open trap disclosure keeps
    // focus inside it while no modal widget is above it. The inert
    // containment above is the primary trap; this catches the rest.
    if (NS._modalStack && NS._modalStack.length > 0) return;
    const traps = openTraps();
    if (traps.length === 0) return;
    const d = traps[traps.length - 1];
    const sel = NS._focusSel || 'a[href],button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])';
    const nodes = Array.from(d.querySelectorAll(sel)).filter(function (el) {
      return (el.offsetParent !== null || el === document.activeElement) && el.closest(HOOK) === d;
    });
    if (nodes.length === 0) return;
    const first = nodes[0], last = nodes[nodes.length - 1];
    if (e.shiftKey && (document.activeElement === first || !d.contains(document.activeElement))) {
      e.preventDefault(); last.focus();
    } else if (!e.shiftKey && (document.activeElement === last || !d.contains(document.activeElement))) {
      e.preventDefault(); first.focus();
    }
  });

  // Light dismiss: a click outside an open popup disclosure closes it.
  // Capture phase, so the popup closes before the click's own target
  // acts (another popup's summary opens after this one closed). A
  // click, not a pointerdown: closing on the press can move the layout
  // under the pointer, and the release then lands on something else.
  // A click on the popup's own controller (a caller-owned menu trigger
  // outside the details) is the controller's to handle.
  document.addEventListener('click', (e) => {
    const t = e.target;
    for (const d of document.querySelectorAll(HOOK + '[data-hui-disclosure-dismiss][open]')) {
      if (d.contains(t)) continue;
      const c = controllerOf(d);
      if (c && c.contains(t)) continue;
      d.removeAttribute('open');
    }
  }, true);

  // A click on an ordinary link inside a disclosure closes it (the
  // destination page does not need the menu open); a persistent
  // disclosure keeps its state by contract.
  document.addEventListener('click', (e) => {
    const t = e.target;
    const a = t && t.closest && t.closest('a[href]');
    if (!a) return;
    const d = a.closest(HOOK + ':not([data-hui-disclosure-persist])');
    if (d) d.removeAttribute('open');
  });

  // Close the non-persistent disclosures on a client-side navigation —
  // the document they belonged to is going away — and restore the
  // persistent ones from the store. The kernel hands the
  // post-navigation document to scan() below as well; this listener is
  // the belt to that braces for markup that arrives re-parented.
  window.addEventListener('gofastr:navigate', () => {
    for (const d of document.querySelectorAll(HOOK)) {
      if (d.hasAttribute('data-hui-disclosure-persist')) restore(d);
      else if (d.open) d.removeAttribute('open');
    }
  });

  // ─── the arrival pass ────────────────────────────────────────────

  function scan(root) {
    const scope = root && root.querySelectorAll ? root : document;
    for (const d of within(scope, HOOK)) {
      restore(d);
      mirror(d);
    }
  }

  function within(root, sel) {
    const out = [];
    if (root.matches && root.matches(sel)) out.push(root);
    if (root.querySelectorAll) out.push.apply(out, root.querySelectorAll(sel));
    return out;
  }

  scan(document);
  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;
})();
