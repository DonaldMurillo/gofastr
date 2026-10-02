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

  // ─── listeners, installed once ───────────────────────────────────

  document.addEventListener('toggle', (e) => {
    const d = e.target;
    if (!d || d.tagName !== 'DETAILS' || !d.hasAttribute('data-hui-disclosure')) return;
    mirror(d);
    record(d);
    if (d.hasAttribute('data-hui-disclosure-trap')) applyTrap(d, d.open);
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
