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
//   - closes it on Escape without moving the pointer or focus, and keeps
//     it closed until both have left the wrapper;
//   - places it beside the trigger: CSS anchor positioning where the
//     browser has it, otherwise fixed coordinates on the preferred side,
//     flipped when that side has no room and clamped into the viewport,
//     kept in step on scroll and resize.
(() => {
  'use strict';
  const NAME = 'headless-tooltip';
  const NS = window.__gofastr = window.__gofastr || {};
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;

  const ROOT = '[data-hui-tooltip]';
  const HIDE_DELAY = 120;
  const GAP = 6;
  const EDGE = 8;
  // The sheet positions an upgraded pop with anchor-scope on the wrapper
  // and position-area on the pop; a browser with neither is placed here.
  const anchored = !!(window.CSS && CSS.supports && CSS.supports('anchor-scope: --x') && CSS.supports('position-area: top'));

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
    root.__huiTip = { root, pop, hovered: false, dismissed: false };
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
      if (open && !open.hovered && !focusInside(open)) hideNow();
    }, HIDE_DELAY);
  }

  // The preferred side, then its opposite; left and right follow the
  // reading direction.
  function sides(tip) {
    const want = tip.root.getAttribute('data-hui-tooltip-placement') || 'top';
    const rtl = getComputedStyle(tip.root).direction === 'rtl';
    const phys = { top: 'top', bottom: 'bottom', left: rtl ? 'right' : 'left', right: rtl ? 'left' : 'right' }[want] || 'top';
    const opposite = { top: 'bottom', bottom: 'top', left: 'right', right: 'left' }[phys];
    return [phys, opposite];
  }
  function place(tip) {
    if (anchored || open !== tip) return;
    const r = tip.root.getBoundingClientRect();
    const p = tip.pop.getBoundingClientRect();
    const vw = document.documentElement.clientWidth;
    const vh = document.documentElement.clientHeight;
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
    const [want, other] = sides(tip);
    const side = fits(want) || !fits(other) ? want : other;
    const clamp = (v, size, max) => Math.min(Math.max(v, EDGE), Math.max(EDGE, max - size - EDGE));
    tip.pop.style.top = clamp(at[side].top, p.height, vh) + 'px';
    tip.pop.style.left = clamp(at[side].left, p.width, vw) + 'px';
  }

  function tipAt(node) {
    const root = node && node.closest ? node.closest(ROOT) : null;
    return root ? upgrade(root) : null;
  }

  document.addEventListener('pointerover', (e) => {
    const tip = tipAt(e.target);
    if (!tip) return;
    tip.hovered = true;
    show(tip);
  }, true);
  document.addEventListener('pointerout', (e) => {
    const root = e.target && e.target.closest ? e.target.closest(ROOT) : null;
    const tip = root && root.__huiTip;
    if (!tip || (e.relatedTarget && root.contains(e.relatedTarget))) return;
    tip.hovered = false;
    if (!focusInside(tip)) tip.dismissed = false;
    if (open === tip) hideSoon();
  }, true);
  document.addEventListener('focusin', (e) => {
    const tip = tipAt(e.target);
    if (tip) show(tip);
  }, true);
  document.addEventListener('focusout', (e) => {
    const root = e.target && e.target.closest ? e.target.closest(ROOT) : null;
    const tip = root && root.__huiTip;
    if (!tip || (e.relatedTarget && root.contains(e.relatedTarget))) return;
    if (!tip.hovered) tip.dismissed = false;
    if (open === tip && !tip.hovered) hideNow();
  }, true);
  // Escape closes the open tooltip and goes no further, so a dialog the
  // tooltip sits in stays open; with no tooltip open, or only one whose
  // wrapper has left the page, it passes through.
  document.addEventListener('keydown', (e) => {
    if (e.key !== 'Escape' || !open || e.isComposing || e.keyCode === 229) return;
    if (!open.root.isConnected) { hideNow(); return; }
    open.dismissed = true;
    hideNow();
    e.stopImmediatePropagation();
    e.preventDefault();
  }, true);
  let frame = 0;
  const follow = () => {
    if (!open || anchored || frame) return;
    frame = requestAnimationFrame(() => {
      frame = 0;
      if (open && !open.root.isConnected) { hideNow(); return; }
      if (open) place(open);
    });
  };
  window.addEventListener('scroll', follow, true);
  window.addEventListener('resize', follow);

  // A trigger focused or hovered before the module ran opens now.
  const focused = document.activeElement;
  const early = focused && focused !== document.body ? tipAt(focused) : null;
  if (early) {
    early.hovered = early.root.matches(':hover');
    show(early);
  } else {
    const under = document.querySelectorAll(ROOT + ':hover');
    const tip = under.length ? upgrade(under[under.length - 1]) : null;
    if (tip) {
      tip.hovered = true;
      show(tip);
    }
  }
})();
