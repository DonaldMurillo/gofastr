// GoFastr runtime module, Menu keyboard navigation
//
// Document-level keydown handler for [role="menuitem"] /
// [role="menuitemradio"] elements inside a [role="menu"] panel:
//   ArrowDown / ArrowUp   : roving focus within the item's OWN panel
//                           (wraps at edges). A submenu's rows never
//                           leak into the parent panel's rotation and
//                           vice versa: rows are scoped by
//                           closest('[role=menu]') === panel.
//   Home / End            : jump to first / last enabled item of the
//                           own panel
//   ArrowRight (LTR)      : on a submenu parent row, open its nested
//   ArrowLeft  (swapped   : panel and focus its first row; on any row
//   in RTL)               : inside a submenu, close that submenu and
//                           return focus to its parent row
//   Tab                   : close the whole disclosure chain and let
//                           Tab fall through naturally
//   Printable single key  : type-ahead jump to the next item whose
//                           label starts with the accumulated prefix
//                           (800ms inactivity resets the buffer; the
//                           label is the .ui-menu__label child so the
//                           radio check and submenu caret pseudo-
//                           elements never pollute the match)
//
// Click handler: activating a [role="menuitemradio"] row checks it and
// unchecks its group siblings client-side — every [data-fui-menu-radio]
// row of the same group within the same MENU, submenus included (the
// same mutex ToggleAction groups use). The server stays the source of
// truth for a row carrying RPC/Href; this only keeps pure-client menus
// from reading stale until a re-render.
//
// Loads on demand:
//   - core's marker scanner watches for [data-fui-menu] and idle-loads
//     this module (nested submenu <details data-fui-menu> rows hit the
//     same entry).
//   - hover/focus prefetch via data-fui-prefetch="menu" warms it.
//
// The "focus first menuitem on disclosure open" behaviour lives in the
// disclosure module's toggle listener, integrated with the
// aria-expanded mirror, not worth duplicating across modules.
(() => {
  'use strict';
  window.__gofastr = window.__gofastr || {};
  const NS = window.__gofastr;

  const ITEM = '[role="menuitem"],[role="menuitemradio"]';
  const FUI_DETAILS = 'details[data-fui-disclosure]';

  // Rows of ONE panel: the closest() filter keeps a submenu's rows out
  // of the parent's rotation, which is what makes roving focus hold at
  // depth instead of walking straight through the submenu boundary.
  const rows = (panel) =>
    Array.from(panel.querySelectorAll(ITEM)).filter(
      (n) => n.closest('[role="menu"]') === panel && n.getAttribute('aria-disabled') !== 'true'
    );

  // Type-ahead matches the label span, falling back to textContent for
  // hand-written menuitems without one.
  const label = (n) => {
    const l = n.querySelector(':scope > .ui-menu__label');
    return ((l ? l.textContent : n.textContent) || '').trim().toLowerCase();
  };

  // The nested panel a parent row (a <summary>) discloses, if any.
  const subOf = (row) => {
    if (!row || row.tagName !== 'SUMMARY') return null;
    const d = row.parentElement;
    if (!d || d.tagName !== 'DETAILS') return null;
    return d.querySelector(':scope > [role="menu"]');
  };

  let _menuTypeBuf = '', _menuTypeAt = 0;
  document.addEventListener('keydown', (e) => {
    const item = e.target && e.target.closest && e.target.closest(ITEM);
    if (!item) return;
    const panel = item.closest('[role="menu"]');
    if (!panel) return;

    // Submenu open: ArrowRight in LTR, ArrowLeft in RTL. The open
    // itself runs through the native <details> toggle so the
    // disclosure module's aria-expanded mirror and focus-on-open fire
    // exactly as they do for a pointer click.
    const rtl = getComputedStyle(item).direction === 'rtl';
    if (e.key === (rtl ? 'ArrowLeft' : 'ArrowRight')) {
      const sub = item.getAttribute('aria-disabled') === 'true' ? null : subOf(item);
      if (sub) {
        e.preventDefault();
        sub.parentElement.setAttribute('open', '');
        const first = rows(sub)[0];
        if (first) first.focus();
      }
      return;
    }
    // Submenu close: ArrowLeft in LTR, ArrowRight in RTL. Only when the
    // enclosing panel IS a submenu — its own <summary> carries
    // role=menuitem (the top-level trigger summary does not).
    if (e.key === (rtl ? 'ArrowRight' : 'ArrowLeft')) {
      const d = panel.closest(FUI_DETAILS);
      const s = d && d.querySelector(':scope > summary');
      if (s && s.getAttribute('role') === 'menuitem') {
        e.preventDefault();
        d.removeAttribute('open');
        s.focus();
      }
      return;
    }

    const items = rows(panel);
    if (items.length === 0) return;
    const idx = items.indexOf(item);
    const move = (to) => {
      e.preventDefault();
      items[(to + items.length) % items.length].focus();
    };
    if (e.key === 'ArrowDown') return move(idx + 1);
    if (e.key === 'ArrowUp')   return move(idx - 1);
    if (e.key === 'Home')      return move(0);
    if (e.key === 'End')       return move(items.length - 1);
    if (e.key === 'Tab') {
      // Close the whole disclosure chain (scoped to data-fui-disclosure
      // so an unrelated plain <details> ancestor is left alone) so
      // focus escapes the menu, not just the innermost panel.
      let d = panel.closest(FUI_DETAILS);
      while (d) {
        d.removeAttribute('open');
        d = d.parentElement ? d.parentElement.closest(FUI_DETAILS) : null;
      }
      return; // do NOT preventDefault, let Tab move focus
    }
    // Type-ahead: a printable single-character key jumps to the
    // next item whose label starts with the accumulated prefix.
    if (e.key.length === 1 && !e.ctrlKey && !e.metaKey && !e.altKey) {
      const now = Date.now();
      if (now - _menuTypeAt > 800) _menuTypeBuf = '';
      _menuTypeAt = now;
      _menuTypeBuf += e.key.toLowerCase();
      for (let i = 1; i <= items.length; i++) {
        const cand = items[(idx + i) % items.length];
        if (label(cand).startsWith(_menuTypeBuf)) {
          e.preventDefault();
          cand.focus();
          return;
        }
      }
    }
  });

  // Radio arbitration, delegated so island-swapped rows are covered.
  // Scope: same data-fui-menu-radio group value anywhere in the same
  // MENU — a group may span the top panel and submenus, so a theme
  // picker split across a "More" submenu keeps exactly one checked
  // row. The scope root is the OUTERMOST [data-fui-menu] (every
  // nested submenu carries its own wrapper, so a bare closest() would
  // stop one level short and split the group again); a hand-authored
  // radio with no menu wrapper anywhere falls back to its [role=menu]
  // panel. Rows without a group key form an implicit group of one
  // (the row still self-checks).
  document.addEventListener('click', (e) => {
    const r = e.target && e.target.closest && e.target.closest('[role="menuitemradio"]');
    if (!r || r.getAttribute('aria-disabled') === 'true') return;
    let scope = r.closest('[data-fui-menu]'), up;
    if (scope) {
      while (scope.parentElement && (up = scope.parentElement.closest('[data-fui-menu]'))) scope = up;
    } else {
      scope = r.closest('[role="menu"]');
    }
    if (!scope) return;
    const group = r.getAttribute('data-fui-menu-radio');
    for (const sib of scope.querySelectorAll('[role="menuitemradio"]')) {
      // group === null (a hand-authored radio with no group key, never
      // emitted by framework/ui) is an implicit group of ONE: only the
      // activated row, never every radio in the scope.
      const same = group === null ? sib === r : sib.getAttribute('data-fui-menu-radio') === group;
      if (same) {
        sib.setAttribute('aria-checked', sib === r ? 'true' : 'false');
      }
    }
  });

  (NS.loadedModules ||= {}).menu = true;
})();
