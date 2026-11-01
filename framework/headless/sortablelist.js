// headless-sortablelist: the behaviour module for this package's
// sortable lists — drag-and-drop reorder plus the keyboard model
// (Space grabs, Arrow Up/Down moves within the column, Arrow
// Left/Right crosses to an adjacent column of the same group, the one
// on screen to that side in RTL, Space drops, Escape or focus leaving
// the row cancels) — and the server round trip the pattern shipped,
// with the server authoritative: non-2xx reverts the DOM unless a
// newer commit on the list has gone out since.
//
// The commit contract:
//   - a same-container reorder POSTs order=<comma-separated keys>,
//     plus container=<id> when the list carries one and
//     version=<token> when versioned;
//   - a cross-container drop POSTs the destination order plus
//     moved=<key> and always carries container=;
//   - when the list is versioned, a 409 fires the conflict path
//     (GET the conflict endpoint, replace the list's rows with the
//     fresh server-rendered ones) instead of a blanket rollback; the
//     409 body is read under hard bounds first (JSON content-type,
//     ~4 KB, {"error":{"message":string}}, capped ~300 chars) so a
//     hostile body cannot buffer or inject. A real message is
//     announced AND shown as an error toast (the live region is
//     visually hidden), and it replaces the generic conflict copy;
//     the generic copy never toasts.
//
// Every sentence the module says arrived as a data-hui-sortable-s-*
// attribute the component rendered from its Strings, so a translated
// page announces in its own language; the {label}/{list}/{position}
// tokens are written in when the move has happened. No inline style
// is ever written; the grabbed and dragging states are classes.
(function () {
  'use strict';
  const NAME = 'headless-sortablelist';
  const NS = window.__gofastr = window.__gofastr || {};
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;

  function listOf(item) {
    return item && item.closest && item.closest('[data-hui-sortable]');
  }

  function rowsOf(list) {
    return Array.from(list.querySelectorAll(':scope > [data-hui-sortable-item]'));
  }

  function attr(el, name) {
    return el && el.getAttribute ? (el.getAttribute(name) || '') : '';
  }

  // fill substitutes the announcement's tokens: {label} the row's
  // name, {list} the list's own name, {position} the 1-based place.
  function fill(tpl, map) {
    return tpl.replace(/\{\{(label|list|position)\}\}/g, function (_, k) {
      return map[k] !== undefined ? String(map[k]) : '';
    });
  }

  // canCross: over and src are different lists of the same group.
  function canCross(overList, srcList) {
    if (!overList || overList === srcList) return false;
    const g = attr(overList, 'data-hui-sortable-group');
    return g !== '' && g === attr(srcList, 'data-hui-sortable-group');
  }

  // CSRF: forward meta[name="csrf-token"] via X-CSRF-Token so the
  // auth.CSRF middleware accepts this state-changing POST.
  function csrfHeaders() {
    const headers = { 'Content-Type': 'application/x-www-form-urlencoded' };
    const meta = document.querySelector('meta[name="csrf-token"]');
    if (meta) {
      const tok = meta.getAttribute('content');
      if (tok) headers['X-CSRF-Token'] = tok;
    }
    return headers;
  }

  // post sends the list's order. The per-column id rides EVERY commit
  // when the list carries it, same-container reorders included, so the
  // server can route the write without inferring the column from the
  // keys; a cross-container landing adds moved=<key> and ALWAYS
  // carries container= (empty when the list has no configured id).
  function post(list, moved) {
    const rpc = attr(list, 'data-hui-sortable-rpc');
    if (!rpc || !window.__gofastr?._originOK?.(rpc)) return Promise.resolve();
    const c = attr(list, 'data-hui-sortable-container'), v = attr(list, 'data-hui-sortable-version');
    let body = 'order=' + encodeURIComponent(rowsOf(list).map(function (r) { return attr(r, 'data-hui-sort-key'); }).join(','));
    if (v) body += '&version=' + encodeURIComponent(v);
    if (moved !== undefined) body += '&moved=' + encodeURIComponent(moved);
    if (c || moved !== undefined) body += '&container=' + encodeURIComponent(c);
    return fetch(rpc, {
      method: 'POST', credentials: 'same-origin', headers: csrfHeaders(), body: body,
    });
  }

  // ── aria-live announcements ────────────────────────────────────────
  // Mirrors the feedback module's copy path: blank then set text after ~30ms so AT re-reads.
  function announce(list, key, map) {
    let live = document.getElementById('hui-sortable-live');
    if (!live) {
      live = document.createElement('div');
      live.id = 'hui-sortable-live';
      live.setAttribute('role', 'status');
      live.setAttribute('aria-live', 'polite');
      live.setAttribute('data-hui-sortable-live', '');
      document.body.appendChild(live);
    }
    const msg = fill(attr(list, key), map || {});
    live.textContent = '';
    setTimeout(function () { live.textContent = msg; }, 30);
  }

  // readBounded: read at most `max` chars from res's body via the
  // stream reader, a hard safety bound so a hostile/buggy server
  // can't make us buffer an unbounded 409 body. cb(txt).
  function readBounded(res, max, cb) {
    if (!res.body || !res.body.getReader) {
      res.text().then(function (t) { cb(t.slice(0, max)); }).catch(function () { cb(''); });
      return;
    }
    const r = res.body.getReader(), dec = new TextDecoder();
    let out = '', n = 0;
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
  // problem-detail body. JSON content-type only, ~4 KB max, string
  // message capped ~300 chars; malformed/oversized/empty → cb('') so
  // the caller falls back to the generic copy.
  function conflictMessage(res, cb) {
    let ct = '';
    try { ct = (res.headers.get('Content-Type') || '').toLowerCase(); } catch (_) {}
    if (ct.indexOf('application/json') < 0) { cb(''); return; }
    readBounded(res, 4096, function (txt) {
      if (!txt) { cb(''); return; }
      try {
        const j = JSON.parse(txt);
        const m = j && j.error && typeof j.error.message === 'string' ? j.error.message : '';
        cb(m ? m.slice(0, 300) : '');
      } catch (_) { cb(''); }
    });
  }

  function colLabel(list) {
    return attr(list, 'aria-label') || '';
  }

  function posOf(item) {
    const list = listOf(item);
    return list ? rowsOf(list).indexOf(item) + 1 : 0;
  }

  // ── Commit + rollback ──────────────────────────────────────────────
  // restore reverts the DOM to the pre-move state.
  function restore(dest, prevOrder, srcList, srcSnap, movedItem) {
    if (srcList && srcList !== dest) {
      if (srcSnap) srcSnap.forEach(function (r) { srcList.appendChild(r); });
      if (movedItem && movedItem.parentNode === dest) dest.removeChild(movedItem);
    } else if (prevOrder) {
      prevOrder.forEach(function (r) { dest.appendChild(r); });
    }
  }

  function commit(dest, prevOrder, srcList, srcSnap, movedItem) {
    const isCross = srcList && srcList !== dest;
    const p = isCross
      ? post(dest, movedItem.getAttribute('data-hui-sort-key'))
      : post(dest);
    // Two drops in quick succession are two commits in flight, settling
    // in any order. A commit sends the list's whole order, so once a
    // newer commit has gone out the newer one owns the list: an older
    // commit's late failure reverts nothing (it would paint an order
    // older than the one the newer commit saved) and says nothing.
    const lists = isCross ? [dest, srcList] : [dest];
    const mine = lists.map(function (l) { return (l.__huiSeq = (l.__huiSeq || 0) + 1); });
    const restoreFn = function () {
      const newest = lists.every(function (l, i) { return l.__huiSeq === mine[i]; });
      if (newest) restore(dest, prevOrder, srcList, srcSnap, movedItem);
      return newest;
    };
    p.then(function (res) {
      // A followed 3xx (an expired session sent the commit to the
      // sign-in page) is another URL's 200, not this commit's: the
      // order was not saved, so it reverts like any failed commit. No
      // navigator hand-off here: the module sits on the shared 3 KB
      // line, and the reverted announcement already says the save
      // failed; the reader's next navigation meets the same redirect.
      if (!res || !res.ok || res.redirected) {
        const is409 = res && res.status === 409;
        if (is409 && dest.hasAttribute('data-hui-sortable-version')) {
          conflictMessage(res, function (msg) {
            const crpc = attr(dest, 'data-hui-sortable-conflict');
            // A real server message is the announcement AND an error
            // toast — the live region is visually hidden, so without
            // the toast a sighted reader learns nothing of the
            // conflict. The generic copies below never toast, and a
            // message replaces them the way the retired module's
            // finishConflict did.
            if (msg) { announceRaw(msg); fireToast(msg); }
            if (crpc && window.__gofastr?._originOK?.(crpc)) {
              fetch(crpc, { credentials: 'same-origin' })
                .then(function (r) {
                  // Same convention as poll.js/rpc.js: an HTTP error must
                  // reach .catch, never the innerHTML mount — an error
                  // body reflects the request URL and would replace live
                  // page markup with reflected output. A followed 3xx is
                  // another URL's markup (a sign-in page), never the rows.
                  if (!r.ok || r.redirected) throw new Error('conflict refresh failed: ' + r.status);
                  return r.text();
                })
                .then(function (html) {
                  dest.innerHTML = html;
                  if (srcList && srcList !== dest && srcSnap)
                    srcSnap.forEach(function (r) { srcList.appendChild(r); });
                  if (!msg) announce(dest, 'data-hui-sortable-s-conflict-refreshed');
                })
                .catch(function () { if (restoreFn() && !msg) announce(dest, 'data-hui-sortable-s-conflict-reverted'); });
              return;
            }
            if (restoreFn() && !msg) announce(dest, 'data-hui-sortable-s-conflict-reverted');
          });
          return;
        }
        if (restoreFn()) announce(dest, is409 ? 'data-hui-sortable-s-conflict-reverted' : 'data-hui-sortable-s-reverted');
      } else {
        window.__gofastr._inval?.(res);
        announce(dest, 'data-hui-sortable-s-saved');
      }
    }).catch(function () { if (restoreFn()) announce(dest, 'data-hui-sortable-s-reverted'); });
  }

  // announceRaw says a server-provided message through the live
  // region (the bounded 409 detail), never a sentence of our own.
  function announceRaw(msg) {
    const live = document.getElementById('hui-sortable-live');
    if (!live) return; // announce() created it on the first move
    live.textContent = '';
    setTimeout(function () { live.textContent = msg; }, 30);
  }

  // fireToast: the visible half of a conflict message, through the
  // framework's toast surface (the retired module's #83 path):
  // NS.toast when the feedback module is already loaded, else the
  // kernel's loadModule('headless-feedback') first — its promise is
  // cached, so the already-loaded case costs no round trip. The
  // optional chains short-circuit the whole expression on a page
  // with no kernel (a bare test page): the live region stays the
  // always-on surface, this is the one a sighted reader sees. Never
  // called with the generic copy.
  function fireToast(msg) {
    NS.loadModule?.('headless-feedback').then(function () {
      NS.toast?.({ variant: 'danger', title: msg, ttl: 6000 });
    }).catch(function () {});
  }

  // ── Drag and drop ──────────────────────────────────────────────────
  let dragSrc = null;
  let dragPrev = null;     // source list snapshot at dragstart
  let dragSrcList = null;

  document.addEventListener('dragstart', function (ev) {
    const item = ev.target && ev.target.closest && ev.target.closest('[data-hui-sortable-item]');
    if (!item) return;
    dragSrc = item;
    dragSrcList = listOf(item);
    dragPrev = rowsOf(dragSrcList).slice();
    item.classList.add('is-dragging');
    if (ev.dataTransfer) {
      ev.dataTransfer.effectAllowed = 'move';
      ev.dataTransfer.setData('text/plain', item.getAttribute('data-hui-sort-key') || '');
    }
  });

  document.addEventListener('dragover', function (ev) {
    if (!dragSrc) return;
    const over = ev.target && ev.target.closest && ev.target.closest('[data-hui-sortable-item]');
    let overList;
    if (over) {
      overList = listOf(over);
    } else {
      // Empty list: no item to hit-test, so accept the list element
      // itself as the drop target (dropping into an empty column).
      overList = ev.target && ev.target.closest && ev.target.closest('[data-hui-sortable]');
    }
    if (!overList) return;
    const srcList = listOf(dragSrc);
    if (overList !== srcList && !canCross(overList, srcList)) return;
    ev.preventDefault();
    if (over === dragSrc) return;
    if (over) {
      const rect = over.getBoundingClientRect();
      const after = (ev.clientY - rect.top) > rect.height / 2;
      over.parentNode.insertBefore(dragSrc, after ? over.nextSibling : over);
    } else {
      // Empty column: append.
      overList.appendChild(dragSrc);
    }
  });

  // drop commits a finished move, drag or keyboard, when it changed
  // anything: a landing in another list always, a reorder when the
  // order moved.
  function drop(item, srcList, prev) {
    const dest = listOf(item);
    if (!dest) return;
    const after = rowsOf(dest);
    if (dest !== srcList) commit(dest, null, srcList, prev, item);
    else if (prev.length !== after.length || prev.some(function (r, i) { return r !== after[i]; })) commit(dest, prev, null, null, null);
  }

  document.addEventListener('dragend', function () {
    if (!dragSrc) return;
    dragSrc.classList.remove('is-dragging');
    drop(dragSrc, dragSrcList, dragPrev || []);
    dragSrc = null;
    dragPrev = null;
    dragSrcList = null;
  });

  // ── Keyboard ───────────────────────────────────────────────────────
  let kbGrab = null;
  let kbPrev = null;       // source list snapshot at grab
  let kbSrcList = null;

  // endGrab lets go of the grabbed row; cancelGrab first puts it back
  // where the grab found it. Escape cancels, and so does focus leaving
  // the row (a click elsewhere, Tab), which would otherwise leave an
  // unsaved move on screen with the row still grabbed. refocus is
  // Escape's: focus stays on the row; a blur leaves it where the
  // reader sent it.
  function endGrab(t) {
    t.classList.remove('is-grabbed');
    t.removeAttribute('aria-grabbed');
    kbGrab = kbPrev = kbSrcList = null;
  }
  function cancelGrab(t, refocus) {
    kbPrev.forEach(function (r) { kbSrcList.appendChild(r); });
    endGrab(t);
    if (refocus) t.focus();
    announce(listOf(t), 'data-hui-sortable-s-cancelled');
  }

  // A move re-inserts the focused row, and the engine may blur it on
  // the way (the move calls focus() straight back): the check waits a
  // task, and a grab whose row has focus again is still a grab.
  document.addEventListener('focusout', function (ev) {
    const t = ev.target;
    if (t === kbGrab) setTimeout(function () { if (kbGrab === t && document.activeElement !== t) cancelGrab(t); }, 0);
  });

  document.addEventListener('keydown', function (ev) {
    const t = ev.target;
    if (!t || !t.matches || !t.matches('[data-hui-sortable-item]')) return;

    if (ev.key === ' ' || ev.key === 'Spacebar') {
      ev.preventDefault();
      if (kbGrab === t) {
        // Release & commit if changed.
        const src = kbSrcList, prev = kbPrev;
        endGrab(t);
        drop(t, src, prev);
        return;
      }
      // Grab.
      kbGrab = t;
      kbSrcList = listOf(t);
      kbPrev = rowsOf(kbSrcList).slice();
      t.classList.add('is-grabbed');
      t.setAttribute('aria-grabbed', 'true');
      announce(kbSrcList, 'data-hui-sortable-s-grabbed', { label: attr(t, 'aria-label') });
      return;
    }

    if (ev.key === 'Escape' && kbGrab === t) {
      ev.preventDefault();
      cancelGrab(t, true);
      return;
    }

    if (kbGrab !== t) return;

    if (ev.key === 'ArrowUp' || ev.key === 'ArrowDown') {
      ev.preventDefault();
      const sib = ev.key === 'ArrowUp' ? t.previousElementSibling : t.nextElementSibling;
      if (!sib || !sib.hasAttribute('data-hui-sortable-item')) return;
      if (ev.key === 'ArrowUp') sib.parentNode.insertBefore(t, sib);
      else sib.parentNode.insertBefore(t, sib.nextSibling);
      t.focus();
      announce(listOf(t), 'data-hui-sortable-s-position', { position: posOf(t), list: colLabel(listOf(t)) });
      return;
    }

    if (ev.key === 'ArrowLeft' || ev.key === 'ArrowRight') {
      ev.preventDefault();
      const list = listOf(t);
      const g = attr(list, 'data-hui-sortable-group');
      if (!g) return;
      const cols = Array.from(document.querySelectorAll('[data-hui-sortable-group="' + CSS.escape(g) + '"]'));
      const idx = cols.indexOf(list);
      if (idx < 0) return;
      // The columns run in document order along the inline axis: in a
      // right-to-left board the next column sits on the left, so the
      // arrows follow the screen, as the tabs and the carousel do.
      const target = cols[idx + ((ev.key === 'ArrowRight') === (getComputedStyle(list).direction !== 'rtl') ? 1 : -1)];
      if (!target) return;
      target.appendChild(t);
      t.focus();
      announce(target, 'data-hui-sortable-s-moved', { list: colLabel(target), position: posOf(t) });
      return;
    }
  });

  // The listeners are document-level and delegated, so markup that
  // arrives after load (a conflict refresh, an island swap) needs no
  // per-root pass; the scanner exists because the kernel's contract
  // hands every inserted subtree to the module.
  function scan() {}
  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;
})();
