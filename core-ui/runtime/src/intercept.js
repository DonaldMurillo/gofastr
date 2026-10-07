// GoFastr runtime module, intercepting routes
//
// A detail screen that presents as an overlay when you reach it from
// inside the app, and as its own full page when you land on it directly.
// Registered server-side with app.InterceptFrom("/products",
// app.ScreenDrawer); the route manifest carries {from, also, as} per route and
// core loads this module only when at least one route declares it.
//
// The rules that keep this honest:
//
//   - SSR-first is untouched. A hard load, a refresh, or an external
//     link renders the canonical full page. Only a soft navigation that
//     STARTED on the declared origin is diverted.
//   - The server decides. We ask with X-Gofastr-Intercept and say where
//     we came from; the response only counts as an overlay if it comes
//     back with X-Gofastr-Overlay. No header, no overlay, we hand the
//     navigation back to the normal loadPage path.
//   - The page underneath stays mounted. Closing is a history move, so
//     Back, ESC, the backdrop and any data-cui-intercept-close button
//     are one code path and returning to the list costs no refetch.
//   - Intercepted HTML never enters the screen cache. The cache is
//     keyed by path and holds canonical page renders; storing an
//     overlay variant there would poison a later direct visit.
//
// Stacking: an intercepted link clicked INSIDE an open pane opens the
// target as a new layer over the current one (the container's children,
// in stack order), one raw pushState per open. At most four layers (a
// record and three related records over it); a fifth open is refused
// with a toast and changes nothing, so no layer's content is discarded
// to make room.
// Lower layers stay in the DOM untouched — their unsaved edits survive
// — and go inert + aria-hidden; only the top layer takes focus and the
// Tab trap. Back closes exactly the top layer and restores focus to the
// control that opened it; a history move the leave guard declines is
// undone with history.go back to the entry it left, so the history list
// stays as it was in either direction.
//
// A link inside a pane that changes only the pane's own query re-renders
// INSIDE the pane (the pane's URL gains one history entry per click,
// the same contract a list at top level follows) and never drops the
// pane by navigating the whole page. A link marked
// data-cui-intercept-swap does the same for another path: the target
// renders in the pane, fetched with the pane's own origin.
(() => {
  'use strict';
  window.__gofastr = window.__gofastr || {};
  const NS = window.__gofastr;

  const OVERLAY_ID = 'cui-intercept';
  const MAX_LAYERS = 4;
  // Each layer: { el, key, p0, cur, url, fromURL, restoreFocus }. A
  // layer owns the history entries p0..p0+cur (its open and its query
  // changes), counted from the page under the stack at 0, so a layer
  // opened over another starts one past the lower layer's current entry.
  const layers = [];
  let underPath = ''; // the page under the stack, captured at first open
  // Identity for history entries: stackId names the open stack, each
  // layer gets a key. RUN keeps entries from an earlier document load
  // from matching this one's.
  const RUN = Math.random().toString(36).slice(2) + ':';
  let seq = 0;
  let stackId = '';
  // Bumped by every move that changes what the stack shows. A fetch
  // captures it and drops its answer when it moved meanwhile, so a pane
  // closed (or superseded) mid-fetch is never written into or revived.
  let epoch = 0;
  // The entry a declined move's history.go is heading back to: its
  // popstate is the repair arriving, not a move of its own.
  let repair = null;
  // The anchor of the click core is handling. Recorded on window in the
  // capture phase, so it is set before core's document listener runs;
  // document.activeElement is no substitute (Safari never focuses a
  // clicked link).
  let clicked = null;
  window.addEventListener('click', (e) => { clicked = e.target.closest?.('a') || null; }, true);

  const routes = () => (Array.isArray(window.__gofastr_routes) ? window.__gofastr_routes : []);

  // Match a resolved path against the manifest's route patterns. Static
  // segments must match exactly; ':param' and '{param}' match one
  // segment; a trailing catch-all matches the remainder.
  function matchRoute(pattern, path) {
    const segs = pattern.split('/').filter(Boolean);
    const s = path.split('/').filter(Boolean);
    for (let i = 0; i < segs.length; i++) {
      const seg = segs[i];
      const dyn = seg[0] === ':' || seg[0] === '{';
      if (dyn && (seg.endsWith('...') || seg.endsWith('*') || seg.endsWith('...}'))) return true;
      if (i >= s.length) return false;
      if (!dyn && seg !== s[i]) return false;
    }
    return segs.length === s.length;
  }

  const routeFor = (path) => routes().find((r) => r.path && matchRoute(r.path, path));
  const pathOf = (u) => u.split('?')[0].split('#')[0];
  const top = () => layers[layers.length - 1];
  // Does the route's intercept open over the origin route o? Its from
  // pattern, or any of its also patterns.
  const fromOK = (target, o) => o.path === target.intercept.from || (target.intercept.also || []).includes(o.path);

  // Every history write here is a RAW pushState on purpose: currentPath
  // must stay on the page UNDER the stack, so popstate diffs inside the
  // router see no path change and never refetch the list underneath.
  // The entry is tagged {s: stack, k: layer, i: index in the layer}, so
  // popstate resolves the entry it landed on exactly (one URL can recur
  // in a pane's history) and knows how far the move went.
  function rawPush(t) { history.pushState({ cui: { s: stackId, k: t.key, i: t.cur } }, '', t.url); }

  // A fresh render of a layer, wearing the presentation the server
  // chose for it. Each layer carries its own: a stack can mix them. The
  // pane loads the stylesheet of each component it brings, as every
  // other swap path does.
  function swap(t, res) {
    t.el.innerHTML = res.html;
    t.el.setAttribute('data-cui-intercept-as', res.as);
    if (NS.scanAndLoadCSS) NS.scanAndLoadCSS(t.el);
  }

  function overlayHost() {
    let el = document.getElementById(OVERLAY_ID);
    if (el) return el;
    el = document.createElement('div');
    el.id = OVERLAY_ID;
    el.setAttribute('data-cui-intercept-overlay', '');
    document.body.appendChild(el);
    return el;
  }

  function setStacking() {
    for (let i = 0; i < layers.length; i++) {
      const el = layers[i].el;
      if (i === layers.length - 1) {
        el.removeAttribute('inert');
        el.removeAttribute('aria-hidden');
      } else {
        el.setAttribute('inert', '');
        el.setAttribute('aria-hidden', 'true');
      }
    }
  }

  function focusFirst(el) {
    const sel = NS._focusSel || 'a[href],button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled])';
    const first = el.querySelector(sel);
    const target = first || el.firstElementChild;
    if (!target) return;
    if (!first && target.setAttribute) target.setAttribute('tabindex', '-1');
    try { target.focus({ preventScroll: true }); } catch (_) { /* not focusable */ }
  }

  function refocus(el) {
    if (el && typeof el.focus === 'function') {
      try { el.focus({ preventScroll: true }); } catch (_) { el.focus(); }
    }
  }

  // The leave guard hook (headless-leaveguard, loaded when a
  // data-hui-leave-guard form is on the page). scope is the element (or
  // elements) whose content the move would discard.
  function guardOK(scope) {
    return !NS._leaveGuard || NS._leaveGuard.ok(scope);
  }

  // fetchOverlay asks the server for the overlay variant of path,
  // naming fromURL as the origin the way a host resolves it. Resolves
  // null when the server declines (the caller falls back to a plain
  // navigation) and rejects on network failure.
  function fetchOverlay(path, fromURL) {
    const hdrs = {
      'X-Gofastr-Navigate': '1',
      'X-Gofastr-Intercept': '1',
      'X-Gofastr-From': fromURL,
    };
    if (NS._markup) hdrs['X-Gofastr-Markup'] = NS._markup;
    return fetch(path, { headers: hdrs, credentials: 'same-origin' }).then((r) => {
      // An intercepted response can invalidate regular screens even
      // though the overlay itself is never cached.
      if (r.ok) NS._inval?.(r);
      const as = r.headers.get('X-Gofastr-Overlay');
      if (!r.ok || !as || r.headers.get('X-Gofastr-Location')) return null;
      return r.text().then((html) => ({ html, as }));
    });
  }

  function mountLayer(res, path, hash, fromURL, trigger) {
    const below = top();
    if (!below) {
      // The page under the stack is entry 0 of the new stack.
      underPath = location.pathname + location.search;
      stackId = RUN + ++seq;
      try { history.replaceState(Object.assign({}, history.state, { cui: { s: stackId } }), '', location.href); } catch (_) {}
    }
    const t = { el: document.createElement('div'), key: RUN + ++seq, p0: below ? below.p0 + below.cur + 1 : 1, cur: 0, url: path + (hash || ''), fromURL, restoreFocus: trigger };
    swap(t, res);
    overlayHost().appendChild(t.el);
    if (NS.doc) NS.doc.lockScroll('intercept');
    layers.push(t);
    setStacking();
    rawPush(t);
    focusFirst(t.el);
    // No explicit rescan: core's MutationObserver watches document.body
    // with subtree:true and demand-loads modules for markers in newly
    // inserted nodes, which is exactly how dynamically-opened widget
    // chrome gets wired.
  }

  // A query/hash-only change of the pane's own URL re-renders inside
  // the pane: one history entry per click, the contract a list at top
  // level follows, never a whole-page navigation that drops the pane.
  function claimQuery(path, hash) {
    const t = top();
    const u = path + (hash || '');
    const e = ++epoch;
    fetchOverlay(path, t.fromURL)
      .then((res) => {
        if (e !== epoch) return;
        if (!res) { fallbackNav(path, hash); return; }
        swap(t, res);
        // The push drops the entries ahead of this one, the same as the
        // browser does: the layer now ends here.
        if (u !== t.url) { t.cur++; t.url = u; rawPush(t); }
        focusFirst(t.el);
      })
      .catch(() => { if (e === epoch) fallbackNav(path, hash); });
  }

  // Close the top layer from a direct gesture (Esc, the close control,
  // the backdrop): guard, drop the DOM, restore focus to the control
  // that opened it, then consume the layer's history entries in one
  // move. The popstate that follows finds the stack already settled.
  function closeTopDirect() {
    const t = top();
    if (!t) return;
    if (!guardOK(t.el)) return;
    epoch++;
    layers.pop();
    t.el.remove();
    settleAfterDrop(t.restoreFocus);
    history.go(-(t.cur + 1)); // the entries this layer owns
  }

  // Drop every layer without a history move: a real navigation has
  // taken over the URL (gofastr:navigate) or a popstate left the stack.
  function closeAllNow() {
    epoch++;
    if (!layers.length) return;
    layers.length = 0;
    const el = document.getElementById(OVERLAY_ID);
    if (el) el.remove();
    if (NS.doc) NS.doc.unlockScroll('intercept');
  }

  // After dropping layer(s): expose the new top (or nothing), restore
  // the presentation and the scroll lock, and hand focus back.
  function settleAfterDrop(focus) {
    if (layers.length) {
      setStacking();
    } else {
      const el = document.getElementById(OVERLAY_ID);
      if (el) el.remove();
      if (NS.doc) NS.doc.unlockScroll('intercept');
    }
    refocus(focus);
  }

  // Hand a navigation back to the normal SPA path. loadPage is private
  // to core, so reach it the way the browser does: move the URL, then
  // fire popstate, core's handler sees a path change and loads it. That
  // avoids exporting anything new from a bundle with no room.
  function fallbackNav(path, hash) {
    // Deliberately NOT NS._pushURL: the choke point syncs currentPath,
    // and the popstate handler below loads only when the URL DIFFERS
    // from currentPath, a synced write would make the synthetic event
    // a no-op. The raw push leaves currentPath stale on purpose; the
    // handler stamps the entry's id itself when it finds none.
    history.pushState(null, '', path + (hash || ''));
    window.dispatchEvent(new PopStateEvent('popstate'));
  }

  // What a link to path, clicked at anchor, does here: null when the
  // module does not claim it, else { kind: 'query' | 'open' | 'refuse',
  // origin }. The one decision both the link handler and the leave
  // guard's scope query read, so the guard asks about exactly what the
  // move will discard.
  function decide(path, anchor) {
    const cont = document.getElementById(OVERLAY_ID);
    const target = routeFor(pathOf(path));
    if (layers.length && cont) {
      const t = top();
      const inPane = !!(anchor && cont.contains(anchor));
      // A link in the top pane marked data-cui-intercept-page opens its
      // target as the page: the stack closes and the router loads it.
      if (inPane && anchor.hasAttribute('data-cui-intercept-page')) return { kind: 'page' };
      // The pane's own URL, query-only change, or a link marked
      // data-cui-intercept-swap (a drawer's previous/next record): render
      // in the pane, one history entry of the layer.
      if (inPane && (pathOf(path) === pathOf(t.url) || (anchor.hasAttribute('data-cui-intercept-swap') && NS._originOK?.(path)))) return { kind: 'query' };
      const origin = inPane ? t.url : underPath;
      if (!target || !target.intercept) return null;
      const o = routeFor(pathOf(origin));
      if (!o || !fromOK(target, o)) return null;
      if (!NS._originOK?.(path)) return null;
      return { kind: layers.length >= MAX_LAYERS ? 'refuse' : 'open', origin };
    }
    if (!target || !target.intercept) return null;
    const o = routeFor(location.pathname);
    if (!o || !fromOK(target, o)) return null;
    if (!NS._originOK?.(path)) return null;
    return { kind: 'open', origin: location.pathname + location.search };
  }

  // The leave guard's question, asked from gofastr:beforenavigate before
  // core calls _intercept: what would this link discard? undefined = the
  // module will not claim it (the router replaces the page: everything);
  // null = nothing (a layer opens over what is there, or the cap refuses
  // it); an element = that layer (a query move re-renders the top pane).
  NS._interceptScope = function (path, anchor) {
    const d = decide(path, anchor);
    if (!d || d.kind === 'page') return undefined;
    return d.kind === 'query' ? top().el : null;
  };

  // Is a stack open? The leave guard stands down on history moves while
  // one is, because _interceptPopstate guards those itself.
  NS._interceptOpen = () => layers.length > 0;

  // Called by core's link handler before it pushes state. Returning true
  // claims the navigation. The recorded click tells a link inside the
  // top pane from one on the page under the stack.
  NS._intercept = function (path, hash) {
    const anchor = clicked;
    clicked = null;
    const d = decide(path, anchor);
    if (!d) return false;
    if (d.kind === 'query') {
      claimQuery(path, hash);
      return true;
    }
    if (d.kind === 'page') {
      // The page takes the top layer's history entry, so Back returns to
      // what was under the stack, not to a layer that is gone. Then the
      // router loads it the way fallbackNav hands a navigation over.
      closeAllNow();
      history.replaceState(null, '', path + (hash || ''));
      window.dispatchEvent(new PopStateEvent('popstate'));
      return true;
    }
    if (d.kind === 'refuse') {
      // Claimed and refused: the stack, the URL and every layer's
      // content stay as they are.
      NS._toastOrFallback?.({ variant: 'warning', title: MAX_LAYERS + ' panels is the limit', body: 'Open this one as a page, or close a panel first.' });
      return true;
    }
    const trigger = anchor || document.activeElement;
    const e = ++epoch;
    fetchOverlay(path, d.origin)
      .then((res) => {
        if (e !== epoch) return;
        if (!res) { fallbackNav(path, hash); return; }
        mountLayer(res, path, hash, d.origin, trigger);
      })
      .catch(() => { if (e === epoch) fallbackNav(path, hash); });
    return true;
  };

  // The rpc module asks this before a success navigation (a save that
  // names where to go next) made by node. When node sits in the top
  // layer and the destination's path is a layer's own (the top one, or
  // one under it) or the page under the stack, the stack returns there
  // instead of leaving: the layers above it close, their history entries
  // consumed in one move, and it re-renders in place with what the save
  // changed. A create opened over a record lands back on the record, a
  // save in a record pane keeps the pane. The leave guard asks first
  // about what the move discards. true = handled; anywhere else is a
  // real navigation, as before.
  NS._interceptReturn = function (path, node) {
    const t = top();
    if (!t || !node || !t.el.contains(node)) return false;
    const p = pathOf(path);
    let i = layers.length - 1;
    while (i >= 0 && pathOf(layers[i].url) !== p) i--;
    if (i < 0 && pathOf(underPath) !== p) return false;
    if (!guardOK(i < 0 ? null : layers.slice(i).map((l) => l.el))) return true;
    const e = ++epoch;
    let focus = null;
    while (layers.length > i + 1) {
      const shut = layers.pop();
      focus = shut.restoreFocus;
      shut.el.remove();
    }
    const lay = top();
    const back = t.p0 + t.cur - (lay ? lay.p0 + lay.cur : 0);
    if (back) {
      // The popstate this move fires is the return arriving, not a move.
      repair = { k: lay?.key, i: lay?.cur };
      history.go(-back);
    }
    settleAfterDrop(focus);
    if (!lay) {
      NS.refresh?.();
      return true;
    }
    fetchOverlay(lay.url, lay.fromURL)
      .then((res) => {
        if (e !== epoch) return;
        if (!res) { fallbackNav(lay.url, ''); return; }
        swap(lay, res);
      })
      .catch(() => { if (e === epoch) fallbackNav(lay.url, ''); });
    return true;
  };

  // Core's popstate handler calls this FIRST: an open stack owns every
  // history move onto an entry one of its layers wrote. true = handled,
  // the router stands down; false = no claim, load as usual (which
  // includes the no-op diff back to the page underneath).
  NS._interceptPopstate = function () {
    const tag = (history.state && history.state.cui) || {};
    if (repair) {
      const r = repair;
      repair = null;
      if (r.k === tag.k && r.i === tag.i) return true;
    }
    epoch++;
    if (!layers.length) return false;
    const t = top();
    // A declined move cannot be cancelled; go back to the entry it left
    // by the distance it moved, so nothing ahead of it is truncated.
    const undo = (p) => { repair = { k: t.key, i: t.cur }; history.go(t.p0 + t.cur - p); };
    const i = tag.s === stackId && tag.k ? layers.findIndex((l) => l.key === tag.k) : -1;
    if (i >= 0) {
      const lay = layers[i];
      const j = tag.i;
      if (lay === t && j === t.cur) return true; // nothing to do
      // What the move discards: the layers closing above i, plus layer
      // i's current content when the move refetches it.
      const scope = layers.slice(i + 1).map((l) => l.el);
      const refetch = j !== lay.cur;
      if (refetch) scope.push(lay.el);
      if (!guardOK(scope)) { undo(lay.p0 + j); return true; }
      // Focus returns to the control that opened the LOWEST closed
      // layer: it lives in layer i, the one left showing.
      let focus = null;
      while (layers.length > i + 1) {
        const shut = layers.pop();
        focus = shut.restoreFocus;
        shut.el.remove();
      }
      if (refetch) {
        const u = location.pathname + location.search + location.hash;
        const e = epoch;
        lay.cur = j;
        lay.url = u;
        fetchOverlay(u, lay.fromURL)
          .then((res) => {
            if (e !== epoch) return;
            if (!res) { fallbackNav(u, ''); return; }
            swap(lay, res);
          })
          .catch(() => { if (e === epoch) fallbackNav(u, ''); });
      }
      settleAfterDrop(focus);
      return true;
    }
    // The entry belongs to none of the layers (the page under the
    // stack, or a page further afield): closing the whole stack still
    // discards every layer's content, and a page further afield replaces
    // the page under the stack too, so the guard has its say here as
    // well (null scope = the whole document). Declined, go back to the
    // stack (an entry outside it has no known distance, so the top's
    // entry is pushed again instead); accepted, drop the stack and let
    // the router load the destination.
    const base = tag.s === stackId && !tag.k;
    if (!guardOK(base ? layers.map((l) => l.el) : null)) {
      if (base) undo(0);
      else rawPush(t);
      return true;
    }
    // Back on the page under the stack, focus returns to the control
    // that opened the first layer.
    const focus = base && layers[0].restoreFocus;
    closeAllNow();
    refocus(focus);
    return false;
  };

  // A real client-side navigation took the URL: the stack's layers are
  // gone with the page they floated over. No history move — the
  // navigation itself owns the entry.
  window.addEventListener('gofastr:navigate', closeAllNow);

  document.addEventListener('keydown', (e) => {
    if (!layers.length || e.key !== 'Escape') return;
    // Surfaces inside the layer that own Escape themselves consume it:
    // a modal widget, an open dropdown, an overlay-mode pane host.
    if (NS._modalStack && NS._modalStack.length) return;
    if (document.querySelector('[data-cui-dropdown-open], [data-hui-pane-mode="overlay"]')) return;
    e.preventDefault();
    closeTopDirect();
  });

  // Tab stays inside the top layer: the overlay is a modal surface
  // (fullscreen scrim, scroll lock), so focus must not reach the page
  // underneath or the inert layers. Capture phase, like the widget
  // focus module's trap; a modal widget opened inside the layer keeps
  // its own trap, so defer to it.
  document.addEventListener('keydown', (e) => {
    if (!layers.length || e.key !== 'Tab') return;
    if (NS._modalStack && NS._modalStack.length) return;
    const items = Array.from(top().el.querySelectorAll(NS._focusSel || 'a[href],button:not([disabled])')).filter((el) => el.offsetParent !== null || el === document.activeElement);
    if (!items.length) { e.preventDefault(); return; }
    const first = items[0], last = items[items.length - 1];
    if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
    else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
  }, true);

  document.addEventListener('click', (e) => {
    if (!layers.length) return;
    const el = document.getElementById(OVERLAY_ID);
    if (el && e.target === el) closeTopDirect();  // backdrop
    if (e.target.closest && e.target.closest('[data-cui-intercept-close]')) {
      e.preventDefault();
      closeTopDirect();
    }
  });

  (NS.loadedModules = NS.loadedModules || {}).intercept = true;
})();
