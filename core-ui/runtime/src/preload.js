// GoFastr runtime module — route preload (next-page HTML prefetch).
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
//     prefetch response are IGNORED — no DOM or store writes; a redirect
//     response is simply not cached. The next real navigation handles
//     all of those.
//   - A route that would present as an overlay from the CURRENT page is
//     never prefetched: overlay HTML never enters any cache.
//   - invalidate() selectors evict prefetched entries too (core calls
//     _invalPreload alongside its own cache sweep).
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
  const routeFor = (path) => routes().find((r) => r.path && matchRoute(r.path, path));

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
  NS._invalPreload = function (sels) {
    for (const s of sels) {
      if (s === '*') { store.clear(); return; }
      if (!s || s[0] !== '/') continue;
      if (s.includes('?')) { store.delete(s); continue; }
      for (const k of store.keys()) if (k === s || k.startsWith(s + '?')) store.delete(k);
    }
  };

  const inFlight = new Set();
  const prefetch = (path) => {
    if (!NS._originOK || !NS._originOK(path)) return;
    if (store.has(path) || inFlight.has(path)) return;
    if (path === location.pathname + location.search || path === location.pathname) return;
    const target = routeFor(path);
    if (!target) return;
    // Never prefetch a route that would overlay the CURRENT page — the
    // click goes through the intercept module and overlay HTML must not
    // enter any cache.
    if (target.intercept) {
      const origin = routeFor(location.pathname);
      if (origin && origin.path === target.intercept.from) return;
    }
    inFlight.add(path);
    fetch(path, {
      headers: {
        'X-Gofastr-Navigate': '1',
        'X-Gofastr-From': location.pathname,
        'X-Gofastr-Prefetch': '1',
      },
      credentials: 'same-origin',
    })
      .then((r) => {
        if (!r.ok || r.headers.get('X-Gofastr-Location')) return null;
        if (r.headers.get('X-Gofastr-Partial') !== 'true') return null;
        return r.text().then((html) => ({
          html,
          title: decodeURIComponent(r.headers.get('X-Gofastr-Title') || document.title),
          layer: r.headers.get('X-Gofastr-Swap') || '',
          at: Date.now(),
        }));
      })
      .then((e) => { if (e) put(path, e); })
      .catch(() => {})
      .finally(() => inFlight.delete(path));
  };

  const modeFor = (href) => {
    let path;
    try { path = new URL(href, location.href).pathname; } catch (_) { return ['', '']; }
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
    if (mode === 'hover') prefetch(path);
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
        prefetch(r.path);
      }
    });
  };

  armVisible();
  armEager();
  window.addEventListener('gofastr:navigate', armVisible);

  (NS.loadedModules ||= {}).preload = true;
})();
