(function () {
  'use strict';
  const NAME = 'headless-leaveguard';
  const NS = window.__gofastr = window.__gofastr || {};
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;

  // One shared "changed" state per guarded form. A form marked
  // data-hui-leave-guard becomes dirty on input/change and clean again
  // after a successful submit (the rpc module's gofastr:formresult puts
  // it back when the answer refuses) or a reset. A move asks only when
  // it would discard a dirty form: a link (gofastr:beforenavigate),
  // Back or Forward on a page (the router's first popstate hook), and
  // the intercept module's close paths (through _leaveGuard.ok, scoped
  // to the layers they drop); beforeunload is armed while any form is
  // dirty so a reload or tab close gets the browser's own prompt. The ask is window.confirm — the same synchronous gate
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

  function onBeforeUnload(e) {
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

  function mark(f, on) {
    if (!f) return;
    if (on) dirty.add(f);
    else dirty.delete(f);
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
  });
  // A submit is the user committing the form; a refused answer (the
  // rpc module's gofastr:formresult with ok:false) puts the dirt back.
  document.addEventListener('submit', function (e) {
    mark(guardOf(e.target), false);
  }, true);
  document.addEventListener('gofastr:formresult', function (e) {
    if (e.detail && e.detail.ok === false) mark(guardOf(e.target), true);
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
  // leaves this page (a hash-only move discards nothing). Declined, the
  // left entry is pushed back and the router stands down.
  let here = location.pathname + location.search;
  let inner = NS._interceptPopstate;
  function guardedPopstate() {
    if (!(NS._interceptOpen && NS._interceptOpen())) {
      const to = location.pathname + location.search;
      if (to !== here) {
        if (!ask(document)) {
          history.pushState(null, '', here);
          return true;
        }
        here = to;
      }
    }
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

  // Closed layers and navigations drop forms (and their dirt) from the
  // document; the listener state follows, and the page the popstate
  // guard compares against moves with the router.
  window.addEventListener('gofastr:navigate', function () {
    here = location.pathname + location.search;
    syncBeforeUnload();
  });

  function scan() { syncBeforeUnload(); }
  scan();
  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;
})();
