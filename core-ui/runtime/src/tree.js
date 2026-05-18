// GoFastr runtime module — TreeView
//
// WAI-ARIA Tree pattern: roving tabindex, arrow-key navigation,
// expand/collapse via Right/Left, Home/End, Enter/Space, and
// printable-key type-ahead.
//
// Expand/collapse is split between two listeners:
//   - A click handler on [data-fui-tree-toggle] that flips the
//     parent treeitem's aria-expanded and shows/hides the child
//     <ul role="group">. Any data-fui-rpc on the toggle still fires
//     (the core RPC dispatcher runs before this handler).
//   - A keydown handler that delegates ArrowRight/ArrowLeft to the
//     toggle (so the keyboard path goes through the same code).
//
// Loads on demand: core.js's _moduleMarkers entry triggers fetch
// when a [role="tree"] is detected in the DOM.
(() => {
  'use strict';

  if (window.__fuiTreeWired) return;
  window.__fuiTreeWired = true;

  // Toggle click flips aria-expanded + child group visibility.
  document.addEventListener('click', (e) => {
    const toggle = e.target && e.target.closest && e.target.closest('[data-fui-tree-toggle]');
    if (!toggle) return;
    const item = toggle.closest('[role="treeitem"]');
    if (!item) return;
    const current = item.getAttribute('aria-expanded');
    if (current === null) return; // leaf — nothing to toggle
    const next = current === 'true' ? 'false' : 'true';
    item.setAttribute('aria-expanded', next);
    const group = item.querySelector(':scope > [role="group"]');
    if (group) {
      if (next === 'true') group.removeAttribute('hidden');
      else group.setAttribute('hidden', '');
    }
  });

  // _treeRows walks the tree and returns the visible (non-hidden)
  // treeitems in document order — used for ArrowDown/Up nav and
  // type-ahead jumps.
  const treeRows = (tree) =>
    Array.from(tree.querySelectorAll('[role="treeitem"]')).filter((n) => {
      let cur = n.parentElement;
      while (cur && cur !== tree) {
        if (cur.hasAttribute && cur.hasAttribute('hidden')) return false;
        cur = cur.parentElement;
      }
      return true;
    });

  const focusItem = (tree, item) => {
    tree.querySelectorAll('[role="treeitem"][tabindex="0"]').forEach((n) =>
      n.setAttribute('tabindex', '-1')
    );
    item.setAttribute('tabindex', '0');
    item.focus();
  };

  let typeBuf = '';
  let typeAt = 0;
  document.addEventListener('keydown', (e) => {
    const item = e.target && e.target.closest && e.target.closest('[role="treeitem"]');
    if (!item) return;
    const tree = item.closest('[role="tree"]');
    if (!tree) return;
    const rows = treeRows(tree);
    const idx = rows.indexOf(item);
    if (idx < 0) return;
    const move = (to) => {
      e.preventDefault();
      focusItem(tree, rows[Math.max(0, Math.min(rows.length - 1, to))]);
    };
    const expanded = item.getAttribute('aria-expanded');
    const isLeaf = expanded === null;
    switch (e.key) {
      case 'ArrowDown': return move(idx + 1);
      case 'ArrowUp':   return move(idx - 1);
      case 'Home':      return move(0);
      case 'End':       return move(rows.length - 1);
      case 'ArrowRight': {
        if (isLeaf) return;
        if (expanded === 'false') {
          e.preventDefault();
          const toggle = item.querySelector(
            ':scope > .tree__row [data-fui-tree-toggle], :scope > [data-fui-tree-toggle]'
          );
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
          const toggle = item.querySelector(
            ':scope > .tree__row [data-fui-tree-toggle], :scope > [data-fui-tree-toggle]'
          );
          if (toggle) toggle.click();
          else item.setAttribute('aria-expanded', 'false');
          return;
        }
        const parent = item.parentElement &&
          item.parentElement.closest &&
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
          const toggle = item.querySelector(
            ':scope > .tree__row [data-fui-tree-toggle], :scope > [data-fui-tree-toggle]'
          );
          if (toggle) toggle.click();
          else item.setAttribute('aria-expanded', expanded === 'true' ? 'false' : 'true');
        } else {
          const link = item.querySelector(
            ':scope > .tree__row a, :scope > .tree__row button, :scope > a, :scope > button'
          );
          if (link) link.click();
        }
        return;
      }
    }
    // Type-ahead: a printable single-character key jumps to the next
    // visible treeitem whose label starts with the accumulated prefix.
    if (e.key.length === 1 && !e.ctrlKey && !e.metaKey && !e.altKey) {
      const now = Date.now();
      if (now - typeAt > 800) typeBuf = '';
      typeAt = now;
      typeBuf += e.key.toLowerCase();
      for (let i = 1; i <= rows.length; i++) {
        const cand = rows[(idx + i) % rows.length];
        const label = (cand.textContent || '').trim().toLowerCase();
        if (label.startsWith(typeBuf)) {
          e.preventDefault();
          focusItem(tree, cand);
          return;
        }
      }
    }
  });

  window.__gofastr = window.__gofastr || {};
  (window.__gofastr.loadedModules ||= {}).tree = true;
})();
