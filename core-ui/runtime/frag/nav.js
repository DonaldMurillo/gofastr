// nav.js: SPA router (spec fragment `nav`, boot class; deps: kernel+signals).
// Owns: <a> click hijack, history.pushState, popstate, screen cache,
// layout-chain-aware swaps (deepest shared layer), document-lifetime
// script boundaries (hard document load at a data-cui-doc scope edge),
// updateActiveLink, document.title writes, the navigate() namespace
// member.
//
// This is the PLAIN navigator: byte-for-byte the pre-layout source
// plus the itemised fixes every page needs (error pages through the
// swap path, pointer-modality focus, the atomic seed merge, the
// data-cui-open anchor guard) and two seams the demand modules own —
// the commit seam (_swapCommit delegates to the transition module so a
// plain page declaring a transition gets one) and the opt-in
// stand-down (a document holding an outlet or area marker hands its
// navigations to the envelope module: the pending path rides a
// one-field slot, the module load starts, and this navigator stands
// down; the module carries its own navigator, cache and tail). The
// epoch lives on the shared NS._navEpoch slot so both navigators and
// the supersede rule (NS._navLive) see one counter.

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
    // vt is the transition pick this entry landed with, read off the
    // hook at write time (the commit settles the pick before the apply
    // writes), so a replay keeps the Back/Forward edge rule.
    screenCache.set(path, { html, title, layer: layer || '',
      vt: (window.__gofastr._navHooks.transition?.pick() || '') });
  };

  // --- Layout chain primitives ---
  // The server marks every layout layer with data-cui-layout-key (its
  // identity) and the layer's content cell with data-cui-layout-slot (the
  // swap target). The route manifest carries each route's chain in
  // `layouts` (outermost → innermost). Document order of the key-marked
  // elements IS the chain order, a wrapper precedes its descendants.
  const domChainKeys = () => {
    const out = [];
    for (const el of document.querySelectorAll('[data-cui-layout-key]')) {
      out.push(el.getAttribute('data-cui-layout-key'));
    }
    return out;
  };
  // Attribute-compare in a loop instead of an attribute selector: keys
  // contain '/' and ':', and getAttribute needs no escaping.
  const findSlot = (key) => {
    if (!key) return null;
    for (const el of document.querySelectorAll('[data-cui-layout-slot]')) {
      if (el.getAttribute('data-cui-layout-slot') === key) return el;
    }
    return null;
  };
  const mainEl = () => document.querySelector('[role="main"]') ?? document.querySelector('main');
  const mainSlotKey = () => {
    const m = mainEl();
    return (m && m.getAttribute('data-cui-layout-slot')) || '';
  };
  // routeEntry: manifest lookup with trailing-slash tolerance and dynamic
  // patterns. Pattern awareness matters for chains, a concrete URL of a
  // "/:param" route must resolve to that route's chain, not to "no
  // layouts" (which read as cross-chain and forced a shell rebuild on
  // every dynamic-route nav).
  // Typed segment constraints, the same tests core-ui/app/constraints.go
  // runs at resolve. The manifest lists dynamic routes in the server's
  // registration order, so with these the first match here is the route
  // the server serves: /orders/help skips /orders/:id:int for
  // /orders/:slug, and reads that route's layouts and docScripts.
  const _segTest = { int: /^\d+$/, uuid: /^[\da-f]{8}(-[\da-f]{4}){3}-[\da-f]{12}$/i, alpha: /^[a-z]+$/i, alnum: /^[a-z\d]+$/i };
  // A segment in the server's routing form (app.RoutePath): percent-
  // decoded, so /orders/%31 is the :id:int route there and here, with a
  // decoded "%" or "/" kept escaped so it stays one segment; a segment
  // that does not decode (%E0%A4) is taken literally, never thrown.
  const _routeSeg = (s) => { try { s = decodeURIComponent(s); } catch (_) {} return s.replace(/[%/]/g, (c) => encodeURIComponent(c)); };
  const routeEntry = (path) => {
    // Routes register unprefixed: /es/pricing looks up /pricing.
    const clean = _unlocalize(path.split('?')[0].split('#')[0]).split('/').map(_routeSeg).join('/');
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
      // Each literal segment must align; a dynamic segment (":name",
      // incl. the catch-all ":name*") matches any value its constraint
      // (":id:int", ":id:uuid", ":id:alpha", ":id:alnum") accepts.
      if (pp.every((seg, i) => seg[0] === ':' ? (_segTest[seg.split(':')[2]] || /./).test(parts[i]) : seg === parts[i])) return entry;
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

  // --- Document-lifetime scripts (capability boundary) ---
  // A script the host registers with a page scope rides the rail as
  // <script src data-cui-doc>, and the route manifest carries each
  // route's set as docScripts. Such a script installs capabilities
  // INTO the document (WebMCP's navigator.modelContext tools are the
  // driving case), and two browser facts make its scope edge a
  // document boundary: removing the tag does not uninstall what the
  // script installed, and a partial swap never runs a body script, so
  // a destination needing a script this document never loaded would
  // stay without it. Navigation across an edge must therefore load a
  // real document; equal sets stay partial.
  // Truth for THIS document is the live DOM (a stale manifest entry
  // must never lend this document's capabilities to a route); the
  // destination's entry carries the empty set when unknown. Compare
  // sorted src lists joined on a newline: the manifest array is sorted
  // server-side, and a newline can never appear in a registered src
  // (the rail rejects control characters), so unlike a comma it cannot
  // make two different sets read as one string.
  const crossesDocBoundary = (destPath) => {
    // Another locale is another document: its lang, its strings table
    // and every server-rendered word differ, not just the screen.
    if (_localeOf(destPath) !== _localeOf(location.pathname)) return true;
    const de = routeEntry(destPath);
    const dest = (de && de.docScripts) || [];
    const srcs = [];
    for (const s of document.querySelectorAll('script[data-cui-doc]')) {
      const v = s.getAttribute('src');
      if (v) srcs.push(v);
    }
    srcs.sort();
    return srcs.join('\n') !== dest.join('\n');
  };
  // A server redirect (X-Gofastr-Location) into another document loads
  // it in place of the requested URL. Both navigators ask before they
  // replace the URL: the boundary reads the locale off location.
  const _docRedirect = (to) => {
    if (!window.__gofastr._originOK(to) || !crossesDocBoundary(to)) return false;
    location.replace(to);
    return true;
  };
  window.__gofastr._docRedirect = _docRedirect;
  // fetch follows a 3xx itself (uihost answers one for any target that
  // is not a safe relative path), so only the response URL says where
  // the reader landed. Another origin or another document loads as a
  // document (null: handled); a page in this document comes back as its
  // path, for the X-Gofastr-Location leg; '' when nothing redirected.
  const _followed = (r, path) => {
    // A policy sending the reader off the app: fetch could not follow
    // it there, so the server names it and the page goes.
    const doc = r.headers.get('X-Gofastr-Document');
    if (doc) {
      if (!/^https?:\/\//i.test(doc)) throw new Error('HTTP 303');
      location.replace(doc);
      return null;
    }
    if (!r.redirected || !r.url) return '';
    if (!window.__gofastr._sameOrigin(r.url)) { location.replace(r.url); return null; }
    const to = resolvePath(r.url);
    return to === path ? '' : _docRedirect(to) ? null : to;
  };
  window.__gofastr._followed = _followed;

  // Page-owned metadata travels with cached page content, independently of
  // the layout boundaries the navigator keeps. Scripts are never copied.
  const syncPageHead = (root) => {
    const carrier = root && (root.matches && root.matches('template[data-cui-page-head]') ? root : root.querySelector('template[data-cui-page-head]'));
    const source = carrier ? new DOMParser().parseFromString(carrier.getAttribute('data-cui-page-head') || '', 'text/html') : (root && root.nodeType === 9 ? root : null);
    if (!source) return;
    const incoming = [];
    for (const node of source.querySelectorAll('meta[data-cui-page-meta],link[data-cui-page-meta]')) {
      if (node.tagName === 'META' && (!node.hasAttribute('name') && !node.hasAttribute('property') || node.hasAttribute('http-equiv'))) continue;
      if (node.tagName === 'LINK') {
        try { if (!/^https?:$/.test(new URL(node.getAttribute('href'), location.href).protocol)) continue; } catch (_) { continue; }
      }
      const copy = node.cloneNode(false);
      for (const attr of Array.from(copy.attributes)) if (/^on/i.test(attr.name)) copy.removeAttribute(attr.name);
      incoming.push(copy);
    }
    document.head.querySelectorAll('meta[data-cui-page-meta],link[data-cui-page-meta]').forEach(node => node.remove());
    incoming.forEach(node => document.head.appendChild(node));
  };
  const docCarrier = (source) => {
    const carrier = source.createElement('template');
    carrier.setAttribute('data-cui-page-head', Array.from(source.head.querySelectorAll('meta[data-cui-page-meta],link[data-cui-page-meta]')).map(node => node.outerHTML).join(''));
    const lang = source.documentElement.getAttribute('lang');
    if (lang) carrier.setAttribute('data-cui-lang', lang);
    const skip = source.querySelector('[data-skip-link]');
    if (skip) carrier.setAttribute('data-cui-skip-label', skip.textContent);
    // The live document's head block is the first page's: after a soft
    // nav its words are the table applyDocShell last set.
    carrier.setAttribute('data-cui-strings', JSON.stringify(source == document ? _says : _json(source.getElementById('gofastr-strings')?.textContent)));
    return carrier.outerHTML;
  };
  window.__gofastr._docCarrier = docCarrier;
  // docSeed is a whole document's page-scoped signal seed, as the
  // #gofastr-signals-partial island a partial carries. The head seed
  // mixes scopes; #gofastr-signals-page names its page-scoped keys, so
  // only those ride and a global keeps its first-seen rule. Placed in a
  // swapped or cached body, the swap's merge applies it: Back to the
  // first page of a visit, or a fetched document, restores page state
  // the way a partial does. A host that names no keys restores none.
  // (A whole document never carries a partial island of its own.)
  // The envelope module reads it too.
  const docSeed = (source) => {
    const flat = _json(source.getElementById('gofastr-signals')?.textContent);
    const names = _json(source.getElementById('gofastr-signals-page')?.textContent);
    if (!flat || !Array.isArray(names)) return '';
    const p = {};
    // A name the seed lacks stays undefined, which JSON drops.
    for (const k of names) p[k] = flat[k];
    // The island is re-parsed from a string: a value holding
    // "</script>" must not end it.
    return '<script type="application/json" id="gofastr-signals-partial">' + JSON.stringify({ p }).replace(/</g, '\\u003c') + '</script>';
  };
  window.__gofastr._docSeed = docSeed;

  // Cache the initial page so back-navigation to it works instantly.
  // Route through cacheScreen() so the LRU cap is enforced uniformly.
  const initialMain = mainEl();
  if (initialMain) {
    // Key with the search string too, every later entry (and
    // currentPath) is keyed pathname+search, and invalidate()
    // matches against that form. The layer is <main>'s own slot key:
    // the boot html spans everything below layer 0.
    cacheScreen(location.pathname + location.search, docCarrier(document) + docSeed(document) + initialMain.innerHTML, document.title, mainSlotKey());
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
  // dedup pattern (one click = one request). Path -> the epoch of the
  // navigation that requested it.
  const _pendingNav = new Map();
  // Monotonic nav generation. Captured at the start of each loadPage
  // call; after any await, a response whose epoch is no longer the
  // latest MUST NOT touch the DOM, currentPath, or history. Without
  // this, a rapid A→B where A's fetch resolves last swaps <main> back
  // to A's content while the URL bar already says B, and a repeat
  // click on B no-ops (fullPath === currentPath), stranding the user.
  // The supersede rule, one decision: "is this navigation still the
  // current one?" Every point that applies DOM, cache or history asks
  // exactly this — in both navigators, the loading scheduler, the
  // parts pump — and nothing else may decide it. The epoch is the
  // shared NS._navEpoch slot, so the plain navigator, the envelope
  // module's navigator and the demand modules all read one counter.
  const _navLive = (epoch) => epoch === window.__gofastr._navEpoch;

  // Mini toast used by loadPage failures, strict-CSP-clean (no
  // inline styles since the .cui-nav-toast class is shipped via
  // frameworkBuiltinCSS).
  const _showNavToast = (msg) => {
    const t = doc.singleton('cui-nav-toast', () => {
      const d = document.createElement('div');
      d.className = 'cui-nav-toast';
      d.setAttribute('role', 'alert');
      return d;
    });
    t.textContent = msg;
    t.classList.add('is-visible');
    clearTimeout(t._fuiTimer);
    t._fuiTimer = setTimeout(() => t.classList.remove('is-visible'), 4000);
  };

  // Wait for component styles before measuring hash/history destinations.
  // A superseding navigation or USER scroll intent cancels either write,
  // including the second pass after two animation frames. Intent is
  // tracked as a counter bumped by wheel/touch/key/pointer input: pixel
  // identity cannot be the cancel test, because the stylesheet wait's
  // own purpose — cold CSS landing after the swap — can grow content
  // above the viewport, and the browser's scroll anchoring then moves
  // scrollY with no user input at all (observed: 1200 → 2682 on a
  // 1482px growth). Cancelling on that drift skipped the first write
  // and the hash target was never reached; only real input cancels.
  let _userScrolls = 0;
  const _userIntent = () => { _userScrolls++; };
  for (const ev of ['wheel', 'touchmove', 'keydown', 'pointerdown']) {
    addEventListener(ev, _userIntent, { capture: true, passive: true });
  }
  const _settleScroll = async (fn) => {
    const epoch = window.__gofastr._navEpoch;
    const u0 = _userScrolls;
    await window.__gofastr._sheetsFor(document);
    if (!_navLive(epoch) || _userScrolls !== u0) return;
    fn();
    const x = scrollX, y = scrollY;
    requestAnimationFrame(() => requestAnimationFrame(() => {
      if (_navLive(epoch) && _userScrolls === u0 && scrollX === x && scrollY === y) fn();
    }));
  };

  // scrollToHash scrolls to the element targeted by the current URL
  // fragment after a SPA swap; falls back to the top when there is no
  // fragment or no matching element. Reads location.hash (set by the
  // click handler's pushState / by the browser on popstate) so back /
  // forward and in-link fragments all land on the right section instead
  // of always jumping to the top. location.hash keeps the fragment
  // percent-encoded (#caf%C3%A9 for id="café"), so the decoded id is
  // tried after the raw one, the order the browser's own fragment
  // scroll uses; a fragment that does not decode (#100%) stays raw.
  const scrollToHash = () => {
    const id = (location.hash || '').replace(/^#/, '');
    _settleScroll(() => {
      if (id) {
        let el = document.getElementById(id);
        if (!el) try { el = document.getElementById(decodeURIComponent(id)); } catch (_) {}
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

  // The PUBLIC alias every runtime call site uses: modules wrap
  // NS._pushURL to observe leaves (the envelope module's scroll
  // records). Internal callers must not bypass it.
  const _pushPub = (url, o) => window.__gofastr._pushURL(url, o);
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

  // swapAtSlot replaces one layer's content cell. Closing disclosures
  // that would float over the destination content is
  // headless-disclosure's contract (framework/headless/disclosure.js,
  // on gofastr:navigate), not the navigator's.
  // Whether the navigation in flight began from a POINTER (detail > 0;
  // keyboard activations synthesize clicks with detail 0). The swap
  // moves focus onto the fresh cell either way, but a pointer-initiated
  // navigation must not paint the focus ring: focusVisible:false keeps
  // the ring keyboard signal.
  let _navPointer = false;
  const _focusSwapTarget = (el) => {
    if (typeof el.focus !== 'function') return;
    try { el.focus({ preventScroll: true, focusVisible: !_navPointer }); } catch (_) { /* older Safari */ }
  };

  const swapAtSlot = (slot, html) => {
    slot.innerHTML = html;
    mergeSeedFromDOM(slot);
    window.__gofastr.scanAndLoadCSS(slot);
    // Move focus onto the fresh content so keyboard users are not
    // stranded on a detached node. Cells carry tabindex="-1".
    _focusSwapTarget(slot);
    return slot;
  };

  // swapShell replaces the whole layout shell (cross-chain navigation: no
  // shared root). newRoot is the destination page's outermost shell, or
  // its bare <main> when the destination has no layouts, so leaving a
  // chain for a plain page drops the old chrome instead of keeping it.
  // Delegated chrome handlers survive the swap; no hard reload (hard
  // rule 4).
  const shellEl = (d) => (d || document).querySelector('[data-cui-layout-key], [data-cui-screen-group]');
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
    // The parent: the new shell root itself carries its layout's
    // data-cui-scope, and the scan reads descendants only.
    window.__gofastr.scanAndLoadCSS(el.parentNode);
    const m = el.matches('main, [role="main"]') ? el : (el.querySelector('[role="main"]') || el.querySelector('main'));
    if (m) _focusSwapTarget(m);
    return el;
  };
  // applyDocShell syncs the document-level markers that ride the swapped
  // payload onto the document itself. <html lang> and the skip link live
  // OUTSIDE the shell the runtime swaps, so the server carries the
  // destination's values (data-cui-lang / data-cui-skip-label, from
  // App.LangForPath + SkipLabelForPath) on the outermost layer it
  // renders; after any swap this copies them onto documentElement.lang
  // (doc.setHtmlAttr, manifest-governed) and the skip link's text.
  // root is the swapped element (the new shell, or the slot that received
  // the payload); a payload with no markers changes nothing.
  const applyDocShell = (root) => {
    syncPageHead(root);
    const fresh = root && root.querySelector('template[data-cui-page-head][data-cui-lang]');
    // The destination's runtime words; none means English.
    if (fresh) _says = _json(fresh.getAttribute('data-cui-strings')) || {};
    const c = fresh || root && (root.matches('[data-cui-lang],[data-cui-skip-label]')
      ? root : root.querySelector('[data-cui-lang],[data-cui-skip-label]'));
    if (!c) return;
    const lang = c.getAttribute('data-cui-lang');
    if (lang) doc.setHtmlAttr('lang', lang);
    const skip = c.getAttribute('data-cui-skip-label');
    const link = skip && document.querySelector('[data-skip-link]');
    if (link) link.textContent = skip;
  };

  window.__gofastr._applyDocShell = applyDocShell;

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
    window.__gofastr._navShown = true;
    applyDocShell(root);
    if (!ps && (location.hash || (prevPath || '').split('?')[0] != path.split('?')[0])) scrollToHash();
    window.dispatchEvent(new CustomEvent('gofastr:navigate', { detail: { path, prevPath, cached, root } }));
    if (ps) {
      // Restore AFTER the navigate listeners ran: overlay scroll-lock
      // releases (panes, modals) fire on that event and would clamp the
      // restored position. Same guarded settle pass as scrollToHash, so
      // a newer navigation's write always wins.
      _settleScroll(() => window.scrollTo(ps[0], ps[1]));
    }
  };

  // A navigation answer that arrives as HTML — 200 or NOT — can be
  // applied through the same swap paths a 200 takes (an error page
  // shows inside the shell instead of toasting). Anything else cannot,
  // and its fetch throws so the toast names the status, a 200 too:
  // its text has no <main>, so it would swap in a blank screen.
  const htmlOnly = (r) => { if (!(r.headers.get('Content-Type') || '').toLowerCase().startsWith('text/html')) throw new Error(`HTTP ${r.status}`); };

  /** Fetch page, swap at the deepest shared layer. Caches for instant back-nav.
      from names the origin route when the caller already moved the URL,
      _pushURL syncs currentPath BEFORE loadPage runs on the click path,
      so capturing currentPath here would report the destination as its
      own origin (and X-Gofastr-From would stop naming the real one). */
  // The commit seam: one navigation's commit routes through the
  // transition module's wrapper when the document declared a
  // transition (a plain page declaring data-cui-vt runs this — its
  // swaps animate); without the module the commit applies directly,
  // the pre-layout behaviour, behind the supersede rule's check.
  // css: the incoming markup. Its component sheets load while the old
  // page is still on screen (kernel _cssReady), before the title, the
  // route announcement or the swap move to the new page; the
  // transition module reads the incoming vocabulary off it.
  const _swapCommit = async (epoch, from, to, apply, pickSrc, css, title) => {
    await window.__gofastr._cssReady(css);
    if (!_navLive(epoch)) return;
    document.title = title;
    announceRoute(title);
    const T = window.__gofastr._navHooks.transition;
    if (T) return T.commit(epoch, from, to, apply, pickSrc, css);
    apply();
  };

  const loadPage = (path, opts) => {
    if (!window.__gofastr._originOK(path)) return;
    if (crossesDocBoundary(path)) { location.assign(path); return; }
    // The opt-in hand-off: a document holding an outlet or area marker
    // hands its navigations to the envelope module. Warm, the
    // delegation above runs them; cold, the load starts now and the
    // module's navigator takes this one when it lands (the click has
    // already pushed the URL, hash included — the module reads the
    // served page off the performance navigation entry for its boot
    // capture, so a from naming a destination that never painted is
    // corrected module-side). A module that fails to load falls back
    // to a whole-document load of the destination.
    const G0 = window.__gofastr;
    const N = G0._navHooks.envelope?.nav;
    if (N || document.querySelector('[data-cui-outlet],[data-cui-area]')) {
      // The pointer modality core recorded for THIS navigation rides
      // the opts: the module demand-loads beside the FIRST fetch,
      // after the click, so a listener of its own would miss that
      // click and paint the ring on the first mouse navigation.
      opts = Object.assign({ pointer: _navPointer }, opts);
      if (N) return N(path, opts);
      // loadModule resolves only when the module registered its hooks,
      // so nav is there on the success arm.
      return G0.loadModule('envelope').then(() => G0._navHooks.envelope.nav(path, opts),
        () => _plainLoadPage(path, Object.assign({ bypassCache: true, forceFull: true }, opts)));
    }
    return _plainLoadPage(path, opts);
  };
  const _plainLoadPage = async (path, { bypassCache = false, forceFull = false, from = null, restore = null } = {}) => {
    // Single gate for every branch below: the SPA navigator's target
    // comes from an href / a data-cui-* attribute / a server header,
    // and a cross-origin one must never be fetched with the page's
    // credentials and swapped into the DOM. A javascript: or data: URL
    // resolves to a null origin, so this subsumes the scheme check too.
    if (!window.__gofastr._originOK(path)) return;
    // Document-boundary backstop: the click hijack, navigate(), and
    // popstate gate this BEFORE any history write; this arm covers the
    // X-Gofastr-Location redirect leg and any future caller.
    if (crossesDocBoundary(path)) { location.assign(path); return; }
    // Dedup in-flight nav (10 clicks → 1 fetch), but only while the
    // pending one is the live navigation: on A→B→A the pending A holds
    // a stale epoch, and returning here left B live, committing under
    // the URL /a. A replay (cache or prefetch) drops its entry, so a
    // forced fetch never joins the copy the force exists to replace.
    if (_pendingNav.get(path) === window.__gofastr._navEpoch) return;
    const myEpoch = ++window.__gofastr._navEpoch;
    _pendingNav.set(path, myEpoch);
    // The transition module's direction seam (its twin sits in the
    // module navigator's entry): binds the recorded direction and
    // pick-edge rule to THIS navigation's epoch before any await.
    window.__gofastr._navHooks.transition?.take(myEpoch);
    const prevPath = from || currentPath;
    if (window.__gofastr._navBase === null) window.__gofastr._navBase = prevPath;
    currentPath = path;
    // Consume the popstate's restore target NOW, into this navigation,
    // left in module state, a superseded back/forward's position leaked
    // into whichever navigation ran finishNav next.
    const ps = restore;
    // Surface "I heard you" feedback to assistive tech and screen
    // readers while the fetch is in flight. The CSS hook can show a
    // progress strip via [aria-busy="true"] on documentElement.
    doc.setHtmlAttr('aria-busy', 'true');

    try {
      const layouts = routeLayouts(path);
      // bypassCache: post-mutation navigation (data-cui-rpc-navigate,
      // navigate({force:true})) must show fresh server state, never the
      // cached copy captured before the mutation.
      const cached = (bypassCache || forceFull) ? null : getCachedScreen(path);
      // A cached entry is replayable iff the layer it renders below is
      // live in the DOM right now ('' = both pages are layout-less).
      if (cached) {
        let { html, layer } = cached;
        // An entry keyed above this move's deepest shared layer (the boot
        // entry sits at <main>'s slot) replays only that layer's cell, with
        // the entry's page head and seed: replayed whole, it rebuilt the
        // kept layer's DOM. The envelope module re-captures on leave.
        const sd = sharedDepth(layouts), deep = sd && domChainKeys()[sd - 1];
        if (deep && layer !== deep) {
          const t = document.createElement('template');
          t.innerHTML = html;
          const cell = t.content.querySelector(`[data-cui-layout-slot="${CSS.escape(deep)}"]`);
          if (cell) {
            html = [...t.content.children].filter((e) => e.matches('template[data-cui-page-head],#gofastr-signals-partial')).map((e) => e.outerHTML).join('') + cell.innerHTML;
            layer = deep;
          }
        }
        const slot = layer
          ? findSlot(layer)
          : ((layouts.length === 0 && domChainKeys().length === 0) ? mainEl() : null);
        if (slot) {
          _pendingNav.delete(path);
          await _swapCommit(myEpoch, prevPath, path, () => {
            finishNav(path, prevPath, true, swapAtSlot(slot, html), ps);
          }, cached, html, cached.title);
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
          // A layout-less entry swaps into <main> only when the live DOM
          // is layout-less too, as for the screen cache above.
          const slot = pf.layer ? findSlot(pf.layer) : ((layouts.length === 0 && domChainKeys().length === 0) ? mainEl() : null);
          if (slot) {
            _pendingNav.delete(path);
            await _swapCommit(myEpoch, prevPath, path, () => {
              cacheScreen(path, pf.html, pf.title, pf.layer);
              finishNav(path, prevPath, false, swapAtSlot(slot, pf.html), ps);
            }, null, pf.html, pf.title);
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
      // path, the server echoed a swap boundary this DOM doesn't have;
      // it wins on its own, chains or not: gated behind "either side has
      // a chain" it re-entered the partial branch for a layout-less
      // destination and the same missing boundary looped the fetch.
      if (forceFull || ((layouts.length > 0 || domChainKeys().length > 0) && sharedDepth(layouts) === 0)) {
        const fr = await fetch(path);
      if (!_navLive(myEpoch)) return;
        window.__gofastr._inval(fr);
        const pdoc = new DOMParser().parseFromString(await fr.text(), 'text/html');
      if (!_navLive(myEpoch)) return;
        // The path keeps the search string: the cache key and the URL
        // bar must carry a redirect-added query (e.g. ?next=/admin).
        const to = _followed(fr, path);
        if (to === null) return;
        // After the redirect: a Document answer is bodiless.
        htmlOnly(fr);
        const dest = to || path;
        if (to) { _pushPub(dest, { replace: true }); currentPath = dest; }
        const t = pdoc.querySelector('title')?.textContent || document.title;
        // The full fetch re-renders chrome under the CURRENT session,
        // if the server re-minted (restart/rotation/expiry), the fresh
        // head carries the new stream id. Copy it onto the live meta so
        // the SSE reconnect loop recovers here too, not only on the
        // partial branch's X-Gofastr-Session path.
        const fm = sseMeta(pdoc), lm = sseMeta();
        if (fm && lm) lm.setAttribute('content', fm.getAttribute('content'));
        const nm = pdoc.querySelector('main');
        await _swapCommit(myEpoch, prevPath, dest, () => {
          const sourceRoot = shellEl(pdoc) || nm;
          // The carrier rides <main>, so the cache entry below (and the
          // whole-main fallback) keeps the page's language and words.
          (nm || sourceRoot)?.insertAdjacentHTML("afterbegin", docCarrier(pdoc) + docSeed(pdoc));
          const el = swapShell(sourceRoot);
          if (!el) {
            // No live shell to replace (layout-less origin), fall back to
            // a whole-main swap; chain markers arrive with the content.
            const m = mainEl();
            if (m) swapAtSlot(m, nm ? nm.innerHTML : '');
          }
          // An error page is shown, never replayed.
          if (fr.ok) cacheScreen(dest, nm ? nm.innerHTML : '', t, nm ? (nm.getAttribute('data-cui-layout-slot') || '') : '');
          finishNav(dest, prevPath, false, el || mainEl(), ps);
        }, null, pdoc, t);
        return;
      }

      // Partial fetch. X-Gofastr-From names the origin route so the
      // server renders only the layers the two routes do NOT share and
      // echoes the swap boundary in X-Gofastr-Swap. X-Gofastr-Markup is
      // the kernel's _markup, spelled as a literal here (it costs fewer
      // gzip bytes than the property read; TestKernelMarkupMatchesGo
      // holds both spellings to runtime.MarkupVersion).
      const hdrs = { 'X-Gofastr-Navigate': '1', 'X-Gofastr-Markup': '2' };
      const fromPath = (prevPath || '').split('?')[0];
      if (fromPath && routeEntry(fromPath)) hdrs['X-Gofastr-From'] = fromPath;
      const resp = await fetch(path, { headers: hdrs });
      if (!_navLive(myEpoch)) return;
      // Apply a session rollover BEFORE the ok-check: the server re-mints
      // (and names the fresh stream id) on 404 / policy-block partials
      // too, and the browser has already stored the new cookie, if we
      // threw first, the meta would keep the dead id and never recover
      // (the next OK nav presents the now-valid cookie, so no header).
      const rs = resp.headers.get('X-Gofastr-Session'), rm = rs && sseMeta();
      if (rm) rm.setAttribute('content', rm.getAttribute('content').replace(/([?&]session=)[^&]*/, (_, p) => p + rs));
      // A session change is an identity change: one identity's pages
      // must never replay for another.
      if (rs) window.__gofastr.invalidate('*');
      const notOk = !resp.ok;

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
      // A followed 3xx takes the same leg as the header.
      const followed = _followed(resp, path);
      if (followed === null) return;
      const redirectTo = resp.headers.get('X-Gofastr-Location') || followed;
      if (redirectTo) {
        _pendingNav.delete(path);
        if (_docRedirect(redirectTo)) return;
        // pushState was already called by the click handler with the
        // requested path; replace it with the redirect destination so
        // the URL bar matches what we're about to load.
        _pushPub(redirectTo, { replace: true });
        currentPath = redirectTo;
        doc.removeHtmlAttr('aria-busy');
        // Keep bypassCache across the redirect: a post-mutation nav
        // must not serve the redirect target from the screen cache,
        // and keep the ORIGINAL origin so the redirect leg's subtree
        // partial renders against where the user actually came from.
        return loadPage(redirectTo, { bypassCache, from: prevPath, restore: ps });
      }

      // After the redirect legs: their 200s carry no body.
      htmlOnly(resp);
      const html = await resp.text();
      if (!_navLive(myEpoch)) return;

      // Compute title BEFORE swapping content so document.title is
      // already correct when AT or extensions observe the new state.
      const partial = resp.headers.get('X-Gofastr-Partial') === 'true';
      let title, body;
      let swapKey = (partial && resp.headers.get('X-Gofastr-Swap')) || '';
      // uihost percent-encodes a key holding non-ASCII or %, since fetch
      // reads header bytes as Latin-1; a key an older server sent raw
      // stays as it came.
      try { swapKey = decodeURIComponent(swapKey); } catch (_) {}
      if (partial) {
        title = window.__gofastr._title(resp);
        body = html;
      } else {
        const pdoc = new DOMParser().parseFromString(html, 'text/html');
        const nm = pdoc.querySelector('main');
        title = pdoc.querySelector('title')?.textContent || document.title;
        body = docCarrier(pdoc) + docSeed(pdoc) + (nm?.innerHTML ?? '');
        swapKey = nm?.getAttribute('data-cui-layout-slot') || '';
      }
      // The swap boundary must be live in the DOM; a miss means the
      // manifest and server disagree (deploy skew), recover with a
      // full-page load of the destination rather than a wrong-cell swap.
      const slot = swapKey ? findSlot(swapKey) : ((layouts.length === 0) ? mainEl() : null);
      if (!slot) {
        _pendingNav.delete(path);
        return loadPage(path, { bypassCache: true, forceFull: true, from: prevPath, restore: ps });
      }
      await _swapCommit(myEpoch, prevPath, path, () => {
        const root = swapAtSlot(slot, body);
        if (!notOk) cacheScreen(path, body, title, swapKey);
        finishNav(path, prevPath, false, root, ps);
      }, resp, body, title);
    } catch (err) {
      if (!_navLive(myEpoch)) return;
      // CLAUDE.md hard rule 4, no location.href fallback. Surface a
      // toast and stay on the current page; URL has already been
      // pushState'd by the click handler so revert it, to the page the
      // DOM still shows: prevPath names a superseded navigation's
      // destination when this one started while that one loaded.
      console.warn('[gofastr] Nav failed:', err);
      _showNavToast(err && /^HTTP \d+$/.test(err.message)
        ? say('runtime.load_failed_status', 'Could not load {{path}} ({{status}})', { path, status: err.message })
        : say('runtime.load_failed', 'Could not load {{path}}: check your connection', { path }));
      const base = window.__gofastr._navBase;
      _pushPub(base || location.pathname, { replace: true });
      currentPath = base;
    } finally {
      // A stale navigation leaves a newer one's dedup entry alone.
      if (_pendingNav.get(path) === myEpoch) _pendingNav.delete(path);
      // Only the latest nav owns the aria-busy flag and the base; a
      // superseded nav that bailed early must leave both for the
      // in-flight one.
      if (_navLive(myEpoch)) {
        doc.removeHtmlAttr('aria-busy');
        window.__gofastr._navBase = null;
      }
    }
  };

  // Announce the new page title via aria-live region so assistive
  // technology hears the route change (document.title mutations alone
  // aren't reported on most screen readers).
  let _announceTimer = 0;
  const announceRoute = (title) => {
    const r = document.getElementById('cui-route-announce');
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
    const data = _json(el.textContent);
    el.remove();
    if (!data) return;
    const G = window.__gofastr;
    const store = G && G._signals;
    if (!store) return;
    const page = data.p || {};
    // P7-C atomic merge: every CHANGED value installs silently first,
    // then notifies once — a computed over two page signals observes
    // one page's state on every evaluation, never a mid-merge mix.
    const changed = [];
    // own(), never a truthy store[k]: a key like valueOf names an
    // Object.prototype member, not a signal.
    for (const k in page) {
      if (!own(page, k)) continue;
      if (isReservedSignalKey(k)) continue;
      const v = page[k];
      if (own(store, k) && store[k].value === v) continue;
      if (own(store, k)) store[k].value = v;
      else store[k] = { value: v, listeners: [] };
      changed.push([k, v]);
    }
    for (const [k, v] of changed) G.setSignal(k, v);
    const glob = data.g || {};
    for (const k in glob) {
      if (!own(glob, k)) continue;
      if (isReservedSignalKey(k)) continue;
      if (!own(store, k)) store[k] = { value: glob[k], listeners: [] };
    }
  };
  // The intercept module's pane swap applies its answer's seed too.
  window.__gofastr._mergeSeed = mergeSeedFromDOM;

  // Link clicks: cross-page navigation (/a → /b) is intercepted and
  // handled client-side via partial fetch + cache. No hard refresh.
  // This is the Angular-router-style behavior described in
  // core-ui/ARCHITECTURE.md ("Page → page navigation"). In-page state
  // changes are NOT routes, they go through data-cui-rpc on islands
  // and never hit this handler.
  //
  // Cmd/Ctrl/Shift/Alt-click, target=_blank, external links, and
  // unknown routes fall through to default browser navigation.
  document.addEventListener('click', (e) => {
    // A page listener that ran first (on the anchor or an ancestor)
    // and prevented the click owns it, as it would without the router.
    // The signal delegate's own prevent (_cuiSig) is not one of those.
    if (e.defaultPrevented && !e._cuiSig) return;
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
    // data-cui-rpc anchors are RPC triggers, not navigation.
    if (anchor.hasAttribute('data-cui-rpc')) return;
    // data-cui-open anchors are widget triggers, not navigation: the
    // widgets-boot delegator preventDefaults the click and opens the
    // widget, and the no-script page keeps the href as the fallback
    // destination. A hijacked SPA navigation here meant one click BOTH
    // navigated and opened the widget.
    if (anchor.hasAttribute('data-cui-open')) return;
    // data-cui-nav="off" opts a link out of SPA navigation entirely:
    // hosts whose destination page depends on full-load scripts (legacy
    // page-runtime initializers) need a real document load.
    if (anchor.getAttribute('data-cui-nav') === 'off') return;

    const fullPath = resolvePath(href);
    // A document-lifetime script's scope edge is a document boundary.
    // Stand down BEFORE preventDefault so the browser performs a real
    // navigation with ordinary link semantics (history, focus, target),
    // exactly like a data-cui-nav="off" link.
    if (crossesDocBoundary(fullPath)) return;
    // Preserve the #fragment: resolvePath strips it (path-only is what
    // route matching + cache keys want), but the URL bar and the
    // post-nav scroll target need it. loadPage reads location.hash, so
    // pushState must carry the fragment.
    let navHash = '';
    try { navHash = new URL(href, location.href).hash; } catch (_) { /* malformed href */ }
    if (fullPath === currentPath) {
      // Already there (or on the way: a repeat click while the page
      // loads). A fragment is the browser's own in-page jump; anything
      // else no-ops, the browser would reload the whole document.
      if (!navHash) e.preventDefault();
      return;
    }
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
    // An intercepting route presents as an overlay when reached from its
    // declared origin. The module owns the URL and the fetch in that
    // case; returning true means it took the navigation.
    if (window.__gofastr._intercept?.(fullPath, navHash)) return;
    // Capture the origin BEFORE _pushURL syncs currentPath to the
    // destination, loadPage's X-Gofastr-From must name where the user
    // came from.
    const origin = currentPath;
    // detail > 0 is a real pointer click; Enter/Space on a focused
    // link synthesize a click with detail 0 (keyboard: keep the ring).
    _navPointer = e.detail > 0;
    _pushPub(fullPath + navHash);
    loadPage(fullPath, { from: origin });
  });

  // Stateful query params describe in-page state (open panes, widget
  // deep links). A history move whose ONLY search diff is stateful must
  // not refetch the screen, refetching discarded the client-mounted
  // widget, which is why Forward across a deep link never worked. The
  // set is built at popstate time from what the page actually declares:
  // the widget catalog's deepLinkKey/deepLinkParams plus every
  // [data-hui-pane-deeplink] attribute in the DOM. Everything else
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
    // headless.PaneHost declares data-hui-pane-deeplink.
    for (const el of document.querySelectorAll('[data-hui-pane-deeplink]')) {
      const p = el.getAttribute('data-hui-pane-deeplink');
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
      // Every value counts, in order: ?tag=a&tag=b is not ?tag=a&tag=c,
      // and an empty ?tag= is not an absent tag.
      const av = as.getAll(k), bv = bs.getAll(k);
      if (av.length === bv.length && av.every((v, i) => v === bv[i])) continue;
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
    // An open intercept stack owns history moves within its layers: the
    // module closes or refetches panes itself and re-pushes an entry a
    // leave guard declined, and the stack's URLs never moved the
    // router's currentPath, so the diff below would misread them. The
    // module loads only when a route declares an intercept; without it
    // this is one absent-property read.
    if (window.__gofastr._interceptPopstate?.()) return;
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
    if (path === currentPath) {
      // Same page, another entry (Back across an in-page #anchor jump):
      // manual restoration left the viewport where it was, so put back
      // the position the entry recorded. A fresh fragment entry (the
      // browser's own jump fires popstate too) has none: the browser
      // scrolls it.
      const p = _scrollStore[_entryId];
      if (p) _settleScroll(() => window.scrollTo(p[0], p[1]));
    } else if (currentPath !== '') {
      if (_onlyStatefulDiff(currentPath, path)) {
        currentPath = path;
      } else if (crossesDocBoundary(path)) {
        // Reachable only through deploy skew or history written around
        // the router (a crossing click loads a new document, so one
        // document never holds both sides). Load the destination
        // rather than lend this document's capabilities to it.
        location.replace(path);
      } else {
        // The restore target rides the opts: the delegated (module)
        // navigator receives it the same way the plain one does.
        loadPage(path, { restore: _scrollStore[_entryId] || null });
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
    _settleScroll,
    // --- Router API ---

    // The supersede rule, one decision, shared with the demand
    // modules (src/envelope.js, loading.js, parts.js read it).
    _navLive,

    /** Programmatically navigate to a path. force re-fetches even when
        the path is the current page and bypasses the screen cache,
        use it after a mutation so the destination reflects new state. */
    navigate(path, { replace = false, force = false } = {}) {
      if (path === currentPath && !force) return;
      // Security: reject attacker-controllable schemes BEFORE
      // touching the URL bar. Server-rendered data-cui-push-state
      // attributes (e.g. on a combobox option) and signal-bound
      // hrefs are the trust boundary; navigate() is the choke point
      // for all programmatic SPA navigation, so the guard lives
      // here. Reuses the same gate as the signal-bound anchors etc.
      if (!this._originOK(path)) return;
      // Document boundary: load a real document instead of swapping.
      // assign/replace keep the push/replace shape the caller asked for.
      if (crossesDocBoundary(path)) {
        if (replace) { try { location.replace(path); } catch (_) {} }
        else location.assign(path);
        return;
      }
      const origin = currentPath;
      _navPointer = false; // programmatic: the browser's heuristic decides
      _pushPub(path, { replace: replace || path === currentPath });
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
        // A selector with "?" is exact; a queryless one evicts the
        // pathname and all its query variants (a stale list is stale on
        // every page/filter of it). Cache keys keep the locale prefix;
        // selectors never carry one.
        for (const k of screenCache.keys()) {
          const u = _unlocalize(k);
          if (s.includes('?') ? u === s : u === s || u.startsWith(s + '?')) screenCache.delete(k);
        }
      }
    },

    /** Re-fetch and re-render the current screen from the server,
        bypassing the cache. Goes straight to loadPage, history is not
        touched, so a #fragment on the URL survives. */
    refresh() { _navPointer = false; loadPage(currentPath, { bypassCache: true }); },

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
