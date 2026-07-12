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
// A trigger resolves its host via closest [data-fui-pane-host], or via
// data-fui-pane-host-target="<id>" for triggers outside the host.
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

  // Per-instance state. Multiple pane-hosts per page must not share it.
  const st = (host) => {
    let s = states.get(host);
    if (!s) { s = { stack: [], triggers: {} }; states.set(host, s); }
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

  function openPane(host, pane, trigger) {
    if (!has(host, pane)) return;
    const s = st(host);
    const el = paneEl(host, pane);
    el.removeAttribute('hidden');
    host.classList.add('ui-pane-host--' + pane + '-open');
    if (!s.stack.includes(pane)) s.stack.push(pane);
    if (trigger) s.triggers[pane] = trigger;
    focusFirst(el);
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
    host.dispatchEvent(new CustomEvent('pane-host:close', { bubbles: true, detail: { pane } }));
    const trig = s.triggers[pane];
    if (trig && typeof trig.focus === 'function') {
      try { trig.focus({ preventScroll: true }); } catch (_) { trig.focus(); }
    }
    delete s.triggers[pane];
    syncMode(host);
  }

  function swapPane(host, pane) {
    if (!has(host, pane)) return;
    const sib = sibling(pane);
    if (has(host, sib) && st(host).stack.indexOf(sib) >= 0) closePane(host, sib);
    openPane(host, pane, null);
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
        swapPane(host, trig.getAttribute('data-fui-pane-swap'));
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
  NS.swapPane = (hostId, pane) => { const h = findHost(hostId); if (h) swapPane(h, pane); };

  // Single delegated listeners (installed once).
  document.addEventListener('click', onClick);
  document.addEventListener('keydown', onKeydown);

  // One shared matchMedia listener re-syncs every host on viewport change.
  window.matchMedia(MQ).addEventListener('change', () => {
    document.querySelectorAll('[data-fui-pane-host]').forEach(syncMode);
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
