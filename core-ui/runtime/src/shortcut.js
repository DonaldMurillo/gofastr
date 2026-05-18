// Shortcut runtime module — page-level (non-widget) bindings for
// data-fui-shortcut-focus + data-fui-shortcut-click.
//
// widgets.js already binds these INSIDE widgets; this module covers
// the same markers outside widgets so app chrome (GlobalSearch in a
// page header, ⌘K hint in a marketing nav, etc.) can use the
// declarative shortcut syntax without being wrapped in a widget.
//
// Extra wrinkle: data-fui-shortcut-target lets a non-focusable
// wrapper carry the chord while focus lands on a descendant — the
// runtime queries the wrapper for the target selector and focuses
// the matched element. Useful for compositions where the chord lives
// on a wrapper but the focus target is an input deep inside (e.g. a
// styled search-bar wrapping a Combobox).
(function () {
  'use strict';

  function parseCombo(combo) {
    const parts = combo.split('+').map(function (s) { return s.trim().toLowerCase(); });
    let key = '';
    let mod = false, shift = false, alt = false;
    parts.forEach(function (p) {
      if (p === 'mod' || p === 'meta' || p === 'ctrl' || p === 'cmd') mod = true;
      else if (p === 'shift') shift = true;
      else if (p === 'alt' || p === 'option') alt = true;
      else key = p;
    });
    return { key: key, mod: mod, shift: shift, alt: alt };
  }

  function bindFocus(el) {
    if (el.dataset.fuiShortcutFocusBound === '1') return;
    el.dataset.fuiShortcutFocusBound = '1';
    const combo = el.getAttribute('data-fui-shortcut-focus') || '';
    if (!combo) return;
    const m = parseCombo(combo);
    document.addEventListener('keydown', function (e) {
      if (!m.key) return;
      if (e.key.toLowerCase() !== m.key) return;
      if (m.mod && !(e.metaKey || e.ctrlKey)) return;
      if (m.shift && !e.shiftKey) return;
      if (m.alt && !e.altKey) return;
      if (e.isComposing) return;
      // Don't intercept while typing into a text-like input — except
      // when the chord includes a modifier (then it's an intentional
      // hotkey, not a typed character).
      const inField = document.activeElement && /^(INPUT|TEXTAREA|SELECT)$/.test(document.activeElement.tagName);
      if (inField && !m.mod && !m.alt) return;
      e.preventDefault();
      const target = resolveTarget(el);
      if (target) {
        try { target.focus(); target.select && target.select(); } catch (_) {}
      }
    });
  }

  function bindClick(el) {
    if (el.dataset.fuiShortcutClickBound === '1') return;
    el.dataset.fuiShortcutClickBound = '1';
    const combo = el.getAttribute('data-fui-shortcut-click') || '';
    if (!combo) return;
    const m = parseCombo(combo);
    document.addEventListener('keydown', function (e) {
      if (!m.key) return;
      if (e.key.toLowerCase() !== m.key) return;
      if (m.mod && !(e.metaKey || e.ctrlKey)) return;
      if (m.shift && !e.shiftKey) return;
      if (m.alt && !e.altKey) return;
      if (e.isComposing) return;
      const inField = document.activeElement && /^(INPUT|TEXTAREA|SELECT)$/.test(document.activeElement.tagName);
      if (inField && !m.mod && !m.alt) return;
      e.preventDefault();
      el.click();
    });
  }

  function resolveTarget(el) {
    const sel = el.getAttribute('data-fui-shortcut-target');
    if (sel) {
      const t = document.querySelector(sel);
      if (t) return t;
    }
    return el;
  }

  function scan(root) {
    const scope = root && root.querySelectorAll ? root : document;
    scope.querySelectorAll('[data-fui-shortcut-focus]').forEach(bindFocus);
    scope.querySelectorAll('[data-fui-shortcut-click]').forEach(bindClick);
  }

  scan(document);
  document.addEventListener('gofastr:navigate', function () { scan(document); });

  window.__gofastr = window.__gofastr || {};
  window.__gofastr.shortcut = { rescan: scan };
})();
