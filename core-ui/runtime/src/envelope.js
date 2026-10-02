// GoFastr runtime module, envelope
//
// The fills envelope, snapshots, leave-capture and element scroll
// anchors (spike/layout-proto, layout-client, layout-motion P12,
// layout-static P13). Loaded when the document holds an outlet or
// area marker ([data-fui-outlet] / [data-fui-area]) — at boot or
// after any apply, the kernel's marker scan covers both.
//
// Before it loads the navigator sends no X-Gofastr-Fills header (the
// server answers a plain partial), restores window scroll by pixels,
// and captures no fills snapshots: a page with no outlets has none of
// any of this, which is exactly why the module exists.
//
// The seam is NS._navHooks.envelope; the navigator's side of each
// call is marked "seam" in frag/nav.js.
(() => {
  'use strict';
  window.__gofastr = window.__gofastr || {};
  const NS = window.__gofastr;

  // Attribute-compare in a loop, like core's findSlot: addresses
  // contain ':' / '/' / '#' / '~', and getAttribute needs no selector
  // escaping.
  const findFillTarget = (addr) => {
    if (!addr) return null;
    for (const el of document.querySelectorAll('[data-fui-outlet],[data-fui-area]')) {
      if (el.getAttribute('data-fui-outlet') === addr || el.getAttribute('data-fui-area') === addr) return el;
    }
    return null;
  };

  // Chain walkers, duplicated from core's nav fragment: this module
  // reads FETCHED documents through them (the `d` parameter), and a
  // page without outlets never runs either walker — which is why they
  // live here, not in the core bundle.
  const domChainKeys = (d) => {
    const out = [];
    for (const el of (d || document).querySelectorAll('[data-fui-layout-key]')) {
      out.push(el.getAttribute('data-fui-layout-key'));
    }
    return out;
  };
  const findSlot = (key, d) => {
    if (!key) return null;
    for (const el of (d || document).querySelectorAll('[data-fui-layout-slot]')) {
      if (el.getAttribute('data-fui-layout-slot') === key) return el;
    }
    return null;
  };

  // ─── The carried core ────────────────────────────────────────────────
  //
  // This module owns its navigations, so it carries the navigator
  // pieces it once borrowed from frag/nav.js as its own copies: the
  // screen cache (its OWN map, seeded at evaluation from the live
  // DOM), the seed merge (with the route fold), the swap/focus/finish
  // tail, the toast, the retry, and the epoch bump. Everything shared
  // with core goes through the public namespace: NS.currentPath,
  // NS._setCurrentPath, NS._pushURL, NS._inval, NS.invalidate, the
  // NS._navEpoch slot and the one decision, NS._navLive.

  const G = NS;
  let _pend = new Set();          // in-flight dedup, module side
  let _navPointer = false;        // pointer modality, set from nav's `pointer` opt

  // The module's screen cache: same LRU contract as core's, its own
  // map. Fields are the module's own shape (fills/seed/parts/vt
  // direct, no opaque blob — nothing outside reads them).
  const screenCache = new Map();
  const MAX_CACHE_SIZE = 20;
  const cacheScreen = (path, html, title, layer, fills, seed, partAddrs, parts, vt) => {
    if (screenCache.has(path)) screenCache.delete(path);
    if (screenCache.size >= MAX_CACHE_SIZE) {
      screenCache.delete(screenCache.keys().next().value);
    }
    screenCache.set(path, {
      html, title, layer: layer || '',
      fills: (fills && fills.length) ? fills : null,
      seed: seed || null,
      partAddrs: partAddrs || null,
      parts: parts || new Map(),
      vt: vt || '',
    });
  };
  const getCachedScreen = (path) => {
    const v = screenCache.get(path);
    if (v) { screenCache.delete(path); screenCache.set(path, v); }
    return v;
  };

  // Prototype-pollution guard, the signals fragment's rule copied
  // module-side (core exports none of its closures): a seed key named
  // __proto__/constructor/prototype would re-parent the _signals
  // store instead of creating an own data property.
  const isReservedSignalKey = (k) =>
    k === '__proto__' || k === 'constructor' || k === 'prototype';

  // The atomic seed merge (P7-C) with the route fold: the values a
  // cache replay must restore are tracked here, per the live route.
  const applyPartialSeed = (data) => {
    const store = G._signals;
    if (!store) return;
    const page = data.p || {};
    const changed = [];
    for (const k in page) {
      if (!Object.prototype.hasOwnProperty.call(page, k)) continue;
      if (isReservedSignalKey(k)) continue;
      const v = page[k];
      if (Object.prototype.hasOwnProperty.call(store, k) && store[k].value === v) continue;
      if (store[k]) store[k].value = v;
      else store[k] = { value: v, listeners: [] };
      changed.push([k, v]);
    }
    for (const [k, v] of changed) G.setSignal(k, v);
    const rs = routeSeedFrom(page);
    if (rs) _routeSeedP = rs;
    const glob = data.g || {};
    for (const k in glob) {
      if (!Object.prototype.hasOwnProperty.call(glob, k)) continue;
      if (isReservedSignalKey(k)) continue;
      if (!store[k]) store[k] = { value: glob[k], listeners: [] };
    }
  };

  const mergeSeedFromDOM = (root) => {
    if (!root || !root.querySelector) return;
    const el = root.querySelector('#gofastr-signals-partial');
    if (!el) return;
    let data = null;
    try { data = JSON.parse(el.textContent || 'null'); } catch (_) { return; }
    el.remove();
    if (data) applyPartialSeed(data);
  };

  let _announceTimer = 0;
  const announceRoute = (title) => {
    const r = document.getElementById('fui-route-announce');
    if (!r || !title) return;
    if (_announceTimer) { clearTimeout(_announceTimer); _announceTimer = 0; }
    if (r.textContent === title) return;
    r.textContent = '';
    _announceTimer = setTimeout(() => { r.textContent = title; _announceTimer = 0; }, 50);
  };

  const _showNavToast = (msg) => {
    const t = NS.doc.singleton('fui-nav-toast', () => {
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

  const _focusSwapTarget = (el) => {
    if (typeof el.focus !== 'function') return;
    try { el.focus({ preventScroll: true, focusVisible: !_navPointer }); } catch (_) { /* older Safari */ }
  };
  // _navPointer is core's modality record for the navigation being
  // applied, handed over with each delegated load (nav's `pointer`
  // opt). This module never listens for clicks itself: it
  // demand-loads beside the first navigation's fetch, after that
  // click, so its own listener would miss it and the ring would
  // paint on the first mouse navigation.

  const swapAtSlot = (slot, html) => {
    slot.innerHTML = html;
    mergeSeedFromDOM(slot);
    if (NS.scanAndLoadCSS) NS.scanAndLoadCSS(slot);
    _focusSwapTarget(slot);
    return slot;
  };
  const shellEl = (d) => (d || document).querySelector('[data-fui-layout-key], [data-fui-screen-group]');
  const swapShell = (newRoot) => {
    const cur = shellEl() || _mainEl();
    if (!cur || !newRoot) return null;
    const el = document.importNode(newRoot, true);
    cur.replaceWith(el);
    NS.doc.reattach();
    mergeSeedFromDOM(el);
    // The parent: the new shell root itself carries its layout's
    // data-fui-scope, and the scan reads descendants only.
    if (NS.scanAndLoadCSS) NS.scanAndLoadCSS(el.parentNode);
    const m = el.matches('main, [role="main"]') ? el : (el.querySelector('[role="main"]') || el.querySelector('main'));
    if (m && m.focus) _focusSwapTarget(m);
    return el;
  };
  const applyDocShell = (root) => {
    const c = root && (root.matches('[data-fui-lang],[data-fui-skip-label]')
      ? root : root.querySelector('[data-fui-lang],[data-fui-skip-label]'));
    if (c) {
      const lang = c.getAttribute('data-fui-lang');
      if (lang) NS.doc.setHtmlAttr('lang', lang);
      const skip = c.getAttribute('data-fui-skip-label');
      const link = skip && document.querySelector('[data-skip-link]');
      if (link) link.textContent = skip;
    }
  };

  const _settleScroll = (fn) => NS._settleScroll(fn);
  const scrollToHash = () => {
    const id = (location.hash || '').replace(/^#/, '');
    _settleScroll(() => {
      if (id) {
        const el = document.getElementById(id);
        if (el) { el.scrollIntoView({ block: 'start' }); return; }
      }
      window.scrollTo(0, 0);
    });
  };
  const finishNav = (path, prevPath, cached, root, ps) => {
    applyDocShell(root);
    if (!ps) scrollToHash();
    window.dispatchEvent(new CustomEvent('gofastr:navigate', { detail: { path, prevPath, cached, root } }));
    if (ps) {
      _settleScroll(() => {
        if (!restoreScroll(path, ps)) window.scrollTo(ps[0], ps[1]);
      });
    }
  };

  // The commit seam's module twin: same shape as core's _swapCommit
  // (the transition module wraps it when the document declares one).
  const swapCommit = (epoch, from, to, apply, pickSrc) => {
    const T = NS._navHooks.transition;
    if (T) return T.commit(epoch, from, to, apply, pickSrc);
    if (NS._navLive(epoch)) apply();
  };

  const bumpEpoch = () => ++NS._navEpoch;

  // Parse an envelope body into a detached <template> (inert) and
  // collect fills by looping the DIRECT children (no :scope selector).
  // The template addressed by the bare swap key is the primary.
  const parseEnvelope = (html, primaryAddr) => {
    const tpl = document.createElement('template');
    tpl.innerHTML = html;
    const out = { seed: null, primary: { html: '' }, fills: [] };
    for (const el of tpl.content.children) {
      if (el.tagName === 'SCRIPT' && el.id === 'gofastr-signals-partial') {
        out.seed = el;
        continue;
      }
      if (el.tagName !== 'TEMPLATE') continue;
      const addr = el.getAttribute('data-fui-fill');
      if (!addr) continue;
      const fill = { addr, html: el.innerHTML };
      if (addr === primaryAddr) out.primary = fill;
      else out.fills.push(fill);
    }
    return out;
  };

  // Resolve every fill target BEFORE touching the DOM. Any miss means
  // the DOM and the server disagree about the kept layers; recover with
  // a full-page load rather than a half-applied envelope. A target
  // that CONTAINS another target is the same disagreement in a worse
  // costume: applying both writes the inner fill into a node the outer
  // write is about to orphan, so it is a miss too (DESIGN test plan:
  // TestNestedTargetsFullLoad).
  const resolveFillTargets = (fills) => {
    const resolved = [];
    for (const f of fills) {
      const t = findFillTarget(f.addr);
      if (!t) return null;
      resolved.push([t, f]);
    }
    for (let i = 0; i < resolved.length; i++) {
      for (let j = 0; j < resolved.length; j++) {
        if (i !== j && resolved[i][0].contains(resolved[j][0])) return null;
      }
    }
    return resolved;
  };

  // Apply the non-primary fills. P8-B (spike/layout-client): NO
  // hashes, no skip — every kept-layer fill is re-applied on every
  // navigation. Outlet state survives through the LAYER CHAIN (a fill
  // whose DOM must persist lives in a nested layout layer the chain
  // keeps), never through fill bytes.
  const applyFillTargets = (resolved) => {
    for (const [t, f] of resolved) {
      t.innerHTML = f.html;
      if (NS.scanAndLoadCSS) NS.scanAndLoadCSS(t);
    }
  };

  // Apply one envelope: the primary always (the ordinary slot swap,
  // through core's swapAtSlot so seed merge + CSS + focus all run),
  // then the fills, then the detached seed island merged through the
  // shared partial path.
  const applyEnvelope = (slot, primaryHtml, resolved, seedEl) => {
    swapAtSlot(slot, primaryHtml);
    applyFillTargets(resolved);
    if (seedEl) {
      try { applyPartialSeed(JSON.parse(seedEl.textContent || 'null')); } catch (_) { /* malformed island */ }
    }
    return slot;
  };

  // captureLiveFills snapshots every outlet/area OUTSIDE the primary
  // slot (layer 0's fills; an inner layer's outlets travel inside the
  // primary HTML itself), taking the bytes as served. Used by the
  // snapshots; replay then restores them exactly as served for a
  // fetched entry. d, when given, is a parsed document (the P13-A
  // reader reads the fetched one).
  const captureLiveFills = (slot, d) => {
    const fills = [];
    for (const el of (d || document).querySelectorAll('[data-fui-outlet],[data-fui-area]')) {
      if (slot && slot.contains(el)) continue;
      const addr = el.getAttribute('data-fui-outlet') || el.getAttribute('data-fui-area');
      if (!addr) continue;
      fills.push({ addr, html: el.innerHTML });
    }
    return fills;
  };

  // routeSeedIsland materializes a synthesized {p:...} seed as an
  // inert script element (the shape #gofastr-signals-partial has).
  const routeSeedIsland = (seed) => {
    if (!seed) return null;
    const el = document.createElement('script');
    el.type = 'application/json';
    el.id = 'gofastr-signals-partial';
    el.textContent = JSON.stringify(seed);
    return el;
  };

  // P7-B (spike/layout-client): a first paint carries no partial seed
  // island, but its HEAD seed holds the route.* values (SeedFor seeds
  // every referenced name). Synthesize a partial-shaped, route-only
  // seed from it so a Back replay of the boot entry rewrites route
  // state like every other replay (page-scoped keys only — globals
  // keep their first-seen rule).
  // routeSeedFrom folds a parsed FLAT head seed (the #gofastr-signals
  // island) into a partial-shaped {p} carrying only the route.* keys:
  // the flat island mixes scopes (globals beside page-scoped values),
  // and feeding a global through the page arm would clobber a value
  // the user mutated. P7-C: shared by the boot capture (live head) and
  // the cross-chain full-document swap (fetched document's head).
  const routeSeedFrom = (flat) => {
    const p = {};
    for (const k in flat) {
      if (Object.prototype.hasOwnProperty.call(flat, k) && k.indexOf('route.') === 0) p[k] = flat[k];
    }
    return Object.keys(p).length ? { p } : null;
  };
  // routeSeedFromHead reads a document's flat head island; d, when
  // given, is a parsed document (the P13-A reader reads the fetched
  // one).
  const routeSeedFromHead = (d) => {
    const el = (d || document).querySelector('script#gofastr-signals');
    if (!el) return null;
    try { return routeSeedFrom(JSON.parse(el.textContent || '{}')); } catch (_) { return null; }
  };
  // _routeSeedP tracks the route.* values a cache replay must restore.
  // The head island is a FIRST-PAINT artifact — after any SPA
  // navigation the store has moved and the head has not, so reading it
  // back (the old routeSeedFromHead fallback) seeded a replay with the
  // ORIGIN route's values (P13-B: a Back after a re-captured entry
  // showed the boot page's bindings). Truth here instead: the boot
  // head's values first, then every applied page seed that carries
  // route keys REPLACES the subset (SeedFor stamps every referenced
  // name of the route table, absent params included, so the subset is
  // complete for the page it belongs to).
  let _routeSeedP = routeSeedFromHead();
  const captureEnvelopeSnapshot = (slot) => ({
    fills: captureLiveFills(slot),
    seed: document.querySelector('script#gofastr-signals-partial') || routeSeedIsland(_routeSeedP),
  });

  // P13-A (spike/layout-static): read a FULL document as an envelope.
  // A static host serves whole documents and ignores the runtime's
  // request headers, so the non-envelope fetch path derives what a
  // served envelope carries from the fetched document itself, using
  // the same boundary rule the server applies to X-Gofastr-Swap: the
  // deepest layer key present in BOTH the live DOM and the fetched
  // document (their chains diverge below it). That shared layer's
  // content cell takes the document's cell content (the primary);
  // every outlet and area OUTSIDE the swapped slot — in the document —
  // is a fill applied through the same resolveFillTargets /
  // applyEnvelope path a served envelope takes (P8-B: re-applied every
  // navigation, no hashes). The seed rides along too: the document's
  // partial island when it carries one, else its route keys folded
  // from the head island (the P7-C route-only scoping rule). Returns
  // null on any miss (no shared layer, no matching slot); the caller
  // falls back to the plain whole-main swap it always had.
  const readDocEnvelope = (pdoc) => {
    const live = domChainKeys(), doc = domChainKeys(pdoc);
    let d = 0;
    while (d < live.length && d < doc.length && live[d] === doc[d]) d++;
    if (d === 0) return null; // no shared layer: caller's whole-main swap
    const swapKey = live[d - 1];
    const slot = findSlot(swapKey);
    const docSlot = findSlot(swapKey, pdoc);
    if (!slot || !docSlot) return null;
    // captureLiveFills' walk, on the fetched document: everything the
    // chain keeps above the boundary travels as a fill.
    const seed = pdoc.querySelector('script#gofastr-signals-partial') || routeSeedIsland(routeSeedFromHead(pdoc));
    return {
      swapKey, slot, html: docSlot.innerHTML, fills: captureLiveFills(docSlot, pdoc), seed,
      title: pdoc.querySelector('title')?.textContent || document.title,
    };
  };

  // ─── Element scroll anchors (P12, spike/layout-motion) ───────────────
  //
  // Keyed by PATH (the module reads location directly; core's
  // entry-id store is not exported), recorded at every leave: the
  // NS._pushURL wrap covers clicks and programmatic pushes, the
  // popstate listener covers history moves.
  //
  // Scroll restore covers ELEMENT scroll too, not just the window. On
  // leave (popstate / a push out) the runtime records the window
  // position AND every element with a non-zero scrollTop/scrollLeft,
  // keyed by a DOM path anchored at the nearest layout layer: paths
  // below a layer key survive that layer's own swap, which is exactly
  // the Back case (the kept layers' panes are re-applied from the
  // cached envelope at scrollTop 0 and need their position back).
  //
  // P12-B: beside the pixel offsets each recorded position also
  // carries an ANCHOR — the first element visible at the top of the
  // window/pane with a stable identity (id, else data-key, else its
  // DOM path) and its offset from that top — and the restore scrolls
  // so the anchor sits at the same offset again, falling back to the
  // pixel offset when the anchor is gone. The anchor survives content
  // ABOVE the position changing height on Back; pixels do not.

  // _domPath: steps joined on '|', walking UP from the element to the
  // nearest [data-fui-layout-key] ancestor ('K<key>') or the body
  // ('B'). A step is TAG+occurrence among same-tag siblings. The
  // layout key makes the path addressable without a CSS selector
  // (keys contain ':' and '/', selector-hostile) and stable across the
  // layer's own content swap.
  const _domPath = (el) => {
    const steps = [];
    for (let n = el; n && n.nodeType === 1 && n !== document.body; n = n.parentElement) {
      if (n.hasAttribute && n.hasAttribute('data-fui-layout-key')) {
        return 'K' + n.getAttribute('data-fui-layout-key') + '|' + steps.join('|');
      }
      let i = 0;
      for (let s = n; s; s = s.previousElementSibling) if (s.tagName === n.tagName) i++;
      steps.unshift(n.tagName + i);
    }
    return 'B|' + steps.join('|');
  };
  // _pathEl resolves a _domPath back to a live element, or null (the
  // content above changed shape; the caller skips that pane).
  const _pathEl = (path) => {
    const steps = path.split('|');
    let node = document.body;
    if (steps[0].charAt(0) === 'K') {
      const key = steps[0].slice(1);
      node = null;
      for (const el of document.querySelectorAll('[data-fui-layout-key]')) {
        if (el.getAttribute('data-fui-layout-key') === key) { node = el; break; }
      }
      if (!node) return null;
    }
    for (let i = 1; i < steps.length && node; i++) {
      const tag = steps[i].replace(/\d+$/, '');
      const idx = +steps[i].slice(tag.length);
      let k = 0;
      node = Array.prototype.find.call(node.children, (c) => c.tagName === tag && ++k === idx) || null;
    }
    return node;
  };
  // _anchorId is an element's stable identity for anchoring: '#id',
  // '[data-key]', else its DOM path (path identity is no stronger than
  // the pixel record, but costs nothing).
  const _anchorId = (el) => {
    const id = el.getAttribute('id');
    if (id) return '#' + id;
    const k = el.getAttribute('data-key');
    if (k) return '[' + k + ']';
    return _domPath(el);
  };
  // _anchorEl resolves an anchor identity back to a live element.
  // data-key is matched by walking (values are DOM input; building a
  // selector from them would need escaping).
  const _anchorEl = (a) => {
    if (!a || !a.id) return null;
    if (a.id.charAt(0) === '#') return document.getElementById(a.id.slice(1));
    if (a.id.charAt(0) === '[') {
      const key = a.id.slice(1, -1);
      for (const el of document.querySelectorAll('[data-key]')) {
        if (el.getAttribute('data-key') === key) return el;
      }
      return null;
    }
    return _pathEl(a.id);
  };
  // _anchorAt finds the element the user's position is "at": the first
  // element in document order whose content crosses the given
  // viewport-relative top edge, then DESCENDED into the deepest child
  // that still crosses it — the innermost element (a row, a paragraph),
  // never the containers around it (a 200-row nav also "crosses" the
  // edge and is useless as a place marker). Returns its stable identity
  // and its offset from the edge.
  const _crosses = (el, edgeY) => {
    const r = el.getBoundingClientRect();
    if (r.height <= 0 || r.bottom <= edgeY || r.top > edgeY + 8) return false;
    // A scrolled-out row is not a window anchor, even when its unclipped
    // rectangle crosses the window edge.
    for (let p = el.parentElement; p && p !== document.documentElement; p = p.parentElement) {
      if (getComputedStyle(p).overflowY !== 'visible') {
        const clip = p.getBoundingClientRect();
        if (clip.bottom <= edgeY || clip.top > edgeY + 8) return false;
      }
    }
    const pos = getComputedStyle(el).position;
    return pos !== 'fixed' && pos !== 'sticky';
  };
  const _anchorAt = (edgeY) => {
    let cand = null;
    for (const el of document.querySelectorAll('main *, [data-fui-outlet] *, [data-fui-area] *')) {
      if (_crosses(el, edgeY)) { cand = el; break; }
    }
    if (!cand) return null;
    for (;;) {
      let next = null;
      for (const c of cand.children) {
        if (_crosses(c, edgeY)) { next = c; break; }
      }
      if (!next) break;
      cand = next;
    }
    return { id: _anchorId(cand), off: Math.round(cand.getBoundingClientRect().top - edgeY) };
  };

  // routeInfo resolves a path against the RAW manifest
  // (window.__gofastr_routes — the kernel's route table keeps only the
  // fields the plain navigator reads) and captures the dynamic segments
  // (the arm core's routeMatch leaves out). The loading and parts
  // modules read their per-route fields (loading, deferred) off the
  // entry it returns. substTmpl fills a manifest-carried template with
  // the params; names the pattern did not capture stay literal.
  const _segName = (seg) => seg.slice(1).split(':')[0].replace(/\*$/, '');
  const routeInfo = (path) => {
    const clean = path.split('?')[0].split('#')[0];
    const list = window.__gofastr_routes || [];
    for (const e of list) {
      const pattern = (e && (e.path || e.Path)) || '';
      if (pattern === clean) return { entry: e, params: null };
    }
    for (const e of list) {
      const pattern = (e && (e.path || e.Path)) || '';
      if (!pattern.includes(':')) continue;
      const parts = clean.split('/').filter(Boolean);
      const pp = pattern.split('/').filter(Boolean);
      if (pattern.includes('*') ? parts.length < pp.length : pp.length !== parts.length) continue;
      if (!pp.every((seg, i) => seg.startsWith(':') || seg === parts[i])) continue;
      const params = {};
      const catchAll = pp.length && pp[pp.length - 1].endsWith('*');
      const upto = catchAll ? pp.length - 1 : pp.length;
      for (let i = 0; i < upto; i++) {
        if (pp[i].startsWith(':')) params[_segName(pp[i])] = parts[i];
      }
      if (catchAll) params[_segName(pp[pp.length - 1])] = parts.slice(pp.length - 1).join('/');
      return { entry: e, params };
    }
    return null;
  };
  const routeParams = (path) => {
    const m = routeInfo(path);
    return m && m.params;
  };
  const substTmpl = (tmpl, params) => (params && tmpl.includes('{'))
  ? tmpl.replace(/\{(\w+)\}/g, (m, n) => (Object.prototype.hasOwnProperty.call(params, n) ? params[n] : m))
  : tmpl;

  // The anchor + element-pane records (P12) live here, keyed by the
  // same history entry ids core's pixel store uses. Bare pixels
  // survive reloads through core's sessionStorage; anchors persist
  // under their own key at capture time (captures ride leave/push
  // choke points, low frequency, no throttle needed).
  const ANCHOR_KEY = 'gofastr:anchors';
  let _anchors = (() => {
    try { return JSON.parse(sessionStorage.getItem(ANCHOR_KEY)) || {}; } catch (_) { return {}; }
  })();
  // ─── The layout navigator ────────────────────────────────────────────
  //
  // The seam core's loadPage delegates to once this module is loaded.
  // It is the plain navigator plus everything a document with outlets
  // needs: the fills envelope, the busy marks, the leave-capture,
  // loading content, parallel parts, view transitions. Everything it
  // borrows from core is an NS._ export (the cache, the shared tail,
  // the commit router, the seed merge); everything else lives here.
  const _mainEl = () => document.querySelector('[role="main"]') ?? document.querySelector('main');
  // _liveDomPath tracks the page the DOM shows so the leave-capture
  // below only fires when the leaving page is what the DOM holds. A
  // NOT-OK answer (a 404 outlet outcome is request-derived — ErrNoFill
  // declines per request) leaves it STALE on purpose: the capture then
  // misses, so a 404 page is never cached by any path and Back
  // refetches a fresh verdict (DESIGN "404 outlet" / apply step 13).
  // What the DOM actually shows. The module only ever loads before
  // its first navigation applies (the hand-off runs it, or the parts
  // module's deferred walk loads it at boot), so the DOM shows the
  // SERVED page — read off the performance navigation entry, which a
  // click's pushState cannot rewrite the way it rewrites location.
  let _liveDomPath = (() => {
    try {
      const n = performance.getEntriesByType('navigation')[0];
      if (n && n.name) { const u = new URL(n.name); return u.pathname + u.search; }
    } catch (_) { /* ancient browsers: location is the best guess */ }
    return location.pathname + location.search;
  })();
  const _pending = new Set();
  const _done = (path, prevPath, cached, root, ps, notOk) => {
    if (!notOk) _liveDomPath = path;
    finishNav(path, prevPath, cached, root, ps);
  };
  const sseMeta = (d) => (d || document).querySelector('meta[name="gofastr-sse"]');
  const respIsHTML = (r) => (r.headers.get('Content-Type') || '').toLowerCase().startsWith('text/html');
  const _resolvePath = (href) => {
    try {
      const u = new URL(href, location.href);
      return u.pathname + u.search;
    } catch (_) { return href; }
  };
  const routeLayouts = (path) => {
    const m = routeInfo(path);
    const ls = ((m && m.entry && m.entry.layouts) || []);
    return ls.some((k) => k.includes('{')) ? ls.map((k) => substTmpl(k, m.params)) : ls;
  };
  const sharedDepth = (layouts) => {
    const dom = domChainKeys();
    let d = 0;
    while (d < dom.length && d < layouts.length && layouts[d] && dom[d] === layouts[d]) d++;
    return d;
  };

  // retry re-enters navigation for a redirect answer or a full-load
  // recovery. The transition module's pick-decided rule (a Back edge
  // must not be overridden by the retry's own answer) survives the
  // re-entry through the stick seam; the DIRECTION does not — a retry
  // leg commits as 'forward', exactly as the threaded calls it
  // replaces behaved.
  const retry = (path, o) => {
    _pend.delete(path);
    return nav(path, o);
  };
  const nav = async (path, o) => {
    const { bypassCache = false, forceFull = false, from = null, restore = null, pointer = false } = o || {};
    // Core's modality record for THIS navigation, bound at entry
    // before the first await: the swap tail focuses with it.
    _navPointer = pointer;
    // Dedup in-flight nav, but only while path is still the
    // destination (the epoch makes a stale fetch harmless).
    if (_pend.has(path) && NS.currentPath === path) return;
    _pend.add(path);
    const myEpoch = bumpEpoch();
    const H = NS._navHooks;
    const P = H.parts;
    // The destination's deferred outlets (manifest `deferred`): one
    // part request per address beside the page fetch; begin also
    // aborts the older navigation's parts (Back's leave-capture must
    // never race a part apply). The state exists early so the loading
    // scheduler can hand each deferred region's entry to the part
    // that owns it. A TAKEOVER navigation (the plain navigator started
    // this fetch before the module loaded and hands it over) launched
    // no parts — its page request sent no X-Gofastr-Defer, the
    // deferred outlets render inline with the page.
    // The leave record: at this navigation's entry the DOM (and its
    // scroll) still shows the page being left — _liveDomPath, corrected
    // or not, is what the leave-capture and the anchors must key on.
    // The wrapped NS._pushURL and the popstate listener this replaced
    // never saw core's closure pushes; recording here sees every leave.
    recordScroll(_liveDomPath);
    const parts = P ? P.begin(path, myEpoch) : null;
    const prevPath = from || NS.currentPath;

    NS._setCurrentPath(path);
    // Consume the popstate's restore target into this navigation.
    let ps = restore;
    if (!ps) ps = null;
    NS.doc.setHtmlAttr('aria-busy', 'true');
    // P4-B: mark every region this navigation can change — every
    // outlet and area of the layers the DOM will keep, plus the swap
    // slot (the boundary the server is about to name). The dim itself
    // is theme CSS with a transition delay; the mark is what a screen
    // reader is told while the old content stays visible.
  const marks = [];
  let load = null;
  // _applied records the regions a commit's apply REPLACED on the
  // navigation's loading state (its sweep and finish need the set: a
  // shown entry whose region was not replaced restores its park).
  const _applied = (slot, resolved) => {
    if (!load) return;
    load.applied.add(slot);
    if (resolved) for (const [t] of resolved) load.applied.add(t);
  };
    const busyDepth = sharedDepth(routeLayouts(path));
    if (busyDepth > 0) {
      const keys = domChainKeys();
      for (const el of document.querySelectorAll('[data-fui-outlet],[data-fui-area]')) {
        const addr = el.getAttribute('data-fui-outlet') || el.getAttribute('data-fui-area') || '';
        for (let i = 0; i < busyDepth; i++) {
          if (addr.startsWith(keys[i] + '#') || addr.startsWith(keys[i] + '~')) {
            el.setAttribute('aria-busy', 'true');
            marks.push(el);
            break;
          }
        }
      }
      const busySlot = findSlot(keys[busyDepth - 1]);
      if (busySlot) {
        busySlot.setAttribute('aria-busy', 'true'); marks.push(busySlot);
        // P13-B: refresh the LEAVING page's cache entry keyed at THIS
        // navigation's swap layer, before anything touches the DOM.
        // An entry captured shallower replays by replacing every layer
        // below its key — a Back whose boundary is deeper would
        // destroy the kept layers' DOM (the /items list pane and its
        // typed filter). The parts record rides it too: which
        // addresses the leaving page deferred and what landed.
        // (A page whose own answer was NOT OK keeps _liveDomPath
        // stale, so this condition misses and it is never captured.)
        if (prevPath && prevPath !== path && _liveDomPath === prevPath) {
          const prevEntry = getCachedScreen(prevPath);
          const lsnap = captureEnvelopeSnapshot(busySlot);
          const ps0 = P && P.leaveSnap(prevEntry);
          cacheScreen(prevPath, busySlot.innerHTML, document.title, keys[busyDepth - 1],
            lsnap.fills, lsnap.seed, ps0 && ps0.partAddrs, ps0 && ps0.parts);
        }
        // The loading module starts loading at evaluation; the first
        // navigation may beat its script. The marks paint now (the
        // dim does not wait for it); only the SCHEDULER needs the
        // module, so the wait sits here, behind the supersede rule.
        if (_loadingReady && !H.loading) {
          await _loadingReady;
          if (!NS._navLive(myEpoch)) return;
        }
        // P9: the marked regions' loading content schedules through
        // the loading module.
        load = H.loading?.schedule({ marks, path, epoch: myEpoch, parts }) || null;
      }
    }
    // Bind the transition module's recorded direction and pick-edge to
    // THIS navigation's epoch — before any await, same as the plain
    // navigator's entry; the module's commit reads it back by epoch.
    // A page that never loads the transition module records nothing
    // and every navigation commits as 'forward'.
    H.transition?.take(myEpoch);
    try {
      const layouts = routeLayouts(path);
      const cached = (bypassCache || forceFull) ? null : getCachedScreen(path);
      if (cached) {
        const cx = cached;
        const slot = cached.layer
          ? findSlot(cached.layer)
          : ((layouts.length === 0 && domChainKeys().length === 0) ? _mainEl() : null);
        if (slot && (cx.fills || cx.partAddrs)) {
          // Fills envelope entry: replay the whole snapshot. A fill
          // target missing from the DOM is the same deploy-skew class
          // as a missing layer: full load. Resolve every target
          // BEFORE committing so the abort decision never lands inside
          // a transition's update callback.
          // A deferred entry replays the same way: apply what it
          // holds — the held parts land beside the fills, the missing
          // parts are requested fresh, their regions showing the
          // loading content the cached bytes carry.
          const resolved = resolveFillTargets(cx.fills || []);
          const heldAddrs = cx.partAddrs
            ? cx.partAddrs.filter((a) => cx.parts.has(a))
            : [];
          if (!resolved) {
            _pend.delete(path);
            return retry(path, { bypassCache: true, forceFull: true, from: prevPath, restore: ps });
          }
          document.title = cached.title;
          announceRoute(cached.title);
          if (parts && P) {
            P.launch(parts, cx.partAddrs
              ? cx.partAddrs.filter((a) => !heldAddrs.includes(a))
              : []);
            parts.pending = true;
          }
          swapCommit(myEpoch, prevPath, path, () => {
            applyEnvelope(slot, cached.html, resolved, cx.seed);
            _applied(slot, resolved);
            if (parts && P && P.commit(parts, true)) {
              for (const a of heldAddrs) P.applyNow(parts, a, { html: cx.parts.get(a), seed: null });
              P.flush(parts);
            }
            _done(path, prevPath, true, slot, ps);
          }, cached);
          return;
        }
        if (slot) {
          document.title = cached.title;
          announceRoute(cached.title);
          swapCommit(myEpoch, prevPath, path, () => {
            _applied(slot);
            _done(path, prevPath, true, swapAtSlot(slot, cached.html), ps);
          }, cached);
          return;
        }
      }

      // Prefetched entry (preload module): an envelope body parses into
      // primary + fills + seed, the same shape a fetched partial
      // applies; a plain one swaps as before. A prefetched page was
      // deferred: the missing parts launch now — the prefetch itself
      // only ever fetched the page.
      if (!cached && !bypassCache && !forceFull && NS._takePrefetched) {
        const pf = NS._takePrefetched(path);
        if (pf) {
          const slot = pf.layer ? findSlot(pf.layer) : ((layouts.length === 0) ? _mainEl() : null);
          if (slot) {
            let body = pf.html, pfFills = null, pfSeed = null;
            if (pf.envelope) {
              const snap = parseEnvelope(pf.html, pf.layer);
              body = snap.primary.html;
              pfFills = snap.fills;
              pfSeed = snap.seed;
            }
            const pfResolved = pfFills ? resolveFillTargets(pfFills) : null;
            document.title = pf.title;
            announceRoute(pf.title);
            if (parts && P) {
              P.launch(parts, parts.addrs);
              parts.pending = true;
            }
            swapCommit(myEpoch, prevPath, path, () => {
              cacheScreen(path, body, pf.title, pf.layer,
                (pfFills && pfResolved) ? pfFills : null, pfSeed || null,
                parts ? parts.addrs : null, parts ? parts.parts : null);
              if (pfResolved) applyEnvelope(slot, body, pfResolved, pfSeed);
              else swapAtSlot(slot, body);
              _applied(slot, pfResolved);
              if (parts && P && P.commit(parts, true)) P.flush(parts);
              _done(path, prevPath, false, slot, ps);
            }, null);
            return;
          }
        }
      }

      // Cross-chain nav (no shared root): fetch the FULL page and
      // replace the shell. A whole-document answer renders every fill
      // INLINE (deferral speeds client navigations only): the parts
      // are redundant, abort them now, while the old DOM still stands,
      // so their regions restore rather than fight the shell swap.
      if (forceFull || ((layouts.length > 0 || domChainKeys().length > 0) && sharedDepth(layouts) === 0)) {
        if (P && parts) P.kill(parts);
        const fr = await fetch(path);
        if (!NS._navLive(myEpoch)) return;
        if (!fr.ok && !respIsHTML(fr)) throw new Error(`HTTP ${fr.status}`);
        NS._inval(fr);
        const pdoc = new DOMParser().parseFromString(await fr.text(), 'text/html');
        if (!NS._navLive(myEpoch)) return;
        let dest = path;
        if (fr.redirected && fr.url) dest = _resolvePath(fr.url);
        if (dest !== path) { NS._pushURL(dest, { replace: true }); NS._setCurrentPath(dest); }
        const t = pdoc.querySelector('title')?.textContent || document.title;
        document.title = t;
        announceRoute(t);
        const fm = sseMeta(pdoc), lm = sseMeta();
        if (fm && lm) lm.setAttribute('content', fm.getAttribute('content'));
        const nm = pdoc.querySelector('main');
        swapCommit(myEpoch, prevPath, dest, () => {
          // The whole-shell swap: cross-chain replaces the outermost
          // shell, falling back to whole-main for a layout-less origin.
          const el = swapShell(shellEl(pdoc) || nm);
          // The fetched head's route keys fold through the same atomic
          // merge (route-only for the scoping reason on routeFold;
          // non-route page-scoped signals keep their correct SSR
          // stamps — pre-existing behavior, recorded). swapShell's
          // merge finds no PARTIAL island in a full document, and the
          // live head still carries the ORIGIN's seed — without this,
          // the store's route.* stays one chain behind the DOM after
          // every cross-chain swap.
          const hEl = pdoc.querySelector('script#gofastr-signals');
          if (hEl) {
            try {
              const rsd = routeSeedFrom(JSON.parse(hEl.textContent || '{}'));
              if (rsd) applyPartialSeed(rsd);
            } catch (_) { /* a malformed head island changes nothing */ }
          }
          if (fr.ok) {
            const fsnap = captureEnvelopeSnapshot(_mainEl());
            cacheScreen(dest, nm ? nm.innerHTML : '', t,
              nm ? (nm.getAttribute('data-fui-layout-slot') || '') : '', fsnap.fills, fsnap.seed);
          }
          _done(dest, prevPath, false, el || _mainEl(), ps, !fr.ok);
        }, null);
        return;
      }

      // Partial fetch. X-Gofastr-Fills negotiates the envelope (the
      // module being loaded is what makes the header truthful: it
      // loads exactly when the document holds an outlet or area
      // marker). X-Gofastr-Defer tells the server the deferred
      // outlets' loaders can be skipped: their parts are already in
      // flight beside this page request. fetchOpts carries priority
      // 'high', cache:'no-store' (the parts and the page fetch the
      // SAME URL, and Chrome serializes same-URL fetches behind its
      // HTTP-cache write lock), and the abort signal.
      const hdrs = { 'X-Gofastr-Navigate': '1', 'X-Gofastr-Fills': '2' };
      const fromPath = (prevPath || '').split('?')[0];
      if (fromPath && routeInfo(fromPath)) hdrs['X-Gofastr-From'] = fromPath;
      if (parts) {
        hdrs['X-Gofastr-Defer'] = '1';
        P.launch(parts, parts.addrs);
      }
      const resp = await fetch(path, Object.assign({ headers: hdrs }, parts && parts.fetchOpts));
      if (!NS._navLive(myEpoch)) return;
      const rs = resp.headers.get('X-Gofastr-Session'), rm = rs && sseMeta();
      if (rm) rm.setAttribute('content', rm.getAttribute('content').replace(/([?&]session=)[^&]*/, '$1' + rs));
      // A session change is an identity change: one identity's pages
      // must never replay for another.
      if (rs) NS.invalidate('*');
      if (!resp.ok && !respIsHTML(resp)) throw new Error(`HTTP ${resp.status}`);
      const notOk = !resp.ok;
      NS._inval(resp);
      const redirectTo = resp.headers.get('X-Gofastr-Location');
      if (redirectTo) {
        NS._pushURL(redirectTo, { replace: true });
        NS._setCurrentPath(redirectTo);
        _pend.delete(path);
        NS.doc.removeHtmlAttr('aria-busy');
        // A redirect discards the parts and their buffer: nothing this
        // navigation fetched for `path` may land.
        if (P && parts) P.kill(parts);
        return retry(redirectTo, { bypassCache, from: prevPath, restore: ps });
      }
      // Every response body is read with text(): Chrome keeps no body
      // for a fetch() read through a stream reader and reports the
      // request as net::ERR_ABORTED in DevTools even when the page
      // read it to the end.
      const html = await resp.text();
      if (!NS._navLive(myEpoch)) return;
      // P9-A/P9-ANIM: honor Min across every shown loading region and
      // run each one's exit animation — ONE hold for the whole apply
      // transaction (the seed merge is global). A fast response that
      // beat every After timer holds and exits nothing.
      if (load) {
        await load.wait();
        if (!NS._navLive(myEpoch)) return;
      }

      // The envelope answer: parse inert, resolve every target before
      // touching the DOM (any miss recovers with a full load), apply,
      // cache the snapshot, finish. The commit applies the page, then
      // the buffered parts: parts that landed first wait in the
      // buffer, so nothing they carry paints ahead of the page — an
      // outlet below the swap boundary only exists after the commit.
      if (resp.headers.get('X-Gofastr-Envelope') === '2') {
        const envSwapKey = resp.headers.get('X-Gofastr-Swap') || '';
        const snap = parseEnvelope(html, envSwapKey);
        const envSlot = envSwapKey ? findSlot(envSwapKey) : null;
        const resolved = envSlot ? resolveFillTargets(snap.fills) : null;
        if (!envSlot || !resolved) {
          _pend.delete(path);
          return retry(path, { bypassCache: true, forceFull: true, from: prevPath, restore: ps });
        }
        const envTitle = decodeURIComponent(resp.headers.get('X-Gofastr-Title') || document.title);
        document.title = envTitle;
        announceRoute(envTitle);
        if (parts) parts.pending = true;
        swapCommit(myEpoch, prevPath, path, () => {
          applyEnvelope(envSlot, snap.primary.html, resolved, snap.seed);
          _applied(envSlot, resolved);
          if (load) load.sweep();
          if (parts && P && P.commit(parts, !notOk)) P.flush(parts);
          if (!notOk) cacheScreen(path, snap.primary.html, envTitle, envSwapKey,
            snap.fills, snap.seed,
            parts ? parts.addrs : null, parts ? parts.parts : null);
          _done(path, prevPath, false, envSlot, ps, notOk);
        }, resp);
        return;
      }

      let title, body, partial = resp.headers.get('X-Gofastr-Partial') === 'true';
      let swapKey = (partial && resp.headers.get('X-Gofastr-Swap')) || '';
      if (partial) {
        title = decodeURIComponent(resp.headers.get('X-Gofastr-Title') || document.title);
        body = html;
      } else {
        const pdoc = new DOMParser().parseFromString(html, 'text/html');
        // A full document — a static host that ignores the navigate
        // headers — is read as an envelope first: swap at the deepest
        // shared layer, outlets outside it as fills, so a whole-document
        // answer behaves exactly like the partial it should have been.
        // Any miss falls through to the whole-main swap below.
        const docEnv = readDocEnvelope(pdoc);
        const docResolved = docEnv ? resolveFillTargets(docEnv.fills) : null;
        if (docEnv && docResolved) {
          let dest = path;
          if (resp.redirected && resp.url) dest = _resolvePath(resp.url);
          if (dest !== path) { NS._pushURL(dest, { replace: true }); NS._setCurrentPath(dest); }
          document.title = docEnv.title;
          announceRoute(docEnv.title);
          if (parts) parts.pending = true;
          swapCommit(myEpoch, prevPath, dest, () => {
            applyEnvelope(docEnv.slot, docEnv.html, docResolved, docEnv.seed);
            _applied(docEnv.slot, docResolved);
            if (load) load.sweep();
            if (P && parts) { parts.committed = true; P.kill(parts); }
            if (!notOk) cacheScreen(dest, docEnv.html, docEnv.title, docEnv.swapKey, docEnv.fills, docEnv.seed);
            _done(dest, prevPath, false, docEnv.slot, ps, notOk);
          }, null);
          return;
        }
        const nm = pdoc.querySelector('main');
        title = pdoc.querySelector('title')?.textContent || document.title;
        body = nm?.innerHTML ?? '';
        swapKey = nm?.getAttribute('data-fui-layout-slot') || '';
      }
      const slot = swapKey ? findSlot(swapKey) : ((layouts.length === 0) ? _mainEl() : null);
      if (!slot) {
        _pend.delete(path);
        return retry(path, { bypassCache: true, forceFull: true, from: prevPath, restore: ps });
      }
      // Version rule (DESIGN "Mixed versions"): a partial WITHOUT
      // X-Gofastr-Envelope is an old server's answer — it carries no
      // fills, so applying it would swap the primary and leave every
      // outlet and area OUTSIDE the target slot stale with no repair.
      // A same-chain answer from a NEW server always carries the
      // envelope when the kept layers hold outlets or areas (the
      // header above is exactly that negotiation), so markers outside
      // the slot here means the server cannot refresh them: recover
      // with a full-document load instead of applying.
      if (partial) {
        for (const el of document.querySelectorAll('[data-fui-outlet],[data-fui-area]')) {
          if (!slot.contains(el)) {
            _pend.delete(path);
            return retry(path, { bypassCache: true, forceFull: true, from: prevPath, restore: ps });
          }
        }
      }
      document.title = title;
      announceRoute(title);
      if (parts) parts.pending = true;
      swapCommit(myEpoch, prevPath, path, () => {
        const root = swapAtSlot(slot, body);
        _applied(slot);
        if (load) load.sweep();
        if (P) P.land(parts, notOk);
        if (!notOk) cacheScreen(path, body, title, swapKey);
        _done(path, prevPath, false, root, ps, notOk);
      }, resp);
    } catch (err) {
      if (!NS._navLive(myEpoch)) return;
      // Hard rule 4, no location.href fallback. Surface a toast and
      // stay on the current page; the URL has already been pushState'd
      // by the click handler so revert it.
      console.warn('[gofastr] Nav failed:', err);
      _showNavToast(err && /^HTTP \d+$/.test(err.message)
        ? 'Could not load ' + path + ' (' + err.message + ')'
        : 'Could not load ' + path + ' — check your connection');
      NS._pushURL(prevPath || location.pathname, { replace: true });
      NS._setCurrentPath(prevPath);
    } finally {
      _pend.delete(path);
      // P9-A: a navigation that owns the epoch RESTORES (failure,
      // abort); a superseded one only settles — the newer navigation
      // owns the regions now.
      if (load) load.finish(NS._navLive(myEpoch));
      // A navigation that neither committed nor has a commit pending
      // discards its parts: the buffer dies and the regions restore.
      if (P) P.finish(parts);
      for (const el of marks) el.removeAttribute('aria-busy');
      if (NS._navLive(myEpoch)) NS.doc.removeHtmlAttr('aria-busy');
    }
  };

  // recordScroll captures the leaving page's anchors and element
  // panes under its PATH. The NS._pushURL wrap (clicks, programmatic
  // pushes) and a popstate listener (history moves) cover every leave;
  // the boot/pending page is recorded once at evaluation.
  // _paneKey: a pane's stable identity — its id, else data-key, else
  // the DOM path. A refetched page rebuilds the shell; the path steps
  // can shift with the fetched document's nesting, but the id
  // survives, so the restore prefers it.
  const _paneKey = (el) => {
    const id = el.getAttribute && el.getAttribute('id');
    if (id) return '#' + id;
    const k = el.getAttribute && el.getAttribute('data-key');
    if (k) return '[' + k + ']';
    return _domPath(el);
  };
  const _paneEl = (key) => {
    if (!key) return null;
    if (key.charAt(0) === '#') return document.getElementById(key.slice(1));
    if (key.charAt(0) === '[') {
      const k = key.slice(1, -1);
      for (const el of document.querySelectorAll('[data-key]')) {
        if (el.getAttribute('data-key') === k) return el;
      }
      return null;
    }
    return _pathEl(key);
  };

  const recordScroll = (path) => {
    if (!path) return;
    const rec = { a: _anchorAt(0), e: {} };
    for (const el of document.querySelectorAll('*')) {
      if (el.scrollTop || el.scrollLeft) {
        const r = el.getBoundingClientRect();
        rec.e[_paneKey(el)] = {
          t: el.scrollTop | 0, l: el.scrollLeft | 0,
          a: _anchorAt(r.top),
        };
      }
    }
    _anchors[path] = rec;
    const ids = Object.keys(_anchors);
    if (ids.length > 50) {
      ids.sort();
      for (const id2 of ids.slice(0, ids.length - 50)) delete _anchors[id2];
    }
    try { sessionStorage.setItem(ANCHOR_KEY, JSON.stringify(_anchors)); } catch (_) {}
  };

  // restoreScroll: anchor-first restore for the arriving path (the
  // element panes re-take their recorded offsets; the window falls
  // back to core's pixels when the anchor is gone).
  const restoreScroll = (path, ps) => {
    const rec = _anchors[path] || {};
    if (rec.a) {
      const wEl = _anchorEl(rec.a);
      if (wEl) {
        const y = wEl.getBoundingClientRect().top + scrollY - (rec.a.off || 0);
        window.scrollTo(ps[0], Math.max(0, y));
      } else {
        window.scrollTo(ps[0], ps[1]);
      }
    } else {
      window.scrollTo(ps[0], ps[1]);
    }
    if (rec.e) {
      for (const p in rec.e) {
        if (!Object.prototype.hasOwnProperty.call(rec.e, p)) continue;
        const r0 = rec.e[p];
        const el = _paneEl(p);
        if (!el) continue;
        const aEl = _anchorEl(r0.a);
        if (aEl) {
          const r = el.getBoundingClientRect();
          el.scrollTop = el.scrollTop + (aEl.getBoundingClientRect().top - r.top) - (r0.a.off || 0);
        } else if (Array.isArray(r0)) {
          el.scrollTop = r0[0]; el.scrollLeft = r0[1];
        } else {
          el.scrollTop = r0.t; el.scrollLeft = r0.l;
        }
      }
    }
    return true;
  };


  NS._navHooks.envelope = {
    // The navigator seam (frag/nav.js loadPage delegates here).
    nav,
    // The parser/targeter the parts module shares (deferred outlets
    // are outlets: the parts module never loads without this one).
    parse: parseEnvelope,
    target: findFillTarget,
    // The client's share of param-keyed layer keys and deferred
    // addresses (spike/layout-resolve): resolve the path against the
    // manifest's patterns and substitute. Never matches constraints:
    // the server stays the matcher.
    subst: (tmpl, path) => substTmpl(tmpl, routeParams(path)),
    // The RAW manifest entry for a path (plus its captured params): the
    // loading and parts modules read their per-route fields off it.
    manifest: routeInfo,
    // The seed merge and the cache read, module-to-module (the parts
    // module merges a part's seed and records landed parts on this
    // cache's entries; core exports neither).
    applySeed: applyPartialSeed,
    cacheGet: getCachedScreen,

    // Restore a recorded state after a swap — every position
    // ANCHOR-FIRST (scroll so the anchor sits at its recorded offset —
    // survives content above changing height), falling back to the
    // recorded pixels when the anchor is gone. ps is the bare [x,y]
    // from core's pixel store; the anchor record belongs to the
    // arriving PATH. Returns true (handled).
    restore(path, ps) { return restoreScroll(path, ps); },

    // Seam (frag/nav.js applyPartialSeed): fold a merged page seed's
    // route keys into the replay truth.
    routeFold(page) {
      const rs = routeSeedFrom(page);
      if (rs) _routeSeedP = rs;
    },
  };

  // ─── The module's attachment ────────────────────────────────────────
  //
  // The loading module's trigger: its scheduler is called only by
  // THIS navigator, so the template marker is meaningless without it
  // — a page with no outlet marker loads nothing at boot for a
  // template it can never schedule. Started at evaluation (not in
  // frag/boot.js) so the very first navigation's scheduler finds it
  // (the wait above).
  const _loadingReady = document.querySelector('template[data-fui-loading]')
    ? NS.loadModule('loading').catch(() => null) : null;

  // The module loads before any of its navigations applies, so the DOM
  // shows the served page: capture its entry WITH fills (so a Back to
  // it replays them) and record its scroll anchors, both keyed under
  // the served path — currentPath may already name a hand-off's pushed
  // destination, and keying under it would replay the served page's
  // bytes as the destination (TestHandoffDoubleClickCacheClean).
  {
    const m = document.querySelector('[role="main"]') ?? document.querySelector('main');
    if (m) {
      const snap = captureEnvelopeSnapshot(m);
      cacheScreen(_liveDomPath, m.innerHTML, document.title,
        m.getAttribute('data-fui-layout-slot') || '', snap.fills, snap.seed);
    }
    recordScroll(_liveDomPath);
  }

  // Invalidation reaches this module's cache through the public API
  // (rpc/widgets/intercept call NS.invalidate).
  const _origInval = NS.invalidate;
  NS.invalidate = function (...sels) {
    for (const s of sels) {
      if (s === '*') { screenCache.clear(); break; }
      if (!s || s[0] !== '/') continue;
      if (s.includes('?')) { screenCache.delete(s); continue; }
      for (const k of screenCache.keys()) if (k === s || k.startsWith(s + '?')) screenCache.delete(k);
    }
    return _origInval.apply(this, arguments);
  };

  (NS.loadedModules ||= {}).envelope = true;
})();
