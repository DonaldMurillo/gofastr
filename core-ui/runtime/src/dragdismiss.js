// GoFastr runtime module — DragDismiss
//
// Pointer-driven drag-to-close for widgets whose Definition opts in
// via DragDismiss (data-fui-drag-dismiss="true" on the widget root,
// data-fui-drag-handle="true" on the visible handle bar — e.g.
// preset.BottomSheet). Drag is only initiated from the handle so taps
// inside the panel content (scrolling, form input) don't accidentally
// dismiss the sheet.
//
// Thresholds: close on >80px downward distance OR >0.5px/ms downward
// velocity. Snap back otherwise. data-fui-dragging is mirrored onto
// the widget root while the gesture is active (CSS suppresses entrance
// animation and transitions so the live transform isn't fought).
//
// Loads on demand: core's module scanner watches
// [data-fui-drag-dismiss="true"] — present at boot for SSR-inlined
// sheets, and caught by the MutationObserver scan when widget chrome
// is appended to <body> on open. Listeners are document-level and
// installed once (guarded), so no per-navigation rescan is needed.
(() => {
  'use strict';
  window.__gofastr = window.__gofastr || {};
  const NS = window.__gofastr;

  if (!document.__fuiDragDismissDispatch) {
    document.__fuiDragDismissDispatch = true;
    const DISTANCE_THRESHOLD = 80;
    const VELOCITY_THRESHOLD = 0.5; // px per ms
    let active = null;
    document.addEventListener('pointerdown', (e) => {
      if (active) return;
      const handle = e.target && e.target.closest && e.target.closest('[data-fui-drag-handle="true"]');
      if (!handle) return;
      const widget = handle.closest('[data-fui-drag-dismiss="true"]');
      if (!widget) return;
      const name = widget.getAttribute('data-fui-widget') || '';
      // Only primary pointer (left mouse / single touch).
      if (e.button !== undefined && e.button > 0) return;
      active = {
        widget, name, pointerId: e.pointerId,
        startY: e.clientY, startTime: Date.now(),
        lastY: e.clientY, lastTime: Date.now(),
      };
      widget.setAttribute('data-fui-dragging', 'true');
      try { widget.setPointerCapture(e.pointerId); } catch (_) {}
    }, true);
    document.addEventListener('pointermove', (e) => {
      if (!active || e.pointerId !== active.pointerId) return;
      const dy = Math.max(0, e.clientY - active.startY);
      active.widget.style.transform = 'translateY(' + dy + 'px)';
      active.lastY = e.clientY;
      active.lastTime = Date.now();
    }, true);
    function finishDrag(close) {
      const w = active.widget;
      const name = active.name;
      try { w.releasePointerCapture(active.pointerId); } catch (_) {}
      w.removeAttribute('data-fui-dragging');
      if (close && name) {
        if (typeof NS.closeWidget === 'function') {
          try { NS.closeWidget(name); } catch (_) {}
        }
        // Clear transform AFTER close so the panel doesn't briefly
        // snap back before unmount.
        setTimeout(() => { try { w.style.transform = ''; } catch (_) {} }, 0);
      } else {
        // Snap back to the resting position.
        w.style.transform = '';
      }
      active = null;
    }
    document.addEventListener('pointerup', (e) => {
      if (!active || e.pointerId !== active.pointerId) return;
      const dy = Math.max(0, e.clientY - active.startY);
      const dt = Math.max(1, Date.now() - active.startTime);
      const velocity = dy / dt;
      const shouldClose = dy > DISTANCE_THRESHOLD || velocity > VELOCITY_THRESHOLD;
      finishDrag(shouldClose);
    }, true);
    document.addEventListener('pointercancel', (e) => {
      if (!active || e.pointerId !== active.pointerId) return;
      finishDrag(false);
    }, true);
  }

  (NS.loadedModules ||= {}).dragdismiss = true;
})();
