// GoFastr runtime module — Poll
//
// Page-level region polling. An element carrying
//   data-fui-poll="<duration>" data-fui-poll-src="<url>"
// gets GET-fetched on the interval; the response HTML replaces the
// element's innerHTML, then __gofastr.scanAndLoadCSS wires any
// freshly-arrived [data-fui-comp] component styles. Used for passive
// freshness of server-rendered regions that don't warrant an SSE
// channel — the pull-first half of the reactivity model.
//
// Interval syntax: Go-duration style ("5s", "30s", "1m", "1m30s").
// Clamped to a 5-second minimum so a typo can't DoS the server.
//
// Semantics shared with widget polling (widgets.js mountWidget):
//   - ±10% jitter per tick (desynchronise multiple polls on one page)
//   - pause while document.hidden; on visibilitychange → fetch
//     immediately and resume the cadence
//   - on fetch failure: double the interval, cap at 5× base, reset
//     to base on the next success
//   - tear down on element removal (self-check each tick) and on
//     SPA navigation (the _moduleScanners hook reclaims detached
//     elements and wires freshly-swapped-in ones)
//
// Loads on demand: core's marker scanner picks up [data-fui-poll]
// and idle-loads this module. The scanner is also invoked after SPA
// navigation and on MutationObserver-added nodes, so swapped-in
// regions get wired without a full page reload.
(() => {
  'use strict';
  window.__gofastr = window.__gofastr || {};
  const NS = window.__gofastr;

  const MIN_MS = 5000;
  // setTimeout delays are 32-bit: a value above ~24.8 days wraps and
  // fires IMMEDIATELY, turning a long poll into a tight loop. Cap every
  // scheduled delay here (the interval magnitude is preserved for the
  // back-off math; only the single wait is chunked — the tick re-checks
  // and re-arms, so an ultra-long poll simply fires a little early).
  const MAX_DELAY = 2147483647;
  const arm = (fn, ms) => setTimeout(fn, Math.min(Math.max(0, ms), MAX_DELAY));
  const stateByEl = new WeakMap();
  const active = new Set();

  // parseGoDuration parses a Go-style duration string into milliseconds.
  // Supports ns, us, µs, ms, s, m, h, fractions ("1.5m"), and compounds
  // ("1m30s"). The whole string must parse — anchored segments consumed
  // left to right, no gaps — so a typo yields NaN (region not wired)
  // instead of a silently wrong cadence.
  function parseGoDuration(s) {
    if (!s) return NaN;
    const re = /(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/y;
    const MUL = { ns: 1e-6, us: 1e-3, 'µs': 1e-3, ms: 1, s: 1000, m: 60000, h: 3600000 };
    let total = 0;
    let consumed = 0; // exec resets lastIndex to 0 on its final miss
    let m;
    while ((m = re.exec(s)) !== null) {
      total += Number(m[1]) * MUL[m[2]];
      consumed = re.lastIndex;
    }
    return consumed === s.length && consumed > 0 ? total : NaN;
  }

  // _clampedMs is the test surface for the clamp rule: parse a
  // data-fui-poll attribute value and return the effective interval
  // in milliseconds (>= MIN_MS), or NaN when the input is unusable.
  // chromedp exercises this directly so the clamp + parser don't
  // need a 5-second browser wait to verify.
  NS._pollClampedMs = function (raw) {
    const ms = parseGoDuration(raw);
    if (!isFinite(ms) || ms <= 0) return NaN;
    return Math.max(MIN_MS, ms);
  };

  // pollTicked bumps the shared liveness observable (poll analog of
  // sseStatus): one object mutated in place, shared by page + widget
  // polls. Tests and health UI read it.
  function pollTicked() {
    const ps = NS.pollStatus || (NS.pollStatus = { ticks: 0, lastTickAt: 0 });
    ps.ticks++;
    ps.lastTickAt = Date.now();
  }

  function teardown(el) {
    const s = stateByEl.get(el);
    if (!s) return;
    s.stopped = true;
    if (s.timer) { clearTimeout(s.timer); s.timer = null; }
    if (s.onVisible) document.removeEventListener('visibilitychange', s.onVisible);
    stateByEl.delete(el);
    active.delete(el);
  }

  function wireOne(el) {
    if (!el || !el.getAttribute || el.__fuiPollWired) return;
    const raw = el.getAttribute('data-fui-poll') || '';
    const src = el.getAttribute('data-fui-poll-src') || '';
    if (!src) return;
    const parsed = parseGoDuration(raw);
    if (!isFinite(parsed) || parsed <= 0) return;
    const base = Math.max(MIN_MS, parsed);
    el.__fuiPollWired = true;
    const s = {
      base, cap: base * 5, current: base,
      timer: null, stopped: false, onVisible: null, tick: null,
    };
    stateByEl.set(el, s);
    active.add(el);
    s.tick = () => {
      if (s.stopped) return;
      // Self-teardown: element was removed (island swap, SPA nav,
      // or manual removal) without the scanner catching it. The
      // timer holds a closure reference to el until we clear it.
      if (!el.isConnected) { teardown(el); return; }
      if (document.hidden) return; // visibility handler resumes
      // Single-chain guard: a hidden→visible flip while a fetch is
      // pending must not start a second chain — both would reach
      // .finally and each would arm its own timer, multiplying the
      // poll forever. The pending fetch's finally keeps the cadence.
      if (s.inFlight) return;
      s.inFlight = true;
      fetch(src, {
        headers: { 'Accept': 'text/html' },
        credentials: 'same-origin',
      })
        .then((r) => {
          // An HTTP error must reach .catch so back-off applies —
          // a bare `null` return would skip both success and catch,
          // leaving the interval untouched on 500s.
          if (!r.ok) throw new Error('poll: HTTP ' + r.status);
          return r.text();
        })
        .then((html) => {
          if (s.stopped || html == null) return;
          s.current = s.base; // success resets any prior back-off
          // Reuse the same per-region swap pattern setSignal uses
          // for html-mode (innerHTML + scanAndLoadCSS): one innerHTML
          // path, not a hand-rolled second one. scanAndLoadCSS is
          // already exposed on __gofastr and handles component-CSS
          // dedup for any freshly-arrived [data-fui-comp] markers.
          el.innerHTML = html;
          if (NS.scanAndLoadCSS) NS.scanAndLoadCSS(el);
          pollTicked();
        })
        .catch(() => {
          // Double the interval, capped at 5× base; the next
          // success resets to base. Network drop → gentle back-off
          // rather than a tight retry loop.
          s.current = Math.min(s.cap, s.current * 2);
        })
        .finally(() => {
          s.inFlight = false;
          if (s.stopped) return;
          if (!el.isConnected) { teardown(el); return; }
          // ±10% jitter so a page full of polls doesn't synchronise
          // into a thundering herd on every tick.
          const jitter = s.current * (0.9 + Math.random() * 0.2);
          if (s.timer) clearTimeout(s.timer);
          s.timer = arm(s.tick, jitter);
        });
    };
    s.onVisible = () => {
      if (document.hidden) return;
      if (s.timer) { clearTimeout(s.timer); s.timer = null; }
      s.tick(); // immediate refresh on regain, cadence resumes after
    };
    document.addEventListener('visibilitychange', s.onVisible);
    const firstJitter = base * (0.9 + Math.random() * 0.2);
    s.timer = arm(s.tick, firstJitter);
  }

  function wireAll(root) {
    const scope = root && root.querySelectorAll ? root : document;
    if (scope.matches && scope.matches('[data-fui-poll]')) wireOne(scope);
    if (scope.querySelectorAll) {
      scope.querySelectorAll('[data-fui-poll]').forEach(wireOne);
    }
  }

  // reclaimAndWire: tear down timers for elements that have left the
  // DOM (SPA nav, island swap), then wire any freshly-arrived polls.
  // Registered as the _moduleScanners.poll hook — core runtime calls
  // it on gofastr:navigate and on MutationObserver-added-node batches.
  function reclaimAndWire(root) {
    for (const el of Array.from(active)) {
      if (!el.isConnected) teardown(el);
    }
    wireAll(root);
  }


  // _widgetPoll: the widget-level poll loop (Builder.Poll). It lives in
  // this demand-loaded module — not widgets.js — so widget-bearing pages
  // that never poll don't ship the cadence machinery; widgets.js
  // loadModule('poll')s and calls it at mount. Shares the semantics of
  // the page-level poller above (jitter, hidden-pause, back-off,
  // pollStatus) and installs pollStop/pollNow on the widget entry.
  NS._widgetPoll = (cfg, entry) => {
    if (!entry || entry.pollStop) return; // already wired (or gone)
    // Math.trunc, NOT `| 0`: bitwise coercion is 32-bit signed, so a
    // legitimate long interval (e.g. Poll(30*24*time.Hour) → 2.59e9 ms)
    // wraps NEGATIVE and Math.max floors it to 100 ms — a monthly poll
    // becomes a 10 req/s hammer. Trunc preserves the real magnitude.
    const requested = Number(cfg.pollMs);
    if (!Number.isFinite(requested) || requested <= 0) return;
    const base = Math.max(100, Math.trunc(requested));
    const CAP = base * 5;
    let current = base;
    let timer = null;
    let stopped = false;
    let inFlight = false;
    let queued = false; // pollNow landed mid-fetch → re-tick immediately
    const tick = () => {
      if (stopped) return;
      if (document.hidden) return; // visibility handler resumes
      // Single-chain guard: a visibility flip (or pollNow) while a
      // fetch is pending must not arm a second timer chain.
      if (inFlight) return;
      inFlight = true;
      fetch(cfg.statePath, { headers: { 'X-FUI-Widget': cfg.name } })
        .then((r) => {
          // HTTP errors must reach .catch so back-off applies.
          if (!r.ok) throw new Error('poll ' + r.status);
          return r.json();
        })
        .then((state) => {
          if (stopped || !state) return; // dismissed mid-flight: drop
          current = base; // success resets any prior back-off
          for (const k in state) {
            if (!Object.prototype.hasOwnProperty.call(state, k)) continue;
            const prev = NS._signals[k];
            const next = state[k];
            if (prev && prev.value === next) continue; // skip no-op write
            NS.setSignal(k, next);
          }
          pollTicked();
        })
        .catch(() => {
          // Double the interval, capped at 5x base; reset on next success.
          current = Math.min(CAP, current * 2);
        })
        .finally(() => {
          inFlight = false;
          if (stopped) return;
          if (timer) clearTimeout(timer);
          if (queued) {
            // A mutation's pollNow landed while this fetch was in
            // flight — its response may predate the write. Re-fetch
            // immediately so the promised authoritative refresh isn't
            // silently absorbed into the stale cadence response.
            queued = false;
            timer = arm(tick, 0);
            return;
          }
          // ±10% jitter so a page full of polling widgets doesn't
          // synchronise into a thundering herd on every tick.
          const jitter = current * (0.9 + Math.random() * 0.2);
          timer = arm(tick, Math.max(100, jitter));
        });
    };
    const onVisible = () => {
      if (document.hidden) return;
      if (timer) { clearTimeout(timer); timer = null; }
      tick(); // immediate refresh on regain, then cadence resumes
    };
    document.addEventListener('visibilitychange', onVisible);
    entry.pollStop = () => {
      stopped = true;
      if (timer) { clearTimeout(timer); timer = null; }
      document.removeEventListener('visibilitychange', onVisible);
    };
    // pollNow: immediate authoritative re-fetch, used by dispatchRPC
    // after a successful mutation so the widget reflects its own
    // writes at once instead of waiting out the cadence.
    entry.pollNow = () => {
      if (stopped) return;
      if (inFlight) { queued = true; return; } // coalesce; finally re-ticks
      if (timer) { clearTimeout(timer); timer = null; }
      tick(); // cadence resumes via the tick's own finally
    };
    // Seed the cycle. The initial mount-hydration fetch already ran in
    // widgets.js; this is the first scheduled re-fetch.
    const firstJitter = base * (0.9 + Math.random() * 0.2);
    timer = arm(tick, Math.max(100, firstJitter));
  };

  ((NS._moduleScanners ||= {})).poll = reclaimAndWire;
  (NS.loadedModules ||= {}).poll = true;
  wireAll(document);
})();
