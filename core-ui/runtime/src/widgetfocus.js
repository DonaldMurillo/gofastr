// Keyboard dismissal and focus containment for modal/popover widgets. Loaded
// only when a mounted widget requests Escape handling or owns a backdrop.
(function () {
  'use strict';
  const G = window.__gofastr;
  // A modal <dialog> outside the top layer's root (the confirm module's,
  // opened with showModal over a widget) owns the keyboard while it
  // shows: the browser makes everything else inert and closes it on
  // Escape itself, so neither handler below may act for the widget.
  const foreign = (root) => {
    let d = null;
    try { d = document.querySelector('dialog:modal'); } catch (_) {}
    return !!d && !(root && root.contains(d));
  };

  if (!document.__fuiModalEsc) {
    document.__fuiModalEsc = true;
    document.addEventListener('keydown', function (e) {
      if (e.key !== 'Escape') return;
      // The newest layer owns Escape: the top popover when it opened
      // above the top modal (one opened before sits under the scrim),
      // else the top modal. A layer that refuses Escape still owns it,
      // so the key never reaches the layer below.
      const reg = (n) => n && G._widgets && Object.prototype.hasOwnProperty.call(G._widgets, n)
        ? G._widgets[n]
        : undefined;
      const ms = G._modalStack || [], ps = G._popoverStack || [];
      const mName = ms[ms.length - 1], pName = ps[ps.length - 1];
      const m = reg(mName), p = reg(pName);
      const name = p && (!m || (p.seq || 0) > (m.seq || 0)) ? pName : mName;
      const top = name === pName ? p : m;
      if (top && top.closeOnEscape && !foreign(top.root)) {
        e.stopPropagation();
        G.closeWidget(name);
      }
    });
  }

  if (!document.__fuiModalTab) {
    document.__fuiModalTab = true;
    document.addEventListener('keydown', function (e) {
      if (e.key !== 'Tab' || !G._modalStack || !G._modalStack.length) return;
      const name = G._modalStack[G._modalStack.length - 1];
      const root = G._widgets && Object.prototype.hasOwnProperty.call(G._widgets, name)
        ? G._widgets[name]?.root
        : undefined;
      if (!root || foreign(root)) return;
      const nodes = Array.from(root.querySelectorAll(G._focusSel)).filter(function (el) {
        return el.offsetParent !== null || el === document.activeElement;
      });
      if (!nodes.length) return;
      const first = nodes[0], last = nodes[nodes.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault(); last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault(); first.focus();
      } else if (!root.contains(document.activeElement)) {
        e.preventDefault(); first.focus();
      }
    }, true);
  }

  // Document-level handlers read the live modal/popover stacks, so no
  // rescan hook is needed, just the loader's loaded flag.
  (G.loadedModules ||= {}).widgetfocus = true;
})();
