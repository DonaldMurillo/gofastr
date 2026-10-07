(function () {
  'use strict';
  const NAME = 'headless-leaveguard';
  const NS = window.__gofastr = window.__gofastr || {};
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;

  // One shared "changed" state per guarded form. A form marked
  // data-hui-leave-guard becomes dirty on input/change and clean again
  // once a submit commits (the rpc module's gofastr:formresult ok, or a
  // native submit nobody cancelled) or on a reset. A move asks only when
  // it would discard a dirty form: a link (gofastr:beforenavigate),
  // Back or Forward on a page (the router's first popstate hook), and
  // the intercept module's close paths (through _leaveGuard.ok, scoped
  // to the layers they drop); beforeunload is armed while any form is
  // dirty so a reload or tab close gets the browser's own prompt. The
  // ask is window.confirm — the same synchronous gate
  // data-cui-confirm uses — because the cancellable hooks it answers
  // are synchronous events; an async dialog cannot answer them.
  const GUARD = '[data-hui-leave-guard]';
  const dirty = new WeakSet();
  let armed = false;

  function guardOf(el) {
    return el && el.closest ? el.closest(GUARD) : null;
  }

  // dirtyIn walks the scope the move would discard: an element (a
  // layer), several (a stack's closing layers), or the whole document.
  function dirtyIn(scope) {
    const roots = scope ? (Array.isArray(scope) ? scope : [scope]) : [document];
    for (const root of roots) {
      if (!root) continue;
      if (root.matches && root.matches(GUARD) && dirty.has(root)) return root;
      if (root.querySelectorAll) {
        for (const f of root.querySelectorAll(GUARD)) {
          if (dirty.has(f)) return f;
        }
      }
    }
    return null;
  }

  function messageOf(f) {
    return f.getAttribute('data-hui-leave-guard-message') || 'You have unsaved changes.';
  }

  // Asks the document at the moment it fires: a dirty form removed since
  // (a closed layer, a replaced pane, a dismissed widget) discards
  // nothing on reload, and no removal path has to remember to disarm.
  function onBeforeUnload(e) {
    if (!dirtyIn(document)) { syncBeforeUnload(); return; }
    e.preventDefault();
    e.returnValue = '';
  }

  function syncBeforeUnload() {
    const any = !!dirtyIn(document);
    if (any && !armed) {
      window.addEventListener('beforeunload', onBeforeUnload);
      armed = true;
    } else if (!any && armed) {
      window.removeEventListener('beforeunload', onBeforeUnload);
      armed = false;
    }
  }

  // The state also shows as data-hui-dirty, on the form and on every
  // control that names it by form= from outside (a header Save), so
  // styling can follow it.
  function setDirty(el, on) {
    if (on) el.setAttribute('data-hui-dirty', '');
    else el.removeAttribute('data-hui-dirty');
  }

  function reflect(f, on) {
    setDirty(f, on);
    if (!f.id) return;
    for (const c of document.querySelectorAll('[form="' + CSS.escape(f.id) + '"]')) {
      setDirty(c, on);
    }
  }

  function mark(f, on) {
    if (!f) return;
    if (on) dirty.add(f);
    else dirty.delete(f);
    reflect(f, on);
    syncBeforeUnload();
  }

  // Capture so a component that stops propagation cannot hide an edit.
  document.addEventListener('input', function (e) {
    mark(guardOf(e.target), true);
  }, true);
  document.addEventListener('change', function (e) {
    mark(guardOf(e.target), true);
  }, true);
  document.addEventListener('reset', function (e) {
    mark(guardOf(e.target), false);
  }, true);
  // A submit commits the form only when it goes ahead. The router's
  // submit listener cancels both a declined data-cui-confirm and every
  // submit it carries over RPC, so the outcome is read at each end: a
  // native submit still uncancelled when it reaches window (after every
  // document listener) leaves the page, and a carried one reports
  // through gofastr:formresult; until then the edits are unsaved.
  window.addEventListener('submit', function (e) {
    if (!e.defaultPrevented) mark(guardOf(e.target), false);
  });
  document.addEventListener('gofastr:formresult', function (e) {
    if (e.detail) mark(guardOf(e.target), e.detail.ok === false);
  });

  function ask(scope) {
    const f = dirtyIn(scope);
    if (!f || typeof window.confirm !== 'function') return true;
    return !!window.confirm(messageOf(f));
  }

  // Fires only for clicks the router is about to take. The intercept
  // module, when loaded, says what the click discards: nothing when it
  // stacks a layer over the edits (or refuses at the cap), the top pane
  // when a query move re-renders it, and the whole document when it
  // will not claim the click at all.
  document.addEventListener('gofastr:beforenavigate', function (e) {
    const d = e.detail || {};
    const scope = NS._interceptScope ? NS._interceptScope(d.path || '', d.anchor) : undefined;
    if (scope === null) return;
    if (!ask(scope)) e.preventDefault();
  });

  // Back and Forward. Back cannot be cancelled, and a popstate listener
  // of our own would run after the router's (it registered first), which
  // may already have swapped a cached page in. So the guard rides the
  // one hook the router calls before it does anything:
  // _interceptPopstate, true = handled, stand down. The property is
  // wrapped rather than assigned so the intercept module, loaded before
  // or after this one, keeps its claim: an open stack guards its own
  // moves, and the guard asks only when no stack is open and the move
  // leaves this page (a hash-only move discards nothing). A move whose
  // only change is a pane or widget deep link asks too: it can drop a
  // form inside that pane or widget, and the guard cannot see which.
  //
  // Declined, the move is undone with history.go(delta), so no entry is
  // added or dropped. Every entry the router writes through _pushURL is
  // tagged {hlg: {d, i}}: d names this numbering, i the entry's
  // position in it. A push numbers its entry one past the entry it
  // left, and only when that entry carries a tag of the same numbering;
  // anything else starts a fresh one, so a position is never derived
  // across an entry the guard did not write (an intercept layer's raw
  // push). A destination without a usable tag falls back to pushing
  // the left URL again. `repair` is the position the undo must land
  // on; a landing anywhere else falls back the same way.
  let here = location.pathname + location.search;
  let inner = NS._interceptPopstate;
  let numbering = '';
  let pos = 0;
  let repair = null;
  function tagOf() {
    const t = history.state && history.state.hlg;
    return t && t.d === numbering ? t : null;
  }
  function tag(i) {
    if (i === null) {
      numbering = Math.random().toString(36).slice(2);
      i = 0;
    }
    pos = i;
    try { history.replaceState(Object.assign({}, history.state, { hlg: { d: numbering, i: i } }), '', location.href); } catch (_) {}
  }
  function pushBack() {
    history.pushState(null, '', here);
    tag(null);
  }
  function guardedPopstate() {
    const t = tagOf();
    const to = location.pathname + location.search;
    if (repair !== null) {
      const want = repair;
      repair = null;
      if (!t || t.i !== want || to !== here) pushBack();
      return true;
    }
    if (!(NS._interceptOpen && NS._interceptOpen())) {
      if (to !== here) {
        if (!ask(document)) {
          if (t && pos !== null && t.i !== pos) {
            repair = pos;
            history.go(pos - t.i);
          } else {
            pushBack();
          }
          return true;
        }
        here = to;
      }
    }
    pos = t ? t.i : null;
    return inner ? inner() : false;
  }
  Object.defineProperty(NS, '_interceptPopstate', {
    configurable: true,
    get: function () { return guardedPopstate; },
    set: function (f) { inner = f; },
  });

  // The kernel-side seam: the intercept module's close paths (Esc, the
  // close control, a popstate that would close layers or refetch a
  // pane) ask through here, scoped to the content the move discards.
  NS._leaveGuard = { ok: ask };

  // The page the popstate guard compares against moves with the router:
  // every history write it makes (a click, navigate(), an RPC's
  // X-Gofastr-Push-State, a widget deep link) goes through _pushURL,
  // and a load it starts from popstate ends in gofastr:navigate. A
  // navigation's dropped forms are the scanner's job (the kernel reruns
  // it on gofastr:navigate).
  // The same wrap numbers the entry: a push one past a tagged entry it
  // left, a replace (which writes a fresh state) the left entry's own
  // position.
  const push = NS._pushURL;
  if (typeof push === 'function') {
    NS._pushURL = function (url, o) {
      const left = tagOf();
      const r = push.apply(this, arguments);
      here = location.pathname + location.search;
      tag(left ? left.i + (o && o.replace ? 0 : 1) : null);
      return r;
    };
  }
  tag(null);
  window.addEventListener('gofastr:navigate', function () {
    here = location.pathname + location.search;
  });

  function scan() { syncBeforeUnload(); }
  scan();
  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;
})();
