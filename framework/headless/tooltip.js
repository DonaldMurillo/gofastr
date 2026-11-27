// headless-tooltip: the behaviour module for headless.Tooltip
// ([data-hui-tooltip] wrapper, [data-hui-tooltip-pop] role=tooltip pop).
// Without it the kit's sheet shows the pop on hover and focus. On a
// wrapper's first hover or focus (or at load, for one that already has
// either) the module makes the pop a manual popover, so it renders in the top layer where no ancestor's overflow
// clips it, marks the wrapper data-hui-tooltip-live so the sheet stops
// showing the pop by :hover, and from then on:
//   - shows the pop while the pointer is over the wrapper or the pop, or
//     focus is inside the wrapper (one tooltip open at a time);
//   - keeps it open while the pointer crosses from the trigger onto the
//     pop, so the pop can be hovered (WCAG 1.4.13);
//   - keeps it open after a tap on a trigger that cannot take focus,
//     until the next tap elsewhere, as the browser's sticky :hover does;
//   - closes it on Escape without moving the pointer or focus (an Escape
//     another handler already took is left alone), and keeps it closed
//     until both have left the wrapper;
//   - keeps a click on the pop on the pop, so it does not reach a label
//     or row the tooltip sits in, and dismisses the tooltip, so a control
//     the pop covered takes the next click;
//   - places it beside the trigger: CSS anchor positioning where the
//     skin's sheet does it, otherwise fixed coordinates on the preferred
//     side, then the opposite one, then (for left and right) below and
//     above, clamped into the viewport, kept in step on scroll and
//     resize and hidden while the trigger is clipped out of view.
(() => {
  'use strict';
  const NAME = 'headless-tooltip';
  const NS = window.__gofastr = window.__gofastr || {};
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;

  const ROOT = '[data-hui-tooltip]';
  const HIDE_DELAY = 120;
  const EDGE = 8;
  // The kit's sheet positions an upgraded pop with anchor-scope on the
  // wrapper and position-area on the pop; a browser without them, or a
  // skin that does not set them, is placed here.
  const anchorCSS = !!(window.CSS && CSS.supports && CSS.supports('anchor-scope: --x') && CSS.supports('position-area: top'));

  let open = null;
  let hideTimer = 0;

  function popOf(root) {
    for (const el of root.children) if (el.hasAttribute('data-hui-tooltip-pop')) return el;
    return null;
  }
  function upgrade(root) {
    if (root.__huiTip) return root.__huiTip;
    const pop = popOf(root);
    if (!pop || typeof pop.showPopover !== 'function') return null;
    pop.popover = 'manual';
    root.setAttribute('data-hui-tooltip-live', '');
    const area = anchorCSS ? getComputedStyle(pop).getPropertyValue('position-area').trim() : '';
    root.__huiTip = { root, pop, hovered: false, dismissed: false, pinned: false, anchored: area !== '' && area !== 'none' };
    return root.__huiTip;
  }
  function focusInside(tip) {
    const a = document.activeElement;
    return !!a && a !== document.body && tip.root.contains(a);
  }
  function hideNow() {
    clearTimeout(hideTimer);
    if (!open) return;
    const tip = open;
    open = null;
    tip.pinned = false;
    try { tip.pop.hidePopover(); } catch (_) {}
  }
  function show(tip) {
    if (tip.dismissed || !tip.root.isConnected) return;
    clearTimeout(hideTimer);
    if (open && open !== tip) hideNow();
    if (open === tip) return;
    try { tip.pop.showPopover(); } catch (_) { return; }
    open = tip;
    place(tip);
  }
  function hideSoon() {
    clearTimeout(hideTimer);
    hideTimer = setTimeout(() => {
      if (!open || open.pinned || focusInside(open)) return;
      hideNow();
      // A tooltip whose trigger still has focus, closed when this one
      // opened over it, comes back.
      const focused = document.activeElement;
      const back = focused && focused !== document.body ? tipAt(focused) : null;
      if (back && !back.dismissed) show(back);
    }, HIDE_DELAY);
  }

  // The preferred side, then its opposite, then for left and right below
  // and above, as the sheet's position-try-fallbacks; left and right
  // follow the reading direction.
  function sides(tip) {
    const want = tip.root.getAttribute('data-hui-tooltip-placement') || 'top';
    const rtl = getComputedStyle(tip.root).direction === 'rtl';
    const phys = { top: 'top', bottom: 'bottom', left: rtl ? 'right' : 'left', right: rtl ? 'left' : 'right' }[want] || 'top';
    const opposite = { top: 'bottom', bottom: 'top', left: 'right', right: 'left' }[phys];
    return phys === 'top' || phys === 'bottom' ? [phys, opposite] : [phys, opposite, 'bottom', 'top'];
  }
  // The pop's distance from its trigger: the sheet's --fui-tooltip-gap,
  // measured once, or 6px for a skin that does not set it.
  function gapOf(tip) {
    if (tip.gap === undefined) {
      const probe = document.createElement('span');
      probe.style.cssText = 'position:absolute;visibility:hidden;inline-size:var(--fui-tooltip-gap, 6px);block-size:0';
      tip.root.appendChild(probe);
      tip.gap = probe.getBoundingClientRect().width;
      probe.remove();
    }
    return tip.gap;
  }
  // Whether el is scrolled out of the viewport or of a box that clips it.
  function clippedOut(el) {
    const r = el.getBoundingClientRect();
    // Only the axes a box clips on: overflow-x: clip leaves y visible.
    const out = (c, x, y) => (y && (r.bottom <= c.top || r.top >= c.bottom)) || (x && (r.right <= c.left || r.left >= c.right));
    if (out({ top: 0, left: 0, bottom: window.innerHeight, right: window.innerWidth }, true, true)) return true;
    for (let a = el.parentElement; a && a !== document.body && a !== document.documentElement; a = a.parentElement) {
      const s = getComputedStyle(a);
      if (out(a.getBoundingClientRect(), s.overflowX !== 'visible', s.overflowY !== 'visible')) return true;
    }
    return false;
  }
  function place(tip) {
    if (tip.anchored || open !== tip) return;
    const r = tip.root.getBoundingClientRect();
    const p = tip.pop.getBoundingClientRect();
    const vw = document.documentElement.clientWidth;
    const vh = document.documentElement.clientHeight;
    const GAP = gapOf(tip);
    const at = {
      top: { top: r.top - p.height - GAP, left: r.left + (r.width - p.width) / 2 },
      bottom: { top: r.bottom + GAP, left: r.left + (r.width - p.width) / 2 },
      left: { top: r.top + (r.height - p.height) / 2, left: r.left - p.width - GAP },
      right: { top: r.top + (r.height - p.height) / 2, left: r.right + GAP },
    };
    const fits = (s) => s === 'top' ? at.top.top >= EDGE
      : s === 'bottom' ? at.bottom.top + p.height <= vh - EDGE
      : s === 'left' ? at.left.left >= EDGE
      : at.right.left + p.width <= vw - EDGE;
    const order = sides(tip);
    const side = order.find(fits) || order[0];
    const clamp = (v, size, max) => Math.min(Math.max(v, EDGE), Math.max(EDGE, max - size - EDGE));
    // Placed on its own: a skin without the kit's sheet leaves the
    // browser's popover insets and margin, which would centre it.
    tip.pop.style.inset = 'auto';
    tip.pop.style.margin = '0';
    tip.pop.style.top = clamp(at[side].top, p.height, vh) + 'px';
    tip.pop.style.left = clamp(at[side].left, p.width, vw) + 'px';
    tip.pop.style.visibility = clippedOut(tip.root) ? 'hidden' : '';
  }

  function rootOf(node) {
    return node && node.closest ? node.closest(ROOT) : null;
  }
  function tipAt(node) {
    const root = rootOf(node);
    return root ? upgrade(root) : null;
  }
  // Each upgraded tooltip from node's outward that the move to `to` leaves.
  function leftTips(node, to) {
    const left = [];
    for (let root = rootOf(node); root; root = rootOf(root.parentElement)) {
      if (to && root.contains(to)) break;
      if (root.__huiTip) left.push(root.__huiTip);
    }
    return left;
  }

  // The pointer is over every tooltip whose wrapper holds it; only the
  // innermost opens.
  document.addEventListener('pointerover', (e) => {
    const tip = tipAt(e.target);
    if (!tip) return;
    for (let root = tip.root; root; root = rootOf(root.parentElement)) {
      const t = upgrade(root);
      if (t) t.hovered = true;
    }
    show(tip);
  }, true);
  // A touch pointer goes out as the finger lifts: the tap pins the pop
  // until the next pointerdown outside its wrapper.
  document.addEventListener('pointerout', (e) => {
    for (const tip of leftTips(e.target, e.relatedTarget)) {
      tip.hovered = false;
      if (!focusInside(tip)) tip.dismissed = false;
      if (open !== tip) continue;
      if (e.pointerType === 'touch') tip.pinned = true;
      else hideSoon();
    }
  }, true);
  document.addEventListener('pointerdown', (e) => {
    if (open && open.pinned && !open.root.contains(e.target)) hideNow();
  }, true);
  document.addEventListener('focusin', (e) => {
    const tip = tipAt(e.target);
    if (tip) show(tip);
  }, true);
  document.addEventListener('focusout', (e) => {
    for (const tip of leftTips(e.target, e.relatedTarget)) {
      if (!tip.hovered) tip.dismissed = false;
      if (open === tip && !tip.hovered && !tip.pinned) hideNow();
    }
  }, true);
  // Escape closes the open tooltip and goes no further, so a dialog the
  // tooltip sits in stays open; with no tooltip open, or only one whose
  // wrapper has left the page or whose pop other code has hidden, it
  // passes through.
  document.addEventListener('keydown', (e) => {
    if (e.key !== 'Escape' || !open || e.isComposing || e.keyCode === 229 || e.defaultPrevented) return;
    if (!open.root.isConnected || !open.pop.matches(':popover-open')) { hideNow(); return; }
    open.dismissed = true;
    hideNow();
    e.stopImmediatePropagation();
    e.preventDefault();
  }, true);
  // The pop takes the pointer so it can be hovered; a click on it is a
  // click on the pop, not on the label or row the tooltip sits in, and it
  // dismisses the tooltip, so a control the pop covered takes the next.
  document.addEventListener('click', (e) => {
    const pop = e.target && e.target.closest ? e.target.closest('[data-hui-tooltip-pop]') : null;
    if (!pop || pop.popover !== 'manual') return;
    e.preventDefault();
    e.stopPropagation();
    if (open && open.pop === pop) {
      open.dismissed = true;
      hideNow();
    }
  }, true);
  let frame = 0;
  const follow = () => {
    if (!open || open.anchored || frame) return;
    frame = requestAnimationFrame(() => {
      frame = 0;
      if (open && !open.root.isConnected) { hideNow(); return; }
      if (open) place(open);
    });
  };
  window.addEventListener('scroll', follow, true);
  window.addEventListener('resize', follow);

  // A trigger focused or hovered before the module ran opens now; when
  // focus and the pointer are on different tooltips, focus wins, and the
  // hovered one is upgraded so the sheet's :hover reveal no longer shows it.
  const focused = document.activeElement;
  const focusTip = focused && focused !== document.body ? tipAt(focused) : null;
  const under = document.querySelectorAll(ROOT + ':hover');
  const hoverTip = under.length ? upgrade(under[under.length - 1]) : null;
  if (hoverTip) hoverTip.hovered = true;
  if (focusTip || hoverTip) show(focusTip || hoverTip);
})();
