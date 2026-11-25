// headless-sortabledrag: the pointer drag of this package's sortable
// lists, for mouse, pen and touch alike. It requires
// headless-sortablelist and ends every drag through that module's
// commit (NS._sortable), so a drop POSTs, announces and reverts
// exactly as a keyboard drop does.
//
// The grip is the handle: a pointer pressed there is captured by it,
// so a touch anywhere else on the row still scrolls the page (the kit
// sheet sets touch-action none on the grip alone). Nothing moves
// while the pointer does: a class marks where the row would land
// (is-drop-before or is-drop-after on a row, is-drop-target on a list
// with no other row), and the release moves the row once. A release
// off every list moves nothing and says nothing; a lost capture
// (pointercancel, the grip removed) and Escape cancel the way Escape
// cancels a keyboard grab, and a row the page re-rendered away is never
// moved. No inline style is ever written.
(function () {
  'use strict';
  const NAME = 'headless-sortabledrag';
  const NS = window.__gofastr = window.__gofastr || {};
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;

  // pd: the drag in progress; pd.at is [list, row or null, after],
  // where the row would land, or null where it would not move.
  let pd = null;

  function mark(on) {
    const a = pd.at;
    if (a) (a[1] || a[0]).classList.toggle(a[1] ? (a[2] ? 'is-drop-after' : 'is-drop-before') : 'is-drop-target', on);
  }

  function end(keep) {
    const p = pd, S = NS._sortable;
    // A row the page re-rendered mid-drag is gone: moving the detached
    // node would put a second copy in the fresh list.
    const live = p.item.isConnected, a = keep && live && p.at;
    mark(false);
    pd = null;
    S.dragging = false;
    p.item.classList.remove('is-dragging');
    try { p.grip.releasePointerCapture(p.id); } catch (_) {}
    if (!a) { if (!keep && live) S.announce(S.listOf(p.item), 'data-hui-sortable-s-cancelled'); return; }
    const src = S.listOf(p.item), prev = S.rowsOf(src);
    if (!a[1]) a[0].append(p.item); else if (a[2]) a[1].after(p.item); else a[1].before(p.item);
    S.settle(p.item, src, prev);
  }

  document.addEventListener('pointerdown', (ev) => {
    const grip = ev.target.closest?.('[data-hui-sortable-grip]');
    const item = grip?.closest('[data-hui-sortable-item]');
    if (!item || pd || ev.button || !NS._sortable) return;
    ev.preventDefault();
    NS._sortable.cancelGrab();
    try { grip.setPointerCapture(ev.pointerId); } catch (_) {}
    pd = { item, grip, id: ev.pointerId, at: null };
    NS._sortable.dragging = true;
    item.classList.add('is-dragging');
  });

  // aim marks where the row would land under the pointer at ev.
  function aim(ev) {
    if (!pd.item.isConnected) { pd.at = null; return; }
    const S = NS._sortable, el = document.elementFromPoint(ev.clientX, ev.clientY);
    const list = el?.closest('[data-hui-sortable]'), src = S.listOf(pd.item);
    mark(false);
    if (!list || (list !== src && !S.canCross(list, src))) pd.at = null;
    else {
      // Only a row of this list lands the drop: over a list nested in a
      // row, closest() would find the outer row and drop beside it.
      let row = el.closest('[data-hui-sortable-item]');
      if (row && row.parentElement !== list) row = null;
      const rest = S.rowsOf(list).filter((r) => r !== pd.item);
      if (row === pd.item) pd.at = null;
      else if (row) {
        const r = row.getBoundingClientRect();
        pd.at = [list, row, ev.clientY > r.top + r.height / 2];
      } else if (!rest.length) pd.at = [list, null];
      else {
        // A gap between rows (or the list's padding): land before the
        // first row whose middle is below the pointer, else after the last.
        const below = rest.find((x) => { const r = x.getBoundingClientRect(); return ev.clientY < r.top + r.height / 2; });
        pd.at = below ? [list, below, false] : [list, rest[rest.length - 1], true];
      }
      // A spot beside the row's own place is no move at all.
      const a = pd.at;
      if (a && a[1] && (a[2] ? a[1].nextElementSibling : a[1].previousElementSibling) === pd.item) pd.at = null;
    }
    mark(true);
  }

  document.addEventListener('pointermove', (ev) => { if (pd?.id === ev.pointerId) aim(ev); });
  document.addEventListener('pointerup', (ev) => { if (pd?.id === ev.pointerId) { aim(ev); end(true); } });
  // A removed grip loses its capture at the document, not at the grip.
  document.addEventListener('lostpointercapture', (ev) => {
    if (pd && (pd.grip === ev.target || !pd.grip.isConnected)) end(false);
  });
  document.addEventListener('keydown', (ev) => {
    if (pd && ev.key === 'Escape' && !ev.isComposing && ev.keyCode !== 229) { ev.preventDefault(); end(false); }
  });

  // Every listener is document-level and delegated; the scanner exists
  // because the kernel's contract hands every inserted subtree to the
  // module.
  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = function () {};
})();
