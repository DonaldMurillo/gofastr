// Shortcut runtime module — bindings for data-fui-shortcut-focus +
// data-fui-shortcut-click, inside widgets and out. This module is the
// sole owner of shortcut wiring (widgets.js demand-loads it via the
// marker scan; app chrome — GlobalSearch in a page header, ⌘K hint in
// a marketing nav — uses the same declarative syntax).
//
// One document-level keydown listener queries the LIVE DOM per press.
// No per-element listeners: remounted widgets (close → reopen builds
// fresh DOM) need no rebinding, and detached first-mount elements can
// never fire or accumulate handlers.
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

  function matches(e, combo) {
    const m = parseCombo(combo);
    if (!m.key) return false;
    if (e.key.toLowerCase() !== m.key) return false;
    if (m.mod && !(e.metaKey || e.ctrlKey)) return false;
    if (m.shift && !e.shiftKey) return false;
    if (m.alt && !e.altKey) return false;
    // Don't intercept while typing into a text-like input — except
    // when the chord includes a modifier (then it's an intentional
    // hotkey, not a typed character).
    const inField = document.activeElement && /^(INPUT|TEXTAREA|SELECT)$/.test(document.activeElement.tagName);
    if (inField && !m.mod && !m.alt) return false;
    return true;
  }

  function resolveTarget(el) {
    const sel = el.getAttribute('data-fui-shortcut-target');
    if (sel) {
      const t = el.querySelector(sel) || document.querySelector(sel);
      if (t) return t;
    }
    return el;
  }

  if (!document.__fuiShortcutDoc) {
    document.__fuiShortcutDoc = true;
    document.addEventListener('keydown', function (e) {
      if (e.isComposing) return;
      // First connected match wins — deterministic when a chord is
      // declared on more than one element (e.g. a stale SSR duplicate).
      const els = document.querySelectorAll('[data-fui-shortcut-focus],[data-fui-shortcut-click]');
      for (const el of els) {
        if (!el.isConnected) continue;
        const focusCombo = el.getAttribute('data-fui-shortcut-focus');
        if (focusCombo && matches(e, focusCombo)) {
          e.preventDefault();
          const target = resolveTarget(el);
          try { target.focus(); target.select && target.select(); } catch (_) {}
          return;
        }
        const clickCombo = el.getAttribute('data-fui-shortcut-click');
        if (clickCombo && matches(e, clickCombo)) {
          e.preventDefault();
          el.click();
          return;
        }
      }
    });
  }

  // Standard module self-registration. The live-DOM listener needs no
  // per-element wiring, so the scanner is a no-op kept for the loop
  // contract; the loaded flag stops _scanForModules re-fetching.
  window.__gofastr = window.__gofastr || {};
  (window.__gofastr._moduleScanners ||= {}).shortcut = function () {};
  (window.__gofastr.loadedModules ||= {}).shortcut = true;
  // Legacy hook preserved for external callers.
  window.__gofastr.shortcut = { rescan: function () {} };
})();
