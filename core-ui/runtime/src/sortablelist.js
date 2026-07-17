// SortableList runtime module — drag-and-drop reorder + keyboard
// fallback. Posts the new order to data-fui-sortable-rpc as
// `order=<comma-separated-keys>`; reverts on non-2xx.
//
// Cross-container (kanban): lists sharing a data-fui-sortable-group
// allow items to drag/keyboard between them (including into empty
// columns — #82). A cross-container drop POSTs
// order=<dest keys>&moved=<key>&container=<col id> to the destination
// list's RPC. A same-container reorder POSTs order=<keys> plus
// container=<col id> when the list has data-fui-sortable-container
// configured (#84); lists without that attr keep the legacy
// order=-only payload.
//
// Version-aware 409: when data-fui-sortable-version is set, a 409
// response fires the conflict path (refetch data-fui-sortable-conflict
// HTML into the list) instead of a blanket rollback. Before
// refetching, the 409 body is read under hard bounds (#83): JSON
// content-type, ≤4KB, parses as {"error":{"message":<string>}}, msg
// capped ~300 chars; valid messages are announced + toasted, anything
// malformed/oversized/empty falls back to the generic copy. Without
// the version attr, 409 is treated like any other non-2xx (rollback).
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

  // containerField: the per-column id, sent on EVERY commit when the
  // source list carries data-fui-sortable-container — same-container
  // reorders included (#84) so the server can route the write without
  // inferring the column from the key set. Empty when the list never
  // configured a container (back-compat: payload unchanged).
  function containerField(list) {
    let c = attr(list, 'data-fui-sortable-container');
    return c ? '&container=' + encodeURIComponent(c) : '';
  }

  function postOrder(list) {
    let rpc = attr(list, 'data-fui-sortable-rpc');
    if (!rpc) return Promise.resolve();
    return fetch(rpc, {
      method: 'POST', credentials: 'same-origin', headers: csrfHeaders(),
      body: 'order=' + encodeURIComponent(keysOf(list).join(',')) + versionField(list) + containerField(list),
    });
  }

  function postCross(dest, movedKey) {
    let rpc = attr(dest, 'data-fui-sortable-rpc');
    if (!rpc) return Promise.resolve();
    let body = 'order=' + encodeURIComponent(keysOf(dest).join(','));
    body += '&moved=' + encodeURIComponent(movedKey);
    // Cross-container commits ALWAYS carry container= (empty when the
    // list has no configured id) — unchanged by #84. Same-container
    // commits use containerField() instead, which omits the field
    // entirely for back-compat with container-less lists.
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

  // fireToast: best-effort error toast via the framework toast surface
  // (#83). No-op when neither __gofastr.toast nor loadModule('toasts')
  // is available (e.g. bare test pages) — the live region is the
  // primary, always-on surface; this is secondary.
  function fireToast(msg) {
    let g = window.__gofastr;
    if (!g || !msg) return;
    let cfg = { variant: 'error', title: msg, ttl: 6000 };
    if (typeof g.toast === 'function') { g.toast(cfg); return; }
    if (typeof g.loadModule === 'function') {
      g.loadModule('toasts').then(function () {
        if (typeof g.toast === 'function') g.toast(cfg);
      }).catch(function () {});
    }
  }

  // readBounded: read at most `max` chars from res's body via the
  // stream reader — a hard safety bound so a hostile/buggy server
  // can't make us buffer an unbounded 409 body. Falls back to text()
  // (then slices) only when the stream reader is unavailable. cb(txt).
  function readBounded(res, max, cb) {
    if (!res.body || !res.body.getReader) {
      res.text().then(function (t) { cb(t.slice(0, max)); }).catch(function () { cb(''); });
      return;
    }
    let r = res.body.getReader(), dec = new TextDecoder(), out = '', n = 0;
    (function step() {
      r.read().then(function (x) {
        if (x.done) { cb(out.slice(0, max)); return; }
        if (x.value) { out += dec.decode(x.value, { stream: true }); n += x.value.length; }
        if (n >= max) { try { r.cancel(); } catch (_) {} cb(out.slice(0, max)); }
        else step();
      }).catch(function () { cb(''); });
    })();
  }

  // conflictMessage: safely extract error.message from a 409
  // problem-detail body {error:{code,message}}. Hard safety bounds:
  //   - Content-Type MUST be JSON (HTML / missing → fallback).
  //   - Read at most ~4KB of the body (truncation breaks JSON.parse
  //     → fallback).
  //   - error.message MUST be a string; cap at ~300 chars.
  //   - Empty/malformed/unreadable → cb('') so the caller falls back
  //     to the existing generic copy (today's behavior).
  function conflictMessage(res, cb) {
    let ct = '';
    try { ct = (res.headers.get('Content-Type') || '').toLowerCase(); } catch (_) {}
    if (ct.indexOf('application/json') < 0) { cb(''); return; }
    readBounded(res, 4096, function (txt) {
      if (!txt) { cb(''); return; }
      try {
        let j = JSON.parse(txt);
        let m = j && j.error && typeof j.error.message === 'string' ? j.error.message : '';
        cb(m ? m.slice(0, 300) : '');
      } catch (_) { cb(''); }
    });
  }

  // finishConflict: announce the captured message (or the generic
  // fallback when none) and fire a toast only when a real message
  // exists — never toast the generic copy.
  function finishConflict(msg, fallback) {
    announce(msg || fallback);
    if (msg) fireToast(msg);
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
          // #83: capture the bounded, JSON-only problem-detail message
          // BEFORE refreshing, then surface it via live region + toast.
          // The authoritative conflict refresh still runs afterward.
          conflictMessage(res, function (msg) {
            let crpc = attr(dest, 'data-fui-sortable-conflict');
            if (crpc) {
              fetch(crpc, { credentials: 'same-origin' })
                .then(function (r) { return r.text(); })
                .then(function (html) {
                  dest.innerHTML = html;
                  if (srcList && srcList !== dest && srcSnap)
                    srcSnap.forEach(function (r) { srcList.appendChild(r); });
                  finishConflict(msg, 'Conflict. List refreshed from server.');
                })
                .catch(function () { restoreFn(); finishConflict(msg, 'Conflict. Reverted.'); });
              return;
            }
            console.warn('sortablelist: 409 conflict, no conflict RPC — reverting');
            restoreFn();
            finishConflict(msg, 'Conflict. Reverted.');
          });
          return;
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
    if (!dragSrc) return;
    let over = ev.target && ev.target.closest && ev.target.closest('[data-fui-sortable-item]');
    let overList;
    if (over) {
      overList = listOf(over);
    } else {
      // Empty list: no [data-fui-sortable-item] to hit-test, so
      // accept the list element itself as the drop target (#82 —
      // dropping into an empty Kanban column).
      overList = ev.target && ev.target.closest && ev.target.closest('[data-fui-sortable]');
    }
    if (!overList) return;
    let srcList = listOf(dragSrc);
    if (overList !== srcList && !canCross(overList, srcList)) return;
    ev.preventDefault();
    if (over === dragSrc) return;
    if (over) {
      let rect = over.getBoundingClientRect();
      let after = (ev.clientY - rect.top) > rect.height / 2;
      over.parentNode.insertBefore(dragSrc, after ? over.nextSibling : over);
    } else {
      // Empty column: append.
      overList.appendChild(dragSrc);
    }
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
