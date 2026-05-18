// SortableList runtime module — drag-and-drop reorder + keyboard
// fallback. Posts the new order to data-fui-sortable-rpc as
// `order=<comma-separated-keys>`; reverts on non-2xx.
//
// Keyboard model:
//   Tab into a list item → focus the row
//   Space → grab/release (toggle the .is-grabbed class + aria-grabbed)
//   Arrow Up / Down → while grabbed, move the row up/down
//   Esc → release without committing (snap back to original index)
(function () {
  'use strict';

  function listOf(item) {
    return item && item.closest && item.closest('[data-fui-sortable]');
  }

  function rowsOf(list) {
    return Array.from(list.querySelectorAll(':scope > [data-fui-sortable-item]'));
  }

  function postOrder(list) {
    const rpc = list.getAttribute('data-fui-sortable-rpc');
    if (!rpc) return Promise.resolve();
    const keys = rowsOf(list).map(function (r) {
      return r.getAttribute('data-fui-sort-key') || '';
    });
    const body = 'order=' + encodeURIComponent(keys.join(','));
    return fetch(rpc, {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: body,
    });
  }

  function commit(list, prevOrder) {
    postOrder(list).then(function (res) {
      if (!res || !res.ok) {
        // Restore previous order.
        prevOrder.forEach(function (row) { list.appendChild(row); });
      }
    }).catch(function () {
      prevOrder.forEach(function (row) { list.appendChild(row); });
    });
  }

  // ── Drag and drop ──────────────────────────────────────────────────
  let dragSrc = null;
  let dragPrev = null;

  document.addEventListener('dragstart', function (ev) {
    const item = ev.target && ev.target.closest && ev.target.closest('[data-fui-sortable-item]');
    if (!item) return;
    dragSrc = item;
    dragPrev = rowsOf(listOf(item)).slice(); // snapshot
    item.classList.add('is-dragging');
    if (ev.dataTransfer) {
      ev.dataTransfer.effectAllowed = 'move';
      // Firefox requires a non-empty payload to start the drag.
      ev.dataTransfer.setData('text/plain', item.getAttribute('data-fui-sort-key') || '');
    }
  });

  document.addEventListener('dragover', function (ev) {
    const over = ev.target && ev.target.closest && ev.target.closest('[data-fui-sortable-item]');
    if (!over || !dragSrc) return;
    if (listOf(over) !== listOf(dragSrc)) return;
    ev.preventDefault();
    if (over === dragSrc) return;
    // Insert dragSrc before/after `over` based on cursor y-position.
    const rect = over.getBoundingClientRect();
    const after = (ev.clientY - rect.top) > rect.height / 2;
    over.parentNode.insertBefore(dragSrc, after ? over.nextSibling : over);
  });

  document.addEventListener('dragend', function () {
    if (!dragSrc) return;
    dragSrc.classList.remove('is-dragging');
    const list = listOf(dragSrc);
    const before = dragPrev || [];
    const after  = list ? rowsOf(list) : [];
    let changed = before.length !== after.length;
    if (!changed) {
      for (let i = 0; i < before.length; i++) {
        if (before[i] !== after[i]) { changed = true; break; }
      }
    }
    if (changed && list) commit(list, before);
    dragSrc = null;
    dragPrev = null;
  });

  // ── Keyboard ───────────────────────────────────────────────────────
  // Track the currently-grabbed row globally — only one at a time.
  let kbGrab = null;
  let kbPrev = null;

  document.addEventListener('keydown', function (ev) {
    const t = ev.target;
    if (!t || !t.matches || !t.matches('[data-fui-sortable-item]')) return;

    if (ev.key === ' ' || ev.key === 'Spacebar') {
      ev.preventDefault();
      if (kbGrab === t) {
        // Release & commit if changed.
        t.classList.remove('is-grabbed');
        t.removeAttribute('aria-grabbed');
        const list = listOf(t);
        const after = list ? rowsOf(list) : [];
        let changed = kbPrev && kbPrev.length === after.length;
        if (changed) {
          changed = false;
          for (let i = 0; i < kbPrev.length; i++) {
            if (kbPrev[i] !== after[i]) { changed = true; break; }
          }
        } else {
          changed = true;
        }
        if (changed && list) commit(list, kbPrev || []);
        kbGrab = null;
        kbPrev = null;
        return;
      }
      // Grab.
      kbGrab = t;
      kbPrev = rowsOf(listOf(t)).slice();
      t.classList.add('is-grabbed');
      t.setAttribute('aria-grabbed', 'true');
      return;
    }

    if (ev.key === 'Escape' && kbGrab === t) {
      // Revert without commit.
      ev.preventDefault();
      const list = listOf(t);
      if (list && kbPrev) {
        kbPrev.forEach(function (row) { list.appendChild(row); });
      }
      t.classList.remove('is-grabbed');
      t.removeAttribute('aria-grabbed');
      t.focus();
      kbGrab = null;
      kbPrev = null;
      return;
    }

    if (kbGrab === t && (ev.key === 'ArrowUp' || ev.key === 'ArrowDown')) {
      ev.preventDefault();
      const sibling = ev.key === 'ArrowUp' ? t.previousElementSibling : t.nextElementSibling;
      if (!sibling || !sibling.hasAttribute('data-fui-sortable-item')) return;
      if (ev.key === 'ArrowUp') {
        sibling.parentNode.insertBefore(t, sibling);
      } else {
        sibling.parentNode.insertBefore(t, sibling.nextSibling);
      }
      t.focus();
    }
  });
})();
