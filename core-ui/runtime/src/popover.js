// GoFastr runtime module — Popover anchoring
//
// Positions a freshly-opened popover-style widget next to its trigger
// element. Auto-flips when the preferred side would overflow the
// viewport, draws a directional arrow back to the trigger via
// --ui-popover-arrow-x / --ui-popover-arrow-y CSS variables, and
// tracks the trigger on `window.resize` + `window.scroll` (capture,
// rAF-throttled) so the popover stays glued to the trigger as the
// page reflows.
//
// Loads on demand:
//   - core.js's marker scanner picks up [data-fui-popover-anchor] on
//     a page and idle-loads this module.
//   - hover/focus prefetch via data-fui-prefetch="popover" warms it.
//   - the data-fui-open click handler awaits loadModule('popover')
//     before invoking __gofastr._anchorPopover so the very first
//     click has no positioning flicker.
(() => {
  'use strict';
  window.__gofastr = window.__gofastr || {};
  const NS = window.__gofastr;

  /**
   * @param {string} name     Widget name
   * @param {Element} trigger Element that fired the open (data-fui-open)
   * @param {string} preferred One of "top", "bottom", "left", "right",
   *                           or "auto" (= bottom-first, then top,
   *                           right, left).
   */
  NS._anchorPopover = function (name, trigger, preferred) {
    const widget = NS._widgets && NS._widgets[name];
    if (!widget || !widget.root) return;
    const root = widget.root;
    const pref = (preferred || 'auto').toLowerCase();

    // If we were already anchored to a different trigger (popover
    // re-opened from a sibling), clear the previous trigger's
    // active state + listeners before rebinding.
    const prevTrigger = widget.anchorTrigger;
    if (prevTrigger && prevTrigger !== trigger) {
      prevTrigger.classList.remove('is-popover-trigger-active');
      prevTrigger.removeAttribute('data-fui-popover-trigger');
    }
    if (widget.anchorResize) {
      window.removeEventListener('resize', widget.anchorResize);
      widget.anchorResize = null;
    }
    if (widget.anchorScroll) {
      window.removeEventListener('scroll', widget.anchorScroll, { capture: true });
      widget.anchorScroll = null;
    }
    // Mark trigger as the currently-active source.
    trigger.classList.add('is-popover-trigger-active');
    trigger.setAttribute('data-fui-popover-trigger', name);

    const place = () => {
      const gap = 10; // gap >= arrow size so the pointer fits cleanly
      const margin = 8;
      const tr = trigger.getBoundingClientRect();
      // Set the anchored marker BEFORE measuring so the chrome's
      // border + shadow + max-inline-size are reflected in the
      // bounding rect — without this the measurement is from the
      // un-styled chrome and the placement misses by a few pixels.
      if (!root.hasAttribute('data-fui-popover-side')) {
        root.setAttribute('data-fui-popover-side', 'bottom');
      }
      // Reset overrides so we measure the widget at its natural size.
      root.style.left = '';
      root.style.top = '';
      root.style.right = '';
      root.style.bottom = '';
      const wr = root.getBoundingClientRect();
      const vw = window.innerWidth;
      const vh = window.innerHeight;
      const order = (pref === 'auto')
        ? ['bottom', 'top', 'right', 'left']
        : [pref, 'bottom', 'top', 'right', 'left'].filter((s, i, a) => a.indexOf(s) === i);
      let x = tr.left;
      let y = tr.bottom + gap;
      let chosen = order[0];
      for (const side of order) {
        if (side === 'bottom') {
          x = tr.left;
          y = tr.bottom + gap;
          if (y + wr.height <= vh - margin) { chosen = 'bottom'; break; }
        } else if (side === 'top') {
          x = tr.left;
          y = tr.top - gap - wr.height;
          if (y >= margin) { chosen = 'top'; break; }
        } else if (side === 'right') {
          x = tr.right + gap;
          y = tr.top;
          if (x + wr.width <= vw - margin) { chosen = 'right'; break; }
        } else if (side === 'left') {
          x = tr.left - gap - wr.width;
          y = tr.top;
          if (x >= margin) { chosen = 'left'; break; }
        }
        chosen = side;
      }
      // Clamp into the viewport.
      x = Math.max(margin, Math.min(x, vw - wr.width - margin));
      y = Math.max(margin, Math.min(y, vh - wr.height - margin));
      root.style.position = 'fixed';
      root.style.left = x + 'px';
      root.style.top = y + 'px';
      root.style.right = 'auto';
      root.style.bottom = 'auto';
      root.setAttribute('data-fui-popover-side', chosen);
      // Arrow offset — distance from popover's anchored edge to the
      // center of the trigger, so the arrow always sits below the
      // originating button regardless of clamping.
      if (chosen === 'top' || chosen === 'bottom') {
        const arrowX = (tr.left + tr.width / 2) - x;
        root.style.setProperty('--ui-popover-arrow-x', Math.max(12, Math.min(arrowX, wr.width - 12)) + 'px');
      } else {
        const arrowY = (tr.top + tr.height / 2) - y;
        root.style.setProperty('--ui-popover-arrow-y', Math.max(12, Math.min(arrowY, wr.height - 12)) + 'px');
      }
    };
    place();
    // Reposition on viewport resize AND on scroll — the popover is
    // position:fixed, so without these the trigger moves under the
    // page scroll while the popover stays glued to the viewport.
    // Listeners run via requestAnimationFrame so we get one place()
    // per frame even on a furious wheel-spin. capture:true picks up
    // scroll events from ANY ancestor (overflow:auto containers)
    // without listing them explicitly. passive:true preserves
    // smooth scrolling.
    let rafPending = false;
    const schedulePlace = () => {
      if (rafPending) return;
      rafPending = true;
      requestAnimationFrame(() => {
        rafPending = false;
        place();
      });
    };
    const onResize = schedulePlace;
    const onScroll = schedulePlace;
    window.addEventListener('resize', onResize);
    window.addEventListener('scroll', onScroll, { passive: true, capture: true });
    widget.anchorResize = onResize;
    widget.anchorScroll = onScroll;
    widget.anchorTrigger = trigger;
  };

  (NS.loadedModules ||= {}).popover = true;
})();
