// headless-sortablelist: the behaviour module for this package's
// sortable lists — drag-and-drop reorder plus the keyboard model
// (Space grabs, Arrow Up/Down moves within the column, Arrow
// Left/Right crosses to an adjacent column of the same group, the one
// on screen to that side in RTL, Space drops, Escape or focus leaving
// the row cancels, a new grab cancels the old) — and the server round
// trip the pattern shipped, with the server authoritative: non-2xx
// reverts the DOM to the last order the server confirmed, unless a
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

  const listOf = (item) => item?.closest?.('[data-hui-sortable]');

  const rowsOf = (list) => Array.from(list.querySelectorAll(':scope > [data-hui-sortable-item]'));

  const attr = (el, name) => el?.getAttribute?.(name) || '';

  // fill substitutes the announcement's tokens: {label} the row's
  // name, {list} the list's own name, {position} the 1-based place.
  function fill(tpl, map) {
    return tpl.replace(/\{\{(label|list|position)\}\}/g, (_, k) => String(map[k] ?? ''));
  }

  // canCross: over and src are different lists of the same group.
  function canCross(overList, srcList) {
    if (!overList || overList === srcList) return false;
    const g = attr(overList, 'data-hui-sortable-group');
    return !!g && g === attr(srcList, 'data-hui-sortable-group');
  }

  // CSRF: forward meta[name="csrf-token"] via X-CSRF-Token so the
  // auth.CSRF middleware accepts this state-changing POST.
  function csrfHeaders() {
    const headers = { 'Content-Type': 'application/x-www-form-urlencoded' };
    const tok = attr(document.querySelector('meta[name="csrf-token"]'), 'content');
    if (tok) headers['X-CSRF-Token'] = tok;
    return headers;
  }

  // post sends the list's order, plus version=<token> when versioned.
  // A same-container reorder adds container=<id> when the list carries
  // one, so the server can route the write without inferring the
  // column from the keys; a crossing (moved set) adds moved=<key> and
  // ALWAYS carries container= (empty when the list has no configured
  // id).
  function post(list, moved) {
    const rpc = attr(list, 'data-hui-sortable-rpc');
    if (!rpc || !NS._originOK?.(rpc)) return Promise.resolve();
    const c = attr(list, 'data-hui-sortable-container'), v = attr(list, 'data-hui-sortable-version');
    let body = 'order=' + encodeURIComponent(rowsOf(list).map((r) => attr(r, 'data-hui-sort-key')).join(','));
    if (v) body += '&version=' + encodeURIComponent(v);
    if (moved) body += '&moved=' + encodeURIComponent(attr(moved, 'data-hui-sort-key'));
    if (c || moved) body += '&container=' + encodeURIComponent(c);
    return fetch(rpc, { method: 'POST', credentials: 'same-origin', headers: csrfHeaders(), body: body });
  }

  // ── aria-live announcements ────────────────────────────────────────
  // Mirrors the feedback module's copy path: blank then set text after
  // ~30ms so AT re-reads. raw is a server-provided message (the bounded
  // 409 detail), said as it is; every other sentence is the list's own.
  function announce(list, key, map, raw) {
    let live = document.getElementById('hui-sortable-live');
    if (!live) {
      live = document.createElement('div');
      live.id = 'hui-sortable-live';
      live.setAttribute('role', 'status');
      live.setAttribute('aria-live', 'polite');
      live.setAttribute('data-hui-sortable-live', '');
      document.body.appendChild(live);
    }
    const msg = raw || fill(attr(list, key), map || {});
    live.textContent = '';
    setTimeout(() => { live.textContent = msg; }, 30);
  }

  // readBounded: read at most `max` chars from res's body via the
  // stream reader, a hard safety bound so a hostile/buggy server
  // can't make us buffer an unbounded 409 body. cb(txt).
  function readBounded(res, max, cb) {
    // No stream reader, no read: the caller's generic copy stands.
    const r = res.body?.getReader?.(), dec = new TextDecoder();
    if (!r) return cb('');
    let out = '', n = 0;
    (function step() {
      r.read().then((x) => {
        if (x.done) return cb(out.slice(0, max));
        if (x.value) { out += dec.decode(x.value, { stream: true }); n += x.value.length; }
        if (n >= max) { try { r.cancel(); } catch (_) {} cb(out.slice(0, max)); }
        else step();
      }, () => cb(''));
    })();
  }

  // conflictMessage: safely extract error.message from a 409
  // problem-detail body. JSON content-type only, ~4 KB max, string
  // message capped ~300 chars; malformed/oversized/empty → cb('') so
  // the caller falls back to the generic copy.
  function conflictMessage(res, cb) {
    if (!(res.headers.get('Content-Type') || '').toLowerCase().includes('application/json')) return cb('');
    readBounded(res, 4096, (txt) => {
      // An empty or malformed body throws in the parse: generic copy.
      try {
        const m = JSON.parse(txt)?.error?.message;
        cb(typeof m === 'string' ? m.slice(0, 300) : '');
      } catch (_) { cb(''); }
    });
  }

  function posOf(item) {
    const list = listOf(item);
    return list ? rowsOf(list).indexOf(item) + 1 : 0;
  }

  // ── Commit + rollback ──────────────────────────────────────────────
  // Drops in quick succession are commits in flight together, settling
  // in any order, and each one sends its list's whole order. So a
  // rollback never goes back to the order its own move started from
  // (that order can hold an earlier move the server never saved): each
  // list keeps the last order the server confirmed, __huiAck, taken
  // from the screen when a move starts with nothing in flight and
  // replaced by every success newer than the one it holds. Whenever
  // the newest commit on a list has failed, the list shows that order:
  // a failure superseded by a newer commit changes nothing, and an
  // older commit that succeeds after the newest failed moves the list
  // to the order it saved. putBack restores one list; in a cross-list
  // move the destination goes first, so the source can take its row
  // back.

  // commit sends a finished move: prev is the rows the moved row's list
  // held before it; srcList and movedItem are set for a crossing.
  function commit(dest, prev, srcList, movedItem) {
    const lists = srcList ? [dest, srcList] : [dest];
    const pres = srcList ? [rowsOf(dest).filter((r) => r !== movedItem), prev] : [prev];
    lists.forEach((l, i) => { if (!l.__huiN) l.__huiAck = pres[i]; l.__huiN = (l.__huiN || 0) + 1; });
    const sent = lists.map(rowsOf);
    const mine = lists.map((l) => (l.__huiSeq = (l.__huiSeq || 0) + 1));
    const p = post(dest, movedItem);
    // record notes the outcome on every list the commit sent and
    // reports whether it was the newest on all of them, the one whose
    // outcome is announced.
    const record = (ok) => lists.map((l, i) => {
      l.__huiN--;
      if (ok && mine[i] > (l.__huiOk || 0)) { l.__huiAck = sent[i]; l.__huiOk = mine[i]; }
      if (!ok && mine[i] === l.__huiSeq) l.__huiBad = mine[i];
      if (l.__huiBad === l.__huiSeq) putBack(l, l.__huiAck);
      return mine[i] === l.__huiSeq;
    }).every(Boolean);
    const fail = (key) => { if (record(false) && key) announce(dest, key); };
    p.then((res) => {
      // A followed 3xx (an expired session sent the commit to the
      // sign-in page) is another URL's 200, not this commit's: the
      // order was not saved, so it reverts like any failed commit. No
      // navigator hand-off here: the module sits on the shared 3 KB
      // line, and the reverted announcement already says the save
      // failed; the reader's next navigation meets the same redirect.
      if (res && res.ok && !res.redirected) {
        record(true);
        NS._inval?.(res);
        announce(dest, 'data-hui-sortable-s-saved');
        return;
      }
      const is409 = res && res.status === 409;
      if (!is409 || !dest.hasAttribute('data-hui-sortable-version')) return fail(is409 ? 'data-hui-sortable-s-conflict-reverted' : 'data-hui-sortable-s-reverted');
      conflictMessage(res, (msg) => {
        const crpc = attr(dest, 'data-hui-sortable-conflict');
        // A real server message is the announcement AND an error
        // toast — the live region is visually hidden, so without the
        // toast a sighted reader learns nothing of the conflict. The
        // generic copies never toast, and a message replaces them the
        // way the retired module's finishConflict did.
        if (msg) { announce(dest, '', 0, msg); fireToast(msg); }
        const reverted = msg ? '' : 'data-hui-sortable-s-conflict-reverted';
        if (!crpc || !NS._originOK?.(crpc)) return fail(reverted);
        fetch(crpc, { credentials: 'same-origin' })
          .then((r) => {
            // Same convention as poll.js/rpc.js: an HTTP error must
            // reach the rejection, never the innerHTML mount — an error
            // body reflects the request URL and would replace live page
            // markup with reflected output. A followed 3xx is another
            // URL's markup (a sign-in page), never the rows.
            if (!r.ok || r.redirected) throw new Error('conflict refresh failed: ' + r.status);
            return r.text();
          })
          .then((html) => {
            // The fresh rows are the server's order: the destination
            // confirms them, and the record puts the source back.
            dest.innerHTML = html;
            dest.__huiAck = rowsOf(dest);
            dest.__huiOk = dest.__huiSeq;
            record(false);
            if (!msg) announce(dest, 'data-hui-sortable-s-conflict-refreshed');
          }, () => fail(reverted));
      });
    }, () => fail('data-hui-sortable-s-reverted'));
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
    NS.loadModule?.('headless-feedback').then(() => {
      NS.toast?.({ variant: 'danger', title: msg, ttl: 6000 });
    }, () => {});
  }

  // settle commits a finished move when it changed anything: item is
  // the moved row, src the list it left, prev that list's rows before.
  function settle(item, src, prev) {
    const dest = listOf(item);
    if (!dest) return;
    const after = rowsOf(dest);
    if (dest !== src) commit(dest, prev, src, item);
    else if (!prev || prev.length !== after.length || prev.some((r, i) => r !== after[i]))
      commit(dest, prev || []);
  }

  // putBack undoes a live move: every row of the source list returns
  // to its old place, the moved row included wherever it went.
  function putBack(src, prev) {
    if (src && prev) prev.forEach((r) => src.appendChild(r));
  }

  // ── Drag and drop ──────────────────────────────────────────────────
  // dragPrev: the source list's rows at dragstart; dropped: a drop
  // landed on a list this drag.
  let dragSrc = null, dragPrev = null, dragSrcList = null, dropped = false;

  document.addEventListener('dragstart', (ev) => {
    const item = ev.target.closest?.('[data-hui-sortable-item]');
    if (!item) return;
    dragSrc = item;
    dragSrcList = listOf(item);
    dragPrev = rowsOf(dragSrcList);
    dropped = false;
    item.classList.add('is-dragging');
    if (ev.dataTransfer) {
      ev.dataTransfer.effectAllowed = 'move';
      ev.dataTransfer.setData('text/plain', attr(item, 'data-hui-sort-key'));
    }
  });

  // dropTarget: the list a drag over ev.target may land in, or null.
  function dropTarget(ev) {
    const t = ev.target.closest?.('[data-hui-sortable]');
    const src = listOf(dragSrc);
    return t && (t === src || canCross(t, src)) ? t : null;
  }

  document.addEventListener('dragover', (ev) => {
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
  document.addEventListener('drop', (ev) => {
    if (!dragSrc || !dropTarget(ev)) return;
    ev.preventDefault();
    dropped = true;
  });

  document.addEventListener('dragend', () => {
    if (!dragSrc) return;
    dragSrc.classList.remove('is-dragging');
    if (dropped) settle(dragSrc, dragSrcList, dragPrev);
    else putBack(dragSrcList, dragPrev);
    dragSrc = dragPrev = dragSrcList = null;
  });

  // ── Keyboard ───────────────────────────────────────────────────────
  let kbGrab = null, kbPrev = null, kbSrcList = null; // kbPrev: the source list's rows at grab

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
  document.addEventListener('focusout', (ev) => {
    if (!kbGrab || ev.target !== kbGrab) return;
    queueMicrotask(() => {
      if (kbGrab && document.activeElement !== kbGrab) kbEnd(false);
    });
  });

  document.addEventListener('keydown', (ev) => {
    const t = ev.target;
    if (!t?.matches?.('[data-hui-sortable-item]')) return;

    if (ev.key === ' ' || ev.key === 'Spacebar') {
      ev.preventDefault();
      if (kbGrab === t) { kbEnd(true); return; }
      // A grab still held on another row is cancelled first, so the
      // next drop commits only its own move.
      if (kbGrab) kbEnd(false);
      kbGrab = t;
      kbSrcList = listOf(t);
      kbPrev = rowsOf(kbSrcList);
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
      sib.parentNode.insertBefore(t, ev.key === 'ArrowUp' ? sib : sib.nextSibling);
      t.focus();
      announce(listOf(t), 'data-hui-sortable-s-position', { position: posOf(t), list: attr(listOf(t), 'aria-label') });
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
      announce(target, 'data-hui-sortable-s-moved', { list: attr(target, 'aria-label'), position: posOf(t) });
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
