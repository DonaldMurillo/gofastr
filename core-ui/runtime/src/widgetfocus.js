// Keyboard dismissal and focus containment for modal/popover widgets. Loaded
// only when a mounted widget requests Escape handling or owns a backdrop.
(function () {
  'use strict';
  const G = window.__gofastr;

  if (!document.__fuiModalEsc) {
    document.__fuiModalEsc = true;
    document.addEventListener('keydown', function (e) {
      if (e.key !== 'Escape') return;
      if (G._modalStack && G._modalStack.length) {
        const name = G._modalStack[G._modalStack.length - 1];
        const top = G._widgets[name];
        if (top && top.closeOnEscape) {
          e.stopPropagation();
          G.closeWidget(name);
          return;
        }
      }
      if (G._popoverStack && G._popoverStack.length) {
        const name = G._popoverStack[G._popoverStack.length - 1];
        const top = G._widgets[name];
        if (top && top.closeOnEscape) {
          e.stopPropagation();
          G.closeWidget(name);
        }
      }
    });
  }

  if (!document.__fuiModalTab) {
    document.__fuiModalTab = true;
    document.addEventListener('keydown', function (e) {
      if (e.key !== 'Tab' || !G._modalStack || !G._modalStack.length) return;
      const name = G._modalStack[G._modalStack.length - 1];
      const root = G._widgets[name]?.root;
      if (!root) return;
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
  // rescan hook is needed — just the loader's loaded flag.
  (G.loadedModules ||= {}).widgetfocus = true;
})();
