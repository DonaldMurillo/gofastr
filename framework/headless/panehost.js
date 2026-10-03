// headless-panehost: the behaviour module for this package's pane
// hosts. The panes ship server-rendered (hidden on closed ones); the
// module owns the lifecycle — open/close/swap through the
// data-hui-pane-* controls, focus handoff on open and restore on
// close, the responsive drawer below 768px (an open pane becomes a
// fixed overlay drawer with a Tab trap over the kernel's focus
// selector, the kernel's refcounted scroll lock, and Escape /
// backdrop-click to close), the programmatic API and events, and the
// optional query deep link (Back/refresh parity). It replaces the
// retired core-ui/runtime panehost module. Nothing here fetches pane
// content: a trigger that loads content uses the island/RPC rails
// inside the pane's region.
(function () {
  'use strict';
  const NAME = 'headless-panehost';
  const NS = window.__gofastr = window.__gofastr || {};
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;

  // The breakpoint literal MUST match the @media in framework/ui's
  // ui-pane-host stylesheet (currently (max-width: 768px)): if the two
  // disagree, the grid collapses where the drawer does not (or vice
  // versa) and a pane is either a squeezed column or an overlay with
  // no chrome.
  const MQ = '(max-width: 768px)';
  const SLOTS = ['secondary', 'tertiary'];

  function within(root, sel) {
    const out = [];
    if (root.matches && root.matches(sel)) out.push(root);
    if (root.querySelectorAll) out.push.apply(out, root.querySelectorAll(sel));
    return out;
  }

  function paneOf(host, slot) {
    // The slot is DOM input (a trigger attribute); it names a fixed
    // pane set, so refuse anything outside it before the selector —
    // a crafted value matches nothing and the click is a no-op.
    if (SLOTS.indexOf(slot) === -1) return null;
    return host.querySelector(':scope > [data-hui-pane="' + CSS.escape(slot) + '"]');
  }

  // The open list lives on the hook the render seeds and the module
  // maintains: data-hui-pane-open is the space-separated list of open
  // side panes, in slot order — the same state the stylesheet's column
  // rules key off, so script and sheet can never disagree about what
  // is open.
  function openList(host) {
    const v = host.getAttribute('data-hui-pane-open');
    return v ? v.split(' ').filter(function (s) { return SLOTS.indexOf(s) !== -1; }) : [];
  }

  function setOpenList(host, list) {
    if (list.length) host.setAttribute('data-hui-pane-open', list.join(' '));
    else host.removeAttribute('data-hui-pane-open');
  }

  function topmost(host) {
    const open = openList(host);
    return open.length ? open[open.length - 1] : null;
  }

  // The query deep link (opt-in per host through data-hui-pane-deeplink).
  // Pane state stays in-page state; the parameter only records it so a
  // refresh, a shared link and Back reproduce what is on screen. Every
  // write goes through the navigator's history choke point so the
  // entry id and currentPath stay coherent (scroll restore, the
  // stateful-param diff on popstate); the embed composition has no
  // choke point and gets no URL writes. `syncing` suppresses writes
  // while the module is REACTING to the URL, so a replay never appends
  // to history.
  let syncing = false;

  function writeURL(host, mutate) {
    const param = host.getAttribute('data-hui-pane-deeplink');
    if (!param || typeof NS._pushURL !== 'function') return;
    const url = new URL(location.href);
    if (!mutate(param, url.searchParams)) return;
    const q = url.searchParams.toString();
    try { NS._pushURL(url.pathname + (q ? '?' + q : '') + url.hash); } catch (_) {}
  }

  // An unkeyed trigger opens the pane without addressing it: there is
  // nothing to put in the URL, so the URL is left alone.
  function pushPane(host, slot, key) {
    if (!key) return;
    const value = slot + ':' + key;
    writeURL(host, function (param, sp) {
      if (sp.get(param) === value) return false;
      sp.set(param, value);
      return true;
    });
  }

  // Closing strips the parameter only when it describes the pane that
  // closed, never a sibling's deep link.
  function stripPane(host, slot) {
    writeURL(host, function (param, sp) {
      const cur = sp.get(param);
      if (!cur || cur.split(':')[0] !== slot) return false;
      sp.delete(param);
      return true;
    });
  }

  // Bring one host in line with the URL (popstate). The keyed trigger
  // is replayed so the pane's CONTENT comes back too: it carries the
  // open control and the RPC that fills the region, so one click
  // rebuilds the state the URL describes. A key with no trigger on
  // this page opens the pane empty.
  function syncFromURL(host) {
    const param = host.getAttribute('data-hui-pane-deeplink');
    if (!param) return;
    const raw = new URL(location.href).searchParams.get(param) || '';
    const colon = raw.indexOf(':');
    const slot = colon === -1 ? raw : raw.slice(0, colon);
    const key = colon === -1 ? '' : raw.slice(colon + 1);
    syncing = true;
    try {
      if (!slot || SLOTS.indexOf(slot) === -1 || !paneOf(host, slot)) {
        for (const s of SLOTS) if (paneOf(host, s) && !paneOf(host, s).hasAttribute('hidden')) closePane(host, s);
        return;
      }
      const trigger = key && host.querySelector('[data-hui-pane-key="' + CSS.escape(key) + '"]');
      if (trigger) trigger.click();
      else openPane(host, slot, '');
      for (const s of SLOTS) if (s !== slot && paneOf(host, s) && !paneOf(host, s).hasAttribute('hidden')) closePane(host, s);
    } finally {
      syncing = false;
    }
  }

  function openPane(host, slot, key) {
    const pane = paneOf(host, slot);
    if (!pane) return;
    pane.removeAttribute('hidden');
    const open = openList(host);
    if (open.indexOf(slot) === -1) open.push(slot);
    setOpenList(host, open);
    // Focus handoff: the first tabbable in the pane.
    const sel = NS._focusSel || 'a[href],button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])';
    const first = pane.querySelector(sel);
    if (first) { try { first.focus({ preventScroll: true }); } catch (_) {} }
    if (!syncing) pushPane(host, slot, key);
    host.dispatchEvent(new CustomEvent('pane-host:open', { bubbles: true, detail: { pane: slot } }));
    syncMode(host);
  }

  function closePane(host, slot) {
    const pane = paneOf(host, slot);
    if (!pane) return;
    pane.setAttribute('hidden', '');
    setOpenList(host, openList(host).filter(function (s) { return s !== slot; }));
    // Focus restore: back to the last trigger that opened the pane.
    const reg = host.__huiPaneTrigger;
    const t = reg && Object.prototype.hasOwnProperty.call(reg, slot) ? reg[slot] : null;
    if (t && t.isConnected) { try { t.focus({ preventScroll: true }); } catch (_) {} }
    if (!syncing) stripPane(host, slot);
    host.dispatchEvent(new CustomEvent('pane-host:close', { bubbles: true, detail: { pane: slot } }));
    // A programmatically-opened pane registered no trigger; the
    // registry may not exist at all.
    if (host.__huiPaneTrigger) delete host.__huiPaneTrigger[slot];
    syncMode(host);
  }

  function swapPane(host, slot, key) {
    // A swap is one user action and leaves one history entry: the
    // sibling's close writes nothing, the open below records the
    // resulting state.
    const was = syncing;
    syncing = true;
    try { for (const s of SLOTS) if (s !== slot) closePane(host, s); } finally { syncing = was; }
    openPane(host, slot, key);
  }

  // The trigger remembers itself for the close-time focus restore.
  function rememberTrigger(trigger, host, slot) {
    host.__huiPaneTrigger = host.__huiPaneTrigger || {};
    host.__huiPaneTrigger[slot] = trigger;
  }

  // A trigger resolves its host by data-hui-pane-host-target (triggers
  // outside the host, e.g. a global toolbar), else by containment,
  // else the first host on the page (the documented one-host shape).
  function resolveHost(el) {
    const id = el.getAttribute('data-hui-pane-host-target');
    if (id) return document.getElementById(id);
    const inner = el.closest('[data-hui-panehost]');
    if (inner) return inner;
    return document.querySelector('[data-hui-panehost]');
  }

  document.addEventListener('click', function (e) {
    const trig = e.target && e.target.closest && e.target.closest('[data-hui-pane-open-control],[data-hui-pane-close],[data-hui-pane-swap]');
    if (trig) {
      const host = resolveHost(trig);
      if (!host) return;
      e.preventDefault();
      const open = trig.getAttribute('data-hui-pane-open-control');
      if (open) { rememberTrigger(trig, host, open); openPane(host, open, trig.getAttribute('data-hui-pane-key') || ''); return; }
      if (trig.hasAttribute('data-hui-pane-close')) {
        // Presence, not value: a bare data-hui-pane-close (empty value)
        // closes the topmost open pane.
        closePane(host, trig.getAttribute('data-hui-pane-close') || topmost(host));
        return;
      }
      const swap = trig.getAttribute('data-hui-pane-swap');
      if (swap) { rememberTrigger(trig, host, swap); swapPane(host, swap, trig.getAttribute('data-hui-pane-key') || ''); return; }
      return;
    }
    // Backdrop click in overlay mode: the sheet's scrim is the host's
    // ::before, so the click lands on the host itself.
    const host = e.target.closest && e.target.closest('[data-hui-panehost]');
    if (host && host.getAttribute('data-hui-pane-mode') === 'overlay' && e.target === host) {
      const top = topmost(host);
      if (top) closePane(host, top);
    }
  });

  function scrollOwner(host) {
    return 'panehost:' + (host.id || '');
  }

  // The responsive overlay. data-hui-pane-mode="overlay" is set ONLY
  // while the viewport is under the breakpoint AND a side pane is open
  // (the sheet's scrim and drawer chrome key off it, so an attribute
  // on a closed page would paint a scrim over everything); it is
  // REMOVED otherwise. Entering overlay mode takes the kernel's
  // refcounted scroll lock, leaving releases it.
  function syncMode(host) {
    const wantOverlay = window.matchMedia(MQ).matches && !!topmost(host);
    const isOverlay = host.getAttribute('data-hui-pane-mode') === 'overlay';
    if (wantOverlay === isOverlay) return;
    if (wantOverlay) {
      host.setAttribute('data-hui-pane-mode', 'overlay');
      if (NS.doc) NS.doc.lockScroll(scrollOwner(host));
    } else {
      host.removeAttribute('data-hui-pane-mode');
      if (NS.doc) NS.doc.unlockScroll(scrollOwner(host));
    }
  }

  // Escape closes the topmost pane ONLY in overlay mode (an inline
  // column is page content, not a light-dismiss surface), deferring to
  // an open modal widget first. Tab is trapped inside the topmost open
  // pane while in overlay mode — the same containment technique the
  // disclosure module spells, over the kernel's focus selector, never
  // touching the widgets module's private modal stack.
  document.addEventListener('keydown', function (e) {
    const host = document.querySelector('[data-hui-pane-mode="overlay"]');
    if (!host) return;
    if (e.key === 'Escape') {
      if (NS._modalStack && NS._modalStack.length > 0) return;
      const slot = topmost(host);
      if (slot) { e.preventDefault(); closePane(host, slot); }
      return;
    }
    if (e.key !== 'Tab') return;
    if (NS._modalStack && NS._modalStack.length > 0) return;
    const slot = topmost(host);
    if (!slot) return;
    const pane = paneOf(host, slot);
    if (!pane) return;
    const sel = NS._focusSel || 'a[href],button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])';
    const items = pane.querySelectorAll(sel);
    if (!items.length) { e.preventDefault(); return; }
    const first = items[0], last = items[items.length - 1];
    if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
    else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
  });

  // Programmatic API mirroring openWidget / closeWidget: the host by
  // its id, else the first host on the page.
  function findHost(hostId) {
    return document.getElementById(hostId) ||
      document.querySelector('[data-hui-panehost]');
  }
  NS.openPane = function (hostId, pane) { const h = findHost(hostId); if (h) openPane(h, pane, ''); };
  NS.closePane = function (hostId, pane) { const h = findHost(hostId); if (h) closePane(h, pane || topmost(h)); };
  NS.swapPane = function (hostId, pane) { const h = findHost(hostId); if (h) swapPane(h, pane, ''); };

  // Deep-link strip on load: the URL's parameter opens its pane after
  // hydration when the server could not pre-render it.
  function applyDeepLink(host) {
    const param = host.getAttribute('data-hui-pane-deeplink');
    if (!param) return;
    const v = new URLSearchParams(location.search).get(param);
    if (!v) return;
    const colon = v.indexOf(':');
    const slot = colon === -1 ? v : v.slice(0, colon);
    if (slot !== 'secondary' && slot !== 'tertiary') return;
    if (!paneOf(host, slot)) return;
    if (openList(host).indexOf(slot) !== -1) return;
    openPane(host, slot, '');
  }

  // Back/forward replays pane state for deep-linked hosts; hosts
  // without the marker are untouched.
  window.addEventListener('popstate', function () {
    for (const host of document.querySelectorAll('[data-hui-pane-deeplink]')) syncFromURL(host);
  });

  // Release every overlay lock and clear the mode on a client-side
  // navigation, so the next page is not left scroll-locked by a host
  // that is now detached.
  window.addEventListener('gofastr:navigate', function () {
    for (const host of document.querySelectorAll('[data-hui-panehost]')) {
      if (host.getAttribute('data-hui-pane-mode') === 'overlay') {
        host.removeAttribute('data-hui-pane-mode');
        if (NS.doc) NS.doc.unlockScroll(scrollOwner(host));
      }
    }
  });

  function scan(root) {
    const scope = root && root.querySelectorAll ? root : document;
    for (const host of within(scope, '[data-hui-panehost]')) {
      syncMode(host);
      applyDeepLink(host);
    }
  }

  // One shared matchMedia listener re-syncs every host on viewport
  // change (crossing the breakpoint widens/narrows every drawer at
  // once, no per-host listener to leak).
  window.matchMedia(MQ).addEventListener('change', function () {
    for (const host of document.querySelectorAll('[data-hui-panehost]')) syncMode(host);
  });

  scan(document);
  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;
})();
