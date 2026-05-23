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
    if (typeof pinchScannerHook === 'function') pinchScannerHook(root);
  }

  // Prev/Next button clicks.
  // State may be null when the modal mounted catalog-lazily AFTER
  // this module's initial scan (no MutationObserver was attached
  // because the modal didn't exist yet). Bootstrap on-demand by
  // calling recordOpen() before stepping.
  //
  // TODO(security/correctness): the click + keydown handlers are
  // attached on document and the shared `state` is module-scoped,
  // so two Lightbox widgets on one page can cross-talk (Prev on
  // widget B steps widget A's group). The signal names src/group/
  // alt/caption are also unnamespaced — opening lightbox A leaves
  // lightbox B's bound nodes flashing. Scope handlers to the
  // closest `[data-fui-lightbox]` ancestor and namespace signals
  // per-widget when a real two-instance use case lands.
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

  // ─── Pinch-to-zoom ─────────────────────────────────────────────────
  // Two-pointer pinch on the .ui-lightbox__full <img>:
  //   - Scale is bounded to [1.0, 4.0]; below 1.0 snaps back on release.
  //   - When scaled >1.0, single-pointer drag pans the image.
  //   - Double-tap toggles 1× ↔ 2×.
  //
  // The pinch state is per-viewer instance (one open lightbox at a time
  // in practice, but we key by the element to stay robust). On modal
  // close the transform is reset so the next open starts at 1×.
  function _installPinchZoom() {
    if (document.__fuiLightboxPinch) return;
    document.__fuiLightboxPinch = true;
    const MIN_SCALE = 1;
    const MAX_SCALE = 4;
    const TAP_DELAY = 280; // ms
    const TAP_DISTANCE = 12; // px
    const zoomStates = new WeakMap();
    function getState(img) {
      let s = zoomStates.get(img);
      if (!s) {
        s = {
          scale: 1, tx: 0, ty: 0,
          pointers: new Map(),
          pinchStartDist: 0, pinchStartScale: 1,
          panStartX: 0, panStartY: 0, panStartTx: 0, panStartTy: 0,
          lastTapTime: 0, lastTapX: 0, lastTapY: 0,
        };
        zoomStates.set(img, s);
      }
      return s;
    }
    function apply(img, st) {
      img.style.transform =
        'translate(' + st.tx + 'px, ' + st.ty + 'px) scale(' + st.scale + ')';
      img.style.transformOrigin = 'center center';
      img.toggleAttribute('data-fui-zoomed', st.scale > 1.0001);
    }
    function reset(img, st) {
      st.scale = 1; st.tx = 0; st.ty = 0;
      st.pointers.clear();
      img.style.transform = '';
      img.removeAttribute('data-fui-zoomed');
    }
    function midpoint(p1, p2) {
      return { x: (p1.x + p2.x) / 2, y: (p1.y + p2.y) / 2 };
    }
    function distance(p1, p2) {
      const dx = p1.x - p2.x, dy = p1.y - p2.y;
      return Math.hypot(dx, dy);
    }
    function isLightboxImg(target) {
      return target && target.matches && target.matches('.ui-lightbox__full');
    }
    document.addEventListener('pointerdown', (e) => {
      if (!isLightboxImg(e.target)) return;
      const img = e.target;
      const st = getState(img);
      st.pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
      try { img.setPointerCapture(e.pointerId); } catch (_) {}
      // Two pointers → start pinch.
      if (st.pointers.size === 2) {
        const [a, b] = Array.from(st.pointers.values());
        st.pinchStartDist = distance(a, b);
        st.pinchStartScale = st.scale;
      } else if (st.pointers.size === 1) {
        // One pointer → record pan start (only active when zoomed).
        const p = Array.from(st.pointers.values())[0];
        st.panStartX = p.x; st.panStartY = p.y;
        st.panStartTx = st.tx; st.panStartTy = st.ty;
        // Stash for double-tap detection on pointerup.
        st._tapCandidate = { x: p.x, y: p.y, t: Date.now() };
      }
    }, true);
    document.addEventListener('pointermove', (e) => {
      if (!isLightboxImg(e.target)) return;
      const img = e.target;
      const st = getState(img);
      if (!st.pointers.has(e.pointerId)) return;
      st.pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
      if (st.pointers.size === 2) {
        const [a, b] = Array.from(st.pointers.values());
        const d = distance(a, b);
        if (st.pinchStartDist > 0) {
          let s = st.pinchStartScale * (d / st.pinchStartDist);
          if (s < MIN_SCALE * 0.6) s = MIN_SCALE * 0.6; // allow brief overshoot
          if (s > MAX_SCALE) s = MAX_SCALE;
          st.scale = s;
          apply(img, st);
          st._tapCandidate = null;
        }
      } else if (st.pointers.size === 1 && st.scale > 1.001) {
        const p = Array.from(st.pointers.values())[0];
        const dx = p.x - st.panStartX;
        const dy = p.y - st.panStartY;
        st.tx = st.panStartTx + dx;
        st.ty = st.panStartTy + dy;
        apply(img, st);
        st._tapCandidate = null;
      }
    }, true);
    function endPointer(e, cancelled) {
      if (!isLightboxImg(e.target)) return;
      const img = e.target;
      const st = getState(img);
      const tap = st._tapCandidate;
      st.pointers.delete(e.pointerId);
      try { img.releasePointerCapture(e.pointerId); } catch (_) {}
      // Snap-back below 1×.
      if (st.pointers.size === 0 && st.scale < MIN_SCALE) {
        st.scale = MIN_SCALE; st.tx = 0; st.ty = 0;
        apply(img, st);
      }
      if (cancelled) return;
      // Double-tap detection (single-pointer, didn't move much).
      if (st.pointers.size === 0 && tap) {
        const now = Date.now();
        const movedX = Math.abs(e.clientX - tap.x);
        const movedY = Math.abs(e.clientY - tap.y);
        if (movedX < TAP_DISTANCE && movedY < TAP_DISTANCE) {
          if (now - st.lastTapTime < TAP_DELAY &&
              Math.abs(tap.x - st.lastTapX) < TAP_DISTANCE &&
              Math.abs(tap.y - st.lastTapY) < TAP_DISTANCE) {
            // Toggle 1× ↔ 2×.
            if (st.scale > 1.001) {
              st.scale = 1; st.tx = 0; st.ty = 0;
            } else {
              st.scale = 2;
            }
            apply(img, st);
            st.lastTapTime = 0;
            return;
          }
          st.lastTapTime = now;
          st.lastTapX = tap.x;
          st.lastTapY = tap.y;
        }
      }
    }
    document.addEventListener('pointerup', (e) => endPointer(e, false), true);
    document.addEventListener('pointercancel', (e) => endPointer(e, true), true);

    // Reset zoom whenever the lightbox modal closes.
    function resetAllOnClose(modal) {
      if (isOpen(modal)) return;
      modal.querySelectorAll('.ui-lightbox__full').forEach((img) => {
        const st = zoomStates.get(img);
        if (st) reset(img, st);
      });
    }
    // Watch the lightbox modal for hidden-attr changes (open/close)
    // so we reset zoom on close. Runs whenever the main `scan()` finds
    // a viewer (initial + SPA-nav + MutationObserver re-scan).
    pinchScannerHook = function (root) {
      const scope = root && root.querySelectorAll ? root : document;
      scope.querySelectorAll('[data-fui-comp="ui-lightbox"][data-fui-lightbox]').forEach((v) => {
        const m = modalOf(v);
        if (!m || m.dataset.fuiLightboxPinchWatched === '1') return;
        m.dataset.fuiLightboxPinchWatched = '1';
        new MutationObserver((records) => {
          for (const r of records) {
            if (r.attributeName === 'hidden') resetAllOnClose(m);
          }
        }).observe(m, { attributes: true });
      });
    };
  }
  let pinchScannerHook = null;
  _installPinchZoom();

  scan(document);
  document.addEventListener('gofastr:navigate', function () { scan(document); });
  window.__gofastr = window.__gofastr || {};
  window.__gofastr.lightbox = { rescan: scan };
})();
