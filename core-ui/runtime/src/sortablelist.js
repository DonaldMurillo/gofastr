// SortableList runtime module — drag-and-drop reorder + keyboard
// fallback. Posts the new order to data-fui-sortable-rpc as
// `order=<comma-separated-keys>`; reverts on non-2xx.
//
// Cross-container (kanban): lists sharing a data-fui-sortable-group
// allow items to drag/keyboard between them. A cross-container drop
// POSTs order=<dest keys>&moved=<key>&container=<col id> to the
// destination list's RPC. Same-container reorders send only order=.
//
// Version-aware 409: when data-fui-sortable-version is set, a 409
// response fires the conflict path (refetch data-fui-sortable-conflict
// HTML into the list) instead of a blanket rollback. Without the
// version attr, 409 is treated like any other non-2xx (rollback).
//
// Keyboard model:
//   Space → grab/release (toggle .is-grabbed + aria-grabbed)
//   Arrow Up/Down → move within column while grabbed
//   Arrow Left/Right → move to adjacent column (same group)
//   Esc → release without committing (snap back)
(function () {
  'use strict';

  function listOf(item) {
    return item && item.closest && item.closest('[data-fui-sortable]');
  }

  function rowsOf(list) {
    return Array.from(list.querySelectorAll(':scope > [data-fui-sortable-item]'));
  }

  function attr(el, name) {
    return el && el.getAttribute ? (el.getAttribute(name) || '') : '';
  }

  // canCross: over and src are in different lists of the same group.
  function canCross(overList, srcList) {
    if (!overList || overList === srcList) return false;
    let g = attr(overList, 'data-fui-sortable-group');
    return g !== '' && g === attr(srcList, 'data-fui-sortable-group');
  }

  // CSRF: forward meta[name="csrf-token"] via X-CSRF-Token so the
  // auth.CSRF middleware accepts this state-changing POST.
  function csrfHeaders() {
    let headers = { 'Content-Type': 'application/x-www-form-urlencoded' };
    let meta = document.querySelector('meta[name="csrf-token"]');
    if (meta) {
      let tok = meta.getAttribute('content');
      if (tok) headers['X-CSRF-Token'] = tok;
    }
    return headers;
  }

  function keysOf(list) {
    return rowsOf(list).map(function (r) {
      return r.getAttribute('data-fui-sort-key') || '';
    });
  }

  function versionField(list) {
    let v = attr(list, 'data-fui-sortable-version');
    return v ? '&version=' + encodeURIComponent(v) : '';
  }

  function postOrder(list) {
    let rpc = attr(list, 'data-fui-sortable-rpc');
    if (!rpc) return Promise.resolve();
    return fetch(rpc, {
      method: 'POST', credentials: 'same-origin', headers: csrfHeaders(),
      body: 'order=' + encodeURIComponent(keysOf(list).join(',')) + versionField(list),
    });
  }

  function postCross(dest, movedKey) {
    let rpc = attr(dest, 'data-fui-sortable-rpc');
    if (!rpc) return Promise.resolve();
    let body = 'order=' + encodeURIComponent(keysOf(dest).join(','));
    body += '&moved=' + encodeURIComponent(movedKey);
    body += '&container=' + encodeURIComponent(attr(dest, 'data-fui-sortable-container'));
    body += versionField(dest);
    return fetch(rpc, {
      method: 'POST', credentials: 'same-origin', headers: csrfHeaders(), body: body,
    });
  }

  // ── aria-live announcements ────────────────────────────────────────
  // Mirrors copy.js: blank then set text after ~30ms so AT re-reads.
  function announce(msg) {
    let live = document.getElementById('fui-sortable-live');
    if (!live) {
      live = document.createElement('div');
      live.id = 'fui-sortable-live';
      live.setAttribute('role', 'status');
      live.setAttribute('aria-live', 'polite');
      live.className = 'ui-sortable-list__sr';
      document.body.appendChild(live);
    }
    live.textContent = '';
    setTimeout(function () { live.textContent = msg; }, 30);
  }

  function colLabel(list) {
    return attr(list, 'aria-label') || 'list';
  }

  function posOf(item) {
    let list = listOf(item);
    return list ? rowsOf(list).indexOf(item) + 1 : 0;
  }

  // ── Commit + rollback ──────────────────────────────────────────────
  // restore: reverts the DOM to pre-move state. For same-container,
  // re-appends prevOrder to dest. For cross-container, re-appends
  // srcSnap to srcList (puts the moved item back) and removes the
  // movedItem from dest (it wasn't there originally).
  function restore(dest, prevOrder, srcList, srcSnap, movedItem) {
    if (srcList && srcList !== dest) {
      if (srcSnap) srcSnap.forEach(function (r) { srcList.appendChild(r); });
      if (movedItem && movedItem.parentNode === dest) dest.removeChild(movedItem);
    } else if (prevOrder) {
      prevOrder.forEach(function (r) { dest.appendChild(r); });
    }
  }

  function commit(dest, prevOrder, srcList, srcSnap, movedItem) {
    let isCross = srcList && srcList !== dest;
    let p = isCross
      ? postCross(dest, movedItem.getAttribute('data-fui-sort-key'))
      : postOrder(dest);
    let restoreFn = function () { restore(dest, prevOrder, srcList, srcSnap, movedItem); };
    p.then(function (res) {
      if (!res || !res.ok) {
        let is409 = res && res.status === 409;
        if (is409 && dest.hasAttribute('data-fui-sortable-version')) {
          let crpc = attr(dest, 'data-fui-sortable-conflict');
          if (crpc) {
            fetch(crpc, { credentials: 'same-origin' })
              .then(function (r) { return r.text(); })
              .then(function (html) {
                dest.innerHTML = html;
                if (srcList && srcList !== dest && srcSnap)
                  srcSnap.forEach(function (r) { srcList.appendChild(r); });
                announce('Conflict. List refreshed from server.');
              })
              .catch(function () { restoreFn(); announce('Conflict. Reverted.'); });
            return;
          }
          console.warn('sortablelist: 409 conflict, no conflict RPC — reverting');
        }
        restoreFn();
        announce(is409 ? 'Conflict. Reverted.' : 'Save failed. Reverted.');
      } else {
        announce('Order saved.');
      }
    }).catch(function () { restoreFn(); announce('Save failed. Reverted.'); });
  }

  // ── Drag and drop ──────────────────────────────────────────────────
  let dragSrc = null;
  let dragPrev = null;     // source list snapshot at dragstart
  let dragSrcList = null;

  document.addEventListener('dragstart', function (ev) {
    let item = ev.target && ev.target.closest && ev.target.closest('[data-fui-sortable-item]');
    if (!item) return;
    dragSrc = item;
    dragSrcList = listOf(item);
    dragPrev = rowsOf(dragSrcList).slice();
    item.classList.add('is-dragging');
    if (ev.dataTransfer) {
      ev.dataTransfer.effectAllowed = 'move';
      ev.dataTransfer.setData('text/plain', item.getAttribute('data-fui-sort-key') || '');
    }
  });

  document.addEventListener('dragover', function (ev) {
    let over = ev.target && ev.target.closest && ev.target.closest('[data-fui-sortable-item]');
    if (!over || !dragSrc) return;
    let overList = listOf(over);
    let srcList = listOf(dragSrc);
    if (overList !== srcList && !canCross(overList, srcList)) return;
    ev.preventDefault();
    if (over === dragSrc) return;
    let rect = over.getBoundingClientRect();
    let after = (ev.clientY - rect.top) > rect.height / 2;
    over.parentNode.insertBefore(dragSrc, after ? over.nextSibling : over);
  });

  document.addEventListener('dragend', function () {
    if (!dragSrc) return;
    dragSrc.classList.remove('is-dragging');
    let srcList = dragSrcList;
    let destList = listOf(dragSrc);
    let crossed = destList !== srcList;
    let before = dragPrev || [];
    let after = destList ? rowsOf(destList) : [];
    let changed = crossed;
    if (!changed) {
      changed = before.length !== after.length;
      if (!changed) {
        for (let i = 0; i < before.length; i++) {
          if (before[i] !== after[i]) { changed = true; break; }
        }
      }
    }
    if (changed && destList) {
      if (crossed) commit(destList, null, srcList, dragPrev, dragSrc);
      else commit(destList, before, null, null, null);
    }
    dragSrc = null;
    dragPrev = null;
    dragSrcList = null;
  });

  // ── Keyboard ───────────────────────────────────────────────────────
  let kbGrab = null;
  let kbPrev = null;       // source list snapshot at grab
  let kbSrcList = null;

  document.addEventListener('keydown', function (ev) {
    let t = ev.target;
    if (!t || !t.matches || !t.matches('[data-fui-sortable-item]')) return;

    if (ev.key === ' ' || ev.key === 'Spacebar') {
      ev.preventDefault();
      if (kbGrab === t) {
        // Release & commit if changed.
        t.classList.remove('is-grabbed');
        t.removeAttribute('aria-grabbed');
        let destList = listOf(t);
        let crossed = destList !== kbSrcList;
        let after = destList ? rowsOf(destList) : [];
        let changed = crossed;
        if (!changed) {
          changed = kbPrev && kbPrev.length === after.length;
          if (changed) {
            changed = false;
            for (let i = 0; i < kbPrev.length; i++) {
              if (kbPrev[i] !== after[i]) { changed = true; break; }
            }
          } else {
            changed = true;
          }
        }
        if (changed && destList) {
          if (crossed) commit(destList, null, kbSrcList, kbPrev, t);
          else commit(destList, kbPrev || [], null, null, null);
        }
        kbGrab = null;
        kbPrev = null;
        kbSrcList = null;
        return;
      }
      // Grab.
      kbGrab = t;
      kbSrcList = listOf(t);
      kbPrev = rowsOf(kbSrcList).slice();
      t.classList.add('is-grabbed');
      t.setAttribute('aria-grabbed', 'true');
      announce('Grabbed ' + (attr(t, 'aria-label') || 'item') + '. Arrow keys to move, Space to drop.');
      return;
    }

    if (ev.key === 'Escape' && kbGrab === t) {
      ev.preventDefault();
      if (kbSrcList && kbPrev) kbPrev.forEach(function (r) { kbSrcList.appendChild(r); });
      t.classList.remove('is-grabbed');
      t.removeAttribute('aria-grabbed');
      t.focus();
      kbGrab = null;
      kbPrev = null;
      kbSrcList = null;
      announce('Cancelled.');
      return;
    }

    if (kbGrab !== t) return;

    if (ev.key === 'ArrowUp' || ev.key === 'ArrowDown') {
      ev.preventDefault();
      let sib = ev.key === 'ArrowUp' ? t.previousElementSibling : t.nextElementSibling;
      if (!sib || !sib.hasAttribute('data-fui-sortable-item')) return;
      if (ev.key === 'ArrowUp') sib.parentNode.insertBefore(t, sib);
      else sib.parentNode.insertBefore(t, sib.nextSibling);
      t.focus();
      announce('Position ' + posOf(t) + ' in ' + colLabel(listOf(t)) + '.');
      return;
    }

    if (ev.key === 'ArrowLeft' || ev.key === 'ArrowRight') {
      ev.preventDefault();
      let list = listOf(t);
      let g = attr(list, 'data-fui-sortable-group');
      if (!g) return;
      let cols = Array.from(document.querySelectorAll('[data-fui-sortable-group="' + CSS.escape(g) + '"]'));
      let idx = cols.indexOf(list);
      if (idx < 0) return;
      let target = cols[ev.key === 'ArrowLeft' ? idx - 1 : idx + 1];
      if (!target) return;
      target.appendChild(t);
      t.focus();
      announce('Moved to ' + colLabel(target) + ', position ' + posOf(t) + '.');
      return;
    }
  });

  window.__gofastr = window.__gofastr || {};
  (window.__gofastr.loadedModules ||= {}).sortablelist = true;
})();
