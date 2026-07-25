// GoFastr runtime module — PaneHost
//
// Wires the pane-host layout primitive: a primary pane that is always
// visible plus one or two openable side panes (secondary / tertiary).
// Owns the pane lifecycle — open / close / swap, focus handoff on open,
// focus restore on close, and a responsive collapse where, below 768px,
// an open side pane becomes a fixed overlay drawer (backdrop scrim +
// focus trap + scroll lock + ESC-to-close) instead of an inline column.
//
// It does NOT fetch pane content. To fill a pane from a link, use the
// existing data-fui-rpc + data-fui-rpc-signal rail broadcasting into a
// data-fui-signal + data-fui-signal-mode="html" region inside the pane.
//
// Triggers (attribute-driven, delegated):
//   data-fui-pane-open="secondary|tertiary"   open that pane
//   data-fui-pane-close="secondary|tertiary"  close it (bare = topmost)
//   data-fui-pane-swap="secondary|tertiary"   open it, close the sibling
//   data-fui-pane-key="<key>"                 what this trigger opens
// A trigger resolves its host via closest [data-fui-pane-host], or via
// data-fui-pane-host-target="<id>" for triggers outside the host.
//
// URL round-tripping is opt-in per host via data-fui-pane-deeplink
//="<param>". Opening through a keyed trigger writes
// `?<param>=<pane>:<key>`; closing strips it; Back replays the state by
// re-clicking the matching trigger. The server renders first paint from
// the same parameter (ui.PaneDeepLink), so a shared link is not a flash
// of closed pane.
//
// Programmatic API (mirrors openWidget / closeWidget):
//   __gofastr.openPane(hostId, pane)
//   __gofastr.closePane(hostId, pane)
//   __gofastr.swapPane(hostId, pane)
//
// Loads on demand when a [data-fui-pane-host] element appears. The
// breakpoint literal MUST match the @media in the ui-pane-host CSS.
(() => {
  'use strict';
  window.__gofastr = window.__gofastr || {};
  const NS = window.__gofastr;

  const MQ = '(max-width: 768px)';
  const states = new WeakMap();     // host -> per-instance state (Hard Rule 12)
  const overlayHosts = new Set();   // hosts currently in overlay drawer mode

  const SIDE = ['secondary', 'tertiary'];

  // Per-instance state. Multiple pane-hosts per page must not share it.
  // A pane the SERVER rendered open is already visible, so seed the
  // stack from the DOM — otherwise topmost() reports nothing open and
  // ESC, overlay-drawer mode, and bare close all skip a pane the user
  // can plainly see.
  const st = (host) => {
    let s = states.get(host);
    if (!s) {
      s = { stack: [], triggers: {} };
      for (const p of SIDE) {
        const el = paneEl(host, p);
        if (el && !el.hasAttribute('hidden')) s.stack.push(p);
      }
      states.set(host, s);
    }
    return s;
  };
  const paneEl = (host, pane) => host.querySelector('[data-fui-pane="' + pane + '"]');
  const has = (host, pane) => !!paneEl(host, pane);
  const topmost = (host) => { const k = st(host).stack; return k.length ? k[k.length - 1] : null; };
  const sibling = (pane) => (pane === 'secondary' ? 'tertiary' : 'secondary');
  const scrollOwner = (host) => 'panehost:' + (host.id || '');

  function resolveHost(btn) {
    const id = btn.getAttribute('data-fui-pane-host-target');
    if (id) return document.getElementById(id);
    return btn.closest('[data-fui-pane-host]');
  }

  function focusFirst(el) {
    const f = el.querySelector(NS._focusSel);
    const target = f || el;
    if (!f) el.setAttribute('tabindex', '-1'); // make the region itself focusable
    try { target.focus({ preventScroll: true }); } catch (_) { target.focus(); }
  }

  // ─── URL round-tripping (opt-in via data-fui-pane-deeplink) ───────
  //
  // The host names a query parameter; opening through a trigger that
  // declares data-fui-pane-key writes `?<param>=<pane>:<key>`, closing
  // strips it, and Back moves between those states. Pane state is still
  // in-page state (Hard Rule 1) — the parameter only records it so a
  // refresh or a shared link reproduces what is on screen, exactly as
  // widget deep links do. Push/strip both use pushState, matching
  // widgetlinks.js so history behaves the same for panes and modals.
  //
  // syncing suppresses URL writes while we are REACTING to the URL, so
  // replaying history never appends to it.
  let syncing = false;
  const dlParam = (host) => host.getAttribute('data-fui-pane-deeplink');

  function writeURL(mutate) {
    const url = new URL(location.href);
    if (!mutate(url.searchParams)) return;
    const q = url.searchParams.toString();
    history.pushState(null, '', url.pathname + (q ? '?' + q : '') + url.hash);
  }

  function pushPane(host, pane, trigger) {
    const param = dlParam(host);
    if (!param || !trigger) return;
    const key = trigger.getAttribute('data-fui-pane-key');
    // An unkeyed trigger opens the pane without addressing it: there is
    // nothing to put in the URL, so leave it alone.
    if (!key) return;
    const value = pane + ':' + key;
    writeURL((sp) => {
      if (sp.get(param) === value) return false;
      sp.set(param, value);
      return true;
    });
  }

  function stripPane(host, pane) {
    const param = dlParam(host);
    if (!param) return;
    writeURL((sp) => {
      const cur = sp.get(param);
      // Only clear the parameter if it is describing the pane that just
      // closed — never clobber a sibling pane's deep link.
      if (!cur || cur.split(':')[0] !== pane) return false;
      sp.delete(param);
      return true;
    });
  }

  // Bring one host in line with the current URL. Called on popstate.
  function syncFromURL(host) {
    const param = dlParam(host);
    if (!param) return;
    const raw = new URL(location.href).searchParams.get(param) || '';
    const [pane, ...rest] = raw.split(':');
    const key = rest.join(':');
    syncing = true;
    try {
      if (!pane || SIDE.indexOf(pane) < 0 || !has(host, pane)) {
        // No (or unusable) deep link: nothing addressed should be open.
        for (const p of st(host).stack.slice()) closePane(host, p);
        return;
      }
      // Replay the trigger so the pane's CONTENT is restored too — it
      // carries the open marker and the RPC that fills the region, so
      // one click rebuilds the state the URL describes. Falls back to
      // opening an empty pane when the trigger is not on this page.
      const trigger = key &&
        host.querySelector('[data-fui-pane-key="' + CSS.escape(key) + '"]');
      if (trigger) trigger.click();
      else openPane(host, pane, null);
      for (const p of st(host).stack.slice()) {
        if (p !== pane) closePane(host, p);
      }
    } finally {
      syncing = false;
    }
  }

  function openPane(host, pane, trigger) {
    if (!has(host, pane)) return;
    const s = st(host);
    const el = paneEl(host, pane);
    el.removeAttribute('hidden');
    host.classList.add('ui-pane-host--' + pane + '-open');
    if (!s.stack.includes(pane)) s.stack.push(pane);
    if (trigger) s.triggers[pane] = trigger;
    focusFirst(el);
    if (!syncing) pushPane(host, pane, trigger);
    host.dispatchEvent(new CustomEvent('pane-host:open', { bubbles: true, detail: { pane } }));
    syncMode(host);
  }

  function closePane(host, pane) {
    const s = st(host);
    if (!pane) pane = topmost(host);
    if (!pane || !has(host, pane)) return;
    const el = paneEl(host, pane);
    el.setAttribute('hidden', '');
    host.classList.remove('ui-pane-host--' + pane + '-open');
    const i = s.stack.indexOf(pane);
    if (i >= 0) s.stack.splice(i, 1);
    if (!syncing) stripPane(host, pane);
    host.dispatchEvent(new CustomEvent('pane-host:close', { bubbles: true, detail: { pane } }));
    const trig = s.triggers[pane];
    if (trig && typeof trig.focus === 'function') {
      try { trig.focus({ preventScroll: true }); } catch (_) { trig.focus(); }
    }
    delete s.triggers[pane];
    syncMode(host);
  }

  function swapPane(host, pane, trigger) {
    if (!has(host, pane)) return;
    const sib = sibling(pane);
    if (has(host, sib) && st(host).stack.indexOf(sib) >= 0) {
      // A swap is one user action and must leave one history entry, so
      // the sibling's close does not write its own. The open below
      // records the resulting state.
      const was = syncing;
      syncing = true;
      try { closePane(host, sib); } finally { syncing = was; }
    }
    openPane(host, pane, trigger || null);
  }

  // Toggle overlay-drawer mode based on viewport + open state. Sets
  // data-fui-pane-mode on the host (CSS keys the drawer chrome off it)
  // and takes/releases the shared refcounted scroll lock.
  function syncMode(host) {
    const wantOverlay = window.matchMedia(MQ).matches && !!topmost(host);
    const isOverlay = host.getAttribute('data-fui-pane-mode') === 'overlay';
    if (wantOverlay === isOverlay) return;
    if (wantOverlay) {
      host.setAttribute('data-fui-pane-mode', 'overlay');
      overlayHosts.add(host);
      if (NS.doc) NS.doc.lockScroll(scrollOwner(host));
    } else {
      host.removeAttribute('data-fui-pane-mode');
      overlayHosts.delete(host);
      if (NS.doc) NS.doc.unlockScroll(scrollOwner(host));
    }
  }

  // ESC closes; Tab is trapped inside the topmost overlay pane. Minimal
  // trap over NS._focusSel — does NOT touch the widgets module's private
  // _modalStack.
  function onKeydown(e) {
    const host = document.querySelector('[data-fui-pane-mode="overlay"]');
    if (!host) return;
    const pane = topmost(host);
    if (!pane) return;
    if (e.key === 'Escape') { e.preventDefault(); closePane(host, pane); return; }
    if (e.key !== 'Tab') return;
    const el = paneEl(host, pane);
    const items = el.querySelectorAll(NS._focusSel);
    if (!items.length) { e.preventDefault(); return; }
    const first = items[0], last = items[items.length - 1];
    if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
    else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
  }

  function onClick(e) {
    const trig = e.target.closest(
      '[data-fui-pane-open],[data-fui-pane-close],[data-fui-pane-swap]');
    if (trig) {
      const host = resolveHost(trig);
      if (!host) return;
      if (trig.hasAttribute('data-fui-pane-open')) {
        openPane(host, trig.getAttribute('data-fui-pane-open'), trig);
      } else if (trig.hasAttribute('data-fui-pane-swap')) {
        swapPane(host, trig.getAttribute('data-fui-pane-swap'), trig);
      } else {
        closePane(host, trig.getAttribute('data-fui-pane-close') || topmost(host));
      }
      return;
    }
    // Backdrop click in overlay mode (lands on the host itself) closes.
    const host = e.target.closest('[data-fui-pane-host]');
    if (host && host.getAttribute('data-fui-pane-mode') === 'overlay' && e.target === host) {
      closePane(host, topmost(host));
    }
  }

  // Programmatic API mirroring openWidget / closeWidget.
  function findHost(hostId) {
    return document.getElementById(hostId) ||
      document.querySelector('[data-fui-pane-host]');
  }
  NS.openPane = (hostId, pane) => { const h = findHost(hostId); if (h) openPane(h, pane, null); };
  NS.closePane = (hostId, pane) => { const h = findHost(hostId); if (h) closePane(h, pane); };
  NS.swapPane = (hostId, pane) => { const h = findHost(hostId); if (h) swapPane(h, pane, null); };

  // Single delegated listeners (installed once).
  document.addEventListener('click', onClick);
  document.addEventListener('keydown', onKeydown);

  // One shared matchMedia listener re-syncs every host on viewport change.
  window.matchMedia(MQ).addEventListener('change', () => {
    document.querySelectorAll('[data-fui-pane-host]').forEach(syncMode);
  });

  // Back / forward replays pane state for deep-linked hosts. Hosts
  // without data-fui-pane-deeplink are untouched, so this listener is
  // inert on every existing PaneHost.
  window.addEventListener('popstate', () => {
    document.querySelectorAll('[data-fui-pane-deeplink]').forEach(syncFromURL);
  });

  // Release overlay scroll locks on SPA navigation (Hard Rule 13) so the
  // next page isn't left scroll-locked by a host that's now detached.
  window.addEventListener('gofastr:navigate', () => {
    document.querySelectorAll('[data-fui-pane-host]').forEach((host) => {
      if (host.getAttribute('data-fui-pane-mode') === 'overlay') {
        host.removeAttribute('data-fui-pane-mode');
        if (NS.doc) NS.doc.unlockScroll(scrollOwner(host));
      }
      overlayHosts.delete(host);
    });
  });

  (NS.loadedModules = NS.loadedModules || {}).panehost = true;
  // Idempotent re-wire after SPA-nav: handlers are delegated, so this
  // only ensures state exists for freshly-swapped hosts and re-syncs
  // their initial overlay mode.
  (NS._moduleScanners = NS._moduleScanners || {}).panehost = (root) => {
    const scope = root && root.querySelectorAll ? root : document;
    scope.querySelectorAll('[data-fui-pane-host]').forEach((h) => { st(h); syncMode(h); });
  };
  requestAnimationFrame(() => {
    document.querySelectorAll('[data-fui-pane-host]').forEach((h) => { st(h); syncMode(h); });
  });
})();
