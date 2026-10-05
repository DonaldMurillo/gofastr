// GoFastr runtime module, Popover anchoring
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
//   - core.js's marker scanner picks up [data-cui-popover-anchor] on
//     a page and idle-loads this module.
//   - hover/focus prefetch via data-cui-prefetch="popover" warms it.
//   - the data-cui-open click handler awaits loadModule('popover')
//     before invoking __gofastr._anchorPopover so the very first
//     click has no positioning flicker.
(() => {
  'use strict';
  window.__gofastr = window.__gofastr || {};
  const NS = window.__gofastr;

  /**
   * @param {string} name     Widget name
   * @param {Element} trigger Element that fired the open (data-cui-open)
   * @param {string} preferred One of "top", "bottom", "left", "right",
   *                           or "auto" (= bottom-first, then top,
   *                           right, left).
   */
  NS._anchorPopover = async function (name, trigger, preferred) {
    const widget = NS._widgets
      && Object.prototype.hasOwnProperty.call(NS._widgets, name)
      && NS._widgets[name];
    if (!widget || !widget.root) return;
    const root = widget.root;
    const pref = (preferred || 'auto').toLowerCase();

    // Measure only after the widget's own stylesheet has applied.
    // mountWidget appends <link data-cui-style> and does NOT await
    // its load; a place() against unstyled chrome measures a
    // full-width root (no max-inline-size yet) and the viewport
    // clamp then pins the popover to the left margin with its arrow
    // stretched back to the trigger. The link load/error events (or
    // a sheet poll for the rare browser that fires neither) settle
    // it; a missing link needs no wait.
    const link = document.querySelector('link[data-cui-style="' + CSS.escape(name) + '"]');
    if (link && !link.sheet) {
      await new Promise((resolve) => {
        let done = false;
        const settle = () => { if (!done) { done = true; resolve(); } };
        link.addEventListener('load', settle, { once: true });
        link.addEventListener('error', settle, { once: true });
        const t0 = performance.now();
        const poll = () => { if (done) return; if (link.sheet || performance.now() - t0 > 2000) settle(); else setTimeout(poll, 50); };
        poll();
      });
      // Closed while we waited (Escape, closeWidget, a navigation):
      // the widget is gone from the registry, so there is nothing to
      // anchor. Marking the trigger and attaching window listeners
      // now would leave the trigger in its open state for good and
      // the listeners running on every scroll.
      if (NS._widgets[name] !== widget || !root.isConnected) return;
    }


    // If we were already anchored to a different trigger (popover
    // re-opened from a sibling), clear the previous trigger's
    // active state + listeners before rebinding.
    const prevTrigger = widget.anchorTrigger;
    if (prevTrigger && prevTrigger !== trigger) {
      prevTrigger.classList.remove('is-popover-trigger-active');
      prevTrigger.removeAttribute('data-cui-popover-trigger');
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
    trigger.setAttribute('data-cui-popover-trigger', name);

    const place = () => {
      const gap = 10; // gap >= arrow size so the pointer fits cleanly
      const margin = 8;
      const tr = trigger.getBoundingClientRect();
      // Set the anchored marker BEFORE measuring so the chrome's
      // border + shadow + max-inline-size are reflected in the
      // bounding rect, without this the measurement is from the
      // un-styled chrome and the placement misses by a few pixels.
      if (!root.hasAttribute('data-cui-popover-side')) {
        root.setAttribute('data-cui-popover-side', 'bottom');
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
      root.setAttribute('data-cui-popover-side', chosen);
      // Arrow offset, distance from popover's anchored edge to the
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
    // Reposition on viewport resize AND on scroll, the popover is
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

  // Dismissal (Escape or an outside click) returns focus to the
  // anchored trigger when focus sat inside the closing widget, fell
  // to the body, or rests on a tabindex=-1 swap marker the navigation
  // machinery stamped — never a user tab stop. A control the user
  // actually focused keeps it. fui:widget-close is every close path's
  // single announce point and fires before teardown, so this covers
  // each dismissal without growing the widgets module.
  document.addEventListener('fui:widget-close', (e) => {
    const st = NS._widgets && NS._widgets[(e.detail || {}).name];
    const at = st && st.anchorTrigger;
    if (!at) return;
    const w = st.root;
    const ae = document.activeElement;
    if (ae === document.body || (w && w.contains && w.contains(ae)) ||
        (ae && ae !== at && ae.getAttribute('tabindex') === '-1')) {
      try { at.focus({ preventScroll: true }); } catch (_) {}
    }
  });

  (NS.loadedModules ||= {}).popover = true;
})();
