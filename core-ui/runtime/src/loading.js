// GoFastr runtime module, loading
//
// Loading content (spike/layout-loading, P9-A/P9-ANIM/P9-B). The
// server renders each configured Loading once, as an inert
// <template data-fui-loading="<addr>" data-fui-after data-fui-min>
// BESIDE its outlet, area, or slot cell, so the browser holds the
// bytes before any fetch starts. Loaded when the document carries
// such a template (marker scan, at boot and after any apply).
//
// Before it loads the busy dim alone shows: the navigator marks the
// regions it will change (aria-busy, core) and no clone parks.
//
// The client knows the regions a navigation will change (the busy
// marks), so after `after` ms of in-flight wait the region's old nodes
// move into an in-document hidden park (they stay connected:
// listeners, input values and poll timers all survive) and the
// template clones in, marked data-fui-loadstate="shown". On apply the
// response replaces the clone (a Min hold keeps it from flashing); on
// failure or a superseded navigation the old nodes come back exactly.
//
// _liveLoading tracks every SHOWN entry across navigations: a fast
// navigation that supersedes a slow one applies over regions still
// carrying the slow one's clone (its fetch has not settled, so its
// finally has not run yet), and the apply sweeps the set so no
// loadstate mark or park outlives its navigation.
(() => {
  'use strict';
  window.__gofastr = window.__gofastr || {};
  const NS = window.__gofastr;

  const _liveLoading = [];
  const findLoadingTpl = (addr) => {
    if (!addr) return null;
    for (const el of document.querySelectorAll('template[data-fui-loading]')) {
      if (el.getAttribute('data-fui-loading') === addr) return el;
    }
    return null;
  };
  const _showLoading = (e) => {
    if (!e.el.isConnected) return;
    e.park = document.createElement('div');
    e.park.hidden = true;
    document.body.appendChild(e.park);
    while (e.el.firstChild) e.park.appendChild(e.el.firstChild);
    e.el.appendChild(e.tpl.content.cloneNode(true));
    if (NS.scanAndLoadCSS) NS.scanAndLoadCSS(e.el);
    e.el.setAttribute('data-fui-loadstate', 'shown');
    e.shownAt = performance.now();
    e.shown = true;
    _liveLoading.push(e);
  };
  // Success settle: the apply already replaced the region's children;
  // drop the park and the state mark.
  const _settleLoading = (e) => {
    if (e.timer) { clearTimeout(e.timer); e.timer = 0; }
    const i = _liveLoading.indexOf(e);
    if (i >= 0) _liveLoading.splice(i, 1);
    if (!e.shown) return;
    if (e.park) { e.park.remove(); e.park = null; }

    if (e.el.isConnected) e.el.removeAttribute('data-fui-loadstate');
    e.shown = false;
  };
  // Failure/supersede restore: the parked nodes come back EXACTLY —
  // same nodes, so input values, listeners and island state survive.
  const _restoreLoading = (e) => {
    if (e.timer) { clearTimeout(e.timer); e.timer = 0; }
    const i = _liveLoading.indexOf(e);
    if (i >= 0) _liveLoading.splice(i, 1);
    if (!e.shown) return;
    if (e.park) {
      if (e.el.isConnected) e.el.replaceChildren(...e.park.childNodes);
      e.park.remove();
      e.park = null;
    }
    if (e.el.isConnected) e.el.removeAttribute('data-fui-loadstate');
    e.shown = false;
  };
  // The apply sweep: every live loading entry's region is one this
  // navigation replaces (entries are only created for such regions),
  // including entries a superseded navigation left behind.
  const _sweepLoading = () => { while (_liveLoading.length) _settleLoading(_liveLoading[0]); };
  // P9-ANIM helpers shared by the loading apply paths: claimEntry
  // retires a region's PENDING entry (its After timer never fired
  // because the content beat it) so the timer cannot park freshly
  // applied content a beat later; holdExit runs a shown region's Min
  // hold and exit animation before its content is replaced.
  const claimEntry = (e) => {
    if (!e) return;
    if (e.timer) { clearTimeout(e.timer); e.timer = 0; }
    _settleLoading(e);
  };
  // P9-ANIM: set the exit state and wait for its animation. The cap
  // (400ms) covers author CSS with no exit animation (animationend
  // never fires) and a hung one; bubbling animationend events from
  // CHILDREN (a skeleton's shimmer) are ignored — only the region's
  // own animation completes the wait.
  const _exitLoading = (e) => new Promise((res) => {
    if (!e.el.isConnected) { res(); return; }
    let done = false;
    const fin = (ev) => {
      if (ev && ev.target !== e.el) return;
      if (done) return;
      done = true;
      e.el.removeEventListener('animationend', fin);
      res();
    };
    e.el.addEventListener('animationend', fin);
    e.timer = setTimeout(fin, 400);
    e.el.setAttribute('data-fui-loadstate', 'exit');
  });
  const holdExit = async (e) => {
    if (!e || !e.shown) return;
    const hold = e.min - (performance.now() - e.shownAt);
    if (hold > 0) await new Promise((r) => { setTimeout(r, hold); });
    await _exitLoading(e);
    _settleLoading(e);
  };

  NS._navHooks.loading = {
    // The parts module's regions ride the same machinery.
    show: _showLoading,
    settle: _settleLoading,
    restore: _restoreLoading,
    claim: claimEntry,
    holdExit,

    // Seam (frag/nav.js busy block): schedule every marked region's
    // loading content. Outlets (by outlet address), areas (by area
    // address, 2026-09-26 "Areas take loading content"), and the swap
    // slot (by its bare layer key) can each carry a loading template;
    // a region that declares none keeps the busy dim only, as before.
    // P9-B: the swap slot can instead carry PER-ROUTE
    // loading content from the route manifest (the target screen's
    // WithLoading declaration); a server template for the same slot
    // wins. A deferred region's entry belongs to its PART fetch
    // (spike/layout-parts), not to loadPage's finish: the part
    // restores it on abort/failure and exits it on land. The After
    // timer arms here like every other region's.
    // Returns the navigation's loading state ({ wait, sweep, finish })
    // the navigator threads through its apply paths.
    schedule(o) {
      const { marks, path, epoch, parts } = o;
      const P = NS._navHooks.parts;
      const m = NS._navHooks.envelope?.manifest(path);
      const en = (m && m.entry) || {};
      // The manifest's loading projection: raw fields, normalized
      // here (kernel passes the route table through untouched).
      const rl = en.loading ? { html: en.loading, after: en.loadingAfter || 0, min: en.loadingMin || 0 } : null;
      const shows = [];
      for (const el of marks) {
        const isSlot = !!el.getAttribute('data-fui-layout-slot');
        let tpl = findLoadingTpl(el.getAttribute('data-fui-outlet')
          || el.getAttribute('data-fui-area')
          || el.getAttribute('data-fui-layout-slot'));
        let after = tpl ? (+tpl.getAttribute('data-fui-after') || 0) : 0;
        let min = tpl ? (+tpl.getAttribute('data-fui-min') || 0) : 0;
        if (parts && tpl && P) {
          const a = el.getAttribute('data-fui-outlet');
          if (a && parts.addrs.includes(a)) {
            P.claimEntry(parts, a, { el, tpl, after: Math.max(0, after), min: Math.max(0, min), epoch });
            continue;
          }
        }
        if (!tpl && isSlot && rl && rl.html) {
          tpl = document.createElement('template');
          tpl.innerHTML = rl.html;
          after = rl.after || 0;
          min = rl.min || 0;
        }
        if (!tpl) continue;
        shows.push({
          el, tpl,
          after: Math.max(0, after),
          min: Math.max(0, min),
          epoch, timer: 0, shown: false, park: null,
        });
      }
      for (const e of shows) {
        e.timer = setTimeout(() => { if (NS._navLive(e.epoch)) _showLoading(e); }, e.after);
      }
      const st = {
        shows,
        // The regions this navigation's apply REPLACED (the slot and
        // the fill targets it wrote). A commit may apply
        // synchronously (no view-transition module: the swap runs
        // directly, not a frame later inside startViewTransition), so
        // the sweep cannot assume the apply replaced every marked
        // region: a shown entry whose region was NOT replaced must
        // still restore its park at finish, not settle it away.
        applied: new Set(),
        // P9-A/P9-ANIM: honor Min across every shown loading region —
        // ONE hold for the whole apply transaction (the seed merge is
        // global; piecemeal application would break its atomicity) —
        // then run the exit animation on every shown region and wait
        // for it (animationend, capped) before the content goes in. A
        // fast response that beat every After timer has nothing shown
        // and skips both.
        async wait() {
          let hold = 0;
          for (const e of shows) {
            if (e.shown) hold = Math.max(hold, e.min - (performance.now() - e.shownAt));
          }
          if (hold > 0) {
            await new Promise((r) => { setTimeout(r, hold); });
          }
          if (shows.some((e) => e.shown)) {
            await Promise.all(shows.filter((e) => e.shown).map(_exitLoading));
          }
        },
        // The apply sweep: entries a superseded navigation left
        // behind settle (their navigation will never finish); this
        // navigation's own entries settle only when the apply
        // REPLACED their region — the rest wait for finish, which
        // restores them.
        sweep() {
          for (const e of _liveLoading.slice()) {
            if (e.st === st && !st.applied.has(e.el)) continue;
            _settleLoading(e);
          }
        },
        // A navigation that owns the epoch RESTORES (failure, abort);
        // a superseded one only settles — the newer navigation owns
        // the regions now, and re-parking is its business. On success
        // a shown entry settles when the apply replaced its region
        // and restores otherwise (a marked region the answer never
        // filled keeps its parked nodes).
        finish(owns) {
          for (const e of shows) {
            if (owns && !st.applied.has(e.el)) _restoreLoading(e);
            else _settleLoading(e);
          }
        },
      };
      for (const e of shows) e.st = st;
      return st;
    },
  };
  (NS.loadedModules ||= {}).loading = true;
})();
