// nav.js: SPA router (spec fragment `nav`, boot class; deps: kernel+signals).
// Owns: <a> click hijack, history.pushState, popstate, screen cache,
// layout-chain-aware swaps (deepest shared layer), updateActiveLink,
// document.title writes, the navigate() namespace member.

  // -----------------------------------------------------------------------
  // Screen cache: stores rendered screens for instant back-navigation.
  // Each entry records the layout LAYER its html renders below ('' = a
  // layout-less page's whole <main>), so replaying it swaps exactly the
  // right content cell, never a class-name guess, never an unwrap pass.
  // -----------------------------------------------------------------------
  const screenCache = new Map(); // path → { html, title, layer }
  // sseMeta reads the live stream-id carrier from a document (default:
  // the live one). The SSE module re-reads it on every reconnect, so
  // pointing it at a fresh session id is the whole recovery contract.
  const sseMeta = (d) => (d || document).querySelector('meta[name="gofastr-sse"]');
  const MAX_CACHE_SIZE = 20;

  // True LRU: Map preserves insertion order, so delete+set on every
  // write/read promotes the path to most-recently-used; oldest entry
  // is always keys().next() when we exceed the cap.
  const cacheScreen = (path, html, title, layer) => {
    if (screenCache.has(path)) screenCache.delete(path);
    if (screenCache.size >= MAX_CACHE_SIZE) {
      const oldest = screenCache.keys().next().value;
      screenCache.delete(oldest);
    }
    screenCache.set(path, { html, title, layer: layer || '' });
  };

  // --- Layout chain primitives ---
  // The server marks every layout layer with data-fui-layout-key (its
  // identity) and the layer's content cell with data-fui-layout-slot (the
  // swap target). The route manifest carries each route's chain in
  // `layouts` (outermost → innermost). Document order of the key-marked
  // elements IS the chain order, a wrapper precedes its descendants.
  const domChainKeys = () => {
    const out = [];
    for (const el of document.querySelectorAll('[data-fui-layout-key]')) {
      out.push(el.getAttribute('data-fui-layout-key'));
    }
    return out;
  };
  // Attribute-compare in a loop instead of an attribute selector: keys
  // contain '/' and ':', and getAttribute needs no escaping.
  const findSlot = (key) => {
    if (!key) return null;
    for (const el of document.querySelectorAll('[data-fui-layout-slot]')) {
      if (el.getAttribute('data-fui-layout-slot') === key) return el;
    }
    return null;
  };
  const mainEl = () => document.querySelector('[role="main"]') ?? document.querySelector('main');
  const mainSlotKey = () => {
    const m = mainEl();
    return (m && m.getAttribute('data-fui-layout-slot')) || '';
  };
  // routeEntry: manifest lookup with trailing-slash tolerance and dynamic
  // patterns. Pattern awareness matters for chains, a concrete URL of a
  // "/:param" route must resolve to that route's chain, not to "no
  // layouts" (which read as cross-chain and forced a shell rebuild on
  // every dynamic-route nav).
  const routeEntry = (path) => {
    const clean = path.split('?')[0].split('#')[0];
    let r = routes.get(clean);
    // A screen group registers its root as "/components/" but links may
    // write "/components", the server redirects between them, so the
    // SPA router must accept both forms.
    if (!r && clean !== '/' && !clean.endsWith('/')) r = routes.get(clean + '/');
    if (!r && clean !== '/' && clean.endsWith('/')) r = routes.get(clean.slice(0, -1));
    if (r) return r;
    const parts = clean.split('/').filter(Boolean);
    for (const [pattern, entry] of routes) {
      if (!pattern.includes(':')) continue;
      const pp = pattern.split('/').filter(Boolean);
      // Catch-all patterns ("*") accept >= prefix length; others exact.
      if (pattern.includes('*') ? parts.length < pp.length : pp.length !== parts.length) continue;
      // Each literal segment must align; dynamic segments (":name",
      // incl. the catch-all ":name*") match anything. Typed constraints
      // (":id:int" / ":id:uuid") are enforced server-side at resolve; a
      // non-conforming value falls through to a normal request there.
      if (pp.every((seg, i) => seg.startsWith(':') || seg === parts[i])) return entry;
    }
    return null;
  };
  const routeLayouts = (path) => {
    const r = routeEntry(path);
    return (r && r.layouts) || [];
  };
  // Count of outermost layers the live DOM shares with the target chain.
  // An empty key never matches (unnamed layers have no stable identity).
  const sharedDepth = (layouts) => {
    const dom = domChainKeys();
    let d = 0;
    while (d < dom.length && d < layouts.length && layouts[d] && dom[d] === layouts[d]) d++;
    return d;
  };

  // Cache the initial page so back-navigation to it works instantly.
  // Route through cacheScreen() so the LRU cap is enforced uniformly.
  const initialMain = mainEl();
  if (initialMain) {
    // Key with the search string too, every later entry (and
    // currentPath) is keyed pathname+search, and invalidate()
    // matches against that form. The layer is <main>'s own slot key:
    // the boot html spans everything below layer 0.
    cacheScreen(location.pathname + location.search, initialMain.innerHTML, document.title, mainSlotKey());
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

  // Resolve relative URLs (e.g. "?p=2") against the current location so
  // query-only links match their owning route.
  const isKnownRoute = (href) => !!routeEntry(resolvePath(href));

  // -----------------------------------------------------------------------
  // Client-side navigation, fetch partial HTML, swap at the deepest
  // layer the current DOM shares with the target route's layout chain.
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
  // Monotonic nav generation. Captured at the start of each loadPage
  // call; after any await, a response whose epoch is no longer the
  // latest MUST NOT touch the DOM, currentPath, or history. Without
  // this, a rapid A→B where A's fetch resolves last swaps <main> back
  // to A's content while the URL bar already says B, and a repeat
  // click on B no-ops (fullPath === currentPath), stranding the user.
  let _navEpoch = 0;
  // Mini toast used by loadPage failures, strict-CSP-clean (no
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

  // _settleScroll runs a scroll write now and once more after the swapped
  // content's layout settles (fonts, late reflow shift the page height the
  // instant after an innerHTML swap). Two guards on the second pass:
  //   - the seq drops a superseded navigation's queued write (a rapid
  //     back-then-forward let the BACK nav's settle scroll-to-top land
  //     AFTER the forward nav had restored its position);
  //   - the position check skips the re-correct when ANYTHING scrolled
  //     since the first write, rAF can be throttled far past the swap
  //     (background tabs, loaded CI runners), and a late settle pass was
  //     yanking a user who had already started scrolling back to where
  //     the navigation landed.
  let _scrollSeq = 0;
  const _settleScroll = (fn) => {
    const seq = ++_scrollSeq;
    fn();
    const x0 = scrollX | 0, y0 = scrollY | 0;
    requestAnimationFrame(() => requestAnimationFrame(() => {
      if (seq === _scrollSeq && (scrollX | 0) === x0 && (scrollY | 0) === y0) fn();
    }));
  };

  // scrollToHash scrolls to the element targeted by the current URL
  // fragment after a SPA swap; falls back to the top when there is no
  // fragment or no matching element. Reads location.hash (set by the
  // click handler's pushState / by the browser on popstate) so back /
  // forward and in-link fragments all land on the right section instead
  // of always jumping to the top.
  const scrollToHash = () => {
    const id = (location.hash || '').replace(/^#/, '');
    _settleScroll(() => {
      if (id) {
        const el = document.getElementById(id);
        if (el) {
          el.scrollIntoView({ block: 'start' });
          return;
        }
      }
      window.scrollTo(0, 0);
    });
  };

  // --- History entry identity + scroll restoration ---
  // Every history entry the runtime writes carries {__fui: <id>} so
  // back/forward can restore the exact scroll position it left.
  // scrollRestoration is manual: the browser's own restore fires before
  // the swapped content exists and clamps to the wrong page's height.
  try { history.scrollRestoration = 'manual'; } catch (_) {}
  const SCROLL_KEY = 'gofastr:scroll';
  // In-memory map id → [x,y], seeded from sessionStorage so a reload
  // keeps its position despite manual restoration; persisted with a
  // trailing throttle (in-memory writes are per scroll event, cheap).
  const _scrollStore = (() => {
    try { return JSON.parse(sessionStorage.getItem(SCROLL_KEY)) || {}; } catch (_) { return {}; }
  })();
  let _entrySeq = 0;
  for (const k in _scrollStore) { const n = +k; if (n > _entrySeq) _entrySeq = n; }
  let _entryId = (history.state && history.state.__fui) || 0;
  if (!_entryId) {
    _entryId = ++_entrySeq;
    try { history.replaceState({ __fui: _entryId }, '', location.href); } catch (_) {}
  } else if (_entryId > _entrySeq) { _entrySeq = _entryId; }
  let _scrollTimer = 0;
  const _persistScroll = () => {
    const ids = Object.keys(_scrollStore);
    // Cap the persisted map; the oldest ids are the least likely to be
    // reachable via Back.
    if (ids.length > 50) {
      ids.sort((a, b) => a - b);
      for (const id of ids.slice(0, ids.length - 50)) delete _scrollStore[id];
    }
    try { sessionStorage.setItem(SCROLL_KEY, JSON.stringify(_scrollStore)); } catch (_) {}
  };
  window.addEventListener('scroll', () => {
    _scrollStore[_entryId] = [scrollX | 0, scrollY | 0];
    if (_scrollTimer) return;
    _scrollTimer = setTimeout(() => { _scrollTimer = 0; _persistScroll(); }, 150);
  }, { passive: true });
  // Reload restore: manual scrollRestoration leaves a reloaded page at
  // the top; the stored position for this entry id puts it back. A URL
  // fragment wins, the browser's native hash scroll is already right.
  if (!location.hash && _scrollStore[_entryId]) {
    const p0 = _scrollStore[_entryId];
    requestAnimationFrame(() => window.scrollTo(p0[0], p0[1]));
  }
  // Scroll target for a popstate-initiated nav in flight; consumed by
  // finishNav INSTEAD of scrollToHash so Back/Forward land where the
  // user left, not at the top.
  let _pendingScroll = null;

  // Single choke point for every history write in the runtime (the click
  // hijack, navigate(), RPC X-Gofastr-Push-State, widget/pane deep
  // links, intercepts). Assigns the entry id for scroll restoration,
  // moves the URL, and keeps currentPath in sync, modules that
  // pushState'd around the router left currentPath stale, so the next
  // popstate mis-diffed the URL change.
  const _pushURL = (url, { replace = false } = {}) => {
    // Capture the outgoing entry's position synchronously, the scroll
    // listener is event-driven and can lag the traversal on slow
    // renderers; this is the last moment the old id is current.
    if (!replace) _scrollStore[_entryId] = [scrollX | 0, scrollY | 0];
    const id = replace ? _entryId : ++_entrySeq;
    try { history[replace ? 'replaceState' : 'pushState']({ __fui: id }, '', url); } catch (_) { return; }
    _entryId = id;
    currentPath = location.pathname + location.search;
  };

  // Close ordinary disclosures inside scope so they do not float over the
  // destination content. Persistent shell controls opt out explicitly.
  const closeDisclosures = (scope) => {
    for (const d of scope.querySelectorAll('details[data-fui-disclosure][open]:not([data-fui-disclosure-persist])')) {
      d.removeAttribute('open');
    }
  };

  // swapAtSlot replaces one layer's content cell. Scope rule: swapping
  // the outermost cell (or a layout-less <main>) closes disclosures
  // document-wide, the user left the page, the hamburger must not float
  // over the new one. A deeper swap closes only within its own layer so
  // outer shell state (an open sidebar section) survives sibling nav.
  const swapAtSlot = (slot, html, outermost) => {
    slot.innerHTML = html;
    mergeSeedFromDOM(slot);
    if (window.__gofastr?.scanAndLoadCSS) window.__gofastr.scanAndLoadCSS(slot);
    closeDisclosures(outermost ? document : (slot.parentElement || slot));
    // Move focus onto the fresh content so keyboard users are not
    // stranded on a detached node. Cells carry tabindex="-1".
    if (typeof slot.focus === 'function') {
      try { slot.focus({ preventScroll: true }); } catch (_) { /* older Safari */ }
    }
    return slot;
  };

  // swapShell replaces the whole layout shell (cross-chain navigation: no
  // shared root). newRoot is the destination page's outermost shell, or
  // its bare <main> when the destination has no layouts, so leaving a
  // chain for a plain page drops the old chrome instead of keeping it.
  // Delegated chrome handlers survive the swap; no hard reload (hard
  // rule 4).
  const shellEl = (d) => (d || document).querySelector('[data-fui-layout-key], [data-fui-screen-group]');
  const swapShell = (newRoot) => {
    const cur = shellEl() || mainEl();
    if (!cur || !newRoot) return null;
    const el = document.importNode(newRoot, true);
    cur.replaceWith(el);
    // Runtime-created body singletons live OUTSIDE the shell, so the
    // swap normally leaves them alone, but a layout that wrapped one
    // (or a future whole-body swap) must not silently drop them.
    doc.reattach();
    mergeSeedFromDOM(el);
    if (window.__gofastr?.scanAndLoadCSS) window.__gofastr.scanAndLoadCSS(el);
    const m = el.matches('main, [role="main"]') ? el : (el.querySelector('[role="main"]') || el.querySelector('main'));
    if (m && m.focus) { try { m.focus({ preventScroll: true }); } catch (_) {} }
    return el;
  };

  // Shared tail of every successful swap. root is the swapped element,
  // exposed on the navigate event so teardown-aware modules can scope
  // cleanup to the region that actually changed. Active-link
  // highlighting rides the gofastr:navigate event (src/activelink.js,
  // idle-loaded), cosmetic post-nav work carved out of core.
  // ps is the restore target BOUND TO THIS NAVIGATION, loadPage
  // consumes the popstate's pending value at entry, so a superseded
  // back/forward can never leak its position into a later click's
  // finishNav.
  const finishNav = (path, prevPath, cached, root, ps) => {
    if (!ps) scrollToHash();
    window.dispatchEvent(new CustomEvent('gofastr:navigate', { detail: { path, prevPath, cached, root } }));
    if (ps) {
      // Restore AFTER the navigate listeners ran: overlay scroll-lock
      // releases (panes, modals) fire on that event and would clamp the
      // restored position. Same guarded settle pass as scrollToHash, so
      // a newer navigation's write always wins.
      _settleScroll(() => window.scrollTo(ps[0], ps[1]));
    }
  };

  /** Fetch page, swap at the deepest shared layer. Caches for instant back-nav.
      from names the origin route when the caller already moved the URL,
      _pushURL syncs currentPath BEFORE loadPage runs on the click path,
      so capturing currentPath here would report the destination as its
      own origin (and X-Gofastr-From would stop naming the real one). */
  const loadPage = async (path, { bypassCache = false, forceFull = false, from = null, restore = null } = {}) => {
    // Single gate for every branch below: the SPA navigator's target
    // comes from an href / a data-fui-* attribute / a server header,
    // and a cross-origin one must never be fetched with the page's
    // credentials and swapped into the DOM. A javascript: or data: URL
    // resolves to a null origin, so this subsumes the scheme check too.
    if (!window.__gofastr._originOK(path)) return;
    // Dedup in-flight nav (10 clicks → 1 fetch), but only while path is
    // still the destination: on A→B→A the pending A fetch holds a stale
    // epoch and is dropped, so returning here left the URL at /a showing B.
    if (_pendingNav.has(path) && currentPath === path) return;
    _pendingNav.add(path);
    const myEpoch = ++_navEpoch;
    const prevPath = from || currentPath;
    currentPath = path;
    // Consume the popstate's restore target NOW, into this navigation,
    // left in module state, a superseded back/forward's position leaked
    // into whichever navigation ran finishNav next.
    let ps = restore;
    if (!ps) { ps = _pendingScroll; _pendingScroll = null; }
    // Surface "I heard you" feedback to assistive tech and screen
    // readers while the fetch is in flight. The CSS hook can show a
    // progress strip via [aria-busy="true"] on documentElement.
    doc.setHtmlAttr('aria-busy', 'true');

    try {
      const layouts = routeLayouts(path);
      // bypassCache: post-mutation navigation (data-fui-rpc-navigate,
      // navigate({force:true})) must show fresh server state, never the
      // cached copy captured before the mutation.
      const cached = (bypassCache || forceFull) ? null : getCachedScreen(path);
      // A cached entry is replayable iff the layer it renders below is
      // live in the DOM right now ('' = both pages are layout-less).
      if (cached) {
        const slot = cached.layer
          ? findSlot(cached.layer)
          : ((layouts.length === 0 && domChainKeys().length === 0) ? mainEl() : null);
        if (slot) {
          // Title first so SR + browser-history see the new title
          // before pushState fires (the click handler does pushState).
          document.title = cached.title;
          announceRoute(cached.title);
          const root = swapAtSlot(slot, cached.html, !cached.layer || cached.layer === domChainKeys()[0]);
          finishNav(path, prevPath, true, root, ps);
          return;
        }
      }

      // Prefetched entry (preload module): same shape as a fetched
      // partial, swap it in when its layer is live, exactly like the
      // screen cache but single-use and TTL-bounded. Post-mutation navs
      // (bypassCache) skip it: it was fetched before the mutation.
      if (!cached && !bypassCache && !forceFull && window.__gofastr._takePrefetched) {
        const pf = window.__gofastr._takePrefetched(path);
        if (pf) {
          const slot = pf.layer ? findSlot(pf.layer) : ((layouts.length === 0) ? mainEl() : null);
          if (slot) {
            document.title = pf.title;
            announceRoute(pf.title);
            const root = swapAtSlot(slot, pf.html, !pf.layer || pf.layer === domChainKeys()[0]);
            cacheScreen(path, pf.html, pf.title, pf.layer);
            finishNav(path, prevPath, false, root, ps);
            return;
          }
        }
      }

      // Cross-chain nav (no shared root): fetch the FULL page (no
      // navigate header → the server returns the whole document) and
      // replace the shell. Taken when EITHER side has a chain the other
      // doesn't share, including a chained page leaving for a
      // layout-less one, where a partial swap would keep the old chrome
      // around the new content. forceFull is the deploy-skew recovery
      // path, the server echoed a swap boundary this DOM doesn't have.
      if ((layouts.length > 0 || domChainKeys().length > 0) && (forceFull || sharedDepth(layouts) === 0)) {
        const fr = await fetch(path);
      if (myEpoch !== _navEpoch) return;
        if (!fr.ok) throw new Error(`HTTP ${fr.status}`);
        window.__gofastr._inval(fr);
        const pdoc = new DOMParser().parseFromString(await fr.text(), 'text/html');
      if (myEpoch !== _navEpoch) return;
        let dest = path;
        // resolvePath keeps the search string, the cache key and the
        // URL bar must carry a redirect-added query (e.g. ?next=/admin).
        if (fr.redirected && fr.url) dest = resolvePath(fr.url);
        if (dest !== path) { _pushURL(dest, { replace: true }); currentPath = dest; }
        const t = pdoc.querySelector('title')?.textContent || document.title;
        document.title = t;
        announceRoute(t);
        // The full fetch re-renders chrome under the CURRENT session,
        // if the server re-minted (restart/rotation/expiry), the fresh
        // head carries the new stream id. Copy it onto the live meta so
        // the SSE reconnect loop recovers here too, not only on the
        // partial branch's X-Gofastr-Session path.
        const fm = sseMeta(pdoc), lm = sseMeta();
        if (fm && lm) lm.setAttribute('content', fm.getAttribute('content'));
        const nm = pdoc.querySelector('main');
        const el = swapShell(shellEl(pdoc) || nm);
        if (!el) {
          // No live shell to replace (layout-less origin), fall back to
          // a whole-main swap; chain markers arrive with the content.
          const m = mainEl();
          if (m) swapAtSlot(m, nm ? nm.innerHTML : '', true);
        }
        cacheScreen(dest, nm ? nm.innerHTML : '', t, nm ? (nm.getAttribute('data-fui-layout-slot') || '') : '');
        finishNav(dest, prevPath, false, el || mainEl(), ps);
        return;
      }

      // Partial fetch. X-Gofastr-From names the origin route so the
      // server renders only the layers the two routes do NOT share and
      // echoes the swap boundary in X-Gofastr-Swap.
      const hdrs = { 'X-Gofastr-Navigate': '1' };
      const fromPath = (prevPath || '').split('?')[0];
      if (fromPath && routeEntry(fromPath)) hdrs['X-Gofastr-From'] = fromPath;
      const resp = await fetch(path, { headers: hdrs });
      if (myEpoch !== _navEpoch) return;
      // Apply a session rollover BEFORE the ok-check: the server re-mints
      // (and names the fresh stream id) on 404 / policy-block partials
      // too, and the browser has already stored the new cookie, if we
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
      // partial, go nav to the new URL instead of trying to swap
      // the empty body in place." Set by uihost on a Redirect policy
      // outcome. The fetch above won't see a 303 (we deliberately use
      // 200 + header to survive redirect:'follow').
      // (Session rollover already applied above, before the ok-check.)
      const redirectTo = resp.headers.get('X-Gofastr-Location');
      if (redirectTo) {
        // pushState was already called by the click handler with the
        // requested path; replace it with the redirect destination so
        // the URL bar matches what we're about to load.
        _pushURL(redirectTo, { replace: true });
        currentPath = redirectTo;
        _pendingNav.delete(path);
        doc.removeHtmlAttr('aria-busy');
        // Keep bypassCache across the redirect: a post-mutation nav
        // must not serve the redirect target from the screen cache,
        // and keep the ORIGINAL origin so the redirect leg's subtree
        // partial renders against where the user actually came from.
        return loadPage(redirectTo, { bypassCache, from: prevPath, restore: ps });
      }

      const html = await resp.text();
      if (myEpoch !== _navEpoch) return;

      // Compute title BEFORE swapping content so document.title is
      // already correct when AT or extensions observe the new state.
      let title, body, partial = resp.headers.get('X-Gofastr-Partial') === 'true';
      let swapKey = (partial && resp.headers.get('X-Gofastr-Swap')) || '';
      if (partial) {
        title = decodeURIComponent(resp.headers.get('X-Gofastr-Title') || document.title);
        body = html;
      } else {
        const pdoc = new DOMParser().parseFromString(html, 'text/html');
        const nm = pdoc.querySelector('main');
        title = pdoc.querySelector('title')?.textContent || document.title;
        body = nm?.innerHTML ?? '';
        swapKey = nm?.getAttribute('data-fui-layout-slot') || '';
      }
      // The swap boundary must be live in the DOM; a miss means the
      // manifest and server disagree (deploy skew), recover with a
      // full-page load of the destination rather than a wrong-cell swap.
      const slot = swapKey ? findSlot(swapKey) : ((layouts.length === 0) ? mainEl() : null);
      if (!slot) {
        _pendingNav.delete(path);
        return loadPage(path, { bypassCache: true, forceFull: true, from: prevPath, restore: ps });
      }
      document.title = title;
      announceRoute(title);
      const root = swapAtSlot(slot, body, !swapKey || swapKey === domChainKeys()[0]);
      cacheScreen(path, body, title, swapKey);
      finishNav(path, prevPath, false, root, ps);
    } catch (err) {
      if (myEpoch !== _navEpoch) return;
      // CLAUDE.md hard rule 4, no location.href fallback. Surface a
      // toast and stay on the current page; URL has already been
      // pushState'd by the click handler so revert it.
      console.warn('[gofastr] Nav failed:', err);
      _showNavToast('Could not load ' + path + ' — check your connection');
      _pushURL(prevPath || location.pathname, { replace: true });
      currentPath = prevPath;
    } finally {
      _pendingNav.delete(path);
      // Only the latest nav owns the aria-busy flag; a superseded nav
      // that bailed early must leave it for the in-flight one to clear.
      if (myEpoch === _navEpoch) doc.removeHtmlAttr('aria-busy');
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
    // If the region already holds this title, do nothing, clearing
    // and re-setting would open a 50ms empty-textContent window for
    // a same-title repeat with no upside (AT already announced it).
    if (r.textContent === title) return;
    // Touch the textContent twice (clear, then set) so AT re-announces
    // when the title actually changes, defensive; cheap.
    r.textContent = '';
    _announceTimer = setTimeout(() => {
      r.textContent = title;
      _announceTimer = 0;
    }, 50);
  };

  // mergeSeedFromDOM applies a partial (SPA-nav) signal seed embedded in
  // freshly-swapped content (#gofastr-signals-partial). Page-scoped names
  // (data.p) are applied unconditionally, the destination page's fresh
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

  // Link clicks: cross-page navigation (/a → /b) is intercepted and
  // handled client-side via partial fetch + cache. No hard refresh.
  // This is the Angular-router-style behavior described in
  // core-ui/ARCHITECTURE.md ("Page → page navigation"). In-page state
  // changes are NOT routes, they go through data-fui-rpc on islands
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
    // Skip downloads, <a download> needs the native click to trigger
    // the save dialog; intercepting fetches the bytes silently into
    // the SPA and the file never reaches the user.
    if (anchor.hasAttribute('download')) return;
    // Skip any non-_self target (covers _blank, _top, _parent, named
    // frames). Previously only _blank was checked, so <a target="_top">
    // inside an iframe got hijacked instead of breaking out.
    // Lowercased because HTML matches the underscore keywords
    // ASCII-case-insensitively: target="_SELF" IS _self, and comparing
    // raw sent it down the skip path, so a link that should have been a
    // soft navigation became a full page load.
    //
    // String() first because this is not always an HTMLAnchorElement.
    // An SVG <a href> matches a[href] and closest() reaches it, and
    // SVGAElement.target is an SVGAnimatedString, not a string: calling
    // .toLowerCase() on it throws, and a throw here kills the whole
    // delegated click handler. Stringifying keeps the old outcome for
    // those (it cannot equal '_self', so the click falls through to the
    // browser) instead of turning a rare element into a broken page.
    if (anchor.target && String(anchor.target).toLowerCase() !== '_self') return;
    if (!isKnownRoute(href)) return;
    // data-fui-rpc anchors are RPC triggers, not navigation.
    if (anchor.hasAttribute('data-fui-rpc')) return;

    const fullPath = resolvePath(href);
    if (fullPath === currentPath) {
      // Already there, let the browser handle the click (focus, scroll, etc.).
      return;
    }
    // Preserve the #fragment: resolvePath strips it (path-only is what
    // route matching + cache keys want), but the URL bar and the
    // post-nav scroll target need it. loadPage reads location.hash, so
    // pushState must carry the fragment.
    let navHash = '';
    try { navHash = new URL(href, location.href).hash; } catch (_) { /* malformed href */ }
    // Suppress the browser default up front: with or without a
    // cancellation below, an intercepted click never becomes a hard
    // page load.
    e.preventDefault();
    // gofastr:beforenavigate: cancellable hook fired on the anchor
    // (bubbles, cancelable) only for clicks the router is about to
    // intercept, after every fall-through check above. A listener that
    // calls preventDefault() claims the click: the router stands down
    // entirely (no pushState, no partial fetch, no intercept overlay)
    // and userland owns the URL and the scroll from that point.
    if (!anchor.dispatchEvent(new CustomEvent('gofastr:beforenavigate', {
      bubbles: true, cancelable: true,
      detail: { href, path: fullPath, hash: navHash, anchor },
    }))) return;
    // Eagerly close an enclosing dismissible disclosure (mobile nav
    // hamburger). Without this, the menu floats over stale content
    // for the entire SPA fetch duration, the user perceives the
    // click as "didn't take".
    anchor.closest('details[data-fui-disclosure]:not([data-fui-disclosure-persist])')?.removeAttribute('open');
    // An intercepting route presents as an overlay when reached from its
    // declared origin. The module owns the URL and the fetch in that
    // case; returning true means it took the navigation.
    if (window.__gofastr._intercept && window.__gofastr._intercept(fullPath, navHash)) return;
    // Capture the origin BEFORE _pushURL syncs currentPath to the
    // destination, loadPage's X-Gofastr-From must name where the user
    // came from.
    const origin = currentPath;
    _pushURL(fullPath + navHash);
    loadPage(fullPath, { from: origin });
  });

  // Stateful query params describe in-page state (open panes, widget
  // deep links). A history move whose ONLY search diff is stateful must
  // not refetch the screen, refetching discarded the client-mounted
  // widget, which is why Forward across a deep link never worked. The
  // set is built at popstate time from what the page actually declares:
  // the widget catalog's deepLinkKey/deepLinkParams plus every
  // [data-fui-pane-deeplink] attribute in the DOM. Everything else
  // (search, filters, ?p=) is screen identity and refetches as before.
  const _statefulParams = () => {
    const set = new Set();
    const cat = window.__gofastr._widgetCatalog || {};
    for (const name in cat) {
      const cfg = cat[name] && cat[name].cfg;
      if (!cfg) continue;
      if (cfg.deepLinkKey) set.add(cfg.deepLinkKey);
      for (const p of cfg.deepLinkParams || []) set.add(p);
    }
    for (const el of document.querySelectorAll('[data-fui-pane-deeplink]')) {
      const p = el.getAttribute('data-fui-pane-deeplink');
      if (p) set.add(p);
    }
    return set;
  };
  const _onlyStatefulDiff = (a, b) => {
    const ap = a.split('?'), bp = b.split('?');
    if (ap[0] !== bp[0]) return false;
    const as = new URLSearchParams(ap[1] || ''), bs = new URLSearchParams(bp[1] || '');
    const keys = new Set([...as.keys(), ...bs.keys()]);
    const stateful = _statefulParams();
    for (const k of keys) {
      if ((as.get(k) || '') === (bs.get(k) || '')) continue;
      if (!stateful.has(k)) return false;
    }
    return true;
  };

  // popstate: a URL change via back/forward triggers a screen-partial
  // re-fetch (cache makes it instant), unless the diff is purely
  // stateful, in which case the deep-link sync below replays the state
  // with zero fetches. Reads history.state, never the event's: the
  // intercept module's synthetic PopStateEvent carries none.
  window.addEventListener('popstate', () => {
    // With manual scrollRestoration the viewport still holds the LEAVING
    // page's position when popstate fires, record it under the old id
    // before switching, so the entry we just left can always restore
    // even if its event-driven capture lagged.
    _scrollStore[_entryId] = [scrollX | 0, scrollY | 0];
    const st = history.state;
    _entryId = (st && st.__fui) || 0;
    if (!_entryId) {
      _entryId = ++_entrySeq;
      try { history.replaceState({ __fui: _entryId }, '', location.href); } catch (_) {}
    }
    const path = location.pathname + location.search;
    if (path !== currentPath && currentPath !== '') {
      if (_onlyStatefulDiff(currentPath, path)) {
        currentPath = path;
      } else {
        _pendingScroll = _scrollStore[_entryId] || null;
        loadPage(path);
      }
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
        the path is the current page and bypasses the screen cache,
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
      const origin = currentPath;
      _pushURL(path, { replace: replace || path === currentPath });
      loadPage(path, { bypassCache: force, from: origin });
    },

    // The history-write choke point, exported for the modules that
    // record in-page state in the URL (rpc push-state, widget + pane
    // deep links, intercept). Writing history around it leaves
    // currentPath stale and breaks scroll restoration.
    _pushURL,

    /** Drop cached screens so the next visit re-fetches. Selectors:
        "/orders" drops that pathname AND every cached query variant;
        "/orders?page=2" drops exactly that entry; "*" clears all.
        Root-relative paths only, anything else is ignored. Never
        touches the live DOM; pair with refresh()/navigate(force) when
        the current screen must re-render too. */
    invalidate(...sels) {
      // Prefetched entries go stale by the same selectors; the preload
      // module registers the hook only when loaded.
      this._invalPreload?.(sels);
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
        bypassing the cache. Goes straight to loadPage, history is not
        touched, so a #fragment on the URL survives. */
    refresh() { loadPage(currentPath, { bypassCache: true }); },

    // X-Gofastr-Invalidate consumer, takes the whole Response (keeps
    // the header literal in one module; the callers in rpc/widgets/
    // intercept stay a few bytes). The value is a JSON string array of
    // selectors, applied on 2xx by nav/RPC/widget/intercept fetches.
    // A malformed value is a producer bug (ui.InvalidateScreens always
    // emits a valid array) and must never break the response that
    // carried it, ignore it. The Array.isArray gate matters: spreading
    // a parsed bare string would evict per-character.
    _inval(r) {
      try {
        const a = JSON.parse(r.headers.get('X-Gofastr-Invalidate'));
        if (Array.isArray(a)) this.invalidate(...a);
      } catch (_) {}
    },
  });
