// GoFastr runtime module, transition
//
// The view-transition machinery (spike/layout-motion P11,
// spike/layout-resolve), loaded only by a document that declares a
// transition: a [data-fui-vt] cell or a data-fui-vt-kinds
// vocabulary, at boot or after any apply (the kernel's marker scan
// covers both). Before it loads — and on every page that never
// declares one — the swap applies directly, as before the layout
// work, and the X-Gofastr-Transition header is ignored.
//
//   - the startViewTransition wrapper around every navigation's
//     commit (the seam core's _swapCommit delegates to): the DOM
//     swap plus the scroll writes inside it, so the transition's new
//     snapshot shows the restored position;
//   - the direction types — 'forward' for clicks, navigate() and
//     forward popstates, 'back' for a popstate going back, 'reload'
//     for refresh() — recorded at the entry points through the
//     pop/reload seams and taken by the navigation entry;
//   - the cancelable gofastr:transition event (same naming family as
//     gofastr:beforenavigate) and the prefers-reduced-motion gate;
//   - the click-during-transition re-delivery (a running transition
//     hit-tests every point to <html>);
//   - the CSSOM view-transition-name mirror for every [data-fui-vt]
//     cell, with data-fui-vt-when's media gate (a style attribute is
//     refused by the default CSP; a CSSOM write is not);
//   - the keyed pick: the page answer carries it in
//     X-Gofastr-Transition, validated against the document's declared
//     vocabulary (data-fui-vt-kinds) and added to the commit's types
//     beside the direction; the history record (history.state.vt)
//     and the Back/Forward edge rule (Back mirrors the pick of the
//     entry being LEFT).
(() => {
  'use strict';
  window.__gofastr = window.__gofastr || {};
  const NS = window.__gofastr;

  // _entryVT tracks the pick of the entry the DOM shows: every
  // navigation records the pick it landed with on the entry navigated
  // to (history.state.vt), a Back popstate uses the ENTRY BEING LEFT
  // (the mirrored edge), a Forward popstate the destination entry's,
  // and a whole-document load the default.
  let _navPick = '';
  let _entryVT = (history.state && history.state.vt) || '';
  // The navigation state the entry points record and the navigator's
  // entry binds: _pending holds what pop()/reload() recorded (a
  // direction and, for pop, the pick-edge rule), _rec is the same
  // state bound to a navigation's EPOCH by take(epoch) — the plain
  // navigator and the envelope navigator call it before any await, so
  // a superseded navigation can neither lose its record nor leak it
  // into a later click, and commit() reads it back by epoch with no
  // threading through either navigator. _pickSticky carries the
  // decided flag across one retry leg (envelope's retry seam): the
  // pick-edge rule must survive a redirect, the direction must not.
  let _pending = null;
  let _rec = null;
  let _lastId = (history.state && history.state.__fui) || 0;
  let _pushed = false;
  const _vtKinds = () => new Set(
    (document.documentElement.getAttribute('data-fui-vt-kinds') || '').split(/\s+/).filter(Boolean));
  const _withPick = (pick) => {
    _navPick = (pick && _vtKinds().has(pick)) ? pick : '';
  };
  const _recordVT = () => {
    _entryVT = _navPick;
    try {
      history.replaceState(Object.assign({}, history.state, { vt: _navPick }), '', location.href);
    } catch (_) { /* a hostile URL: the state is cosmetic */ }
  };
  // _vtNames mirrors every data-fui-vt cell onto the CSSOM
  // view-transition-name. data-fui-vt-when gates a name on a media
  // condition (Transition.Narrow, the master-detail collapse): below
  // the breakpoint the REGION carries the name and the placed cell
  // must not — two live names of one spelling make the browser skip
  // the whole transition. A failing condition CLEARS any earlier
  // mirror, so a resize across the breakpoint moves the name instead
  // of duplicating it.
  const _vtNames = () => {
    for (const el of document.querySelectorAll('[data-fui-vt]')) {
      const n = el.getAttribute('data-fui-vt');
      if (!n) continue;
      const c = el.getAttribute('data-fui-vt-when');
      const on = !c || (window.matchMedia && matchMedia(c).matches);
      try { el.style.viewTransitionName = on ? n : ''; } catch (_) { /* invalid name: nothing to mirror */ }
    }
  };
  // _pickFor decides this navigation's keyed pick and returns its
  // name ('' when none). pickSrc is the pick's source — the page
  // answer (X-Gofastr-Transition) or a replayed entry (its recorded
  // pick); pickDecided means the popstate's edge rule already settled
  // it and the answer must not override it. Records the pick on the
  // entry it lands with.
  const _pickFor = (pickSrc, pickDecided) => {
    if (!pickDecided) {
      _withPick(pickSrc ? (pickSrc.headers ? pickSrc.headers.get('X-Gofastr-Transition') : (pickSrc.vt || '')) : '');
    }
    _recordVT();
    return _navPick;
  };
  let _activeVT = null;
  const _vtReduced = () => !!(window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches);

  NS._navHooks.transition = {
    // Bind the direction and the pick-edge rule to the navigation
    // that is starting (both navigators call this before any await).
    // The direction comes from the history entry ids — core's entries
    // are monotonic, so a smaller id landing is a Back move — and the
    // pushURL wrap below tells a click (or any programmatic push) from
    // a history move: a push preceded this take, so it is an
    // undecided forward whose answer may pick; no push means a
    // popstate, whose edge rule decides NOW (Back mirrors the pick
    // recorded on the entry being LEFT, Forward uses the destination
    // entry's) because the fetch's own header would name the
    // destination's answer, exactly wrong for a Back edge. The
    // refresh() wrap records the reload direction through _pending.
    take(epoch) {
      const id = (history.state && history.state.__fui) || 0;
      let types, decided = false;
      if (_pending) { types = _pending.types; decided = true; _pending = null; }
      else if (_pushed) { types = ['forward']; }
      else {
        types = [id < _lastId ? 'back' : 'forward']; decided = true;
        _withPick(types[0] === 'back' ? _entryVT : ((history.state && history.state.vt) || ''));
        _recordVT();
      }
      _pushed = false;
      _lastId = id;
      _rec = { epoch, types, decided };
    },

    // The pick this navigation landed with (frag/nav.js's cacheScreen
    // reads it at write time, so a replay keeps the edge rule).
    pick: () => _navPick,

    // Seam (frag/nav.js _swapCommit): run one navigation's commit
    // inside a view transition when the browser has one and the user
    // did not ask to reduce motion — the DOM swap plus the scroll
    // writes inside it, so the transition's new snapshot shows the
    // restored position.
    //
    // gofastr:transition (cancelable) fires before the transition
    // starts: preventDefault claims the swap — the runtime still
    // commits it synchronously with NO transition, the handler is
    // expected to run its own. One transition at a time: a navigation
    // committing while another animates skips the old one;
    // skipTransition still RUNS a pending update callback, so the
    // older navigation's DOM change is never lost — only its
    // animation drops.
    commit(epoch, from, to, apply, pickSrc) {
      const run = () => {
        if (!NS._navLive(epoch)) return;
        const r = apply();
        _vtNames();
        return r;
      };
      const sVT = document.startViewTransition;
      if (!sVT || _vtReduced()) { run(); return; }
      // The direction and the pick-edge rule come from the record the
      // navigation's ENTRY bound to this epoch; a navigation the entry
      // never armed (the module loaded mid-flight) takes the forward
      // default.
      const r0 = _rec && _rec.epoch === epoch ? _rec : null;
      const t = (r0 ? r0.types : ['forward']).slice();
      const pick = _pickFor(pickSrc, !!(r0 && r0.decided));
      if (pick) t.push(pick);
      if (!document.dispatchEvent(new CustomEvent('gofastr:transition', {
        cancelable: true,
        detail: { from, to, types: t.slice() },
      }))) { run(); return; }
      if (_activeVT) { try { _activeVT.skipTransition(); } catch (_) {} }
      _vtNames(); // the old-state snapshot must see the names
      let vt = null;
      try { vt = sVT.call(document, { update: run, types: t }); }
      catch (_) { run(); return; }
      _activeVT = vt;
      vt.finished.catch(() => {}).finally(() => { if (_activeVT === vt) _activeVT = null; });
    },
  };

  // A running view transition hit-tests every point to <html>, so a
  // click during the animation reaches no element (measured in the
  // spike: the second of two quick clicks was lost). Capture-phase,
  // before the navigation handler: skip the running transition, then
  // deliver the click to the element now under the pointer.
  document.addEventListener('click', (e) => {
    const vt = document.activeViewTransition;
    if (!vt || e.target !== document.documentElement) return;
    e.preventDefault();
    e.stopImmediatePropagation();
    const x = e.clientX, y = e.clientY;
    vt.skipTransition();
    vt.finished.finally(() => {
      const el = document.elementFromPoint(x, y);
      if (!el || el === document.documentElement) return;
      // A dispatched MouseEvent carries the real click's modality
      // (detail 1, the pointer shape) and its coordinates, so the
      // re-delivered navigation reads as the pointer click it stands
      // in for — el.click() would synthesize detail 0, the keyboard
      // shape, and paint the focus ring — with no side channel into
      // core (frag/nav.js keys _navPointer off e.detail alone).
      try {
        el.dispatchEvent(new MouseEvent('click', {
          bubbles: true, cancelable: true,
          detail: e.detail || 1, clientX: x, clientY: y,
        }));
      } catch (_) { /* a detached element between the checks */ }
    });
  }, true);

  // ─── Self-attachment ────────────────────────────────────────────────
  //
  // The entry-point signals this module once received as core seam
  // calls, self-attached where the signal is public: a _pushURL wrap
  // marks the next navigation as click-shaped (an undecided forward),
  // a refresh() wrap records the reload direction, and the
  // gofastr:navigate event (both navigators dispatch it) carries the
  // swapped payload for the keyed-transition vocabulary. The direction
  // itself folds into take: entry ids are monotonic, so the id the
  // take reads off history.state names the move — no popstate seam.
  const _origPush = NS._pushURL;
  NS._pushURL = function () {
    _pushed = true;
    return _origPush.apply(this, arguments);
  };
  const _origRefresh = NS.refresh;
  NS.refresh = function () {
    _pending = { types: ['reload'] };
    return _origRefresh.apply(this, arguments);
  };
  window.addEventListener('gofastr:navigate', (e) => {
    const root = e.detail && e.detail.root;
    const c = root && (root.matches('[data-fui-vt-kinds]')
      ? root : root.querySelector('[data-fui-vt-kinds]'));
    const kinds = c && c.getAttribute('data-fui-vt-kinds');
    // setAttribute directly: the attribute is module-owned (the
    // kernel's manifest allowlist carries no word for it).
    if (kinds) document.documentElement.setAttribute('data-fui-vt-kinds', kinds);
  });

  (NS.loadedModules ||= {}).transition = true;
})();
