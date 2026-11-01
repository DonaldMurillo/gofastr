// GoFastr runtime module, route preload (next-page HTML prefetch).
//
// Loaded by boot only when the route manifest declares a preload mode on
// at least one route ("hover" | "visible" | "eager"). Prefetches the
// destination's partial-nav response into a small TTL side cache that
// loadPage consults (window.__gofastr._takePrefetched) before fetching,
// so the eventual click paints without a round-trip.
//
// Safety rules (the contract, mirrored by prefetch_e2e_test.go):
//   - GET-only by construction; the request carries X-Gofastr-Prefetch: 1
//     so the server can skip session side effects.
//   - X-Gofastr-Session / X-Gofastr-Invalidate / X-Gofastr-Location on a
//     prefetch response are IGNORED, no DOM or store writes; a redirect
//     response is simply not cached. The next real navigation handles
//     all of those.
//   - A route that would present as an overlay from the CURRENT page is
//     never prefetched: overlay HTML never enters any cache.
//   - invalidate() selectors evict prefetched entries too (core calls
//     _invalPreload alongside its own cache sweep), and a matching
//     prefetch still in flight is never stored.
(function () {
  'use strict';
  const NS = window.__gofastr;

  const routes = () => (Array.isArray(window.__gofastr_routes) ? window.__gofastr_routes : []);
  // Same tolerant matcher as the intercept module: exact, trailing-slash
  // variants, then :param / catch-all patterns.
  const matchRoute = (pattern, path) => {
    if (pattern === path) return true;
    if (path !== '/' && !path.endsWith('/') && pattern === path + '/') return true;
    if (path !== '/' && path.endsWith('/') && pattern === path.slice(0, -1)) return true;
    if (!pattern.includes(':')) return false;
    const pp = pattern.split('/').filter(Boolean);
    const parts = path.split('/').filter(Boolean);
    if (pattern.includes('*') ? parts.length < pp.length : pp.length !== parts.length) return false;
    return pp.every((seg, i) => seg.startsWith(':') || seg === parts[i]);
  };
  // Routes register unprefixed: /es/pricing matches /pricing. A key
  // keeps its query (the click takes the entry by path + search); the
  // route match reads the path alone.
  const routeFor = (path) => {
    const bare = path.split('?')[0];
    const p = NS._unlocalize?.(bare) ?? bare;
    return routes().find((r) => r.path && matchRoute(r.path, p));
  };

  const MAX = 4;
  const store = new Map(); // path → { html, title, layer, at }
  const ttl = () => NS._preloadTTLms || 30000;

  const put = (path, entry) => {
    if (store.has(path)) store.delete(path);
    if (store.size >= MAX) store.delete(store.keys().next().value);
    store.set(path, entry);
  };

  // Consulted by loadPage before it fetches. Entries are single-use and
  // TTL-bounded: prefetched-then-first-viewed content has a tighter
  // freshness bar than back-nav history.
  NS._takePrefetched = function (path) {
    const e = store.get(path);
    if (!e) return null;
    store.delete(path);
    if (Date.now() - e.at > ttl()) return null;
    return e;
  };

  // Same selector semantics as the screen cache: "*" clears; a selector
  // with "?" is exact; a bare path drops the path and its query variants.
  // A prefetch still in flight holds the same stale answer, so a match
  // marks it too and its response is dropped when it lands.
  const hit = (s, k) => {
    // Keys keep the locale prefix; selectors never carry one.
    const u = NS._unlocalize?.(k) ?? k;
    return s === '*' || (s.includes('?') ? u === s : u === s || u.startsWith(s + '?'));
  };
  NS._invalPreload = function (sels) {
    for (const s of sels) {
      if (s !== '*' && (!s || s[0] !== '/')) continue;
      for (const k of store.keys()) if (hit(s, k)) store.delete(k);
      for (const k of inFlight) if (hit(s, k)) stale.add(k);
    }
  };

  const inFlight = new Set();
  const stale = new Set(); // in-flight paths an invalidation reached
  const prefetch = (path, warm) => {
    if (!NS._originOK || !NS._originOK(path)) return;
    if (store.has(path) || inFlight.has(path)) return;
    if (path === location.pathname + location.search || path === location.pathname) return;
    const target = routeFor(path);
    if (!target) return;
    // Never prefetch a route that would overlay the CURRENT page, the
    // click goes through the intercept module and overlay HTML must not
    // enter any cache.
    if (target.intercept) {
      const origin = routeFor(location.pathname);
      if (origin && (origin.path === target.intercept.from || (target.intercept.also || []).includes(origin.path))) return;
    }
    inFlight.add(path);
    // X-Gofastr-Defer (spike/layout-parts): the prefetch fetches the
    // PAGE ONLY — never the parts — but defers it like the eventual
    // click would, so the entry's deferred regions carry their loading
    // content and the click applies it instantly and requests the
    // missing parts beside nothing else.
    const hdrs = {
      'X-Gofastr-Navigate': '1',
      'X-Gofastr-From': location.pathname,
      'X-Gofastr-Prefetch': '1',
    };
    // The kernel's markup generation (frag/kernel.js _markup).
    if (NS._markup) hdrs['X-Gofastr-Markup'] = NS._markup;
    // The fills negotiation mirrors the click path (the opt-in gate):
    // only a document whose envelope module is loaded — one holding an
    // outlet or area marker — asks for the envelope, so a prefetched
    // entry has exactly the shape its click would have fetched.
    if (window.__gofastr._navHooks && window.__gofastr._navHooks.envelope) {
      hdrs['X-Gofastr-Fills'] = '2';
    }
    if (target.deferred && target.deferred.length) hdrs['X-Gofastr-Defer'] = '1';
    // cache:'no-store': the prefetched page shares its URL with the
    // parts the later click fires; Chrome's HTTP-cache write lock
    // would serialize them (spike/layout-parts).
    fetch(path, { headers: hdrs, credentials: 'same-origin', cache: 'no-store' })
      .then((r) => {
        // A redirect, signalled or followed, is the click's to take.
        if (!r.ok || r.redirected || r.headers.get('X-Gofastr-Location')) return null;
        if (r.headers.get('X-Gofastr-Partial') !== 'true') return null;
        // The navigators' decode: uihost percent-encodes a key holding
        // non-ASCII or % (headers read as Latin-1); a key that does not
        // decode stays as sent. Raw, it never matched a live slot.
        let layer = r.headers.get('X-Gofastr-Swap') || '';
        try { layer = decodeURIComponent(layer); } catch (_) {}
        return r.text().then((html) => ({
          html,
          title: NS._title(r),
          layer,
          // An envelope body parses at apply time (nav owns the
          // parser); the flag tells it which shape it holds.
          envelope: r.headers.get('X-Gofastr-Envelope') === '2',
          at: Date.now(),
        }));
      })
      // A hover entry's component sheets start now too, so the click
      // that takes it swaps at once instead of waiting on them; a
      // visible or eager prefetch is no sign of intent, so its sheets
      // wait for the click. An envelope body holds its page in
      // <template> fills, which the kernel's scan does not enter, so
      // each one's content is scanned too.
      .then((e) => {
        if (!e || stale.has(path)) return;
        put(path, e);
        if (!warm) return;
        const t = document.createElement('template');
        t.innerHTML = e.html;
        for (const r of [t, ...t.content.querySelectorAll('template')]) NS.scanAndLoadCSS(r.content);
      })
      .catch(() => {})
      .finally(() => { inFlight.delete(path); stale.delete(path); });
  };

  const modeFor = (href) => {
    let path;
    try { const u = new URL(href, location.href); path = u.pathname + u.search; } catch (_) { return ['', '']; }
    const r = routeFor(path);
    return [r && r.preload ? r.preload : '', path];
  };

  // hover: capture-phase pointerover/focusin, once per element.
  const seen = new WeakSet();
  const onHover = (e) => {
    const a = e.target.closest && e.target.closest('a[href]');
    if (!a || seen.has(a)) return;
    seen.add(a);
    const [mode, path] = modeFor(a.getAttribute('href'));
    if (mode === 'hover') prefetch(path, 1);
  };
  document.addEventListener('pointerover', onHover, true);
  document.addEventListener('focusin', onHover, true);

  // visible: one IntersectionObserver over links to preload:"visible"
  // routes; re-armed after every navigation (new DOM).
  const io = typeof IntersectionObserver === 'function'
    ? new IntersectionObserver((entries) => {
        for (const en of entries) {
          if (!en.isIntersecting) continue;
          io.unobserve(en.target);
          const [mode, path] = modeFor(en.target.getAttribute('href'));
          if (mode === 'visible') prefetch(path);
        }
      })
    : null;
  const armVisible = () => {
    if (!io) return;
    for (const a of document.querySelectorAll('a[href]')) {
      const [mode] = modeFor(a.getAttribute('href'));
      if (mode === 'visible') io.observe(a);
    }
  };

  // eager: at idle, the first few declared routes.
  const idle = window.requestIdleCallback || ((fn) => setTimeout(fn, 200));
  const armEager = () => {
    idle(() => {
      let n = 0;
      for (const r of routes()) {
        if (r.preload !== 'eager' || r.path.includes(':')) continue;
        if (n++ >= 3) break;
        prefetch(NS._lp?.(r.path) ?? r.path);
      }
    });
  };

  armVisible();
  armEager();
  window.addEventListener('gofastr:navigate', armVisible);

  (NS.loadedModules ||= {}).preload = true;
})();
