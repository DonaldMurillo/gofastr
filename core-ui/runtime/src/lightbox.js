// Lightbox runtime module — arrow nav + image preload + tiny extras
// on top of the framework's preset.Modal machinery.
//
// Responsibilities (all opt-in via Lightbox config):
//
//   1. On modal open, identify the gallery group (deeplink param
//      `group=<id>`) and remember the ordered sequence of trigger
//      anchors that share data-fui-lightbox-group=<id>. The current
//      index is the position of the trigger that opened the modal.
//
//   2. Prev/Next button click → step the index, programmatically
//      click the next trigger so the existing data-fui-open + signal
//      pipeline swaps src/alt/caption.
//
//   3. ArrowLeft / ArrowRight while the modal is open → same.
//
//   4. After every open, preload the previous + next image in the
//      sequence so Arrow-key nav feels instant.
//
// Loaded on-demand when an [data-fui-lightbox] marker is in the DOM.
(function () {
  'use strict';

  // Per-open state. Reset whenever the modal opens or closes.
  let state = null;

  function findViewer() {
    return document.querySelector('[data-fui-comp="ui-lightbox"][data-fui-lightbox]');
  }

  function modalOf(viewer) {
    return viewer && viewer.closest('[data-fui-widget]');
  }

  function isOpen(modal) {
    return modal && !modal.hasAttribute('hidden');
  }

  function siblingsFor(group) {
    if (!group) return [];
    return Array.from(document.querySelectorAll(
      '[data-fui-lightbox-group="' + cssEscape(group) + '"]'
    ));
  }

  function cssEscape(s) {
    // Minimal escape — group ids are framework-generated and only
    // contain [a-zA-Z0-9-]; tightening this if user-controlled is a
    // follow-up.
    return s.replace(/[^a-zA-Z0-9_\-]/g, '\\$&');
  }

  function preloadAdjacent() {
    if (!state || state.siblings.length < 2) return;
    const n = state.siblings.length;
    const prev = state.siblings[(state.index - 1 + n) % n];
    const next = state.siblings[(state.index + 1) % n];
    [prev, next].forEach(function (a) {
      const src = srcOf(a);
      if (!src) return;
      if (state.preloaded[src]) return;
      const img = new Image();
      img.src = src;
      state.preloaded[src] = true;
    });
  }

  function srcOf(anchor) {
    // The trigger's data-fui-deeplink has src=…&alt=…&caption=…&group=…
    // Pull the src value directly without round-tripping signals.
    const dl = anchor.getAttribute('data-fui-deeplink') || '';
    for (const pair of dl.split('&')) {
      const eq = pair.indexOf('=');
      if (eq < 0) continue;
      const k = decodeURIComponent(pair.slice(0, eq));
      if (k === 'src') return decodeURIComponent(pair.slice(eq + 1));
    }
    return '';
  }

  function parseDeeplink(s) {
    const out = {};
    if (!s) return out;
    for (const pair of s.split('&')) {
      const eq = pair.indexOf('=');
      if (eq < 0) continue;
      out[decodeURIComponent(pair.slice(0, eq))] = decodeURIComponent(pair.slice(eq + 1));
    }
    return out;
  }

  function step(delta) {
    if (!state) return;
    const n = state.siblings.length;
    if (n < 2) return;
    let i = state.index + delta;
    if (state.loop || (i >= 0 && i < n)) {
      i = (i + n) % n;
    } else {
      return; // at end, no-loop — silently no-op.
    }
    state.index = i;
    // Instead of synthesizing a click on the sibling anchor (which can
    // bubble into the default <a target="_blank"> navigation path on
    // some browsers and is generally indirect), call the runtime's
    // openWidget directly with the parsed deeplink params. openWidget
    // is idempotent on an already-open widget — it just re-fires
    // setSignal for each declared DeepLinkParam, which is exactly
    // what we want: src / alt / caption / group signals update in
    // place, the bound <img src> swaps via the signal pipeline.
    const dl = state.siblings[i].getAttribute('data-fui-deeplink') || '';
    const params = parseDeeplink(dl);
    const ns = window.__gofastr;
    const widgetName = state.modal.getAttribute('data-fui-widget');
    if (ns && typeof ns.openWidget === 'function' && widgetName) {
      ns.openWidget(widgetName, { params: params, pushUrl: false });
    }
    requestAnimationFrame(preloadAdjacent);
  }

  function recordOpen() {
    const viewer = findViewer();
    if (!viewer) { state = null; return; }
    const modal = modalOf(viewer);
    if (!isOpen(modal)) { state = null; return; }

    // group signal is mirrored into a hidden element via
    // data-fui-signal="group" — but we read directly from the global
    // signal store for resilience.
    const ns = window.__gofastr || {};
    const groupSig = ns._signals && ns._signals.group ? ns._signals.group.value : '';
    const group = String(groupSig || '');
    const siblings = siblingsFor(group);
    if (siblings.length === 0) { state = null; return; }
    const curSrc = ns._signals && ns._signals.src ? String(ns._signals.src.value || '') : '';
    // Match by deeplink src value to find current index. Falls back
    // to 0 when no match (group out of sync).
    let idx = 0;
    for (let i = 0; i < siblings.length; i++) {
      if (srcOf(siblings[i]) === curSrc) { idx = i; break; }
    }
    state = {
      modal: modal,
      siblings: siblings,
      index: idx,
      loop: viewer.getAttribute('data-fui-lightbox-nav') === 'true', // arrow nav opt-in implies loop
      preloaded: {},
    };
    preloadAdjacent();
  }

  // Watch for modal open / close. The widget runtime toggles `hidden`
  // on the [data-fui-widget] element when the user opens / closes,
  // so a MutationObserver on `hidden` attr is the canonical hook.
  function watch(modal) {
    if (!modal || modal.dataset.fuiLightboxWatched === '1') return;
    modal.dataset.fuiLightboxWatched = '1';
    new MutationObserver(function (records) {
      for (const r of records) {
        if (r.attributeName === 'hidden') {
          if (isOpen(modal)) {
            // Defer one tick so deeplink-driven signal updates land
            // before we measure.
            setTimeout(recordOpen, 0);
          } else {
            state = null;
          }
          break;
        }
      }
    }).observe(modal, { attributes: true });
  }

  function scan(root) {
    const scope = root && root.querySelectorAll ? root : document;
    scope.querySelectorAll('[data-fui-comp="ui-lightbox"][data-fui-lightbox]').forEach(function (v) {
      const m = modalOf(v);
      if (m) watch(m);
    });
  }

  // Prev/Next button clicks.
  // State may be null when the modal mounted catalog-lazily AFTER
  // this module's initial scan (no MutationObserver was attached
  // because the modal didn't exist yet). Bootstrap on-demand by
  // calling recordOpen() before stepping.
  document.addEventListener('click', function (ev) {
    const prev = ev.target && ev.target.closest && ev.target.closest('[data-fui-lightbox-prev]');
    if (prev) { ev.preventDefault(); if (!state) recordOpen(); step(-1); return; }
    const next = ev.target && ev.target.closest && ev.target.closest('[data-fui-lightbox-next]');
    if (next) { ev.preventDefault(); if (!state) recordOpen(); step(1); return; }
  });

  // ArrowLeft / ArrowRight while modal is open. Same bootstrap-on-
  // demand behaviour as the Prev/Next click handler.
  document.addEventListener('keydown', function (ev) {
    if (ev.key !== 'ArrowLeft' && ev.key !== 'ArrowRight') return;
    if (!state) recordOpen();
    if (!state || !isOpen(state.modal)) return;
    ev.preventDefault();
    step(ev.key === 'ArrowLeft' ? -1 : 1);
  });

  scan(document);
  document.addEventListener('gofastr:navigate', function () { scan(document); });
  window.__gofastr = window.__gofastr || {};
  window.__gofastr.lightbox = { rescan: scan };
})();
