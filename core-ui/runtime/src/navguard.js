// GoFastr runtime module, navigation guards
//
// One ordered list of guards that every managed departure asks before
// anything the move would discard is lost: a link the router takes,
// navigate() (which a GET form, an RPC redirect and a save's
// data-cui-rpc-navigate go through), Back and Forward, and the
// intercept stack's own departures (Esc, the backdrop, the close
// control, Back or Forward inside the stack, a pane's query link, a
// save returning to a layer). A script loads the module and registers:
//
//   await __gofastr.loadModule('navguard');
//   const stop = __gofastr.guardNavigation(async (nav) => { ... });
//
// A guard gets the attempt and answers allow (true or undefined), deny
// (false), or a promise of either; a throw (reading a returned value's
// then included) or a rejection denies and is reported. Guards run in
// registration order, one at a time, and the first denial ends the
// decision, so a later guard is not asked. With no guard registered,
// or when every guard answers synchronously with allow, the move runs
// in the same tick, exactly as without the module. A move that
// discards nothing (an intercept layer opening over the page, one
// refused at the layer cap) is not asked about. A URL-only write
// (replaceURL, an RPC's X-Gofastr-Push-State, a widget or pane deep
// link) is not a departure and is not asked about either.
//
// The attempt (frozen): id, intent ('push' | 'replace' | 'traverse' |
// 'overlay'), from {url, entry}, to {url, entry}, delta, signal and
// affects(node). entry names the history entry: the Navigation API key
// where the browser has the API, else the tag core writes on every
// entry (a numbering, the position in it and the entry's id); null for
// an entry that does not exist yet or that no router wrote. One URL can
// name several entries, so the entry, never the URL, says which. delta
// is the distance of a history move, null when unknown. signal aborts
// when a newer move supersedes this one or its pending guard is
// disposed: the decision no longer counts, and nothing the guard
// already wrote is undone. affects(node) says whether the move discards
// node: an overlay move discards only the layers it closes or
// re-renders, a page move everything. A Back or Forward that changes
// only a pane's or a widget's deep link is a page move too: the guards
// cannot tell which surface it closes, so they are asked as if the
// whole page went.
//
// Back and Forward have already moved the history when popstate fires.
// A traversal the guards do not allow at once is undone first: the
// engine takes the browser back to the entry the page still shows (by
// key through the Navigation API, else history.go by the distance the
// entries' tags give) and swallows that landing; an allow takes it to
// the destination entry again, and that landing alone goes on to the
// router, the leave guard and the intercept stack, without asking
// again. Each landing counts only on the entry it was headed for. A
// denial is reported once the undo has landed, on the entry the page
// shows, with the history list as it was: no entry added, none
// dropped. While one of the engine's own traversals is in flight, a
// new link, navigate() or replaceURL() is refused (reason 'pending'
// for a link): a push from the wrong entry would truncate the one being
// restored.
//
// With no known way back (an entry no router wrote, one from another
// numbering, the Navigation API refusing the traversal, a fallback
// traversal that never lands), nothing is pushed: the browser stays on
// the landing while the page stays on the entry it shows. An allow
// sends the landing on to the router. A denial replaces that landing,
// and only it, with the page's URL; the landing keeps its own identity
// (no entry ever takes another's), and the report says 'unknown'.
//
// An open intercept stack keeps its own exact undo for moves between
// its entries (they carry their positions): the stack asks through
// _navGate, and the allow is bound to the entry its retry lands on. A
// move off the stack to a page further afield is undone and replayed
// like a page's own traversal.
//
// A blocked move fires gofastr:navigateblocked on window, detail
// {attempt, reason, error}; reason is 'denied', 'error' (a guard threw
// or rejected; error holds it), 'disposed' (the pending guard was
// disposed), 'pending' or 'unknown' (refused where no way back was
// known, as above; error holds a guard's throw if one caused it). A
// superseded move does not report: a newer one replaced it.
(() => {
  'use strict';
  window.__gofastr = window.__gofastr || {};
  const NS = window.__gofastr;
  if (NS.loadedModules && NS.loadedModules.navguard) return;

  const N = window.navigation && window.navigation.currentEntry ? window.navigation : null;
  const guards = [];
  let ids = 0;
  // The record whose decision is pending, and the traversal being
  // undone or replayed: {r, src, dst, ok, back, stay}.
  let live = null;
  let move = null;
  // The entry the engine's own traversal is heading to, what to do if
  // it cannot get there, the steps taken toward it and the timer that
  // gives up (or, after a stray landing, heads on).
  let heading = null;
  let lose = null;
  let tries = 0;
  let timer = 0;
  // The landing an approved history retry of the stack's will make,
  // and the stack's undo in flight: {go}, the retry it holds back.
  // Chromium drops a history.go() made before the previous one has
  // landed, so an allow that beats the undo waits for it.
  let approved = null;
  let undo = null;
  // The URL of the approved continuation running now: a synthetic
  // popstate inside it, or the stack's retry, is the move decided.
  let pass = null;
  const held = new WeakSet();

  // The entry the browser is on, {key, index, url}: the Navigation
  // API's entry, or one read off the tag core writes ({__fui: id,
  // __fup: [numbering, position]}). key is numbering, position and id
  // (a position alone would also name the entry a push put there after
  // a Back), index the [numbering, position] pair; both null on an
  // entry no router wrote.
  const cur = () => {
    if (N) return N.currentEntry;
    const s = history.state, p = s && s.__fup, ok = p && s.__fui && Number.isSafeInteger(p[1]);
    return { key: ok ? p[0] + ':' + p[1] + ':' + s.__fui : null, index: ok ? p : null, url: location.href };
  };
  // How far entry b is from entry a; null when unknown: an entry gone
  // from the list, one without a tag, another numbering.
  const dist = (a, b) => N ? (a.index >= 0 && b.index >= 0 ? b.index - a.index : null)
    : a.key && b.key && a.index[0] === b.index[0] ? b.index[1] - a.index[1] : null;
  const here = () => cur().key || location.href + '\n' + JSON.stringify(history.state);
  // The entry the page shows. A push or a replace moves it; a
  // traversal moves it only once the engine lets it through.
  let shown = cur();
  if (N) {
    N.addEventListener('currententrychange', (e) => {
      if (!heading && e.navigationType !== 'traverse') shown = N.currentEntry;
    });
  } else {
    for (const k of ['pushState', 'replaceState']) {
      const f = history[k];
      history[k] = function () {
        const v = f.apply(this, arguments);
        if (!heading) shown = cur();
        return v;
      };
    }
  }

  const abs = (u) => { try { return new URL(u, location.href).href; } catch (_) { return String(u); } };
  // The browser may not be on the entry the page shows: the engine's
  // own traversal is in flight, or a move with no known way back waits
  // on its landing.
  const busy = () => !!heading || !!(move && move.stay);

  function attempt(intent, to, scope, delta, entry, from) {
    const c = new AbortController();
    const list = scope == null ? null : [].concat(scope);
    const a = Object.freeze({
      id: ++ids,
      intent,
      from: Object.freeze({ url: from || shown.url, entry: shown.key }),
      to: Object.freeze({ url: abs(to), entry: entry || null }),
      delta: delta || delta === 0 ? delta : null,
      signal: c.signal,
      affects: (node) => !list || list.some((el) => !!el && el.contains(node)),
    });
    return { a, c, g: null, err: null, why: null, done: null };
  }

  // Runs the guards in order. true or false when every guard answered
  // synchronously, else a promise of the answer. A guard disposed
  // before its turn is skipped; one registered meanwhile waits for the
  // next move.
  function decide(r) {
    const list = guards.slice();
    let i = 0;
    const next = () => {
      while (i < list.length) {
        const g = list[i++];
        if (!guards.includes(g)) continue;
        let v, p;
        try { v = g.fn(r.a); p = !!v && typeof v.then === 'function'; } catch (err) { r.err = err; return false; }
        if (p) {
          r.g = g;
          return Promise.resolve(v).then((ok) => {
            r.g = null;
            return !r.a.signal.aborted && ok !== false && next();
          }, (err) => { r.g = null; r.err = err; return false; });
        }
        if (v === false) return false;
      }
      return true;
    };
    return next();
  }

  // A newer move replaces the pending one: its signal aborts, its late
  // answer is dropped, and an approval it left is void.
  function supersede(why) {
    approved = null;
    if (undo) undo.go = null;
    move = null;
    const r = live;
    live = null;
    if (r) r.c.abort(why);
  }

  // Asks about r: true or false for a synchronous answer, null when a
  // guard moved meanwhile (r is over), undefined while pending, and
  // done(ok) runs once the answer is in, if r is still the live one.
  function ask(r, done) {
    supersede('superseded');
    live = r;
    r.done = done;
    const v = decide(r);
    if (v !== true && v !== false) {
      v.then((ok) => { if (live === r) { live = null; done(ok); } });
      return;
    }
    if (live !== r) return null;
    live = null;
    return v;
  }

  function block(r, reason) {
    if (r.err) console.warn('[gofastr] navigation guard failed:', r.err);
    window.dispatchEvent(new CustomEvent('gofastr:navigateblocked', {
      detail: { attempt: r.a, reason: reason || r.why || (r.err ? 'error' : 'denied'), error: r.err },
    }));
  }

  function run(to, go) {
    pass = abs(to);
    try { return go(); } finally { pass = null; }
  }
  const passing = (to) => {
    if (pass === null || pass !== abs(to)) return false;
    pass = null;
    return true;
  };

  // Core's departure seam (a taken link, navigate(), the intercept
  // module's hand-back to the router). The link's scope comes from the
  // intercept module: null = nothing departs, an element = the top
  // pane re-renders, undefined = the page.
  NS._navAsk = (o, go) => {
    if (passing(o.to) || (o.held && held.delete(o.held) && o.held.to === abs(o.to))) return run(o.to, go);
    const scope = o.anchor && NS._interceptScope ? NS._interceptScope(o.to.split('#')[0], o.anchor) : undefined;
    if (scope === null && !busy()) { supersede('superseded'); return go(); }
    const r = attempt(scope ? 'overlay' : o.replace ? 'replace' : 'push', o.to, scope);
    if (busy()) return block(r, 'pending');
    const v = ask(r, (ok) => (ok ? run(o.to, go) : block(r)));
    if (v) return run(o.to, go);
    if (v === false) block(r);
  };

  // The approval a continuation runs under, kept for a later step of
  // the same move (a page link's walk back out of the stack): single
  // use, bound to the URL.
  NS._navHold = () => {
    if (pass === null) return null;
    const t = { to: pass };
    pass = null;
    held.add(t);
    return t;
  };

  // replaceURL writes the entry the browser is on: refused while that
  // may not be the entry the page shows.
  const replaceURL = NS.replaceURL;
  NS.replaceURL = function (u) { return !busy() && replaceURL.call(this, u); };

  // The intercept stack's guard hook: o = {scope, to, delta, pop, at}.
  // true = go ahead now; false = not now, and retry runs after an
  // allow; null = not now, and the move is being undone here. pop: a
  // history move the stack undoes exactly, whose retry lands where this
  // one did, and the allow is bound to that landing; at: a history move
  // off the stack to a page further afield (intent 'traverse'), undone
  // and replayed here as a page's own traversal is.
  NS._navGate = (o, retry) => {
    if (passing(o.to)) return true;
    const e = cur();
    const d = o.at ? dist(shown, e) : o.delta;
    const r = attempt(o.at ? 'traverse' : 'overlay', o.to, o.scope, d, o.pop || o.at ? e.key : null);
    if (busy()) { block(r, 'pending'); return o.at ? null : false; }
    const mark = o.pop ? here() : null;
    const m = o.at ? { r, src: shown, dst: e, ok: null, back: false, stay: null } : null;
    const v = ask(r, (ok) => {
      if (m) return settle(m, ok);
      if (!ok) return block(r);
      if (mark === null) return run(o.to, retry);
      approved = mark;
      if (undo) undo.go = retry;
      else retry();
    });
    if (v) return true;
    if (m) {
      if (v !== null) begin(m, d, v);
      return null;
    }
    if (v === false) block(r);
    else if (mark !== null) undo = {};
    return false;
  };

  // A traversal the guards have not allowed at once: back to the entry
  // the page shows, by its distance, or with none known, stay put.
  function begin(m, d, v) {
    move = m;
    if (d) travel(m.src, () => lost(m));
    else lost(m);
    if (v === false) settle(m, false);
  }

  function travel(t, fail) {
    heading = t;
    lose = fail;
    tries = 0;
    if (!N) return step();
    let x;
    try { x = N.traverseTo(t.key); } catch (_) { return miss(); }
    x.finished.catch(() => {});
    x.committed.catch(() => { if (heading === t) miss(); });
  }

  // The engine's own traversal cannot land.
  function miss() {
    const f = lose;
    heading = lose = null;
    clearTimeout(timer);
    if (f) f();
  }

  // Without the API: go the distance from where the browser is. A
  // landing elsewhere (a Back the browser ran first, or a go it
  // dropped) heads on from there after a quiet moment, a few times at
  // most; a traversal that never lands gives up.
  function step() {
    if (!heading) return;
    const d = dist(cur(), heading);
    if (!d || ++tries > 4) return miss();
    clearTimeout(timer);
    timer = setTimeout(miss, 2000);
    history.go(d);
  }

  // The traversal record m = {r, src, dst, ok, back, stay}: src is the
  // entry the page shows, dst the one the browser moved to; back is set
  // once the undo has landed; stay, when there is no way back, resolves
  // the decision where the browser is instead.
  function settle(m, ok) {
    if (move !== m) return;
    m.ok = ok;
    if (m.stay) m.stay(ok);
    else if (m.back) back(m);
  }

  // The undo has landed: the browser is on the entry the page shows.
  function back(m) {
    m.back = true;
    if (m.ok) travel(m.dst, () => lost(m));
    else if (m.ok === false) { move = null; block(m.r); }
  }

  // The engine cannot take the browser where m needs it. On the page's
  // own entry: the undo is done, or the approved destination is out of
  // reach ('unknown'). Still on the landing with no way back: m stays
  // there. Anywhere else, a move of the user's own overtook it, and the
  // browser's entry is asked about afresh.
  function lost(m) {
    if (move !== m) return;
    const e = cur();
    if (e.key && e.key === m.src.key) {
      if (!m.back) return back(m);
      move = null;
      return block(m.r, 'unknown');
    }
    if (!m.back && (!m.dst.key || e.key === m.dst.key)) {
      m.stay = stay(m);
      if (m.ok !== null) m.stay(m.ok);
      return;
    }
    move = null;
    window.dispatchEvent(new PopStateEvent('popstate'));
  }

  // No way back: the browser stays on the landing and the page on the
  // entry it shows. Allowed, the landing goes on to the router.
  // Refused, only that landing takes the page's URL, keeping its own
  // identity. A browser that moved on meanwhile belongs to that move.
  function stay(m) {
    const href = location.href, st = history.state;
    return (ok) => {
      move = null;
      if (location.href !== href || history.state !== st) return;
      if (ok) return run(href, () => window.dispatchEvent(new PopStateEvent('popstate')));
      try { history.replaceState({ ...st, ...NS._entryTag(0) }, '', m.src.url); } catch (_) {}
      block(m.r, 'unknown');
    };
  }

  // Core's popstate seam, asked before anything else. true = handled
  // (the router stands down); otherwise the intercept hook's answer.
  NS._navPop = () => {
    const e = cur();
    const legacy = () => {
      shown = e;
      return run(location.href, () => NS._interceptPopstate?.()) || false;
    };
    // A synthetic popstate inside an approved continuation: the
    // intercept module's hand-back (fallbackNav) or claimPanel.
    if (passing(location.href)) return legacy();
    if (heading) {
      // Only the engine's own landing counts. With the API a user move
      // processed before it is overtaken by it (the traversal is by
      // key); without, the engine heads on from where it is.
      if (e.key !== heading.key) {
        if (!N) { clearTimeout(timer); timer = setTimeout(step, 100); }
        return true;
      }
      clearTimeout(timer);
      heading = lose = null;
      const m = move;
      if (m && m.ok && m.back) { move = null; return legacy(); }
      shown = e;
      if (m) back(m);
      return true;
    }
    // A move waiting on its landing is over once the browser leaves it.
    if (move && move.stay) supersede('superseded');
    if (NS._interceptOpen?.()) {
      // The approved retry's landing. The approval outlives the undo's
      // landing, which can arrive after the allow; any move the stack
      // asks about meanwhile supersedes it.
      if (approved !== null && approved === here()) { approved = null; return legacy(); }
      // The first landing after an undo began is the undo's.
      const u = undo;
      undo = null;
      const h = NS._interceptPopstate();
      // Unless the stack's gate began undoing the move: the page still
      // shows the entry it left.
      if (!heading) shown = e;
      if (u && u.go) setTimeout(u.go);
      return h || false;
    }
    approved = null;
    // The page itself: a fragment move, or a stack's walk back to the
    // page under it.
    if (location.pathname + location.search === NS.currentPath) return legacy();
    const d = dist(shown, e);
    const r = attempt('traverse', location.href, undefined, d, e.key, shown.url);
    const m = { r, src: shown, dst: e, ok: null, back: false, stay: null };
    const v = ask(r, (ok) => settle(m, ok));
    if (v) return legacy();
    if (v !== null) begin(m, d, v);
    return true;
  };

  NS.guardNavigation = (fn) => {
    if (typeof fn !== 'function') throw new TypeError('guardNavigation: a guard is a function');
    const g = { fn };
    guards.push(g);
    return () => {
      const i = guards.indexOf(g);
      if (i < 0) return;
      guards.splice(i, 1);
      // Disposed while it decides: the decision ends as a refusal, its
      // history put back as any refusal's is.
      const r = live;
      if (r && r.g === g) {
        live = null;
        r.why = 'disposed';
        r.c.abort('disposed');
        r.done(false);
      }
    };
  };

  (NS.loadedModules ||= {}).navguard = true;
})();
