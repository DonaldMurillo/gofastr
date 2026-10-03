// headless-menu: the behaviour module for this package's menus — the
// keyboard contract a dropdown list owes a keyboard user, on top of
// the disclosure machinery headless-disclosure owns (the aria mirror,
// Escape one level at a time, the close-on-navigate). Loaded by the
// kernel when one of its markers is on the page.
//
// The contract, per panel:
//   ArrowDown / ArrowUp   roving focus within the item's OWN panel,
//                         wrapping at the edges; a submenu's rows never
//                         leak into the parent's rotation.
//   Home / End            first / last enabled row of the own panel.
//   ArrowRight (LTR)      on a submenu parent row: open the nested
//   ArrowLeft  (swapped   panel and focus its first row; inside a
//   in RTL)               submenu: close it and return focus to the
//                         parent row.
//   Tab                   close the whole disclosure chain and let Tab
//                         fall through naturally.
//   Printable key         type-ahead jump to the next row whose label
//                         starts with the accumulated prefix (800ms
//                         reset).
//
// Activating a menuitemradio row checks it and unchecks its group
// siblings client-side — the same group anywhere in the same menu,
// submenus included; the server re-render stays authoritative for rows
// that carry RPC or Href.
//
// The overlay posture coordinates with the widget runtime: Escape
// defers to an open modal widget, and the module declares
// Requires("widgets") so the runtime's modal stack and focus
// utilities are present when a menu is used as one. Open and close
// stay the native details toggle; nothing here reimplements them.
(function () {
  'use strict';
  const NAME = 'headless-menu';
  const NS = window.__gofastr = window.__gofastr || {};
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;

  const ITEM = '[role="menuitem"],[role="menuitemradio"]';
  const DETAILS = 'details[data-hui-disclosure]';
  const TRIGGER_WRAP = '[data-hui-menu-trigger]';
  const INTERACTIVE = 'button, a, input, select, textarea, [role="button"]';

  function within(root, sel) {
    const out = [];
    if (root.matches && root.matches(sel)) out.push(root);
    if (root.querySelectorAll) out.push.apply(out, root.querySelectorAll(sel));
    return out;
  }

  // Type-ahead matches the label span (the last direct span child —
  // an icon span may precede it), falling back to textContent for
  // hand-authored menuitems without one.
  function label(n) {
    const l = n.querySelector(':scope > span:last-of-type');
    return ((l ? l.textContent : n.textContent) || '').trim().toLowerCase();
  }

  // ─── rows and panels ─────────────────────────────────────────────

  // Rows of ONE panel: the closest() filter keeps a submenu's rows out
  // of the parent's rotation, which is what makes roving focus hold at
  // depth.
  function rows(panel) {
    return Array.from(panel.querySelectorAll(ITEM)).filter(
      (n) => n.closest('[role="menu"]') === panel && n.getAttribute('aria-disabled') !== 'true'
    );
  }

  // The nested panel a parent row (a <summary>) discloses, if any.
  function subOf(row) {
    if (!row || row.tagName !== 'SUMMARY') return null;
    const d = row.parentElement;
    if (!d || d.tagName !== 'DETAILS') return null;
    return d.querySelector(':scope > [data-hui-menu-panel]');
  }

  // ─── lazy panels ─────────────────────────────────────────────────
  //
  // A lazy menu ships its rows inside an inert template as the panel's
  // only child, so page-scoped queries cannot see closed-menu rows.
  // Inflate = move the fragment's children into the panel and drop the
  // template; idempotent by construction.
  function inflateLazy(d) {
    const panel = d.querySelector(':scope > [data-hui-menu-panel]');
    const tpl = panel && panel.querySelector(':scope > template[data-hui-menu-lazy]');
    if (!tpl) return;
    panel.insertBefore(tpl.content, tpl);
    tpl.remove();
  }

  // ─── focus on open ───────────────────────────────────────────────

  document.addEventListener('toggle', (e) => {
    const d = e.target;
    if (!d || d.tagName !== 'DETAILS' || !d.hasAttribute('data-hui-menu') || !d.open) return;
    // A lazy panel's rows are still inside their template: mount them
    // BEFORE the focus lookup, or the first open lands focus nowhere.
    inflateLazy(d);
    const panel = d.querySelector(':scope > [data-hui-menu-panel]');
    if (!panel) return;
    // Scoped to the panel and its own menu boundary: when the panel's
    // first row is itself a submenu parent, a plain descendant search
    // matches a row inside the still-closed nested details first —
    // hidden, so focus() is a silent no-op and the menu opens
    // keyboard-dead. A same-panel submenu-parent summary is a
    // legitimate first row.
    const first = rows(panel)[0];
    if (first) first.focus();
  }, true);

  // ─── the keyboard contract ───────────────────────────────────────

  let typeBuf = '', typeAt = 0;

  document.addEventListener('keydown', (e) => {
    // Tab from a caller-owned trigger closes its menu before focus
    // moves on: menuitems are tabindex=-1, so Tab would otherwise jump
    // past the panel and strand the menu open.
    if (e.key === 'Tab') {
      const t = e.target;
      const w = t && t.closest && t.closest(TRIGGER_WRAP);
      if (w) {
        const d = detailsOfTrigger(w);
        if (d) closeChain(d);
      }
    }
    // Space activates a native <button> but not an <a>; an anchor
    // trigger must still open on Space, and the page must not scroll.
    if (e.key === ' ' && e.target && e.target.closest && e.target.matches('a')) {
      const w = e.target.closest(TRIGGER_WRAP);
      const d = w && detailsOfTrigger(w);
      if (d) {
        e.preventDefault();
        toggleTrigger(d);
        return;
      }
    }
    const item = e.target && e.target.closest && e.target.closest(ITEM);
    if (!item) return;
    const panel = item.closest('[role="menu"]');
    if (!panel) return;

    // Submenu open: ArrowRight in LTR, ArrowLeft in RTL. The open
    // itself runs through the native <details> toggle so the
    // disclosure module's mirror and this module's focus-on-open fire
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
    // Submenu close: ArrowLeft in LTR, ArrowRight in RTL. Only when
    // the enclosing panel IS a submenu — its own <summary> carries
    // role=menuitem (the top-level trigger summary does not).
    if (e.key === (rtl ? 'ArrowRight' : 'ArrowLeft')) {
      const d = panel.closest(DETAILS);
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
    if (e.key === 'ArrowUp') return move(idx - 1);
    if (e.key === 'Home') return move(0);
    if (e.key === 'End') return move(items.length - 1);
    if (e.key === 'Tab') {
      // Close the whole disclosure chain so focus escapes the menu,
      // not just the innermost panel; do NOT preventDefault.
      let d = panel.closest(DETAILS);
      while (d) {
        d.removeAttribute('open');
        d = d.parentElement ? d.parentElement.closest(DETAILS) : null;
      }
      return;
    }
    // Type-ahead: a printable single-character key jumps to the next
    // item whose label starts with the accumulated prefix.
    if (e.key.length === 1 && !e.ctrlKey && !e.metaKey && !e.altKey) {
      const now = Date.now();
      if (now - typeAt > 800) typeBuf = '';
      typeAt = now;
      typeBuf += e.key.toLowerCase();
      for (let i = 1; i <= items.length; i++) {
        const cand = items[(idx + i) % items.length];
        if (label(cand).startsWith(typeBuf)) {
          e.preventDefault();
          cand.focus();
          return;
        }
      }
    }
  });

  // ─── radio arbitration ───────────────────────────────────────────
  //
  // Delegated so island-swapped rows are covered. Scope: same group
  // value anywhere in the same MENU — a group may span the top panel
  // and submenus; the scope root is the outermost menu wrapper, and a
  // hand-authored radio with no menu wrapper falls back to its panel.
  // Rows without a group key form an implicit group of one.
  document.addEventListener('click', (e) => {
    const r = e.target && e.target.closest && e.target.closest('[role="menuitemradio"]');
    if (!r || r.getAttribute('aria-disabled') === 'true') return;
    let scope = r.closest('[data-hui-menu]'), up;
    if (scope) {
      while (scope.parentElement && (up = scope.parentElement.closest('[data-hui-menu]'))) scope = up;
    } else {
      scope = r.closest('[role="menu"]');
    }
    if (!scope) return;
    const group = r.getAttribute('data-hui-menu-radio');
    for (const sib of scope.querySelectorAll('[role="menuitemradio"]')) {
      const same = group === null ? sib === r : sib.getAttribute('data-hui-menu-radio') === group;
      if (same) sib.setAttribute('aria-checked', sib === r ? 'true' : 'false');
    }
  });

  // ─── caller-owned trigger elements ───────────────────────────────
  //
  // The wrapper carries the pairing hook; the module makes the first
  // interactive element inside it the disclosure controller: the aria
  // wiring at scan time, the toggle on click (preventDefault — the
  // trigger opens the menu, it does not navigate or submit), and the
  // close on Tab.
  function detailsOfTrigger(w) {
    const id = w.getAttribute('data-hui-menu-trigger');
    if (!id) return null;
    const d = document.querySelector('details[data-hui-menu="' + cssEscape(id) + '"]');
    return d && d.hasAttribute('data-hui-disclosure') ? d : null;
  }

  // headless-disclosure resolves a trigger menu's controller through
  // this exported resolver for its Escape focus-return and mirror.
  function triggerElOf(d) {
    const id = d && d.getAttribute('data-hui-menu');
    if (!id) return null;
    const w = document.querySelector('[data-hui-menu-trigger="' + cssEscape(id) + '"]');
    return w ? w.querySelector(INTERACTIVE) : null;
  }
  NS._huiMenuTriggerOf = triggerElOf;

  function cssEscape(s) {
    if (window.CSS && CSS.escape) return CSS.escape(s);
    let out = '';
    if (s.length > 0 && s.charCodeAt(0) >= 48 && s.charCodeAt(0) <= 57) {
      out = '\\3' + s.charAt(0) + ' ';
      s = s.slice(1);
    }
    return out + s.replace(/([!"#$%&'()*+,./:;<=>?@[\]^`{|}~])/g, '\\$1');
  }

  function closeChain(d) {
    for (const sub of d.querySelectorAll('details[open]')) sub.removeAttribute('open');
    d.removeAttribute('open');
  }
  function toggleTrigger(d) {
    if (d.open) closeChain(d);
    else d.setAttribute('open', '');
  }

  document.addEventListener('click', (e) => {
    if (e.defaultPrevented) return;
    const t = e.target;
    if (!t || !t.closest) return;
    const w = t.closest(TRIGGER_WRAP);
    if (!w) return;
    const d = detailsOfTrigger(w);
    if (!d) return;
    e.preventDefault();
    toggleTrigger(d);
  });

  // ─── the arrival pass ────────────────────────────────────────────

  function wireTrigger(w) {
    const d = detailsOfTrigger(w);
    if (!d) return;
    const el = w.querySelector(INTERACTIVE);
    if (!el) return;
    el.setAttribute('aria-haspopup', 'menu');
    const panel = d.querySelector(':scope > [data-hui-menu-panel]');
    if (panel && panel.id) el.setAttribute('aria-controls', panel.id);
    el.setAttribute('aria-expanded', d.open ? 'true' : 'false');
  }

  function scan(root) {
    const scope = root && root.querySelectorAll ? root : document;
    // A menu that was ALREADY open when this module loaded — a click
    // that beat the idle load, or swapped-in markup — must have its
    // lazy rows mounted here; no toggle will fire for it.
    for (const d of within(scope, 'details[data-hui-menu][open]')) inflateLazy(d);
    for (const w of within(scope, TRIGGER_WRAP)) wireTrigger(w);
  }

  scan(document);
  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;
})();
