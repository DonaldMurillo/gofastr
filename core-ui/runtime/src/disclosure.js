// GoFastr runtime module, Disclosure
//
// Keyboard + assistive-tech behaviour for <details data-fui-disclosure>:
//
//   - aria-expanded mirroring on the <summary> (native <summary> reports
//     as "button" with no expanded state)
//   - Escape closes any open disclosure from anywhere on the page (native
//     <details> only handles Escape while the summary itself has focus)
//   - menu disclosures (data-fui-menu) move focus to the first menuitem
//     on open, so keyboard users land inside the panel without a Tab
//   - an opt-in focus trap (data-fui-disclosure-trap) for mobile drawers
//     and full-sheet popovers, released when the drawer closes, when it
//     is detached from the DOM, or on SPA navigation
//
// Loads on demand: core's module scanner watches
// details[data-fui-disclosure], so the fetch starts in the initial pass
// on any page that has one and never ships to pages that don't. Core
// keeps only the close-on-navigate lines (a bare removeAttribute), whose
// resulting `toggle` event this module picks up like any other.
//
// Listeners are document-level and installed once (guarded), so a
// dynamically-inserted disclosure needs no rescan.
(() => {
  'use strict';
  window.__gofastr = window.__gofastr || {};
  const NS = window.__gofastr;

  // Mirror details.open → summary aria-expanded for screen readers.
  const mirror = (d) => {
    const s = d.querySelector(':scope > summary');
    if (s) s.setAttribute('aria-expanded', d.open ? 'true' : 'false');
  };
  NS._mirrorDisclosure = mirror;

  // Focus trap via `inert`: set inert on every body child that is NOT
  // the drawer's ancestor chain. Tab walking is then naturally confined,
  // because inert removes elements from both the focus order and the AT
  // tree. inertNeighbors records exactly what was toggled so the cleanup
  // is exact, "remove inert from everything" would clobber a host's own
  // inert state.
  const inertNeighbors = new WeakMap();
  // activeTraps is the sibling Set rule 12 asks for: the WeakMap alone is
  // unenumerable, so there is no way to reach an engaged trap that never
  // gets another `toggle`.
  const activeTraps = new Set();
  const applyTrap = (d, open) => {
    if (open) {
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

  // Releasing the trap hangs off the element's own `toggle` event, and a
  // DETACHED <details> never fires one. SPA navigation detaches it two
  // ways: nav.js's cross-layout branch replaces the whole
  // [data-fui-layout] shell (cur.replaceWith), and swapMainContent writes
  // main.innerHTML before its close-open-disclosures sweep can reach a
  // drawer living inside <main>. Either way the inert would stick to every
  // other <body> child, gone from the focus order AND the accessibility
  // tree, for the life of the tab. So watch for the detach directly, and
  // only while a trap is actually engaged (rule 13: clean up per-instance
  // state, then re-scan).
  let trapWatcher = null;
  const releaseStaleTraps = () => {
    for (const d of Array.from(activeTraps)) {
      if (!d.isConnected || !d.open) applyTrap(d, false);
    }
  };
  const syncTrapWatcher = () => {
    if (activeTraps.size > 0 && !trapWatcher) {
      // childList only, releaseStaleTraps mutates attributes, so an
      // attribute-observing watcher would re-enter itself.
      trapWatcher = new MutationObserver(releaseStaleTraps);
      trapWatcher.observe(document.body, { childList: true, subtree: true });
    } else if (activeTraps.size === 0 && trapWatcher) {
      trapWatcher.disconnect();
      trapWatcher = null;
    }
  };

  if (!document.__fuiDisclosureDispatch) {
    document.__fuiDisclosureDispatch = true;

    // 'toggle' fires on every open/close. Delegated at document level in
    // the capture phase, toggle does not bubble, so dynamically
    // inserted disclosures are covered.
    document.addEventListener('toggle', (e) => {
      const d = e.target;
      if (!d || d.tagName !== 'DETAILS' || !d.hasAttribute('data-fui-disclosure')) return;
      mirror(d);
      // Menu disclosure: on open, focus the first menuitem. The native
      // <summary> keeps visible focus until the user moves it with
      // ArrowDown.
      if (d.open && d.hasAttribute('data-fui-menu')) {
        const first = d.querySelector('[role="menuitem"]:not([aria-disabled="true"])');
        if (first) first.focus();
      }
      if (d.hasAttribute('data-fui-disclosure-trap')) applyTrap(d, d.open);
    }, true);

    // Escape closes any open disclosure. An open modal widget takes
    // precedence, its own CloseOnEscape handler runs, and we defer so a
    // single Escape doesn't close both.
    document.addEventListener('keydown', (e) => {
      if (e.key !== 'Escape') return;
      if (NS._modalStack && NS._modalStack.length > 0) return;
      for (const d of document.querySelectorAll('details[data-fui-disclosure][open]')) {
        // Only refocus the summary when focus is already inside this
        // disclosure, otherwise we would yank focus away from whatever
        // the user was actually doing in the main content.
        const wasInside = d.contains(document.activeElement);
        d.removeAttribute('open');
        if (wasInside) d.querySelector('summary')?.focus();
      }
    });
  }

  // Initial pass: sync aria-expanded on every server-rendered
  // disclosure. Re-run after SPA navigation, which swaps in markup this
  // module never saw a toggle for.
  const scan = (root) => {
    for (const d of (root || document).querySelectorAll('details[data-fui-disclosure]')) mirror(d);
  };
  scan(document);
  (NS._moduleScanners ||= {}).disclosure = scan;
  window.addEventListener('gofastr:navigate', () => {
    releaseStaleTraps();
    scan(document);
  });

  (NS.loadedModules ||= {}).disclosure = true;
})();
