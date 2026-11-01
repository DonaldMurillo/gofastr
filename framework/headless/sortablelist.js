// headless-sortablelist: the behaviour module for this package's
// sortable lists — drag-and-drop reorder plus the keyboard model
// (Space grabs, Arrow Up/Down moves within the column, Arrow
// Left/Right crosses to an adjacent column of the same group, Space
// drops, Escape cancels) — and the server round trip the pattern
// shipped, with the server authoritative: non-2xx reverts the DOM.
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

  function keysOf(list) {
    return rowsOf(list).map(function (r) {
      return r.getAttribute('data-hui-sort-key') || '';
    });
  }

  function versionField(list) {
    const v = attr(list, 'data-hui-sortable-version');
    return v ? '&version=' + encodeURIComponent(v) : '';
  }

  // post sends the list's order plus the commit's own fields. A
  // same-container reorder adds container=<id> when the list carries
  // one, so the server can route the write without inferring the
  // column from the keys; a crossing adds moved= and ALWAYS carries
  // container= (empty when the list has no configured id).
  function post(list, fields) {
    const rpc = attr(list, 'data-hui-sortable-rpc');
    if (!rpc || !NS._originOK?.(rpc)) return Promise.resolve();
    return fetch(rpc, {
      method: 'POST', credentials: 'same-origin', headers: csrfHeaders(),
      body: 'order=' + encodeURIComponent(keysOf(list).join(',')) + fields + versionField(list),
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
    const c = attr(dest, 'data-hui-sortable-container');
    const p = post(dest, isCross
      ? '&moved=' + encodeURIComponent(attr(movedItem, 'data-hui-sort-key')) + '&container=' + encodeURIComponent(c)
      : c ? '&container=' + encodeURIComponent(c) : '');
    const restoreFn = function () { restore(dest, prevOrder, srcList, srcSnap, movedItem); };
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
            if (crpc && NS._originOK?.(crpc)) {
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
                .catch(function () { restoreFn(); if (!msg) announce(dest, 'data-hui-sortable-s-conflict-reverted'); });
              return;
            }
            restoreFn();
            if (!msg) announce(dest, 'data-hui-sortable-s-conflict-reverted');
          });
          return;
        }
        restoreFn();
        announce(dest, is409 ? 'data-hui-sortable-s-conflict-reverted' : 'data-hui-sortable-s-reverted');
      } else {
        NS._inval?.(res);
        announce(dest, 'data-hui-sortable-s-saved');
      }
    }).catch(function () { restoreFn(); announce(dest, 'data-hui-sortable-s-reverted'); });
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

  // settle commits a finished move when it changed anything: item is
  // the moved row, src the list it left, prev that list's rows before.
  function settle(item, src, prev) {
    const dest = listOf(item);
    if (!dest) return;
    const after = rowsOf(dest);
    if (dest !== src) commit(dest, null, src, prev, item);
    else if (!prev || prev.length !== after.length || prev.some(function (r, i) { return r !== after[i]; }))
      commit(dest, prev || [], null, null, null);
  }

  // putBack undoes a live move: every row of the source list returns
  // to its old place, the moved row included wherever it went.
  function putBack(src, prev) {
    if (src && prev) prev.forEach(function (r) { src.appendChild(r); });
  }

  // ── Drag and drop ──────────────────────────────────────────────────
  let dragSrc = null;
  let dragPrev = null;     // source list snapshot at dragstart
  let dragSrcList = null;
  let dropped = false;     // a drop landed on a list this drag

  document.addEventListener('dragstart', function (ev) {
    const item = ev.target && ev.target.closest && ev.target.closest('[data-hui-sortable-item]');
    if (!item) return;
    dragSrc = item;
    dragSrcList = listOf(item);
    dragPrev = rowsOf(dragSrcList).slice();
    dropped = false;
    item.classList.add('is-dragging');
    if (ev.dataTransfer) {
      ev.dataTransfer.effectAllowed = 'move';
      ev.dataTransfer.setData('text/plain', item.getAttribute('data-hui-sort-key') || '');
    }
  });

  // dropTarget: the list a drag over ev.target may land in, or null.
  function dropTarget(ev) {
    const t = ev.target && ev.target.closest && ev.target.closest('[data-hui-sortable]');
    const src = listOf(dragSrc);
    return t && (t === src || canCross(t, src)) ? t : null;
  }

  document.addEventListener('dragover', function (ev) {
    if (!dragSrc) return;
    const overList = dropTarget(ev);
    if (!overList) return;
    ev.preventDefault();
    const over = ev.target.closest('[data-hui-sortable-item]');
    if (over === dragSrc) return;
    if (over) {
      const rect = over.getBoundingClientRect();
      const after = (ev.clientY - rect.top) > rect.height / 2;
      over.parentNode.insertBefore(dragSrc, after ? over.nextSibling : over);
    } else {
      // Empty column: no row to hit-test, the list itself takes it.
      overList.appendChild(dragSrc);
    }
  });

  // dragover moves the row live, so only a drop on a list keeps the
  // move. Escape mid-drag, or a release anywhere else, fires dragend
  // with no drop first, and the row goes back uncommitted.
  document.addEventListener('drop', function (ev) {
    if (!dragSrc || !dropTarget(ev)) return;
    ev.preventDefault();
    dropped = true;
  });

  document.addEventListener('dragend', function () {
    if (!dragSrc) return;
    dragSrc.classList.remove('is-dragging');
    if (dropped) settle(dragSrc, dragSrcList, dragPrev);
    else putBack(dragSrcList, dragPrev);
    dragSrc = dragPrev = dragSrcList = null;
  });

  // ── Keyboard ───────────────────────────────────────────────────────
  let kbGrab = null;
  let kbPrev = null;       // source list snapshot at grab
  let kbSrcList = null;

  // kbEnd ends the grab: commit keeps the move, else it reverts.
  function kbEnd(commitIt) {
    const t = kbGrab;
    t.classList.remove('is-grabbed');
    t.removeAttribute('aria-grabbed');
    if (commitIt) settle(t, kbSrcList, kbPrev);
    else putBack(kbSrcList, kbPrev);
    kbGrab = kbPrev = kbSrcList = null;
    if (!commitIt) announce(listOf(t), 'data-hui-sortable-s-cancelled');
    return t;
  }

  // A grabbed row that loses focus (Tab away, a click elsewhere) is
  // cancelled the way Escape cancels it. The check waits a microtask:
  // an arrow move re-inserts the row, which can blur it, and the
  // handler focuses it again straight after.
  document.addEventListener('focusout', function (ev) {
    if (!kbGrab || ev.target !== kbGrab) return;
    queueMicrotask(function () {
      if (kbGrab && document.activeElement !== kbGrab) kbEnd(false);
    });
  });

  document.addEventListener('keydown', function (ev) {
    const t = ev.target;
    if (!t || !t.matches || !t.matches('[data-hui-sortable-item]')) return;

    if (ev.key === ' ' || ev.key === 'Spacebar') {
      ev.preventDefault();
      if (kbGrab === t) { kbEnd(true); return; }
      // A grab still held on another row is cancelled first, so the
      // next drop commits only its own move.
      if (kbGrab) kbEnd(false);
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
      kbEnd(false).focus();
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
      const target = cols[ev.key === 'ArrowLeft' ? idx - 1 : idx + 1];
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
