// GoFastr runtime module — intercepting routes
//
// A detail screen that presents as an overlay when you reach it from
// inside the app, and as its own full page when you land on it directly.
// Registered server-side with app.InterceptFrom("/products",
// app.ScreenDrawer); the route manifest carries {from, as} per route and
// core loads this module only when at least one route declares it.
//
// The rules that keep this honest:
//
//   - SSR-first is untouched. A hard load, a refresh, or an external
//     link renders the canonical full page. Only a soft navigation that
//     STARTED on the declared origin is diverted.
//   - The server decides. We ask with X-Gofastr-Intercept and say where
//     we came from; the response only counts as an overlay if it comes
//     back with X-Gofastr-Overlay. No header, no overlay — we hand the
//     navigation back to the normal loadPage path.
//   - The page underneath stays mounted. Closing is history.back(), so
//     Back, ESC, and the backdrop all resolve to the same thing, and
//     returning to the list costs no refetch.
//   - Intercepted HTML never enters the screen cache. The cache is
//     keyed by path and holds canonical page renders; storing an
//     overlay variant there would poison a later direct visit.
(() => {
  'use strict';
  window.__gofastr = window.__gofastr || {};
  const NS = window.__gofastr;

  const OVERLAY_ID = 'fui-intercept';
  let open = null; // { underPath, restoreFocus }

  const routes = () => (Array.isArray(window.__gofastr_routes) ? window.__gofastr_routes : []);

  // Match a resolved path against the manifest's route patterns. Static
  // segments must match exactly; ':param' and '{param}' match one
  // segment; a trailing catch-all matches the remainder.
  function matchRoute(pattern, path) {
    const p = pattern.split('/').filter(Boolean);
    const s = path.split('/').filter(Boolean);
    for (let i = 0; i < p.length; i++) {
      const seg = p[i];
      const dyn = seg[0] === ':' || seg[0] === '{';
      if (dyn && (seg.endsWith('...') || seg.endsWith('*') || seg.endsWith('...}'))) return true;
      if (i >= s.length) return false;
      if (!dyn && seg !== s[i]) return false;
    }
    return p.length === s.length;
  }

  const routeFor = (path) => routes().find((r) => r.path && matchRoute(r.path, path));

  function overlayHost() {
    let el = document.getElementById(OVERLAY_ID);
    if (el) return el;
    el = document.createElement('div');
    el.id = OVERLAY_ID;
    el.setAttribute('data-fui-intercept-overlay', '');
    document.body.appendChild(el);
    return el;
  }

  function close(fromPopstate) {
    const el = document.getElementById(OVERLAY_ID);
    if (!el || !open) return;
    el.remove();
    if (NS.doc) NS.doc.unlockScroll('intercept');
    const focus = open.restoreFocus;
    open = null;
    if (focus && typeof focus.focus === 'function') {
      try { focus.focus({ preventScroll: true }); } catch (_) { focus.focus(); }
    }
    // Closing IS going back — the overlay owns one history entry, so
    // ESC and the backdrop go through history rather than around it.
    if (!fromPopstate) history.back();
  }

  function mount(html, as, path, hash) {
    const el = overlayHost();
    el.setAttribute('data-fui-intercept-as', as);
    el.innerHTML = html;
    if (NS.doc) NS.doc.lockScroll('intercept');
    // Deliberately NOT NS._pushURL: currentPath must stay on the page
    // UNDER the overlay, so the popstate fired by close()'s
    // history.back() sees no path change and skips the refetch — the
    // list under the overlay never unmounted.
    history.pushState(null, '', path + (hash || ''));
    // Focus the overlay's first focusable so keyboard users land inside
    // it, exactly as the drawer/sheet widgets do.
    const first = el.querySelector(NS._focusSel);
    const target = first || el.firstElementChild;
    if (target) {
      if (!first && target.setAttribute) target.setAttribute('tabindex', '-1');
      try { target.focus({ preventScroll: true }); } catch (_) { /* not focusable */ }
    }
    // No explicit rescan: core's MutationObserver watches document.body
    // with subtree:true and demand-loads modules for markers in newly
    // inserted nodes, which is exactly how dynamically-opened widget
    // chrome gets wired.
  }

  // Hand a navigation back to the normal SPA path. loadPage is private
  // to core, so reach it the way the browser does: move the URL, then
  // fire popstate — core's handler sees a path change and loads it. That
  // avoids exporting anything new from a bundle with no room.
  function fallbackNav(path, hash) {
    // Deliberately NOT NS._pushURL: the choke point syncs currentPath,
    // and the popstate handler below loads only when the URL DIFFERS
    // from currentPath — a synced write would make the synthetic event
    // a no-op. The raw push leaves currentPath stale on purpose; the
    // handler stamps the entry's id itself when it finds none.
    history.pushState(null, '', path + (hash || ''));
    window.dispatchEvent(new PopStateEvent('popstate'));
  }

  // Called by core's link handler before it pushes state. Returning true
  // claims the navigation.
  NS._intercept = function (path, hash) {
    if (open) return false;                       // one overlay at a time
    const target = routeFor(path);
    if (!target || !target.intercept) return false;
    const origin = routeFor(location.pathname);
    if (!origin || origin.path !== target.intercept.from) return false;

    const underPath = location.pathname + location.search;
    const trigger = document.activeElement;
    if (!NS._originOK?.(path)) return false;
    fetch(path, {
      headers: {
        'X-Gofastr-Navigate': '1',
        'X-Gofastr-Intercept': '1',
        'X-Gofastr-From': underPath,
      },
      credentials: 'same-origin',
    })
      .then((r) => {
        // An intercepted response can invalidate regular screens even
        // though the overlay itself is never cached.
        if (r.ok) NS._inval?.(r);
        const as = r.headers.get('X-Gofastr-Overlay');
        // The server declined to intercept (or redirected). Fall back to
        // the ordinary navigation rather than guessing.
        if (!r.ok || !as || r.headers.get('X-Gofastr-Location')) return null;
        return r.text().then((html) => ({ html, as }));
      })
      .then((res) => {
        if (!res) {
          fallbackNav(path, hash);
          return;
        }
        open = { underPath, restoreFocus: trigger };
        mount(res.html, res.as, path, hash);
      })
      .catch(() => fallbackNav(path, hash));
    return true;
  };

  // Any history move while the overlay is up drops it. Returning to the
  // page underneath costs no refetch — the list never unmounted — and
  // core's own popstate handler loads anything further afield.
  window.addEventListener('popstate', () => { if (open) close(true); });

  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && open) { e.preventDefault(); close(false); }
  });

  document.addEventListener('click', (e) => {
    if (!open) return;
    const el = document.getElementById(OVERLAY_ID);
    if (el && e.target === el) close(false);            // backdrop
    if (e.target.closest && e.target.closest('[data-fui-intercept-close]')) {
      e.preventDefault();
      close(false);
    }
  });

  (NS.loadedModules = NS.loadedModules || {}).intercept = true;
})();
