// GoFastr runtime module, parts
//
// Parallel parts (spike/layout-parts). Streaming stopped being the
// transport for client navigations: Chrome keeps no body for a
// fetch() read through a stream reader and reports the request as
// net::ERR_ABORTED, so DevTools cannot show what a navigation
// fetched. A click to a route whose manifest entry lists deferred
// outlets therefore sends the page request with X-Gofastr-Defer (the
// deferred loaders are skipped, their loading content travels in
// place) and, at the same time, one X-Gofastr-Part request per
// deferred address, read with text() — every request shows in
// DevTools with its own body, status and timing.
//
// Loaded when any route in the manifest has deferred outlets (the
// kernel's boot manifest check). Before it loads the page request
// omits X-Gofastr-Defer, so the server renders deferred outlets
// inline: the page is complete on arrival, no parts fly.
//
// Parts are ROUTE requests, never islands: a deferred outlet is part
// of the route's answer. They fetch with priority 'low' beside the
// page's 'high', at most PARTS_MAX_INFLIGHT in flight, the rest
// queued. Parts that land before the page commits wait in a buffer;
// the commit applies the page, then the buffer; later parts apply on
// land through the region's own loadstate exit/enter (never a second
// startViewTransition). A page redirect/block/failure, a 409 part
// reset for any reason but a dead session (the page and the part saw
// different state: reload the whole document, once) or a newer
// navigation discards the buffer and aborts the parts; each part owns
// its region's loading content and puts the parked nodes back on
// abort or failure. The dead-session reset (`session`) is the one
// repairable disagreement: the part waits for the page commit — whose
// Set-Cookie stores the re-minted token — and re-requests itself once;
// a second session reset falls back to the reload.

(() => {
  'use strict';
  window.__gofastr = window.__gofastr || {};
  const NS = window.__gofastr;

  const PARTS_MAX_INFLIGHT = 3;
  // The newest navigation's part state: a new loadPage kills the
  // previous one (abort + restore) before its own leave-capture reads
  // the DOM, so Back's capture never races a part apply.
  let _live = null;

  const newPartsState = (path, addrs, epoch, ctl) => ({
    path, addrs, epoch, ctl,
    entries: new Map(),  // addr → this nav's loading entry for the region
    landed: new Map(),   // addr → {html, seed} waiting for the commit
    parts: new Map(),    // addr → applied/landed html (cache record)
    retried: new Set(),  // addrs that used their one session-reset retry
    sessRetry: new Set(), // addrs waiting for the commit to re-request (session reset)
    committed: false, pending: false, dead: false, reset: false, resetPending: false, launched: false,
    enqueue: null,       // set by launchParts: re-queue one addr on this nav's pump
  });

  // killParts aborts a navigation's part requests and settles their
  // regions: uncommitted → the parked nodes come back exactly (the
  // navigation never landed, the region must look untouched);
  // committed → settle (the page owns the region now, the loading
  // content stays until whatever comes next replaces it).
  const killParts = (st) => {
    if (!st || st.dead) return;
    st.dead = true;
    try { st.ctl.abort(); } catch (_) { /* already gone */ }
    const L = NS._navHooks.loading;
    for (const e of st.entries.values()) {
      if (L) { if (st.committed) L.settle(e); else L.restore(e); }
    }
  };

  // markDeferredRegions runs INSIDE the commit, after the envelope
  // applied: every deferred region now holds its loading content (the
  // server-rendered placeholder fill). Retire the part machinery's
  // pending entries (their After timer must not park the placeholder)
  // and record a shown entry per region so the eventual part apply
  // runs the region's Min hold and exit animation. A live SHOWN entry
  // (the timer fired before the commit) keeps its park.
  const markDeferredRegions = (st) => {
    const E = NS._navHooks.envelope;
    const L = NS._navHooks.loading;
    for (const addr of st.addrs) {
      const el = E && E.target(addr);
      if (!el) continue;
      const prev = st.entries.get(addr);
      if (prev && prev.shown) continue;
      if (prev && L) L.claim(prev);
      const tpl = findLoadingTpl(addr);
      st.entries.set(addr, {
        el, tpl,
        after: 0,
        min: tpl ? (+tpl.getAttribute('data-fui-min') || 0) : 0,
        epoch: st.epoch, timer: 0, shown: true, park: null,
        shownAt: performance.now(),
      });
      el.setAttribute('data-fui-loadstate', 'shown');
    }
  };
  const findLoadingTpl = (addr) => {
    if (!addr) return null;
    for (const el of document.querySelectorAll('template[data-fui-loading]')) {
      if (el.getAttribute('data-fui-loading') === addr) return el;
    }
    return null;
  };

  // applyPartNow writes a part that was HELD until the commit (or a
  // replayed entry's held part): no exit animation — the loading
  // content and this write land in one task, nothing painted between.
  const applyPartNow = (st, addr, p) => {
    const E = NS._navHooks.envelope;
    const L = NS._navHooks.loading;
    const target = E && E.target(addr);
    if (!target) return;
    if (L) L.claim(st.entries.get(addr));
    if (p.seed && E) E.applySeed(p.seed);
    target.innerHTML = p.html;
    if (NS.scanAndLoadCSS) NS.scanAndLoadCSS(target);
    st.parts.set(addr, p.html);
    window.dispatchEvent(new CustomEvent('gofastr:fill', {
      detail: { addr, path: st.path, t: performance.now() },
    }));
  };

  // resetParts reloads the whole document for a page and a part that
  // saw different state, at most once per navigation.
  const resetParts = (st) => {
    if (st.reset) return;
    st.reset = true;
    killParts(st);
    location.reload();
  };

  // flushSessRetries launches the parts that answered a `session`
  // reset while the page was in flight: the commit just applied the
  // page answer, whose Set-Cookie stored the re-minted token, so the
  // one retry per address carries a live session.
  const flushSessRetries = (st) => {
    if (!st.sessRetry.size) return;
    const addrs = Array.from(st.sessRetry);
    st.sessRetry.clear();
    for (const a of addrs) st.enqueue(a);
  };

  // commitParts runs INSIDE the page's commit callback. A page answered
  // with an error page (404, 500) discards its parts: the deferred
  // regions belong to a page that did not render. A reset that landed
  // before the commit fires now, against an OK page only. The session
  // retries follow the commit (the fresh cookie is stored by then).
  const commitParts = (st, ok) => {
    st.committed = true;
    if (!ok) { killParts(st); return false; }
    if (st.resetPending) { resetParts(st); return false; }
    markDeferredRegions(st);
    flushSessRetries(st);
    return true;
  };

  // flushLanded applies the parts that landed while the page was in
  // flight. Runs INSIDE the commit callback, synchronously after
  // markDeferredRegions: the loading content and these writes land in
  // one task, nothing paints between.
  const flushLanded = (st) => {
    for (const [addr, p] of st.landed) {
      st.landed.delete(addr);
      applyPartNow(st, addr, p);
    }
  };

  // applyPart writes a part landing AFTER the commit: its region's
  // Min hold and exit animation run first (the loading content leaves
  // through data-fui-loadstate, per region), then the content, the
  // seed delta, and the gofastr:fill event. Never a view transition:
  // a second startViewTransition would skip the page's.
  const applyPart = async (st, addr, p) => {
    const E = NS._navHooks.envelope;
    const L = NS._navHooks.loading;
    const target = E && E.target(addr);
    if (!target) return; // deploy skew on one part: skip it, the page stands
    if (L) await L.holdExit(st.entries.get(addr));
    if (st.dead || !NS._navLive(st.epoch)) return;
    if (p.seed && E) E.applySeed(p.seed);
    target.innerHTML = p.html;
    if (NS.scanAndLoadCSS) NS.scanAndLoadCSS(target);
    st.parts.set(addr, p.html);
    const ce = E && E.cacheGet && E.cacheGet(st.path);
    if (ce && ce.partAddrs) ce.parts.set(addr, p.html);
    window.dispatchEvent(new CustomEvent('gofastr:fill', {
      detail: { addr, path: st.path, t: performance.now() },
    }));
  };

  // launchParts starts (or resumes) the part fetches: at most
  // PARTS_MAX_INFLIGHT in flight, the rest queued behind completions.
  const launchParts = (st, addrs) => {
    if (st.dead || st.launched) return;
    st.launched = true;
    const queue = addrs.slice();
    let inFlight = 0;
    // enqueue re-queues one address on this navigation's pump: the
    // session-reset retry reuses the whole runPart path (headers,
    // abort signal, in-flight cap, apply) with the fresh cookie.
    st.enqueue = (addr) => { if (!st.dead) { queue.push(addr); pump(); } };
    const done = () => { inFlight--; pump(); };
    const restore = (addr) => {
      const e = st.entries.get(addr);
      const L = NS._navHooks.loading;
      if (e && L) { if (st.committed) L.settle(e); else L.restore(e); }
    };
    const runPart = async (addr) => {
      inFlight++;
      let resp = null, failed = false;
      try {
        // cache:'no-store': same-URL fetches serialize behind
        // Chrome's HTTP-cache write lock otherwise (see the page
        // fetch's comment).
        resp = await fetch(st.path, {
          headers: { 'X-Gofastr-Navigate': '1', 'X-Gofastr-Part': addr },
          priority: 'low',
          cache: 'no-store',
          signal: st.ctl.signal,
        });
      } catch (_) { failed = true; }
      done();
      if (st.dead || !NS._navLive(st.epoch)) { restore(addr); return; }
      // A 409 reset is a whole-page disagreement (a guard redirected,
      // a forged address): apply nothing, abort the siblings, reload
      // the whole document — at most once per navigation. The reset
      // waits for the page's outcome: a page that fails, redirects or
      // answers an error page owns the result, and its parts die with
      // it (commitParts). Only a committed OK page is reloaded.
      // A `session` reset is the one repairable case: the page request
      // beside this part re-mints the dead session and its Set-Cookie
      // lands with the page answer, so the part is re-requested ONCE
      // after the commit (the fresh cookie rides the retry). A second
      // session reset, or any other reason, reloads as above.
      const why = resp ? resp.headers.get('X-Gofastr-Part-Reset') : null;
      if (resp && resp.status === 409 && (why === '1' || why === 'session')) {
        if (why === 'session' && !st.retried.has(addr)) {
          st.retried.add(addr);
          if (st.committed) st.enqueue(addr);
          else st.sessRetry.add(addr);
          return;
        }
        if (st.committed) resetParts(st);
        else st.resetPending = true;
        return;
      }
      if (failed || !resp.ok) { restore(addr); return; }
      // text(), never a stream reader: DevTools keeps the body.
      let text = '';
      try { text = await resp.text(); } catch (_) { restore(addr); return; }
      if (st.dead || !NS._navLive(st.epoch)) { restore(addr); return; }
      if (text.indexOf('data-fui-fill') < 0 && text.indexOf('gofastr-signals-partial') < 0) { restore(addr); return; }
      const snap = NS._navHooks.envelope.parse(text, addr);
      const p = { html: snap.primary.html, seed: null };
      if (snap.seed) {
        try { p.seed = JSON.parse(snap.seed.textContent || 'null'); } catch (_) { p.seed = null; }
      }
      if (st.committed) applyPart(st, addr, p);
      else st.landed.set(addr, p);
    };
    const pump = () => {
      while (!st.dead && queue.length && inFlight < PARTS_MAX_INFLIGHT) runPart(queue.shift());
    };
    pump();
  };

  NS._navHooks.parts = {
    launch: launchParts,
    commit: commitParts,
    flush: flushLanded,
    applyNow: applyPartNow,
    kill: killParts,

    // Seam (frag/nav.js loadPage entry): the destination's deferred
    // outlets (manifest `deferred`), one part request per address
    // beside the page fetch. Returns the navigation's part state
    // (carry it to the commit/finish seams), or null when the route
    // defers nothing. fetchOpts rides the page fetch: priority 'high',
    // cache:'no-store' (the parts and the page fetch the SAME URL, and
    // Chrome serializes same-URL fetches behind its HTTP-cache write
    // lock — without this the high-priority page waits for every
    // low-priority part), and the abort signal.
    begin(path, epoch) {
      if (_live && !_live.dead) killParts(_live); // supersede the older navigation first
      const E = NS._navHooks.envelope;
      const m = E && E.manifest(path);
      const addrs = ((m && m.entry && m.entry.deferred) || []).map((a) => a.includes('{') ? E.subst(a, path) : a);
      if (!addrs.length) return null;
      const ctl = new AbortController();
      const st = newPartsState(path, addrs, epoch, ctl);
      st.fetchOpts = { priority: 'high', cache: 'no-store', signal: ctl.signal };
      _live = st;
      return st;
    },

    // Seam (frag/nav.js loading scheduler): a deferred region's entry
    // belongs to its part fetch, not to loadPage's finish — the part
    // restores it on abort/failure and exits it on land. The After
    // timer arms here like every other region's.
    claimEntry(st, addr, cfg) {
      const pe = {
        el: cfg.el, tpl: cfg.tpl,
        after: cfg.after, min: cfg.min,
        epoch: cfg.epoch, timer: 0, shown: false, park: null,
      };
      st.entries.set(addr, pe);
      pe.timer = setTimeout(() => {
        if (NS._navLive(pe.epoch)) NS._navHooks.loading?.show(pe);
      }, pe.after);
    },

    // Seam (frag/nav.js plain-partial commit): the page landed — OK,
    // the buffer may flush when its parts land; an error page (404,
    // 500) discards its parts: the deferred regions belong to a page
    // that did not render. A pending session retry launches here too:
    // this IS the commit its wait was for.
    land(st, failed) {
      if (!st) return;
      if (failed) { commitParts(st, false); return; }
      st.committed = true;
      if (st.resetPending) { resetParts(st); return; }
      flushSessRetries(st);
    },

    // Seam (frag/nav.js finally): a navigation that neither committed
    // nor has a commit pending discards its parts — the buffer dies
    finish(st) { if (st && !st.committed && !st.pending) killParts(st); },

    // Seam (frag/nav.js leave-capture): WHICH addresses the leaving
    // page's own render deferred (the previous entry knows — the
    // manifest list alone would wrongly defer a page reached as a
    // whole document, whose regions hold their real content inline)
    // and the parts that landed. Unlanded regions hold their loading
    // content in the captured bytes; a replay re-requests them.
    leaveSnap(prevEntry) {
      return (prevEntry && prevEntry.partAddrs)
        ? { partAddrs: prevEntry.partAddrs, parts: prevEntry.parts }
        : null;
    },
  };

  // The deferred trigger's module-side half (this evaluation, not
  // frag/boot.js): when one of the manifest's deferred
  // ADDRESSES is live in this document, the envelope module boot-loads
  // too — the parts launch rides its navigator, and a deferred page's
  // FIRST navigation must already send X-Gofastr-Defer. Every other
  // marker page loads the module at its first navigation instead. A
  // param-keyed deferred layer's manifest key never matches a live DOM
  // key literally; such a page takes the hand-off on its first
  // navigation and its deferred outlets render inline that once. The
  // addresses are matched against the live outlet and area markers by
  // attribute walk (addresses carry ':', '#', '/' and '~' —
  // selector-hostile, and a built selector from manifest strings is a
  // shape this runtime does not ship).
  if (Array.isArray(window.__gofastr_routes) &&
      window.__gofastr_routes.some((r) => (r.deferred || []).some((a) => {
        for (const el of document.querySelectorAll('[data-fui-outlet],[data-fui-area]')) {
          if ((el.getAttribute('data-fui-outlet') || el.getAttribute('data-fui-area')) === a) return true;
        }
        return false;
      }))) {
    NS.loadModule('envelope');
  }

  (NS.loadedModules ||= {}).parts = true;
})();
