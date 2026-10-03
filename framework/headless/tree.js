// headless-tree: the behaviour module for this package's trees, the
// WAI-ARIA tree keyboard contract on the data-hui-tree marker. The
// markup is the server's — rows, groups, links — and nothing here
// adds, removes or fills an entry. What it owns:
//
//   - the roving tabindex: one treeitem carries tabindex=0 and every
//     other carries -1; arrows, Home and End move it,
//   - expand/collapse: ArrowRight opens, ArrowLeft closes, Enter and
//     Space toggle — every keyboard path drives the same toggle
//     button a click drives, so any lazy-load wiring on the toggle
//     (the kernel's rpc primitive) fires either way,
//   - type-ahead over the visible rows,
//   - a pointer click on a row putting it into the focus state.
//
// The toggle itself is aria-hidden (the treeitem's aria-expanded is
// the state a reader gets), so after a toggle click focus is returned
// to the row: aria-hidden on a focused element is a platform warning,
// not a state. No string is said here and no inline style is written.
(function () {
  'use strict';
  const NAME = 'headless-tree';
  const NS = window.__gofastr = window.__gofastr || {};
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;

  function treeOf(el) {
    return el && el.closest && el.closest('[data-hui-tree]');
  }

  // Toggle click flips aria-expanded + child group visibility AND
  // returns focus to the treeitem (see the file comment for why it
  // must not stay on the aria-hidden toggle).
  document.addEventListener('click', function (e) {
    const toggle = e.target && e.target.closest && e.target.closest('[data-hui-tree-toggle]');
    if (!toggle) return;
    const item = toggle.closest('[role="treeitem"]');
    if (!item) return;
    const current = item.getAttribute('aria-expanded');
    if (current === null) return; // leaf: nothing to toggle
    const next = current === 'true' ? 'false' : 'true';
    item.setAttribute('aria-expanded', next);
    const group = item.querySelector(':scope > [role="group"]');
    if (group) {
      if (next === 'true') group.removeAttribute('hidden');
      else group.setAttribute('hidden', '');
    }
    const tree = treeOf(item);
    if (tree) {
      tree.querySelectorAll('[role="treeitem"][tabindex="0"]').forEach(function (n) {
        n.setAttribute('tabindex', '-1');
      });
      item.setAttribute('tabindex', '0');
    }
    item.focus();
  });

  // Click anywhere on a treeitem's row moves the roving tabindex to
  // it (and gives it focus), the WAI-ARIA tree pattern's pointer
  // recommendation. Skips the toggle (its own handler above) and any
  // interactive child (a leaf's link handles focus naturally).
  document.addEventListener('click', function (e) {
    if (!e.target || !e.target.closest) return;
    if (e.target.closest('[data-hui-tree-toggle]')) return;
    const item = e.target.closest('[role="treeitem"]');
    if (!item) return;
    const tree = treeOf(item);
    if (!tree) return;
    tree.querySelectorAll('[role="treeitem"][tabindex="0"]').forEach(function (n) {
      n.setAttribute('tabindex', '-1');
    });
    item.setAttribute('tabindex', '0');
    const interactive = e.target.closest('a, button, input, select, textarea');
    if (!interactive) item.focus();
  });

  // The visible (non-hidden) treeitems in document order, for
  // ArrowDown/Up and type-ahead.
  function treeRows(tree) {
    return Array.from(tree.querySelectorAll('[role="treeitem"]')).filter(function (n) {
      let cur = n.parentElement;
      while (cur && cur !== tree) {
        if (cur.hasAttribute && cur.hasAttribute('hidden')) return false;
        cur = cur.parentElement;
      }
      return true;
    });
  }

  // itemLabel is the row's text as a reader gets it: the aria-hidden
  // toggle's glyph is decoration, not a name, so type-ahead must not
  // match against it (the retired module's textContent read made every
  // branch row start with the arrow).
  function itemLabel(item) {
    let out = '';
    item.childNodes.forEach(function (n) {
      if (n.nodeType === 3) out += n.nodeValue;
      else if (n.nodeType === 1 && n.getAttribute('aria-hidden') !== 'true') out += itemLabel(n);
    });
    return out.trim().toLowerCase();
  }

  function focusItem(tree, item) {
    tree.querySelectorAll('[role="treeitem"][tabindex="0"]').forEach(function (n) {
      n.setAttribute('tabindex', '-1');
    });
    item.setAttribute('tabindex', '0');
    item.focus();
  }

  let typeBuf = '';
  let typeAt = 0;
  document.addEventListener('keydown', function (e) {
    const item = e.target && e.target.closest && e.target.closest('[role="treeitem"]');
    if (!item) return;
    const tree = treeOf(item);
    if (!tree) return;
    const rows = treeRows(tree);
    const idx = rows.indexOf(item);
    if (idx < 0) return;
    const move = function (to) {
      e.preventDefault();
      focusItem(tree, rows[Math.max(0, Math.min(rows.length - 1, to))]);
    };
    const expanded = item.getAttribute('aria-expanded');
    const isLeaf = expanded === null;
    // The toggle lives inside the item's row (a div), not as a
    // direct child: query without the child combinator. The item's
    // own toggle always precedes any descendant's in document order,
    // so the first match is the right one.
    const toggleOf = function () {
      return item.querySelector('[data-hui-tree-toggle]');
    };
    switch (e.key) {
      case 'ArrowDown': return move(idx + 1);
      case 'ArrowUp':   return move(idx - 1);
      case 'Home':      return move(0);
      case 'End':       return move(rows.length - 1);
      case 'ArrowRight': {
        if (isLeaf) return;
        if (expanded === 'false') {
          e.preventDefault();
          const toggle = toggleOf();
          if (toggle) toggle.click();
          else item.setAttribute('aria-expanded', 'true');
          return;
        }
        const firstChild = item.querySelector(':scope > [role="group"] > [role="treeitem"]');
        if (firstChild) {
          e.preventDefault();
          focusItem(tree, firstChild);
        }
        return;
      }
      case 'ArrowLeft': {
        if (!isLeaf && expanded === 'true') {
          e.preventDefault();
          const toggle = toggleOf();
          if (toggle) toggle.click();
          else item.setAttribute('aria-expanded', 'false');
          return;
        }
        const parent = item.parentElement && item.parentElement.closest &&
          item.parentElement.closest('[role="treeitem"]');
        if (parent) {
          e.preventDefault();
          focusItem(tree, parent);
        }
        return;
      }
      case 'Enter':
      case ' ': {
        e.preventDefault();
        if (!isLeaf) {
          const toggle = toggleOf();
          if (toggle) toggle.click();
          else item.setAttribute('aria-expanded', expanded === 'true' ? 'false' : 'true');
        } else {
          const link = item.querySelector(':scope > [role="treeitem"], :scope > a, :scope > button') ||
            item.querySelector('a, button');
          if (link) link.click();
        }
        return;
      }
    }
    // Type-ahead: a printable single-character key jumps to the next
    // visible treeitem whose label starts with the accumulated
    // prefix.
    if (e.key.length === 1 && !e.ctrlKey && !e.metaKey && !e.altKey) {
      const now = Date.now();
      if (now - typeAt > 800) typeBuf = '';
      typeAt = now;
      typeBuf += e.key.toLowerCase();
      for (let i = 1; i <= rows.length; i++) {
        const cand = rows[(idx + i) % rows.length];
        if (itemLabel(cand).startsWith(typeBuf)) {
          e.preventDefault();
          focusItem(tree, cand);
          return;
        }
      }
    }
  });

  // The listeners are document-level and delegated, so markup that
  // arrives after load needs no per-root pass; the scanner exists
  // because the kernel's contract hands every inserted subtree to the
  // module and a module that cannot be re-armed is half a module.
  function scan() {}
  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;
})();
