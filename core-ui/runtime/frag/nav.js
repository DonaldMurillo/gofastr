// nav.js — SPA router (spec fragment `nav`, boot class; deps: kernel+signals).
// Owns: <a> click hijack, history.pushState, popstate, screen cache,
// cross-layout shell swap, screen-group sibling nav, updateActiveLink,
// document.title writes, the navigate() namespace member.

  // -----------------------------------------------------------------------
  // Screen cache — stores rendered screens for instant back-navigation.
  // -----------------------------------------------------------------------
  const screenCache = new Map(); // path → { html, title }
  // sseMeta reads the live stream-id carrier from a document (default:
  // the live one). The SSE module re-reads it on every reconnect, so
  // pointing it at a fresh session id is the whole recovery contract.
  const sseMeta = (d) => (d || document).querySelector('meta[name="gofastr-sse"]');
  const MAX_CACHE_SIZE = 20;

  // True LRU: Map preserves insertion order, so delete+set on every
  // write/read promotes the path to most-recently-used; oldest entry
  // is always keys().next() when we exceed the cap.
  const cacheScreen = (path, html, title) => {
    if (screenCache.has(path)) screenCache.delete(path);
    if (screenCache.size >= MAX_CACHE_SIZE) {
      const oldest = screenCache.keys().next().value;
      screenCache.delete(oldest);
    }
    screenCache.set(path, { html, title });
  };

  // Cache the initial page so back-navigation to it works instantly.
  // Route through cacheScreen() so the LRU cap is enforced uniformly.
  const initialMain = document.querySelector('[role="main"]') ?? document.querySelector('main');
  if (initialMain) {
    // Key with the search string too — every later entry (and
    // currentPath) is keyed pathname+search, and invalidate()
    // matches against that form.
    cacheScreen(location.pathname + location.search, initialMain.innerHTML, document.title);
  }


  // -----------------------------------------------------------------------
  // Client-side router
  // -----------------------------------------------------------------------
  const isInternalLink = (href) => {
    if (!href) return false;
    if (href.startsWith('http') || href.startsWith('//')) return false;
    if (href.startsWith('#') || href.startsWith('mailto:') || href.startsWith('tel:')) return false;
    return true;
  };

  // resolvePath turns any href (absolute or relative, with or without
  // query/hash) into a path+search string anchored at the current
  // location. "?p=2" → "/components/pagination?p=2", "/about" → "/about".
  const resolvePath = (href) => {
    try {
      const u = new URL(href, location.href);
      return u.pathname + u.search;
    } catch (_) { return href; }
  };

  const isKnownRoute = (href) => {
    // Resolve relative URLs (e.g. "?p=2") against the current location
    // so query-only links match their owning route.
    const clean = resolvePath(href).split('?')[0].split('#')[0];
    // Exact match.
    if (routes.has(clean)) return true;
    // Trailing-slash tolerance: a screen group registers its root
    // as "/components/" but a nav link to "/components" (no slash)
    // is semantically the same — the server redirects one to the
    // other. Match both forms so the SPA router doesn't fall through
    // to a hard reload just because the consumer wrote the link
    // without the trailing slash. loadPage will surface the server's
    // canonical form via X-Gofastr-Location if a redirect happens.
    if (clean !== '/' && !clean.endsWith('/') && routes.has(clean + '/')) return true;
    if (clean !== '/' && clean.endsWith('/') && routes.has(clean.slice(0, -1))) return true;
    // Try dynamic route patterns (e.g., /products/:slug, /docs/:path*)
    const parts = clean.split('/').filter(Boolean);
    for (const [pattern] of routes) {
      if (!pattern.includes(':')) continue;
      const pp = pattern.split('/').filter(Boolean);
      // Catch-all patterns ("*") accept >= prefix length; others exact.
      if (pattern.includes('*') ? parts.length < pp.length : pp.length !== parts.length) continue;
      // Each literal segment must align; dynamic segments (":name",
      // incl. the catch-all ":name*") match anything. Typed constraints
      // (":id:int" / ":id:uuid") are enforced server-side at resolve; a
      // non-conforming value falls through to a normal request there.
      if (pp.every((seg, i) => seg.startsWith(':') || seg === parts[i])) return true;
    }
    return false;
  };

  // -----------------------------------------------------------------------
  // Client-side navigation — fetch partial HTML, swap <main> content
  // -----------------------------------------------------------------------

  // Reading promotes the entry to most-recently-used (LRU semantics).
  const getCachedScreen = (path) => {
    const v = screenCache.get(path);
    if (v) { screenCache.delete(path); screenCache.set(path, v); }
    return v;
  };

  // In-flight dedup: if a SPA-nav to the same path is already
  // running, drop the redundant call. Matches the DataTable + search
  // dedup pattern (one click = one request).
  const _pendingNav = new Set();
  // Mini toast used by loadPage failures — strict-CSP-clean (no
  // inline styles since the .fui-nav-toast class is shipped via
  // frameworkBuiltinCSS).
  const _showNavToast = (msg) => {
    const t = doc.singleton('fui-nav-toast', () => {
      const d = document.createElement('div');
      d.className = 'fui-nav-toast';
      d.setAttribute('role', 'alert');
      return d;
    });
    t.textContent = msg;
    t.classList.add('is-visible');
    clearTimeout(t._fuiTimer);
    t._fuiTimer = setTimeout(() => t.classList.remove('is-visible'), 4000);
  };

  // scrollToHash scrolls to the element targeted by the current URL
  // fragment after a SPA swap; falls back to the top when there is no
  // fragment or no matching element. Reads location.hash (set by the
  // click handler's pushState / by the browser on popstate) so back /
  // forward and in-link fragments all land on the right section instead
  // of always jumping to the top.
  const scrollToHash = () => {
    const id = (location.hash || '').replace(/^#/, '');
    const doScroll = () => {
      if (id) {
        const el = document.getElementById(id);
        if (el) {
          el.scrollIntoView({ block: 'start' });
          return;
        }
      }
      window.scrollTo(0, 0);
    };
    doScroll();
    // Re-correct after the swapped content's layout settles — the page
    // height can still be shifting (fonts, late reflow) the instant after
    // the innerHTML swap, which would leave the target over/undershot; a
    // second pass on the next painted frame lands it precisely.
    requestAnimationFrame(() => requestAnimationFrame(doScroll));
  };

  // --- Cross-layout navigation ---
  // When the destination route's layout differs from the current page's, the
  // chrome (header/sidebar/footer) itself changes — swapping only <main> would
  // render the new screen in the wrong shell. We detect it via the route
  // manifest `layout` + the [data-fui-layout] marker the shell carries, then
  // fetch the FULL page and replace the whole shell. No hard reload (hard rule
  // 4): the chrome's interactive bits are delegated, so they survive the swap.
  const domLayout = () => {
    const el = document.querySelector('[data-fui-layout]');
    return el ? el.getAttribute('data-fui-layout') : '';
  };
  const layoutWillChange = (path) => {
    const r = routes.get(path);
    const to = (r && r.layout) || '';
    return !!to && to !== domLayout();
  };
  const swapLayoutShell = (newShellEl) => {
    const cur = document.querySelector('[data-fui-layout]');
    if (!cur || !newShellEl) return false;
    const el = document.importNode(newShellEl, true);
    cur.replaceWith(el);
    // Runtime-created body singletons live OUTSIDE [data-fui-layout],
    // so the swap normally leaves them alone — but a layout that
    // wrapped one (or a future whole-body swap) must not silently drop
    // them. Re-append any created-but-detached singleton.
    doc.reattach();
    mergeSeedFromDOM(el);
    if (window.__gofastr?.scanAndLoadCSS) window.__gofastr.scanAndLoadCSS(el);
    const main = el.querySelector('[role="main"]') || el.querySelector('main');
    if (main && main.focus) { try { main.focus({ preventScroll: true }); } catch (_) {} }
    return true;
  };

  /** Fetch page, swap <main>. Caches for instant back-nav. */
  const loadPage = async (path, { bypassCache = false } = {}) => {
    // Single gate for every branch below: the SPA navigator's target
    // comes from an href / a data-fui-* attribute / a server header,
    // and a cross-origin one must never be fetched with the page's
    // credentials and swapped into <main>. A javascript: or data: URL
    // resolves to a null origin, so this subsumes the scheme check too.
    if (!window.__gofastr._originOK(path)) return;
    // Drop redundant in-flight nav to the same URL (10 clicks → 1 fetch).
    if (_pendingNav.has(path)) return;
    _pendingNav.add(path);
    const prevPath = currentPath;
    currentPath = path;
    // Surface "I heard you" feedback to assistive tech and screen
    // readers while the fetch is in flight. The CSS hook can show a
    // progress strip via [aria-busy="true"] on documentElement.
    doc.setHtmlAttr('aria-busy', 'true');

    try {
      // bypassCache: post-mutation navigation (data-fui-rpc-navigate,
      // navigate({force:true})) must show fresh server state, never the
      // cached copy captured before the mutation.
      const cached = bypassCache ? null : getCachedScreen(path);
      // A shared [data-fui-screen-group] between the two paths proves both
      // render inside the SAME outer shell, so nav is an in-shell content
      // swap even when the manifest layout name differs (a group screen
      // reports its INNER layout, which never matches the OUTERMOST
      // [data-fui-layout] marker — #89). Computed once, gates every branch.
      const grp = findCommonScreenGroup(prevPath || currentPath, path);
      // Skip the cached content-swap when the layout changes (and no shared
      // group) — the cache holds only the <main> fragment, not the new chrome;
      // fall through to a full fetch + shell swap.
      if (cached && (grp || !layoutWillChange(path))) {
        // Title first so SR + browser-history see the new title
        // before pushState fires (the click handler does pushState).
        document.title = cached.title;
        announceRoute(cached.title);
        if (grp) {
          swapScreenGroupContent(grp, cached.html);
        } else {
          swapMainContent(cached.html);
        }
        updateActiveLink(path);
        scrollToHash();
        window.dispatchEvent(new CustomEvent('gofastr:navigate', { detail: { path, prevPath, cached: true } }));
        return;
      }

      // Cross-layout nav: fetch the FULL page (no navigate header → server
      // returns the whole shell, not just <main>) and replace the layout
      // shell. Delegated chrome handlers survive the swap — no hard reload.
      // A shared screen group means the shell is shared → never swap it.
      if (!grp && layoutWillChange(path)) {
        const fr = await fetch(path);
        if (!fr.ok) throw new Error(`HTTP ${fr.status}`);
        window.__gofastr._inval(fr);
        const doc = new DOMParser().parseFromString(await fr.text(), 'text/html');
        let dest = path;
        // resolvePath keeps the search string — the cache key and the
        // URL bar must carry a redirect-added query (e.g. ?next=/admin).
        if (fr.redirected && fr.url) dest = resolvePath(fr.url);
        if (dest !== path) { try { history.replaceState(null, '', dest); } catch (_) {} currentPath = dest; }
        const t = doc.querySelector('title')?.textContent || document.title;
        document.title = t;
        announceRoute(t);
        // The full fetch re-renders chrome under the CURRENT session —
        // if the server re-minted (restart/rotation/expiry), the fresh
        // head carries the new stream id. Copy it onto the live meta so
        // the SSE reconnect loop recovers here too, not only on the
        // partial branch's X-Gofastr-Session path.
        const fm = sseMeta(doc), lm = sseMeta();
        if (fm && lm) lm.setAttribute('content', fm.getAttribute('content'));
        const shell = doc.querySelector('[data-fui-layout]');
        const nm = doc.querySelector('main');
        if (shell && swapLayoutShell(shell)) {
          cacheScreen(dest, nm ? nm.innerHTML : '', t);
        } else {
          swapMainContent(nm ? nm.innerHTML : '');
        }
        updateActiveLink(dest);
        scrollToHash();
        window.dispatchEvent(new CustomEvent('gofastr:navigate', { detail: { path: dest, prevPath, cached: false } }));
        return;
      }

      const resp = await fetch(path, {
        headers: { 'X-Gofastr-Navigate': '1' },
      });
      // Apply a session rollover BEFORE the ok-check: the server re-mints
      // (and names the fresh stream id) on 404 / policy-block partials
      // too, and the browser has already stored the new cookie — if we
      // threw first, the meta would keep the dead id and never recover
      // (the next OK nav presents the now-valid cookie, so no header).
      const rs = resp.headers.get('X-Gofastr-Session'), rm = rs && sseMeta();
      if (rm) rm.setAttribute('content', rm.getAttribute('content').replace(/([?&]session=)[^&]*/, '$1' + rs));
      if (!resp.ok) throw new Error(`HTTP ${resp.status}`);

      // Evict server-named stale screens BEFORE chasing a redirect, so
      // a "mutated + redirected" response drops entries first and the
      // redirect target is then fetched/cached fresh.
      window.__gofastr._inval(resp);

      // X-Gofastr-Location signals "server policy redirected this
      // partial — go nav to the new URL instead of trying to swap
      // the empty body in place." Set by uihost on a Redirect policy
      // outcome. The fetch above won't see a 303 (we deliberately use
      // 200 + header to survive redirect:'follow').
      // (Session rollover already applied above, before the ok-check.)
      const redirectTo = resp.headers.get('X-Gofastr-Location');
      if (redirectTo) {
        // pushState was already called by the click handler with the
        // requested path; replace it with the redirect destination so
        // the URL bar matches what we're about to load.
        try { history.replaceState(null, '', redirectTo); } catch (_) {}
        currentPath = redirectTo;
        _pendingNav.delete(path);
        doc.removeHtmlAttr('aria-busy');
        // Keep bypassCache across the redirect: a post-mutation nav
        // must not serve the redirect target from the screen cache.
        return loadPage(redirectTo, { bypassCache });
      }

      const html = await resp.text();

      // Compute title BEFORE swapping content so document.title is
      // already correct when AT or extensions observe the new state.
      let title, body, partial = resp.headers.get('X-Gofastr-Partial') === 'true';
      if (partial) {
        title = decodeURIComponent(resp.headers.get('X-Gofastr-Title') || document.title);
        body = html;
      } else {
        const doc = new DOMParser().parseFromString(html, 'text/html');
        const nm = doc.querySelector('main');
        title = doc.querySelector('title')?.textContent || document.title;
        body = nm?.innerHTML ?? '';
      }
      document.title = title;
      announceRoute(title);
      // Screen group optimization: preserve layout shell for sibling nav
      // (grp computed once at the top of loadPage).
      if (grp) {
        swapScreenGroupContent(grp, body);
      } else {
        swapMainContent(body);
      }
      cacheScreen(path, body, title);

      updateActiveLink(path);
      scrollToHash();
      window.dispatchEvent(new CustomEvent('gofastr:navigate', { detail: { path, prevPath, cached: false } }));
    } catch (err) {
      // CLAUDE.md hard rule 4 — no location.href fallback. Surface a
      // toast and stay on the current page; URL has already been
      // pushState'd by the click handler so revert it.
      console.warn('[gofastr] Nav failed:', err);
      _showNavToast('Could not load ' + path + ' — check your connection');
      try { history.replaceState(null, '', prevPath || location.pathname); } catch (_) {}
      currentPath = prevPath;
    } finally {
      _pendingNav.delete(path);
      doc.removeHtmlAttr('aria-busy');
    }
  };

  // Announce the new page title via aria-live region so assistive
  // technology hears the route change (document.title mutations alone
  // aren't reported on most screen readers).
  let _announceTimer = 0;
  const announceRoute = (title) => {
    const r = document.getElementById('fui-route-announce');
    if (!r || !title) return;
    // Cancel any in-flight timer from a previous nav so rapid A→B→C
    // navs don't race and leave the live region on the wrong title.
    if (_announceTimer) { clearTimeout(_announceTimer); _announceTimer = 0; }
    // If the region already holds this title, do nothing — clearing
    // and re-setting would open a 50ms empty-textContent window for
    // a same-title repeat with no upside (AT already announced it).
    if (r.textContent === title) return;
    // Touch the textContent twice (clear, then set) so AT re-announces
    // when the title actually changes — defensive; cheap.
    r.textContent = '';
    _announceTimer = setTimeout(() => {
      r.textContent = title;
      _announceTimer = 0;
    }, 50);
  };

  // mergeSeedFromDOM applies a partial (SPA-nav) signal seed embedded in
  // freshly-swapped content (#gofastr-signals-partial). Page-scoped names
  // (data.p) are applied unconditionally — the destination page's fresh
  // state. Globals (data.g) are seeded only when first seen, so a value
  // the user already mutated (cart count) survives navigation.
  const mergeSeedFromDOM = (root) => {
    if (!root || !root.querySelector) return;
    const el = root.querySelector('#gofastr-signals-partial');
    if (!el) return;
    let data = null;
    try { data = JSON.parse(el.textContent || 'null'); } catch (_) { /* ignore */ }
    el.remove();
    if (!data) return;
    const store = window.__gofastr && window.__gofastr._signals;
    if (!store) return;
    const page = data.p || {};
    for (const k in page) {
      if (!Object.prototype.hasOwnProperty.call(page, k)) continue;
      if (isReservedSignalKey(k)) continue;
      if (store[k]) store[k].value = page[k];
      else store[k] = { value: page[k], listeners: [] };
    }
    const glob = data.g || {};
    for (const k in glob) {
      if (!Object.prototype.hasOwnProperty.call(glob, k)) continue;
      if (isReservedSignalKey(k)) continue;
      if (!store[k]) store[k] = { value: glob[k], listeners: [] };
    }
  };

  const swapMainContent = (html) => {
    const main = document.querySelector('[role="main"]') ?? document.querySelector('main');
    if (main) {
      main.innerHTML = html;
      mergeSeedFromDOM(main);
      if (window.__gofastr?.scanAndLoadCSS) window.__gofastr.scanAndLoadCSS(main);
    }
    // Close any open dismissible disclosure (e.g. mobile nav hamburger)
    // so it doesn't float over the destination page. Opt-in via
    // <details data-fui-disclosure>.
    for (const d of document.querySelectorAll('details[data-fui-disclosure][open]')) {
      d.removeAttribute('open');
    }
    // Move focus into the new <main> so keyboard users land on the
    // fresh content rather than being stranded on a now-detached node.
    // Relies on the tabindex="-1" set by html.Main().
    if (main && typeof main.focus === 'function') {
      try { main.focus({ preventScroll: true }); } catch (_) { /* older Safari */ }
    }
  };

  // --- Screen group awareness ---
  // When navigating between siblings inside the same data-fui-screen-group,
  // only swap the group's inner <main> content, preserving the layout shell.
  const findCommonScreenGroup = (fromPath, toPath) => {
    const groups = document.querySelectorAll('[data-fui-screen-group]');
    // Pick the DEEPEST matching group — for nested screen groups the
    // inner group's layout shell is what should survive sibling-nav,
    // not the outer one. We compare by prefix length: longer prefix
    // → more specific → wins.
    // Match with a trailing slash appended so a slashless index path
    // ("/studio") still counts as inside its group (prefix "/studio/") —
    // otherwise the group index's first sibling nav misses the swap (#89).
    let best = null, bestLen = -1;
    for (const g of groups) {
      const pre = g.getAttribute('data-fui-screen-group');
      if (pre && (fromPath + '/').startsWith(pre) && (toPath + '/').startsWith(pre) && pre.length > bestLen) {
        best = g;
        bestLen = pre.length;
      }
    }
    return best;
  };

  const swapScreenGroupContent = (groupEl, html) => {
    // The content cell inside a ScreenGroup layout can be:
    //   1. .layout-content (nested layout — sidebar + content)
    //   2. <main> or [role="main"] (outermost layout)
    // The nested case is the common one: the ScreenGroup wrapper holds
    // a layout-body with sidebar + content. We must swap only the
    // content cell, not the sidebar.
    const target = groupEl.querySelector('.layout-content')
      ?? groupEl.querySelector('[role="main"]')
      ?? groupEl.querySelector('main');
    if (!target) return;

    // When the HTML comes from the SPA cache (seeded at boot from the
    // outer <main>.innerHTML), it contains the FULL screen-group
    // structure (sidebar + content). Extract just the inner content
    // cell so we don't nest the layout inside itself.
    let swapHTML = html;
    const parsed = new DOMParser().parseFromString(html, 'text/html');
    const innerLC = parsed.body && parsed.body.querySelector('.layout-content');
    if (innerLC) {
      swapHTML = innerLC.innerHTML;
    }

    target.innerHTML = swapHTML;
    mergeSeedFromDOM(target);
    if (window.__gofastr?.scanAndLoadCSS) window.__gofastr.scanAndLoadCSS(target);

    // Close disclosures inside the group
    for (const d of groupEl.querySelectorAll('details[data-fui-disclosure][open]')) {
      d.removeAttribute('open');
    }
  };

  // Links with an exact-href match get aria-current=page. A link can
  // opt in to prefix matching via data-fui-match-prefix — useful for
  // primary nav entries like "Components" (href="/components/") that
  // should light up on /components/accordion, /components/card, etc.
  // Prefix matching is OFF by default so breadcrumbs and sidebars (where
  // multiple links share a path prefix) keep their server-rendered
  // single aria-current. Non-matching links get aria-current cleared.
  // Links with NO href (server-rendered MatchPath items in a sidebar
  // where the active determination is prefix-based) are left untouched
  // — only the server has the prefix-match context for those.
  const updateActiveLink = (path) => {
    const navLinks = document.querySelectorAll('nav a');
    for (const link of navLinks) {
      const href = link.getAttribute('href');
      if (!href) continue; // server-managed (MatchPath, dynamic), hands off
      let active = href === path;
      if (!active && link.hasAttribute('data-fui-match-prefix')) {
        const hrefPath = href.split('?')[0].split('#')[0];
        const pathOnly = (path || '').split('?')[0].split('#')[0];
        // "/" is never used as a prefix — otherwise every nav link
        // would match every page.
        if (hrefPath !== '/' && hrefPath.endsWith('/') && pathOnly.startsWith(hrefPath)) {
          active = true;
        }
      }
      if (active) {
        link.setAttribute('aria-current', 'page');
        link.classList.add('active');
      } else {
        link.removeAttribute('aria-current');
        link.classList.remove('active');
      }
    }
  };

  // Link clicks: cross-page navigation (/a → /b) is intercepted and
  // handled client-side via partial fetch + cache. No hard refresh.
  // This is the Angular-router-style behavior described in
  // core-ui/ARCHITECTURE.md ("Page → page navigation"). In-page state
  // changes are NOT routes — they go through data-fui-rpc on islands
  // and never hit this handler.
  //
  // Cmd/Ctrl/Shift/Alt-click, target=_blank, external links, and
  // unknown routes fall through to default browser navigation.
  document.addEventListener('click', (e) => {
    const anchor = e.target.closest('a[href]');
    if (!anchor) return;
    const href = anchor.getAttribute('href');
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    if (!isInternalLink(href)) return;
    // Skip downloads — <a download> needs the native click to trigger
    // the save dialog; intercepting fetches the bytes silently into
    // the SPA and the file never reaches the user.
    if (anchor.hasAttribute('download')) return;
    // Skip any non-_self target (covers _blank, _top, _parent, named
    // frames). Previously only _blank was checked, so <a target="_top">
    // inside an iframe got hijacked instead of breaking out.
    if (anchor.target && anchor.target !== '' && anchor.target !== '_self') return;
    if (!isKnownRoute(href)) return;
    // data-fui-rpc anchors are RPC triggers, not navigation.
    if (anchor.hasAttribute('data-fui-rpc')) return;

    const fullPath = resolvePath(href);
    if (fullPath === currentPath) {
      // Already there — let the browser handle the click (focus, scroll, etc.).
      return;
    }
    e.preventDefault();
    // Eagerly close an enclosing dismissible disclosure (mobile nav
    // hamburger). Without this, the menu floats over stale content
    // for the entire SPA fetch duration — the user perceives the
    // click as "didn't take".
    anchor.closest('details[data-fui-disclosure]')?.removeAttribute('open');
    // Preserve the #fragment: resolvePath strips it (path-only is what
    // route matching + cache keys want), but the URL bar and the
    // post-nav scroll target need it. loadPage reads location.hash, so
    // pushState must carry the fragment.
    let navHash = '';
    try { navHash = new URL(href, location.href).hash; } catch (_) { /* malformed href */ }
    // An intercepting route presents as an overlay when reached from its
    // declared origin. The module owns the URL and the fetch in that
    // case; returning true means it took the navigation.
    if (window.__gofastr._intercept && window.__gofastr._intercept(fullPath, navHash)) return;
    history.pushState(null, '', fullPath + navHash);
    loadPage(fullPath);
  });

  // popstate: a URL change via back/forward triggers a screen-partial
  // re-fetch (cache makes it instant). This covers both cross-page
  // navigations AND in-page state changes pushed via X-Gofastr-Push-State.
  window.addEventListener('popstate', () => {
    const path = location.pathname + location.search;
    if (path !== currentPath && currentPath !== '') {
      loadPage(path);
    }
    // Widget deep links ride the same event: a query-only change means
    // a modal/drawer should open or close. Deferred a tick so it runs
    // after any screen swap loadPage just started.
    setTimeout(() => {
      const G = window.__gofastr;
      if (G && typeof G._syncDeepLinks === 'function') G._syncDeepLinks();
    }, 0);
  });

  // nav namespace member. navigate() is the choke point for all programmatic
  // SPA navigation (scheme guard -> pushState -> loadPage). _originOK is read
  // via `this`, so it resolves at call time regardless of composition order.
  Object.assign(window.__gofastr, {
    // --- Router API ---

    /** Programmatically navigate to a path. force re-fetches even when
        the path is the current page and bypasses the screen cache —
        use it after a mutation so the destination reflects new state. */
    navigate(path, { replace = false, force = false } = {}) {
      if (path === currentPath && !force) return;
      // Security: reject attacker-controllable schemes BEFORE
      // touching the URL bar. Server-rendered data-fui-push-state
      // attributes (e.g. on a combobox option) and signal-bound
      // hrefs are the trust boundary; navigate() is the choke point
      // for all programmatic SPA navigation, so the guard lives
      // here. Reuses the same gate as Lightbox AllowDownload etc.
      if (!this._originOK(path)) return;
      if (replace || path === currentPath) {
        history.replaceState(null, '', path);
      } else {
        history.pushState(null, '', path);
      }
      loadPage(path, { bypassCache: force });
    },

    /** Drop cached screens so the next visit re-fetches. Selectors:
        "/orders" drops that pathname AND every cached query variant;
        "/orders?page=2" drops exactly that entry; "*" clears all.
        Root-relative paths only — anything else is ignored. Never
        touches the live DOM; pair with refresh()/navigate(force) when
        the current screen must re-render too. */
    invalidate(...sels) {
      for (const s of sels) {
        if (s === '*') { screenCache.clear(); return; }
        if (!s || s[0] !== '/') continue;
        if (s.includes('?')) { screenCache.delete(s); continue; }
        // Queryless selector: evict the pathname and all its query
        // variants (a stale list is stale on every page/filter of it).
        for (const k of screenCache.keys()) if (k === s || k.startsWith(s + '?')) screenCache.delete(k);
      }
    },

    /** Re-fetch and re-render the current screen from the server,
        bypassing the cache. Goes straight to loadPage — history is not
        touched, so a #fragment on the URL survives. */
    refresh() { loadPage(currentPath, { bypassCache: true }); },

    // X-Gofastr-Invalidate consumer — takes the whole Response (keeps
    // the header literal in one module; the callers in rpc/widgets/
    // intercept stay a few bytes). The value is a JSON string array of
    // selectors, applied on 2xx by nav/RPC/widget/intercept fetches.
    // A malformed value is a producer bug (ui.InvalidateScreens always
    // emits a valid array) and must never break the response that
    // carried it — ignore it. The Array.isArray gate matters: spreading
    // a parsed bare string would evict per-character.
    _inval(r) {
      try {
        const a = JSON.parse(r.headers.get('X-Gofastr-Invalidate'));
        if (Array.isArray(a)) this.invalidate(...a);
      } catch (_) {}
    },
  });
