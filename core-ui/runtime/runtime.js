// GoFastr Core-UI Runtime v0.4, ES2020+ (composed: full = kernel+signals+nav+widgets-boot)
// Assembled by the Go composer (core-ui/runtime/runtime.go composeFull) from
// core-ui/runtime/frag/*.js. This file is the on-disk canonical form the gate
// tests scan (fragment_map_test.go / integrity_test.go read it via os.ReadFile);
// RuntimeJS() serves the minified composition produced by composeFull().
(() => {
  'use strict';

// kernel.js: always-present substrate (spec fragment `kernel`, boot class).
// Owns: doc state (DOC_MANIFEST), module loader, same-origin guards, the
// data-cui-comp / data-cui-scope CSS scanner, window.__gofastr namespace CREATION (other
// fragments and demand modules extend it via Object.assign), manifest reads,
// component-action dispatch helpers.
// Composed FIRST; every other fragment depends on it.

  // -----------------------------------------------------------------------
  // Global document state (`__gofastr.doc`)
  //
  // The ONE place the runtime writes persistent state onto <html>/<body>.
  // DOC_MANIFEST is the frozen inventory of every allowed <html>
  // attribute, <body> class, and <body> singleton id; the table in
  // core-ui/ARCHITECTURE.md ("Global document state") mirrors it and a
  // parity test (doc_manifest_test.go) fails the build on drift, hard
  // rule 5 applied to document-level state. Writes outside the manifest
  // still land (never break the page) but console.warn the offender.
  //
  // Not covered on purpose:
  //   - data-color-scheme is WRITTEN by colorscheme.js, the separate
  //     synchronous <head> bootstrap that must run before first paint
  //     (FOUC). It is enumerated here as documentation only.
  //   - data-cui-static is written by the static exporter (Go), never
  //     by the runtime. Enumerated as documentation only.
  //   - Transient DOM (e.g. the feedback module's copy textarea) and
  //     pure reads (#cui-route-announce) stay unwrapped.
  //
  // lockScroll/unlockScroll refcount by OWNER (a Set), so two
  // concurrent lockers, a modal over an image overlay, a drawer over a
  // modal, can't fight over documentElement.style.overflow: the lock
  // releases only when the LAST owner unlocks. (Lock lives on <html>,
  // not <body>: overflow:hidden on <body> breaks position:sticky
  // descendants.)
  //
  // singleton(id, factory) returns the existing body child with that id
  // (SSR-provided or previously created) or creates+appends it once.
  // reattach() re-appends any created singleton that lost its parent,
  // the SPA full-shell swap calls it after replacing [data-cui-layout],
  // covering layouts that (incorrectly but survivably) nest chrome the
  // runtime hung on <body>.
  // docEl is the shared <html> handle for the whole core runtime,
  // every documentElement touch goes through it (keeps the minified
  // bundle inside the 12.5 KB gz budget).
  const docEl = document.documentElement;
  // M is the manifest (frozen); _dc is the dev-guard: an unmanifested
  // name still writes (never break the page) but warns so the drift is
  // caught in review / e2e console audits.
  const M = Object.freeze({
    htmlAttrs: Object.freeze('aria-busy data-color-scheme data-cui-os data-cui-static lang'.split(' ')),
    bodyClasses: Object.freeze('cui-sse-down cui-sse-up'.split(' ')),
    singletons: Object.freeze('cui-backtotop-sentinel cui-nav-toast cui-toast-fallback'.split(' ')),
  });
  const _dc = (list, n) => {
    if (!list.includes(n)) console.warn('[gofastr] not in doc.MANIFEST: ' + n);
  };
  const _locks = new Set();
  const _single = {};
  const doc = {
    MANIFEST: M,
    setHtmlAttr(n, v) { _dc(M.htmlAttrs, n); docEl.setAttribute(n, v); },
    removeHtmlAttr(n) { _dc(M.htmlAttrs, n); docEl.removeAttribute(n); },
    bodyClass(n, on) { _dc(M.bodyClasses, n); document.body.classList.toggle(n, !!on); },
    lockScroll(owner) { _locks.add(owner); docEl.style.overflow = 'hidden'; },
    unlockScroll(owner) {
      _locks.delete(owner);
      if (!_locks.size) docEl.style.overflow = '';
    },
    scrollLocked: () => _locks.size > 0,
    appendBody: (el) => document.body.appendChild(el),
    singleton(id, make) {
      _dc(M.singletons, id);
      // Prior creation wins, then an SSR-provided element is adopted
      // (factory never runs), then create + remember for reattach().
      let el = _single[id] || document.getElementById(id);
      if (!el) { el = make(); el.id = id; _single[id] = el; }
      if (!el.isConnected) doc.appendBody(el);
      return el;
    },
    reattach() {
      for (const id in _single) {
        if (!_single[id].isConnected) doc.appendBody(_single[id]);
      }
    },
  };

  // OS hint on <html data-cui-os="mac|other"> so SSR-rendered
  // shortcut hints (framework/ui.ShortcutHint) can display
  // platform-correct mod-key glyphs (⌘ on Mac, Ctrl elsewhere)
  // without per-component JS. Detection is best-effort; functional
  // shortcut matching does not depend on this (parseCombo accepts
  // both metaKey and ctrlKey when Mod is required).
  try {
    const ua = (navigator.userAgentData && navigator.userAgentData.platform) ||
               navigator.platform || '';
    doc.setHtmlAttr('data-cui-os', /Mac|iPhone|iPad|iPod/.test(ua) ? 'mac' : 'other');
  } catch (_) { /* SSR / non-browser */ }

  // data-cui-static on <html> is still written by the static exporter
  // (framework/static.Builder) and read by the widgets demand module
  // (src/widgets.js) for its missing-widget fallback toast. The runtime
  // itself no longer branches on it, composition selects the `static`
  // bundle at build time instead of testing the marker at request time.
  // -----------------------------------------------------------------------
  // Component handler registry
  // -----------------------------------------------------------------------
  const handlers = {};

  // -----------------------------------------------------------------------
  // State store: compiled Go components share state through this
  // -----------------------------------------------------------------------
  const state = {};

  // -----------------------------------------------------------------------
  // Router: known routes from screen registration
  // -----------------------------------------------------------------------
  const routes = new Map(); // path → { preload, layouts, redirect, docScripts }
  let currentPath = location.pathname + location.search;

  const registerRoutes = (routeList) => {
    if (!Array.isArray(routeList)) return;
    for (const r of routeList) {
      routes.set(r.path ?? r.Path, {
        // Prefetch mode: '' (never) | 'hover' | 'visible' | 'eager'.
        preload: r.preload ?? r.Preload ?? '',
        // Layout chain as layer keys, outermost → innermost.
        layouts: r.layouts ?? r.Layouts ?? [],
        redirect: r.redirect ?? r.Redirect ?? '',
        // Document-lifetime scripts in scope for this route (src
        // values). Nav compares the destination's set against the live
        // document's data-cui-doc scripts; a difference is a document
        // boundary, never a partial swap.
        docScripts: r.docScripts ?? r.DocScripts ?? [],
      });
    }
  };

  // Hydrate routes + catalog from inline <script type="application/json">
  // blocks the SSR emits. The browser treats them as inert data (not
  // executable), so they pass strict CSP. Reading happens before
  // first paint of any non-trivial component because runtime.js is
  // injected at the end of <body>, after the JSON blocks in <head>.
  // _json parses server-written JSON text, null for none or garbage.
  const _json = (t) => { try { return JSON.parse(t || 'null'); } catch (_) { return null; } };
  const _readInlineJSON = (id) => _json(document.getElementById(id)?.textContent);
  if (!window.__gofastr_routes) {
    const r = _readInlineJSON('gofastr-routes');
    if (r) window.__gofastr_routes = r;
  }
  if (!window.__gofastr_catalog) {
    const c = _readInlineJSON('gofastr-catalog');
    if (c) window.__gofastr_catalog = c;
  }
  if (!window.__gofastr_locales) {
    const l = _readInlineJSON('gofastr-locales');
    if (l) window.__gofastr_locales = l;
  }

  // Locale prefixes (framework.WithLocalePrefixes): /es/pricing is the
  // /pricing screen in Spanish. Route tables and cache keys hold the
  // unprefixed path, so every lookup strips the prefix first; every
  // runtime fetch of a /__gofastr endpoint or the page itself gains the
  // current page's prefix (_lp) so the server answers in its locale. A
  // link that changes the locale crosses a document boundary (nav.js).
  // _splitLocale returns [tag, path without the prefix]; tag is '' for
  // the default locale or an app without prefixes. base is the static
  // export's BasePath, which sits in front of the prefix.
  const _locBase = () => window.__gofastr_locales?.base || '';
  const _splitLocale = (p) => {
    p = String(p ?? '');
    const L = window.__gofastr_locales, b = _locBase();
    const m = L?.prefixes?.length && p.startsWith(b + '/') && /^\/([^/?#]+)(.*)$/.exec(p.slice(b.length));
    return m && L.prefixes.includes(m[1]) ? [m[1], b + (m[2][0] === '/' ? m[2] : '/' + m[2])] : ['', p];
  };
  const _unlocalize = (p) => _splitLocale(p)[1];
  const _localeOf = (p) => _splitLocale(p)[0];
  const _lp = (u) => {
    u = String(u ?? '');
    const t = _localeOf(location.pathname), b = _locBase();
    return t && u[1] !== '/' && u.startsWith(b + '/') && !_localeOf(u) ? b + '/' + t + u.slice(b.length) : u;
  };

  // Signal store seed: server-provided initial values for the signal
  // bus (core-ui/store). Stashed now; applied to _signals right after
  // the __gofastr namespace is built (below), BEFORE hydration, so
  // getSignal returns the SSR value on first paint instead of undefined.
  if (!window.__gofastr_signals_seed) {
    const sg = _readInlineJSON('gofastr-signals');
    if (sg) window.__gofastr_signals_seed = sg;
  }

  // Bootstrap routes from injected data
  if (Array.isArray(window.__gofastr_routes)) {
    registerRoutes(window.__gofastr_routes);
  }

  // -----------------------------------------------------------------------
  // Helpers
  // -----------------------------------------------------------------------
  // Own-prop read for {} registries keyed by attribute-borne names: an
  // inherited "constructor" is truthy and must not pass as an entry.
  const own = (o, k) => Object.prototype.hasOwnProperty.call(o, k);
  // say is every word the runtime writes for a reader: the host's
  // #gofastr-strings block (core-ui/runtime.StringsJSON, the reader's
  // locale) else the English beside the key, with {{name}} filled from
  // p. A Go test holds each say() call to its registered phrase. No
  // Object.prototype member is a string, so a key like "toString" falls
  // through to the English. A soft nav replaces the table
  // (applyDocShell, frag/nav.js).
  let _says = _readInlineJSON('gofastr-strings') || {};
  const say = (k, en, p) => {
    const s = _says[k];
    return (typeof s == 'string' ? s : en).replace(/\{\{(\w+)\}\}/g, (m, n) => (p && own(p, n) ? p[n] : m));
  };
  // formatNumber writes n the way the page's language (<html lang>)
  // writes numbers, as the server's i18n.Number did for the same text:
  // a count the runtime fills into a phrase reads "1.234" on /es/, not
  // "1234". It reads lang on every call, so a client navigation that
  // changes the language changes the digits; a tag Intl refuses falls
  // back to the browser's own locale.
  const formatNumber = (n) => {
    try { return n.toLocaleString(document.documentElement.lang || undefined); } catch (_) { return n.toLocaleString(); }
  };
  const closestAttr = (el, attr) => {
    const node = el.closest(`[${CSS.escape(attr)}]`);
    return node?.getAttribute(attr) ?? null;
  };

  const collectParams = (el) => {
    if (!el?.attributes) return {};
    const params = {};
    for (const a of el.attributes) {
      if (a.name.startsWith('data-param-')) {
        params[a.name.slice('data-param-'.length)] = a.value;
      }
    }
    return params;
  };

  // -----------------------------------------------------------------------
  // Public API: kernel members only. Core fragments and demand modules add
  // their namespace members via Object.assign. Optional code therefore leaves
  // no dangling references inside this literal.
  // -----------------------------------------------------------------------
  // Public API (what compiled JS calls)
  // -----------------------------------------------------------------------
  window.__gofastr = {
    /** The markup generation this kernel reads (2 = the data-cui-*
        spelling). Every navigation fetch names it in X-Gofastr-Markup;
        a server whose markup differs answers with a body that reloads
        the document instead of a partial this kernel cannot read (see
        core-ui/runtime.MarkupVersion). Modules read it from here, so a
        module loaded into an older kernel sends none; nav.js spells the
        same value as a literal. */
    _markup: '2',

    /** Global document state module. See the DOC_MANIFEST block at the
        top of this file. Split modules (widgets, toasts, backtotop)
        reach it via NS.doc for every persistent <html>/<body> write. */
    doc,
    say,
    formatNumber,
    _lp,
    _unlocalize,
    _localeOf,

    /* DOM attributes name fetch targets throughout the runtime. Keep these
       guards in kernel so navigation cannot race the RPC demand module that
       previously registered them. */
    _sameOrigin(u) {
      try { return new URL(String(u ?? ''), location.href).origin === location.origin; }
      catch (_) { return false; }
    },
    _originOK(u) {
      if (this._sameOrigin(u)) return true;
      console.warn('[gofastr] refused cross-origin fetch:', u);
      return false;
    },
    /* The screen title a response names. uihost percent-encodes the
       header (an untitled screen's carries the app name, its full
       document's title); a host that omits it keeps document.title, which is
       never encoded, so only the header is decoded (a "50% Off" title
       threw out of navigation). */
    _title(r) {
      const t = r.headers.get('X-Gofastr-Title');
      try { return t ? decodeURIComponent(t) : document.title; } catch (_) { return t; }
    },

    /** Reject dangerous schemes when a signal value is about to be
        written into a URL-bearing HTML attribute (href / src / action
        / xlink:href / formaction). Returns true when the value MUST
        be discarded. Allows http(s), mailto, tel, relative paths,
        same-page anchors, and data:image/* (used for inline blob
        previews). Rejects javascript:, vbscript:, and other data:
        payloads.

        This is the runtime-side guard against signal-bound `href` on
        a media viewer's download control + any other widget that
        mirrors an attacker-controllable signal into a click-triggered
        attribute.
    */
    _isUnsafeSignalUrl(attr, value) {
      if (!attr) return false;
      const a = String(attr).toLowerCase();
      if (a !== 'href' && a !== 'src' && a !== 'action' &&
          a !== 'xlink:href' && a !== 'formaction') return false;
      // Strip ALL ASCII whitespace + C0 control bytes (0x00-0x1f)
      // anywhere in the value before resolving the scheme. Browsers
      // remove these during URL parsing (WHATWG), so both leading
      // ("  javascript:") AND interior ("java<TAB>script:",
      // "<NUL>javascript:") control chars must go, or a startsWith()
      // check is defeated by an embedded tab/newline/CR or leading C0.
      const trimmed = String(value || '').replace(/[\s\x00-\x1f]+/g, '').toLowerCase();
      if (trimmed.startsWith('javascript:')) return true;
      if (trimmed.startsWith('vbscript:')) return true;
      if (trimmed.startsWith('data:')) {
        // Allow data:image/* only; everything else (data:text/html,
        // data:application/javascript, etc.) is rejected. NOTE: this
        // intentionally allows data:image/svg+xml, an SVG in an <img>
        // src (the only sink signal-bound `src`/`href` reaches here)
        // renders inertly and does NOT execute its scripts. SVG only runs
        // script when loaded as a *document* (iframe/object/navigation),
        // which is not a signal-URL sink. (Verified by the runtime security e2e suite.)
        return !trimmed.startsWith('data:image/');
      }
      return false;
    },


    /** Register event handlers for a component */
    register(id, events) {
      handlers[id] = events;
    },

    /** Trigger an event on a component */
    trigger(id, event, params) {
      handlers[id]?.[event]?.(params);
    },

    handlers,


    /** Register routes dynamically */
    registerRoutes,

    /** Get current path */
    get currentPath() { return currentPath; },
    /** Split modules cannot close over the IIFE-local route state. */
    _setCurrentPath(path) { currentPath = path; },

    // --- State helpers (compiled Go code uses these) ---

    getState(key, defaultVal) {
      const v = own(state, key) ? state[key] : undefined;
      return v ?? defaultVal;
    },

    setState(key, val) {
      state[key] = val;
    },

    // --- DOM helpers (compiled Go code uses these) ---

    /** Update textContent of first element matching selector */
    updateText(selector, text) {
      const el = document.querySelector(selector);
      if (el) el.textContent = text;
    },

    /** Update innerHTML of first element matching selector */
    updateHTML(selector, html) {
      const el = document.querySelector(selector);
      if (el) el.innerHTML = html;
    },

    /** Set an attribute on first element matching selector */
    setAttr(selector, attr, val) {
      const el = document.querySelector(selector);
      if (el) el.setAttribute(attr, val);
    },

    /** Get value from an input */
    getValue(selector) {
      return document.querySelector(selector)?.value ?? '';
    },

    /** Add a CSS class */
    addClass(selector, cls) {
      document.querySelector(selector)?.classList.add(cls);
    },

    /** Remove a CSS class */
    removeClass(selector, cls) {
      document.querySelector(selector)?.classList.remove(cls);
    },

    /** Toggle a CSS class */
    toggleClass(selector, cls) {
      document.querySelector(selector)?.classList.toggle(cls);
    },

    /** Legacy toast, kept as a forwarding shim so older callers
        (string-only arg) continue to work. The real implementation
        is the cfg-object version defined below; it owns the stack
        widget + lifecycle. */

    /** Fetch partial HTML from server and inject into selector. A
        followed 3xx is another URL's page: nothing injected, ''. */
    async fetchPage(url, selector) {
      if (!this._originOK(url)) return '';
      const r = await fetch(url, { headers: { 'X-Gofastr-Partial': '1' } });
      if (r.redirected) return '';
      const html = await r.text();
      if (selector) {
        const el = document.querySelector(selector);
        if (el) el.innerHTML = html;
      }
      return html;
    },

    /** Sync all [data-bind] elements from current state */
    syncBindings() {
      document.querySelectorAll('[data-bind]').forEach(el => {
        const key = el.getAttribute('data-bind');
        if (key && own(state, key) && state[key] !== undefined) {
          el.value = state[key];
        }
      });
    },

    /** Call a server action and handle the response */
    async serverAction(action, params = {}) {
      // Reaching here means the call was never compiled.
      //
      // The action compiler rewrites the literal "G.serverAction(" into
      // "G._serverActionFor(<componentId>, ", so every registered action
      // arrives with an id. A call the compiler could not see, a computed
      // spelling like G["serverAction"](…), an aliased reference, a call
      // assembled at runtime, keeps this method and posts with an empty
      // componentId, which the server cannot route. That used to be a silent
      // 404 discovered in production; say what actually happened instead.
      //
      // The build gate and the boot walk catch the ordinary spellings. This is
      // the backstop for the ones neither can see, and the reason it lives at
      // runtime is that neither static analysis nor the compiler can resolve
      // them either.
      console.error('[gofastr] serverAction("' + action + '") was not compiled; write it literally.');
      return this._serverActionFor('', action, params);
    },

    /** Call a server action for a specific component */
    async _serverActionFor(componentId, action, params = {}) {
      const sessionCookie = document.cookie.match(/gofastr-session=([^;]+)/);
      const session = sessionCookie ? sessionCookie[1] : '';
      const r = await fetch(_lp('/__gofastr/action'), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        // page: the owning screen's Policy reads this page's route params.
        body: JSON.stringify({ action, params, session, componentId, page: _unlocalize(location.pathname) }),
      });
      // A followed 3xx is another URL's answer: the action failed.
      if (r.ok && !r.redirected) {
        const result = await r.json();
        if (result.message) {
          window.__gofastr._toastOrFallback(result.message);
        }
        return result;
      }
      return null;
    },

    /** loadCSS is a no-op kept for external callers that still invoke
     * window.__gofastr.loadCSS(path). The per-screen chunk endpoint
     * (/__gofastr/css/<path>) now returns 410 GONE, declare CSS per
     * component via registry.RegisterStyle and the runtime loads
     * /__gofastr/comp/<name>.css from the SSR-emitted <link>. */
    loadCSS(_screenPath) { /* no-op */ },

    // Component CSS: three modes share _pendingLinks + data-cui-style dedup.
    // See core-ui/ARCHITECTURE.md for the model. Catalog seeded by /__gofastr/catalog.js.
    _pendingLinks: new Set(),
    // Sheet links a wait has already settled on (loaded, failed or
    // timed out), so a sheet that never answers stalls one wait, not
    // every later one.
    _sheetDone: new WeakSet(),
    loadComponentCSS(name) {
      if (!name || this._pendingLinks.has(name)) return;
      if (document.querySelector('link[data-cui-style="' + CSS.escape(name) + '"]')) return;
      const e = (window.__gofastr_catalog || {})[name];
      if (!e) return;
      this._pendingLinks.add(name);
      const link = document.createElement('link');
      link.rel = 'stylesheet';
      // stylePath may already carry a query (an embed frame appends ?t=<variant>
      // so component CSS resolves under the same theme as app.css), so pick the
      // separator rather than always using '?'. Concatenating a second '?' put
      // the whole thing in one parameter value, which the server read as an
      // unknown theme key AND an absent version, silently serving the app
      // palette with no immutable caching.
      link.href = e.stylePath + (e.version ? (e.stylePath.indexOf('?') >= 0 ? '&' : '?') + 'v=' + e.version : '');
      link.setAttribute('data-cui-style', name);
      link.id = 'cui-css-' + name;
      // A failed fetch (a deploy's 503, a blip) must not pin the name:
      // drop the link, and the claim after a cooldown, so a later scan
      // asks again without a poll or push region re-requesting a sheet
      // that keeps failing on every swap.
      link.onerror = () => { link.remove(); setTimeout(() => this._pendingLinks.delete(name), 3e3); };
      this._insertSheet(link);
    },
    // Component sheets go before app.css (#cui-app-css), the order the
    // server's head keeps, so the host's own rules win cascade ties.
    _insertSheet(link) {
      const app = document.getElementById('cui-app-css');
      app ? app.before(link) : document.head.appendChild(link);
    },
    // Loads the sheet each kit root (data-cui-comp) and owned-style
    // root (data-cui-scope) under root names. Descendants only: a
    // caller whose swapped element may itself be a root scans its
    // parent (swapShell).
    scanAndLoadCSS(root) {
      if (!root?.querySelectorAll) return;
      root.querySelectorAll('[data-cui-comp],[data-cui-scope]').forEach((el) => {
        this.loadComponentCSS(el.dataset.cuiComp);
        this.loadComponentCSS(el.dataset.cuiScope);
      });
    },
    // Starts the sheets of the components under root and resolves once
    // each one root renders has loaded: its own link, or, for a name a
    // first-paint subset covers, the full bundle link it is waiting on.
    // Sheets root does not render (idle prewarms, a prefetch's) are not
    // waited on. Each wait is bounded: a stalled fetch fires neither
    // load nor error, and <link> has no timeout of its own.
    async _sheetsFor(root) {
      // A first-paint subset whose full bundle is not linked yet: the
      // fullbundle module links it, so wait for that module first,
      // bounded like a sheet. One wait, then on: a marker the module
      // does not spend (page content, a module from another release)
      // must not send the wait back to the module forever.
      if (document.querySelector('[data-cui-bundle-href]')) await new Promise((r) => { this.loadModule('fullbundle').then(r, r); setTimeout(r, 3000); });
      this.scanAndLoadCSS(root);
      const links = new Set();
      root.querySelectorAll?.('[data-cui-comp],[data-cui-scope]').forEach((el) => {
        for (const n of [el.dataset.cuiComp, el.dataset.cuiScope]) {
          const l = n && (document.querySelector('link[data-cui-style="' + CSS.escape(n) + '"]') || document.querySelector('link[data-cui-bundle]'));
          if (l && !l.sheet && !this._sheetDone.has(l)) links.add(l);
        }
      });
      return Promise.all([...links].map((l) => new Promise((r) => {
        l.addEventListener('load', r);
        l.addEventListener('error', r);
        setTimeout(r, 3000);
      }).then(() => this._sheetDone.add(l))));
    },
    // A navigation awaits this before it swaps: the incoming markup (an
    // HTML string, parsed inert by DOMParser, or a parsed node) starts
    // its component sheets while the old page is still on screen, so the
    // new page paints styled. Each page links only the sheets it renders,
    // so swapping first and linking after flashed nearly every new page
    // unstyled.
    _cssReady(src) {
      return this._sheetsFor(typeof src == 'string' ? new DOMParser().parseFromString(src, 'text/html') : src);
    },
    _idleQueue: [],
    _idleFlushing: false,
    scheduleIdleLoads() {
      const cat = window.__gofastr_catalog || {};
      for (const name in cat) {
        if (cat[name].loadMode === 'prewarm') this._idleQueue.push(name);
      }
      this._flushIdle();
    },
    _flushIdle() {
      if (this._idleFlushing || !this._idleQueue.length) return;
      this._idleFlushing = true;
      const rIC = window.requestIdleCallback || ((fn) => setTimeout(fn, 200));
      const self = this;
      rIC(() => {
        try {
          const n = self._idleQueue.shift();
          if (n) self.loadComponentCSS(n);
        } finally {
          self._idleFlushing = false;
          if (self._idleQueue.length) self._flushIdle();
        }
      });
    },

    formatInt: (n) => String(n),
    formatFloat: (n, d) => Number(n).toFixed(d),

    // -----------------------------------------------------------------
    // Widgets (core-ui/widget): overlay UIs that mount on top of any
    // page. mountWidget is the runtime entrypoint used by per-widget
    // bootstrap scripts. The host (Go) builds the WidgetDef → emits a
    // tiny init script that calls __gofastr.mountWidget(cfg, chrome).
    // All DOM/SSE/RPC plumbing lives here, in the framework runtime.
    // -----------------------------------------------------------------

    /** Internal widget-state registry. Idempotent: a widget mounted
        twice with the same name is a no-op. */
    _widgets: {},

    /** Names of currently-mounted modal (backdrop'd) widgets, oldest
        at index 0. Drives body-scroll lock + the Tab focus trap so a
        modal opened from inside another modal traps Tab to itself
        rather than to the outer one. */
    _modalStack: [],

    /** Tracks split runtime modules already loaded. The loader checks
        this map before injecting a <script>; modules set their own
        entry to true on load. */
    loadedModules: {},

    /** Load a split runtime module by name (e.g. "popover",
        "popover"). Returns a cached Promise that resolves once the
        module's IIFE has executed. Safe to call concurrently, the
        first call wins, all callers await the same fetch. */
    loadModule,

    /** Selector for focusable elements inside a modal, used by the
        initial-focus pass and the Tab focus trap. */
    _focusSel: 'a[href],button:not([disabled]):not([aria-disabled="true"]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])',

    /** The layout demand modules' seam table (src/envelope.js,
        src/loading.js, src/parts.js, src/transition.js). The navigator
        calls into it defensively: an absent hook is the module not
        having loaded, which means the page declared none of that
        machinery. Modules assign their entry (NS._navHooks.envelope =
        {…}) at evaluation. */
    _navHooks: {},
    // The navigation epoch: a plain counter both navigators (core's
    // plain one and the envelope module's) bump and read through the
    // supersede rule (NS._navLive). A writable slot, not a closure,
    // because the module's navigator owns its own navigations.
    _navEpoch: 0,
    // The path the DOM showed when the pending navigations began, or
    // null while none is pending. A navigation started while another
    // still loads keeps it, so a failure rolls back to the page still
    // shown, never to a superseded destination that never rendered.
    // Both navigators set it at entry and the live one clears it.
    _navBase: null,
    // Set once core's navigator has swapped the DOM. The envelope
    // module keys the page it finds on the served URL, which is only
    // right while no navigation has applied before it loaded.
    _navShown: false,

    // Toast stack runtime (__gofastr.toast, _initToasts, _dismissToast,
    // _toastTimers, _toastSeq) lives in the registered behaviour module
    // headless-feedback (framework/headless/feedback.js), which
    // replaced the retired core-ui/runtime src/toasts.js. The module
    // self-registers those on window.__gofastr when it loads. Core
    // code that calls them (the click delegator for data-cui-toast,
    // the X-Gofastr-Toast header dispatch in dispatchRPC) awaits
    // loadModule('headless-feedback') first so the very first toast on
    // a cold cache still fires.

    // Widget runtime (mountWidget, openWidget, closeWidget,
    // _mountByName, _chromeCache, _deepLink{Push,Strip,Sync}, Modal
    // Esc handler, Modal Tab focus trap) lives in the split-runtime
    // widgets module at core-ui/runtime/src/widgets.js. The module
    // self-registers those on window.__gofastr when loaded.
    //
    // State stays here on the namespace (_widgets, _modalStack,
    // _popoverStack, _focusSel) so other modules (popover) can read
    // it.
    //
    // Stub left below for the very few callers (mostly tests) that
    // ask for openWidget before widgets has had a chance to load;
    // the stub awaits loadModule then forwards.
    async openWidget(name, opts) {
      await this.loadModule('widgets');
      return this.openWidget(name, opts);
    },

    // _dispatchToastHeader is the X-Gofastr-Toast response-header
    // path. It tries the full toasts module first; on failure it
    // falls back to a minimal inline renderer so the user never loses
    // an important message (e.g. "Save failed") to a transient
    // module-load 5xx or network hiccup.
    _dispatchToastHeader(header) {
      let arr;
      try {
        const parsed = JSON.parse(header);
        arr = Array.isArray(parsed) ? parsed : [parsed];
      } catch (_) { return; }
      for (const cfg of arr) this._toastOrFallback(cfg);
    },

    // _toastOrFallback dispatches a single toast cfg, falling back to
    // the inline renderer when the toasts module cannot load or has no
    // stack on the page to render into (NS.toast answers null then:
    // the module builds no container of its own; uihost renders a
    // default stack into every shell, so the fallback is for a page
    // that lost it).
    _toastOrFallback(cfg) {
      return this.loadModule('headless-feedback')
        .then(() => {
          let shown = null;
          try { shown = this.toast(cfg); } catch (_) { shown = null; }
          if (shown == null) { try { this._fallbackToast(cfg); } catch (_) {} }
        })
        .catch(() => { try { this._fallbackToast(cfg); } catch (_) {} });
    },

    // _fallbackToast renders an unstyled-but-visible toast notice when
    // the toasts module can't load. No TTL, no animation, no hover
    // pause, just a labelled live region the user can read and
    // dismiss with the × button. Uses textContent throughout (no
    // innerHTML) so a malicious title can't inject script.
    _fallbackToast(cfg) {
      if (!cfg || !cfg.title) return null;
      // Body singleton (doc.MANIFEST), distinct from the styled
      // [data-cui-toast-stack] container the toasts module owns; the
      // fallback stays deliberately unstyled + module-free.
      const container = doc.singleton('cui-toast-fallback', () => {
        const c = document.createElement('div');
        c.setAttribute('data-cui-toast-fallback', '');
        c.setAttribute('role', 'region');
        c.setAttribute('aria-label', say('runtime.notifications', 'Notifications'));
        c.style.cssText = 'position:fixed;top:1rem;inset-inline-end:1rem;z-index:2147483600;display:grid;gap:.5rem;max-width:min(360px,100vw - 2rem)';
        return c;
      });
      const isAssertive = cfg.variant === 'warning' || cfg.variant === 'danger';
      // mk: tiny element builder (tag, cssText, textContent), inline
      // styles + textContent only (no innerHTML) so a malicious title
      // can't inject script. Styling is the bare minimum that reads as
      // a notice; the styled experience lives in the toasts module.
      const mk = (tag, css, txt) => {
        const n = document.createElement(tag);
        n.style.cssText = css;
        if (txt != null) n.textContent = txt;
        return n;
      };
      const item = mk('div', 'background:#1f2937;color:#fff;padding:12px 16px;border-radius:6px;font:.9rem system-ui;display:flex;gap:12px');
      item.setAttribute('role', isAssertive ? 'alert' : 'status');
      item.setAttribute('aria-live', isAssertive ? 'assertive' : 'polite');
      const text = mk('div', 'flex:1');
      text.appendChild(mk('strong', 'display:block', cfg.title));
      if (cfg.body) {
        text.appendChild(mk('div', '', cfg.body));
      }
      const dismiss = mk('button', 'background:none;border:0;color:inherit;cursor:pointer', '×');
      dismiss.type = 'button';
      dismiss.setAttribute('aria-label', say('runtime.dismiss_notification', 'Dismiss notification'));
      dismiss.addEventListener('click', () => { item.remove(); });
      item.appendChild(text);
      item.appendChild(dismiss);
      container.appendChild(item);
      return item;
    },
  };

// signals.js: signal store + binding (spec fragment `signals`, marker class; deps: kernel).
// Owns: setSignal, signal binding/broadcast, the SSR seed read + application,
// the signal-set/inc/toggle read-modify-write path, aria injection.
  // isReservedSignalKey rejects the JS object keys that, when used as a
  // dynamic property name on the _signals store, mutate the store's
  // prototype chain instead of creating an own data property:
  //   store["__proto__"] = {…}   // invokes the __proto__ setter
  //   store["constructor"]/["prototype"] // shadow built-ins
  // A seed (full-load or partial) carrying such a key would re-parent the
  // _signals object, every not-yet-set signal name would then resolve
  // through the attacker object (cross-signal confusion) and setSignal
  // would mutate the shared prototype. Seed keys are server-controlled
  // today; this is advisory-recommended defense-in-depth (strip
  // __proto__/constructor/prototype before merging). Used by all three
  // seed-merge loops (boot seed + mergeSeedFromDOM page/global).
  const isReservedSignalKey = (k) =>
    k === '__proto__' || k === 'constructor' || k === 'prototype';


  // signals namespace members. _signals is the signal store; setSignal is the
  // write path every signal mutation flows through (prototype-pollution guard,
  // html/attr/text render modes, after-update flash + scroll-to-bottom hooks).
  // MUST run before the seed application below, which reads window.__gofastr._signals.
  Object.assign(window.__gofastr, {
    _signals: {},

    /** Read the current value of a named signal. Returns undefined for
        unset signals. Used by data-cui-signal-inc and data-cui-signal-toggle
        to read-modify-write without an RPC round-trip. */
    getSignal(name) {
      const s = own(this._signals, name) ? this._signals[name] : undefined;
      return s ? s.value : undefined;
    },
    /** Push a value into a named signal and reflect it into all
        [data-cui-signal="<name>"] DOM nodes. Mode is read from the
        node's data-cui-signal-mode attr ("text" default, "html",
        "attr"+data-cui-signal-attr). */
    setSignal(name, value, opts) {
      // Prototype pollution: the reserved-key guard used to live only in
      // the three seed-merge loops, but attribute-controlled keys enter
      // HERE, data-cui-signal-set/-inc/-toggle, and fetched-JSON keys
      // from poll.js and widgets.js. `__proto__:POLLUTED` re-parents the
      // store, and because getSignal is `s ? s.value : undefined` EVERY
      // unset signal then reads back the attacker's value. Pure data
      // corruption, so no CSP stops it; the guard belongs at the write.
      if (isReservedSignalKey(name)) {
        console.warn('[gofastr] refused reserved signal name:', name);
        return;
      }
      let s = own(this._signals, name) ? this._signals[name] : undefined;
      if (!s) { s = this._signals[name] = { value: undefined, listeners: [] }; }
      // opts.untrusted marks a value that came from the URL (the
      // deep-link seed in widgets.js). Recorded on the signal so the
      // html render mode below refuses to treat it as markup, and reset
      // whenever the same signal is next set from a trusted source.
      s.untrusted = !!(opts && opts.untrusted);
      s.value = value;
      for (const fn of s.listeners) {
        try { fn(value); } catch (_) {}
      }
      // Escape the signal name before it enters the selector, a name
      // containing selector metacharacters (e.g. '"]') would otherwise
      // produce an invalid selector and querySelectorAll would THROW,
      // taking setSignal (and every listener it drives) down with it.
      // Same shape as sse.js:76.
      document.querySelectorAll('[data-cui-signal="' + CSS.escape(String(name)) + '"]').forEach((node) => {
        const mode = node.getAttribute('data-cui-signal-mode') || 'text';
        if (mode === 'html') {
          // The html escape hatch is for TRUSTED HTML *strings* only.
          // On a non-2xx response dispatchRPC broadcasts the auto-built
          // error object {ok:false,status,text} into the signal. That
          // object is NOT trusted HTML, and applying it here (via
          // innerHTML OR textContent) would either execute reflected
          // markup or overwrite the existing trusted region with a
          // JSON blob, corrupting the UI on every failed RPC. The
          // documented optimistic-UI invariant, "a failed delete
          // leaves the row/list unchanged", depends on this no-op.
          // Text-mode nodes below still render a human-readable
          // "Request failed (…)" string, so failure feedback is not lost.
          if (typeof value !== 'string') return;
          // A value seeded from location.search is attacker-supplied by
          // construction, `?x=<img onerror=…>` on any page carrying an
          // html-mode binding of `x`. Render it as text instead. The
          // value still reaches the node; it just stops being markup.
          if (s.untrusted) { node.textContent = value; return; }
          node.innerHTML = value;
          window.__gofastr.scanAndLoadCSS(node);
          // Wire any toast items the freshly-swapped HTML brought in.
          // Awaits the toasts module, when an island-driven update
          // injects a toast for the first time, the module loads,
          // then _initToasts runs against the new content.
          if (node.querySelector && node.querySelector('[data-hui-toast]')) {
            window.__gofastr.loadModule('headless-feedback').then(() => {
              window.__gofastr._initToasts(node);
            }).catch(() => {});
          }
        } else if (mode === 'attr') {
          // Folded: the Go allow-list matches names case-insensitively,
          // and the property writes below need the DOM's spelling.
          const attr = (node.getAttribute('data-cui-signal-attr') || 'value').toLowerCase();
          // The attribute NAME is developer-supplied and server-
          // rendered, so the allow-list that keeps a signal out of
          // `srcdoc` / `style` / `on*` lives in Go, at the emitters
          // (core-ui/store.SignalAttrAllowed), refusing to render the
          // binding beats warning about it after it shipped, and costs
          // the runtime no bytes. The value guard below still runs on
          // every client-side update.
          let v = String(value ?? '');
          // URL-bearing attrs (href / src / action / xlink:href /
          // formaction): reject dangerous schemes (javascript:,
          // vbscript:, data: except data:image/*). Stops a signal-
          // driven anchor (e.g. a media viewer's download) from
          // executing arbitrary JS when an attacker controls the
          // signal value via a query-string deeplink param.
          if (window.__gofastr._isUnsafeSignalUrl(attr, v)) v = '';
          // A boolean attribute is on whenever it is present, so
          // disabled="false" disables: false, null, '', 0 and 'false'
          // remove it. disabled, hidden and open reflect the attribute;
          // checked, selected and value also live as properties that
          // stop following it once the reader clicks or types, so the
          // property is written too, by name (no computed-key write).
          if (/^(disabled|hidden|checked|open|selected)$/.test(attr)) {
            const on = !!value && value !== 'false';
            node.toggleAttribute(attr, on);
            if (attr === 'checked') node.checked = on;
            if (attr === 'selected') node.selected = on;
          } else {
            node.setAttribute(attr, v);
            if (attr === 'value' && 'value' in node) node.value = v;
          }
          // Tabs (framework/ui.Tabs): when the wrapper's data-active
          // index changes, mirror it into aria-selected on the strip's
          // role=tab buttons, CSS keys the visual highlight off
          // data-active, but assistive tech reads aria-selected.
          if (attr === 'data-active') {
            node.querySelectorAll('[role="tab"][data-cui-tab-index]').forEach((b) => {
              b.setAttribute('aria-selected', String(b.getAttribute('data-cui-tab-index') === v));
            });
          }
        } else {
          // Task B: when the value is an error object from dispatchRPC
          // ({ok:false, status, text}), render it as a human-readable
          // string instead of raw JSON so users see "Request failed (500)"
          // in their language rather than {"ok":false,"status":500,...}.
          if (value != null && typeof value === 'object' && value.ok === false) {
            const t = value.text ? String(value.text).substring(0, 200) : '';
            node.textContent = say('runtime.request_failed_status', 'Request failed ({{status}})', { status: value.status || '?' }) + (t ? ' \u2014 ' + t : '');
          } else if (value == null) {
            node.textContent = '';
          } else if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') {
            node.textContent = String(value);
          } else {
            node.textContent = JSON.stringify(value);
          }
        }
        // After-update hook: brief flash to signal the value changed.
        // Useful for headers/badges where the user might miss an
        // update otherwise. Duration overridable via
        // data-cui-flash-duration-ms; default 600ms.
        // Task D: skip the flash when the user prefers reduced motion.
        if (node.hasAttribute('data-cui-flash-on-update')) {
          const prefersReduced = window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
          if (!prefersReduced) {
            const dur = parseInt(node.getAttribute('data-cui-flash-duration-ms') || '600', 10);
            node.classList.remove('cui-flash');
            // Force reflow so the next add re-runs the animation.
            // eslint-disable-next-line no-unused-expressions
            node.offsetWidth;
            node.classList.add('cui-flash');
            setTimeout(() => node.classList.remove('cui-flash'), dur);
          }
        }
        // After-update hook: scroll a container to bottom so streaming
        // chat logs / live tails surface new content without manual
        // scrolling. Opt-in via data-cui-scroll-bottom-on-update on
        // the signal node itself or the resolved selector target.
        if (node.hasAttribute('data-cui-scroll-bottom-on-update')) {
          const sel = node.getAttribute('data-cui-scroll-bottom-on-update');
          // The attribute value is a selector by design; a malformed one
          // degrades to the node itself instead of throwing out of the
          // signal fanout pass.
          let target = node;
          if (sel) { try { target = node.querySelector(sel) || document.querySelector(sel) || node; } catch (_) {} }
          // Defer to end of microtask so the new innerHTML lays out first.
          Promise.resolve().then(() => { try { target.scrollTop = target.scrollHeight; } catch (_) {} });
        }
      });
    },

    /** Read the current value of a named signal. */
    signal(name) {
      return (own(this._signals, name) ? this._signals[name] : undefined)?.value;
    },
  });

  // Client signal mutations are core behavior. Keep this delegated listener
  // outside RPC so tabs, counters, and toggles work before any network module
  // loads. It serves widget roots too: their scoped listener handles only RPC
  // and close, and leaves a node carrying a signal mutation to this one.
  document.addEventListener('click', (e) => {
    const node = e.target.closest('[data-cui-signal-set],[data-cui-signal-inc],[data-cui-signal-toggle]');
    if (!node) return;
    // Tagged: this prevent is the runtime's own, not a page listener's,
    // so a link that also sets a signal still navigates (nav.js).
    e.preventDefault();
    e._cuiSig = 1;
    const G = window.__gofastr;

    const set = node.getAttribute('data-cui-signal-set');
    if (set) {
      const sep = set.indexOf(':');
      if (sep > 0) G.setSignal(set.substring(0, sep), set.substring(sep + 1));
    }

    const inc = node.getAttribute('data-cui-signal-inc');
    if (inc) {
      const sep = inc.indexOf(':');
      const name = sep > 0 ? inc.substring(0, sep) : inc;
      const delta = sep > 0 ? Number(inc.substring(sep + 1)) : 1;
      G.setSignal(name, (Number(G.getSignal(name)) || 0) + delta);
    }

    const toggle = node.getAttribute('data-cui-signal-toggle');
    if (toggle) {
      const current = G.getSignal(toggle);
      G.setSignal(toggle, !current || current === 'false' || current === '0');
    }
  });


  // Apply the SSR signal seed (stashed above) to the signal store BEFORE
  // hydration. Existing in-memory values win (the seed never clobbers a
  // value already mutated on the client, relevant for app-global slices
  // across SPA navigations); fresh names are created with no listeners.
  if (window.__gofastr_signals_seed) {
    const store = window.__gofastr._signals;
    const seed = window.__gofastr_signals_seed;
    for (const k in seed) {
      if (!Object.prototype.hasOwnProperty.call(seed, k)) continue;
      if (isReservedSignalKey(k)) continue;
      // Own keys only: store[k] reads the inherited built-in for a
      // signal named "toString" and wrote the seed onto it.
      if (own(store, k)) {
        store[k].value = seed[k];
      } else {
        store[k] = { value: seed[k], listeners: [] };
      }
    }
  }

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

// widgets-boot.js (spec fragment `widgets-boot`, boot class; deps: kernel).
// Owns: the /__gofastr/widgets catalog fetch + auto-mount pass, the
// _widgetCatalog readiness Promise, and the eager open/toast click
// delegators that must be installed before the catalog resolves.

  // Auto-discover registered widgets. The framework runtime is loaded
  // once per page (via /__gofastr/runtime.js); each Mount(r, def) on
  // the server registers in a process-global map; this fetch picks the
  // list up and mounts every widget. 404 means no widgets registered,
  // silently skip (the runtime works for plain pages too).
  // Per-page scoped widget discovery, apps that constrain widgets
  // to specific routes via .Pages / .PagesPrefix / .PagesMatch get
  // a filtered catalog. Widgets with no Routes declared appear on
  // every page (the backwards-compatible default).
  // The eager click delegator (installed below) awaits this readiness
  // Promise before calling openWidget. openWidget reads
  // _widgetCatalog[name] and silently bails if absent, so a click that
  // arrives before the catalog returns must wait for entries to be
  // populated. We set the Promise up immediately and stash the resolver
  // so the .then() of the catalog fetch (which runs after the namespace
  // is assigned further down) can settle it. Stash on the IIFE-local
  // bag below; the namespace assignment at __gofastr = { … } would
  // otherwise wipe direct assignments here.
  let _wcr;
  const _wready = new Promise((resolve) => { _wcr = resolve; });

  // Widget catalog fetch. The live endpoint is session-gated and per-page
  // scoped (?page= filters widgets to the current route). A serverless
  // export never composes widgets-boot, the `static` composition omits it
  // and rpc-stub intercepts data-cui-open clicks, so this fetch only ever
  // runs in the live (full) composition.
  fetch(_lp('/__gofastr/widgets') + '?page=' + encodeURIComponent(_unlocalize(location.pathname)),
        { headers: { 'X-Gofastr-Widget-Discovery': '1' } })
    // A followed 3xx is another URL's list: no catalog.
    .then((r) => (r.ok && !r.redirected ? r.json() : null))
    .then(async (list) => {
      // No catalog, an empty one, or a widgets module that failed to
      // load: settle readiness and stop. The catalog is stored before
      // the load, since it is data the module does not own: a click
      // after a failed load reaches openWidget, which retries the load
      // and finds its widget. Nothing waits on a module that is not
      // coming.
      try {
        if (Array.isArray(list) && list.length) {
          // The widget runtime now ships as a split module. Make sure it's
          // loaded before iterating mounts, covers the case where no
          // [data-cui-widget] marker is present in initial HTML (the
          // marker scanner wouldn't have fired) but server-side
          // registration says there are widgets to mount.
          // loadModule resolves on registration, and widgets.js
          // registers after it assigns mountWidget.
          const cat = window.__gofastr._widgetCatalog = window.__gofastr._widgetCatalog || {};
          for (const item of list) cat[item.cfg.name] = item;
          await window.__gofastr.loadModule('widgets');
          for (const item of list) {
            if (item.hidden) continue; // open later via openWidget(name)
            // Non-hidden widgets auto-mount at boot. Chrome HTML is
            // fetched lazily from cfg.chromePath so the registry stays
            // small; if the page already SSR-inlined this widget (root
            // element exists in DOM), mountWidget short-circuits to a
            // hydrate-only path. Either way, the result is a wired
            // widget root.
            window.__gofastr._mountByName(item.cfg.name);
          }
          // Open any widget whose deep link matches the current URL. On
          // a full page load the host has usually SSR-inlined that
          // chrome (framework/uihost injectWidgetSSR) and _mountByName
          // hydrates it here; this pass is what covers SPA navigations
          // and popstate, where no fresh HTML arrives.
          window.__gofastr._syncDeepLinks();
        }
      } catch (_) {}
      // The eager click delegator (installed at boot, see below)
      // awaits this Promise.
      _wcr();
    })
    .catch(() => { _wcr(); });

  // === EAGER WIDGET DELEGATORS =========================================
  // The data-cui-open click handler, data-cui-toast click handler, and
  // popstate listener used to live inside the /__gofastr/widgets
  // catalog fetch's .then() callback. That meant on a slow network the
  // very first click on an open trigger had no handler to receive it,
  // the catalog hadn't returned yet, so the .then() hadn't run.
  //
  // We install them here at boot, before the catalog fetch. Each
  // handler awaits loadModule('widgets') (via the openWidget stub on
  // __gofastr) so it works regardless of whether the catalog has
  // resolved. Idempotent via document.__fuiOpenDispatch.


  function _installEagerWidgetDelegators() {
    if (document.__fuiOpenDispatch) return;
    document.__fuiOpenDispatch = true;
    document.addEventListener('click', (e) => {
      // Toast trigger: data-cui-toast='<json>' fires a client toast.
      const toastBtn = e.target.closest && e.target.closest('[data-cui-toast]');
      if (toastBtn) {
        e.preventDefault();
        // The header path's dispatcher: it loads the module, and a
        // page with no stack (NS.toast answers null) or a module that
        // fails to load still shows the toast in the kernel's
        // fallback region instead of nothing.
        try {
          const cfg = JSON.parse(toastBtn.getAttribute('data-cui-toast'));
          window.__gofastr._toastOrFallback(cfg);
        } catch (_) {}
        return;
      }
      const btn = e.target.closest && e.target.closest('[data-cui-open]');
      if (!btn) return;
      // The live catalog path mounts the widget module; RPC controls inside
      // the mounted chrome await src/rpc.js in their scoped listeners.
      const name = btn.getAttribute('data-cui-open');
      if (!name) return;
      e.preventDefault();
      const raw = btn.getAttribute('data-cui-deeplink') || '';
      const overrides = {};
      if (raw) {
        // Degrade-don't-throw: a malformed percent escape throws URIError
        // AFTER preventDefault, which would consume the open click; a bad
        // pair is skipped (the selector-guard family's containment).
        for (const pair of raw.split('&')) {
          if (!pair) continue;
          const eq = pair.indexOf('=');
          if (eq < 0) continue;
          try {
            overrides[decodeURIComponent(pair.slice(0, eq))] =
              decodeURIComponent(pair.slice(eq + 1));
          } catch (_) {}
        }
      }
      const anchorPref = btn.getAttribute('data-cui-popover-anchor');
      (async () => {
        // The widgets module + catalog must both be ready before
        // openWidget can find the entry. Awaiting both here keeps the
        // click responsive even on a cold-cache page where the user
        // clicked faster than /__gofastr/widgets returned.
        await window.__gofastr.loadModule('widgets').catch(() => {});
        await _wready;
        // btn rides along so openWidget can read data-cui-ctx (#321):
        // the trigger's context keys the chrome fetch + cache. Read in
        // the module, not here: core bytes are the scarce ones.
        await window.__gofastr.openWidget(name, { params: overrides, pushUrl: true, btn });
        if (anchorPref !== null) {
          await window.__gofastr.loadModule('popover');
          window.__gofastr._anchorPopover(name, btn, anchorPref || 'bottom');
        }
      })();
    });
  }
  _installEagerWidgetDelegators();

  // Re-fetch the widget catalog after SPA-nav so page-scoped widgets
  // registered with .Pages("/route") become available when the user
  // arrives via partial-fetch (instead of a full page load).
  //
  // Without this, the boot-time catalog only contains widgets visible
  // on the initial path; clicking a data-cui-open trigger for a
  // page-scoped widget elsewhere silently bails because the entry is
  // missing from _widgetCatalog.
  //
  // Owned here, not by boot: boot is in every composition, and the
  // static one has no live endpoint to ask (widgets-boot-static keeps
  // its own navigate pass against the dumped catalog).
  //
  // The fetch is idempotent, entries are MERGED into the catalog
  // (existing entries from boot don't get overwritten unless the
  // server returns a changed version). Non-hidden widgets that
  // aren't already mounted are mounted now. Then _syncDeepLinks runs
  // so the URL's modal/drawer query params open the right surface.
  window.addEventListener('gofastr:navigate', (e) => {
    const path = (e && e.detail && e.detail.path) || location.pathname;
    fetch(_lp('/__gofastr/widgets') + '?page=' + encodeURIComponent(_unlocalize(path)),
          { headers: { 'X-Gofastr-Widget-Discovery': '1' } })
      .then((r) => (r.ok && !r.redirected ? r.json() : null))
      .then(async (list) => {
        if (!Array.isArray(list) || list.length === 0) return;
        const G = window.__gofastr;
        if (!G) return;
        // Make sure the widgets module is loaded, the initial page
        // may have had no widgets, so loadModule('widgets') was never
        // triggered and mountWidget isn't on the namespace yet.
        try { await G.loadModule('widgets'); } catch (_) { return; }
        G._widgetCatalog = G._widgetCatalog || {};
        for (const item of list) {
          const cfg = item.cfg;
          G._widgetCatalog[cfg.name] = item;
          // Auto-mount non-hidden widgets that aren't already on the
          // page. Hidden widgets (Modal / Drawer / Popover) stay
          // hidden until openWidget is called from a trigger.
          if (item.hidden) continue;
          if (G._mountByName) G._mountByName(cfg.name);
        }
        if (G._syncDeepLinks) G._syncDeepLinks();
      })
      .catch(() => { /* navigation succeeded; missing catalog is non-fatal */ });
  });

// boot.js: kernel boot tail (always composed LAST, after every other fragment).
// These declarations run AFTER nav/signals/widgets-boot have loaded because
// _initialPass() is invoked synchronously here, probes nav's loadPage, and
// calls _injectSignalAria (signals). Function declarations (loadModule,
// _scanForModules, _prefetch, _installEagerWidgetDelegators) are hoisted into
// the shared IIFE scope so earlier fragments can reference them at event time.

  // Event delegation: [data-action]
  document.addEventListener('click', (e) => {
    const target = e.target.closest('[data-action]');
    if (!target) return;

    const action = target.getAttribute('data-action');
    const name = closestAttr(e.target, 'data-component')
      ?? closestAttr(e.target, 'data-widget');

    if (name && action) {
      e.preventDefault();
      window.__gofastr.trigger(name, action, collectParams(target));
    }
  });

  // Event delegation: [data-action-type]
  for (const eventType of ['input', 'change', 'submit']) {
    document.addEventListener(eventType, (e) => {
      const target = e.target.closest(`[data-action-type="${eventType}"], [data-action-${eventType}]`);
      if (!target) return;

      const action = target.getAttribute(`data-action-${eventType}`) || target.getAttribute('data-action');
      if (!action) return;

      const name = closestAttr(e.target, 'data-component')
        ?? closestAttr(e.target, 'data-widget');

      if (name) {
        e.preventDefault();
        const params = { ...collectParams(target), value: e.target.value ?? '', eventType };
        window.__gofastr.trigger(name, action, params);
      }
    });
  }

  // Two-way binding: [data-bind]
  document.addEventListener('input', (e) => {
    const target = e.target.closest('[data-bind]');
    if (!target) return;
    const key = target.getAttribute('data-bind');
    if (!key) return;
    window.__gofastr.setState(key, target.value);
  });

  // Hydration on first interaction
  const hydrated = new Set();

  const hydrate = (name) => {
    if (hydrated.has(name)) return;

    const el = document.querySelector(`[data-widget="${CSS.escape(name)}"]`)
      ?? document.querySelector(`[data-component="${CSS.escape(name)}"]`);
    if (!el) return;
    // Mark hydrated only once the element was actually found, marking
    // before the lookup made a too-early call (root not yet in the DOM)
    // permanently block that id's behavior script. Never cleared across
    // navs: the behavior script URL is keyed by id, so process-lifetime
    // dedup is correct for re-inserted same-id DOM.
    hydrated.add(name);

    // data-behavior is the most privileged attribute the runtime reads:
    // it becomes a <script src>. Only the one shape the framework emits
    // is honoured, /__gofastr/widget/<id>.js, written by
    // core-ui/component/component.go, because any other value turns
    // attribute injection into script execution. CSP is not the answer
    // here: a SAME-origin JS route (an upload store, a generated SDK
    // .js, a plugin asset) executes under `default-src 'self'`.
    const scriptSrc = el.getAttribute('data-behavior');
    if (scriptSrc && /^\/__gofastr\/widget\/[A-Za-z0-9_-]+\.js(\?[^"'<>]*)?$/.test(scriptSrc)) {
      const script = document.createElement('script');
      script.src = scriptSrc;
      document.head.appendChild(script);
    } else if (scriptSrc) {
      console.warn('[gofastr] refused data-behavior src:', scriptSrc);
    }
  };

  // MutationObserver for auto-hydration
  const setupMutationObserver = () => {
    if (typeof MutationObserver === 'undefined') return;
    if (!document.body) return;

    const setupHydration = (el) => {
      const id = el.getAttribute('data-component') ?? el.getAttribute('data-widget');
      if (!id) return;
      el.addEventListener('focus', () => hydrate(id), { once: true });
      el.addEventListener('mouseenter', () => hydrate(id), { once: true });
    };

    const observeNode = (node) => {
      if (node.nodeType !== 1) return;
      if (node.getAttribute?.('data-component') || node.getAttribute?.('data-widget')) {
        setupHydration(node);
      }
      for (const child of node.querySelectorAll?.('[data-component], [data-widget]') ?? []) {
        setupHydration(child);
      }
      // Demand-load split runtime modules whose marker attributes show
      // up in injected subtrees (RPC innerHTML replacement, signal
      // swaps, island updates). Without this, dynamically-inserted
      // popover triggers / toast stacks would never load their
      // module and behave as dead DOM.
      _scanForModules(node);
      // And re-run scanners of modules that ARE loaded so they wire
      // any newly-inserted elements (toast TTL, drop zones).
      const G = window.__gofastr;
      // Component sheets for every insertion path, not only the swaps
      // that scan for themselves: compiled actions (updateHTML,
      // fetchPage) and module-written markup left theirs unloaded.
      if (G?.scanAndLoadCSS) {
        G.scanAndLoadCSS(node);
        G.loadComponentCSS(node.dataset.cuiComp);
        G.loadComponentCSS(node.dataset.cuiScope);
      }
      if (G && G._moduleScanners) {
        for (const name in G._moduleScanners) {
          if (G.loadedModules && G.loadedModules[name]) {
            try { G._moduleScanners[name](node); } catch (_) {}
          }
        }
      }
    };

    new MutationObserver((mutations) => {
      for (const m of mutations) {
        for (const node of m.addedNodes) observeNode(node);
      }
    }).observe(document.body, { childList: true, subtree: true });
  };

  if (document.body) {
    setupMutationObserver();
  } else {
    document.addEventListener('DOMContentLoaded', setupMutationObserver);
  }

  // SSE Island Support ships in core-ui/runtime/src/sse.js, loaded on
  // demand when the page holds a push target (any [data-island] region
  // or the offline banner). The module self-installs an EventSource and
  // reflects "island" events into matching [data-island] regions, and
  // re-evaluates the document's targets after every apply so leaving
  // the last one closes the stream. Reconnect lives in the module too.

  // === MODULE LOADER ===================================================
  // loadModule(name) returns a cached Promise covering the module AND
  // its requirements. Multiple callers for the same name share one
  // fetch. A module that declared requirements (the behaviours block
  // carries them as r) has every requirement loadModule'd first, in
  // parallel, and only then is its own script appended, so the
  // dependent evaluates with its primitive already registered; a
  // requirement's own requirements load the same way.
  //
  // Readiness is registration, not transport. A module announces
  // itself by setting window.__gofastr.loadedModules[name] to a truthy
  // value, so the promise resolves only when that flag is an own,
  // truthy property after the script's load event. A script that ran
  // and never registered rejects with 'module failed to register' and
  // drops the cached promise, so a retry fetches again rather than
  // handing out a fulfilled promise for a module that is not there.
  // A fetch error rejects the same way, and so does a failed
  // requirement: the dependent's cached promise is dropped too.
  //
  // Cache-busting: the host SSRs the per-module hash into a JSON
  // manifest under <script id="gofastr-runtime-modules">. The loader
  // reads it once; if a name is missing from the manifest, we fall
  // back to an un-versioned URL (works in dev, may pollute caches in
  // prod, the manifest is the source of truth).
  const _moduleManifest = (() => {
    // Live pages assign the global via /__gofastr/manifest.js (loaded
    // before runtime.js); export mode keeps the inline JSON block.
    if (window.__gofastr_runtime_modules) return window.__gofastr_runtime_modules;
    try {
      const el = document.getElementById('gofastr-runtime-modules');
      if (!el) return {};
      return JSON.parse(el.textContent || '{}');
    } catch (_) { return {}; }
  })();
  // Map-keyed, not {}: module names arrive through DOM attributes
  // (data-cui-prefetch, data-behavior) and a plain-object cache keyed by
  // "__proto__" re-parents through the setter while "constructor" reads
  // as a truthy inherited entry. Map keys are plain strings.
  const _modulePromises = new Map();
  function loadModule(name) {
    const lm = window.__gofastr.loadedModules;
    if (lm && own(lm, name) && lm[name]) {
      return Promise.resolve();
    }
    const cached = _modulePromises.get(name);
    if (cached) return cached;
    const modPromise = new Promise((resolve, reject) => {
      // Module names come from DOM attributes (data-cui-prefetch,
      // data-behavior), so they are caller input. Without a shape check
      // a "../../../evil" token normalizes out of the runtime serve
      // route and onto an arbitrary same-origin script, which then runs
      // with the page's full privileges. Every emitted name is a plain
      // identifier. Rejecting from inside the promise, with the loader's
      // own failure message, keeps the guard within the core bundle's
      // gzip budget -- both are bytes this file already carried.
      if (!/^[\w-]+$/.test(name)) return reject(new Error('module failed'));
      const v = _moduleManifest[name] || '';
      const url = '/__gofastr/runtime/' + name + '.js' + (v ? '?v=' + v : '');
      // Requirements live in the registered descriptors only:
      // embedded modules have no channel to declare one. Looked up
      // here rather than at scan time so every load path (marker scan,
      // idle queue, hover prefetch, the interaction bridge) honors
      // them without each knowing about the block.
      const reqs = (_registered.find((m) => m.name === name) || {}).requires || [];
      Promise.all(reqs.map(loadModule)).then(() => {
        const s = document.createElement('script');
        s.src = url;
        s.async = false;
        s.onload = () => {
          const lm2 = window.__gofastr.loadedModules;
          if (!(lm2 && own(lm2, name) && lm2[name])) {
            // The script ran and never registered: not loaded. Drop
            // the cached promise so a retry fetches again.
            _modulePromises.delete(name);
            reject(new Error('module failed to register'));
            return;
          }
          resolve();
        };
        s.onerror = () => {
          // Drop the cached promise so a retry fires a fresh request.
          _modulePromises.delete(name);
          reject(new Error('module failed'));
        };
        document.head.appendChild(s);
      }, () => {
        _modulePromises.delete(name);
        reject(new Error('module failed'));
      });
    });
    // One promise per name covers the requirements and the module, so
    // every caller of this load shares the single fetch above.
    _modulePromises.set(name, modPromise);
    return modPromise;
  }

  // Live compositions keep one document-level bridge so a click or submit
  // that lands while rpc.js is downloading is retained. rpc-stub installs its
  // own static-only guard before boot runs, so static exports never install
  // this bridge or request the server-backed module.
  // The bridge calls preventDefault() BEFORE awaiting src/rpc.js, so a
  // module that never arrives (404 because the host does not serve
  // /__gofastr/runtime/<name>.js, or a network blip) would otherwise eat
  // the user's click or submit in silence. When RPC was compiled into
  // core that could not happen; carving it out reintroduced the risk, so
  // failure has to be loud, and a form has to still go somewhere.
  const _rpcUnavailable = () => {
    console.warn('[gofastr] RPC module unavailable — serve /__gofastr/runtime/rpc.js');
  };
  // A submit button can override the form's action, encoding and browsing
  // context. Resolve those before deciding whether the browser should own it.
  const _formAttr = (form, submitter, name) => submitter && submitter.hasAttribute('form' + name)
    ? submitter.getAttribute('form' + name) : form.getAttribute(name);
  window.__gofastr._formAttr = _formAttr;
  const _rpcFormFallback = (form, submitter) => {
    _rpcUnavailable();
    // Native submit is correct ONLY for a form the browser could have
    // submitted itself, a data-cui-spa form with an ordinary enctype.
    //
    // A data-cui-rpc form targets a JSON API: the resource engine emits it
    // with no enctype at all and rpc.js builds the JSON body, so submitting
    // it natively posts urlencoded (415) or cannot issue its declared
    // PUT/PATCH at all (405). Either way the user is navigated off the page
    // to a raw error and everything they typed is gone, strictly worse
    // than staying put. An application/json enctype is unsendable natively
    // for the same reason. In those cases the warning is the whole remedy.
    if (form.hasAttribute('data-cui-rpc') || form.hasAttribute('data-kiln-tool')) return;
    // The bridge POSTs a form with no method and fetches PUT/PATCH as given;
    // the browser would send either as a GET with the fields in the URL.
    // A GET carries no body, so its enctype never blocks the replay.
    const method = (_formAttr(form, submitter, 'method') || '').toLowerCase();
    if (method !== 'get' && (method !== 'post' || (_formAttr(form, submitter, 'enctype') || '').toLowerCase() === 'application/json')) return;
    // Call the prototype method, not form.submit: HTML named-property
    // lookup shadows it with any control named "submit", so a form
    // carrying <button name="submit"> would throw here and the catch
    // would swallow the user's submission after we already prevented it.
    // Re-enter the native submission algorithm with the original submitter:
    // its name/value and form* overrides are lost by form.submit(). The flag
    // lets both delegated bridges stand down on this synchronous replay.
    form._cuiNativeSubmit = true;
    try { HTMLFormElement.prototype.requestSubmit.call(form, submitter || undefined); }
    catch (_) {}
    finally { delete form._cuiNativeSubmit; }
  };
  // Widget-scoped listeners live in the widgets MODULE and prevent the
  // default before awaiting rpc too, so they need the same recovery. The
  // document bridge respects their preventDefault, while handling plain
  // SPA forms that widget listeners leave to the document.
  window.__gofastr._rpcUnavailable = _rpcUnavailable;
  window.__gofastr._rpcFormFallback = _rpcFormFallback;

  if (!document.__fuiStaticDispatch && !document.__fuiGlobalDispatch) {
    document.__fuiGlobalDispatch = true;
    document.addEventListener('click', async (e) => {
      // A MOUNTED widget root (its own listener wired: __fuiW, set and
      // cleared by the widgets module) owns its RPC clicks. SSR-inlined
      // chrome the catalog has not mounted yet has no listener, so the
      // bridge serves it rather than swallow the click.
      if (e.target.closest('[data-cui-widget]')?.__fuiW) return;
      // Signal mutations win, as they did when one delegator owned both:
      // the old handler set the signal and RETURNED without consulting
      // data-cui-rpc. Two listeners would otherwise both fire on an
      // element carrying each attribute.
      if (e.target.closest('[data-cui-signal-set],[data-cui-signal-inc],[data-cui-signal-toggle]')) return;
      const node = e.target.closest('[data-cui-rpc],[data-kiln-tool]');
      if (!node || node.tagName === 'FORM') return;
      e.preventDefault();
      try {
        await loadModule('rpc');
        await window.__gofastr.dispatchRPC(node);
      } catch (_) { _rpcUnavailable(); }
    });

    document.addEventListener('submit', async (e) => {
      const form = e.target.closest('form');
      if (!form || e.defaultPrevented || form._cuiNativeSubmit === true) return;
      // Confirm gate runs BEFORE every branch below, so a plain native
      // POST form (which leaves at the enctype check) is gated too. The
      // submitter wins over the form: one form can carry several submit
      // buttons of different destructive weight. Callers that already
      // gated pass {confirmed:true} so rpc.js does not prompt twice.
      // The gate cancels the submit and lets the confirm module ask and
      // submit again (marked form._ok); a module that cannot load fails
      // closed.
      const sub = e.submitter;
      const msg = (sub && sub.getAttribute('data-cui-confirm')) || form.getAttribute('data-cui-confirm');
      if (msg && !form._ok) {
        e.preventDefault();
        loadModule('confirm').then(() => window.__gofastr.confirm(form, sub));
        return;
      }
      if (!form.hasAttribute('data-cui-rpc') && !form.hasAttribute('data-kiln-tool')) {
        const target = _formAttr(form, sub, 'target') ?? document.querySelector('base[target]')?.getAttribute('target');
        const method = (_formAttr(form, sub, 'method') || '').toLowerCase();
        const enctype = (_formAttr(form, sub, 'enctype') || '').toLowerCase();
        // Preserve native browsing contexts, dialog submits, image coordinates
        // and plain-text POSTs before any asynchronous module loading.
        if ((target && target.toLowerCase() !== '_self') || method === 'dialog' ||
            (sub && sub.type === 'image') || (method !== 'get' && enctype === 'text/plain')) return;
        const action = _formAttr(form, sub, 'action') || location.href;
        if (!window.__gofastr._sameOrigin(new URL(action, document.baseURI))) return;
        if (enctype !== 'application/json' && !form.hasAttribute('data-cui-spa')) return;
      }
      e.preventDefault();
      try {
        await loadModule('rpc');
        await window.__gofastr.dispatchRPC(form, null, { confirmed: true, submitter: sub });
      } catch (_) { _rpcFormFallback(form, sub); }
    });

    document.addEventListener('input', (e) => {
      // Open a focused combobox immediately. The network request remains
      // debounced in the RPC module, but the listbox should react to typing
      // before the response arrives.
      const combo = e.target && e.target.closest && e.target.closest('[role="combobox"]');
      if (combo) {
        const lbId = combo.getAttribute('aria-controls');
        const lb = lbId ? document.getElementById(lbId) : null;
        if (lb) {
          combo.setAttribute('aria-expanded', 'true');
          lb.removeAttribute('hidden');
        }
      }
      const form = e.target.closest('[data-cui-rpc][data-cui-rpc-trigger="input"]');
      if (!form) return;
      loadModule('rpc')
        .then(() => window.__gofastr.dispatchRPC(form, 'input'))
        .catch(() => {});
    });
  }

  // Hover/focus prefetch: any element with data-cui-prefetch="<name>"
  // kicks off the module fetch as soon as the user hovers or
  // keyboard-focuses it. By the time they click, the module is
  // resolved. Capture phase; an element is marked attempted only once
  // its fetch SUCCEEDS, so there is no churn on every mouse move but a
  // failed fetch is retried on the next hover/focus.
  const _prefetchAttempted = new WeakSet();
  function _prefetch(e) {
    const node = e.target && e.target.closest && e.target.closest('[data-cui-prefetch]');
    if (!node || _prefetchAttempted.has(node)) return;
    // Mark attempted only on success: a failed fetch (network blip,
    // a host not serving the module) must not pin the element, or the
    // bridge never retries — and `tabs` deliberately has no
    // _moduleMarkers entry, so no scanner picks it up either; vacate
    // panels would then stay empty for the page lifetime. Re-hovers
    // while a fetch is in flight cost nothing: loadModule dedups.
    const names = node.getAttribute('data-cui-prefetch').split(/\s+/).filter(Boolean);
    for (const n of names) {
      loadModule(n).then(() => { _prefetchAttempted.add(node); }, () => {});
    }
  }
  document.addEventListener('pointerover', _prefetch, { capture: true, passive: true });
  document.addEventListener('focusin', _prefetch, { capture: true });

  // === DRAG-TO-DISMISS (bottom-sheet style) ============================
  // Pointer-driven drag-to-close for widgets (DragDismiss /
  // preset.BottomSheet) lives in the split-runtime module at
  // core-ui/runtime/src/dragdismiss.js, demand-loaded via the
  // [data-cui-drag-dismiss="true"] scanner below (SSR-inlined sheets
  // load at boot; dynamically-opened chrome is caught by the
  // MutationObserver scan when it's appended to <body>).

  // === DEMAND-LOAD SCANNERS ===========================================
  // Marker-driven modules are rescanned after boot, SPA navigation,
  // and DOM insertion.
  const _moduleMarkers = [
    { name: 'rpc', selector: '[data-cui-rpc],[data-kiln-tool]' },
    // Computed: client-side derived signals (core-ui/store). The module
    // subscribes each [data-cui-computed] node to its dependency signals
    // and recomputes via the host-registered reducer on any change.
    { name: 'computed',   selector: '[data-cui-computed]' },
    // Compute: registered same-origin Web Worker and WebAssembly assets.
    // The marker only loads the imperative __gofastr.compute API.
    { name: 'compute',    selector: '[data-cui-compute]' },
    { name: 'popover',    selector: '[data-cui-popover-anchor]' },
    // SSE: background event stream, opened only for a page that takes
    // pushes. The markers are the PUSH TARGETS (any island — the
    // server can PushUpdate any island id — plus the offline banner
    // that reads the stream's mirrored state), not the availability
    // meta: <meta name="gofastr-sse"> stays on every session-bearing
    // page and now means "SSE exists", while the module (and with it
    // the EventSource, one of the tab's ~6 HTTP/1.1 connections) loads
    // only when a target is on the page. Idle-loaded, never blocks
    // first interaction; the channel only carries push updates, not
    // user actions. See ROADMAP §8 Phase 5.
    { name: 'sse',        selector: '[data-island],[data-hui-system-offline]', idle: true },
    // Widgets: any SSR-inlined widget element or any data-cui-open
    // trigger button anywhere on the page. The catalog auto-mount
    // path explicitly awaits loadModule('widgets') too, so this
    // scanner just covers the marker-on-page path. Idle-loaded,
    // SSR-inlined widget chrome is already on the page; mounting is
    // hydration not first paint. See ROADMAP §8 Phase 5.
    { name: 'widgets',    selector: '[data-cui-widget],[data-cui-open]', idle: true },
    // TextArea autogrow: applies the same auto-resize handler the
    // widget runtime uses for textareas anywhere on the page.
    { name: 'textarea',       selector: 'textarea[data-cui-autogrow]' },
    // DragDismiss: pointer drag-to-close for BottomSheet-style widgets.
    { name: 'dragdismiss', selector: '[data-cui-drag-dismiss="true"]' },
    // Dropdown: click-toggle + click-outside dismiss + Esc close.
    { name: 'dropdown',         selector: '[data-cui-dropdown-wrap]' },
    // Reveal: IntersectionObserver-driven entrance animations.
    { name: 'reveal',           selector: '[data-cui-reveal]' },
    // Animate: signal-driven CSS class toggling.
    { name: 'animate',          selector: '[data-cui-animate-signal]' },
    // Poll: page-level region polling. data-cui-poll="<duration>" +
    // data-cui-poll-src="<url>" re-fetches the URL on the cadence and
    // swaps the response HTML into the element. The module owns
    // parse/clamp/jitter/pause/back-off/teardown; core only loads it.
    { name: 'poll',         selector: '[data-cui-poll]' },
    // Envelope (fills, snapshots, scroll anchors): NOT a boot trigger.
    // The outlet/area marker alone costs nothing until the first
    // navigation that needs the module: frag/nav.js starts its load
    // beside that navigation's page fetch (the opt-in decision of
    // 2026-09-28 — a marketing page whose only layout feature is one
    // outlet must not pay a module request on first paint). The one
    // boot exception is the deferred trigger below.
    // Loading content: the inert server-rendered template beside an
    // outlet. Before it loads the busy dim alone shows.
    // View transitions: the document declares a [data-cui-vt] cell or
    // a data-cui-vt-kinds vocabulary. Before it loads swaps run bare
    // and the X-Gofastr-Transition pick is not read.
    { name: 'transition', selector: '[data-cui-vt-kinds],[data-cui-vt]' },
];

  // Registered behaviours (registry.RegisterBehavior): a component's own
  // package ships its module and declares its markers and interactions;
  // the host lists them beside the manifest, live pages as
  // window.__gofastr_behaviors from /__gofastr/manifest.js, exports and
  // the embed frame as the inline #gofastr-behaviors block. Same scan,
  // same bridge, same loader, same module contract as the table above:
  // the bridge's install loop and _scanForModules both iterate this
  // list after the kernel's own table, so a registered behaviour's
  // interactions are retained and replayed exactly as a table module's
  // are. Parsed here, before the bridge, because the bridge installs
  // its listeners at boot and reads this list in the same pass.
  const _registered = (() => {
    try {
      const o = window.__gofastr_behaviors ||
        JSON.parse((document.getElementById('gofastr-behaviors') || {}).textContent || '{}');
      // Own entries only: the block is JSON from the host, but the
      // kernel never reads a registry through the prototype chain.
      // requires is the r array: the modules loadModule'd before this
      // one (see the loader above). interactions is the x array, in
      // the bridge's own spec shape (event/selector/keys/scope): the
      // registry validates the shape at registration, and the filter
      // keeps a malformed block from reaching the install loop below —
      // a non-array x throws here inside the try, so the kernel drops
      // the registry and boots on its own table rather than dying in
      // that loop, and an entry without an event never installs.
      return Object.entries(o).map(([n, v]) => ({
        name: n,
        selector: v.s.join(','),
        idle: !!v.i,
        requires: v.r || [],
        interactions: (v.x || []).filter((y) => y && y.event),
      }));
    } catch (_) {
      // Silent on purpose: the kernel boots on its own module table
      // either way, and the bytes for a warning here do not clear the
      // core budget (measured: +34 gz at level 6 for the long wording,
      // +17 for the shortest). Re-measure the clearance against
      // budget_test.go's comment history before spending it rather
      // than trusting a number written here; it moves under the
      // kernel.
      return [];
    }
  })();

  // Demand-loaded modules — the kernel's table and registered
  // descriptors alike — may declare interactions that must survive
  // their own cold-cache fetch. This bridge is deliberately
  // metadata-driven: core owns event retention/replay, while feature
  // modules and registered behaviours own selectors and behavior. No
  // optional component's selectors or policy are hard-coded in the
  // always-loaded runtime, and the loop body below is the same for
  // both tables: what it does with a descriptor is already right.
  const _interactionReplay = new WeakSet();
  const _warnModuleUnavailable = (name) => {
    console.warn('[gofastr] ' + name + ' module unavailable — retrying may help');
  };
  // The selector is guarded, not trusted. The Go registry validates
  // every descriptor it builds, but the behaviours block is JSON the
  // host hands the page, and a hand-written or corrupted one can carry
  // a well-shaped entry whose selector the browser refuses — or a
  // keydown with no scope, where querySelector('') throws. That throw
  // lands here, inside an async listener, long after the parser's try:
  // an unhandled rejection on every such event, retention silently
  // dead, nothing in the page saying why. Refusing to resolve a node
  // is the same answer as not matching one.
  const _interactionNode = (e, spec) => {
    try {
      if (spec.event === 'keydown') {
        // A modified key is a browser or OS shortcut, never the module's.
        if (e.altKey || e.ctrlKey || e.metaKey || e.shiftKey ||
            !spec.keys || !spec.keys.includes(e.key) ||
            !document.querySelector(spec.scope || '')) return null;
        return e.target && e.target.dispatchEvent ? e.target : document.body;
      }
      return e.target && e.target.closest && e.target.closest(spec.selector);
    } catch (_) { return null; }
  };
  const _replayInteraction = (e, node) => {
    const init = { bubbles: true, cancelable: true, composed: true };
    if (e.type === 'click') {
      init.view = window;
      init.detail = e.detail;
      init.button = e.button;
      init.buttons = e.buttons;
      init.clientX = e.clientX;
      init.clientY = e.clientY;
    } else if (e.type === 'keydown') {
      init.key = e.key;
      init.code = e.code;
      init.location = e.location;
      init.repeat = e.repeat;
      init.ctrlKey = e.ctrlKey;
      init.altKey = e.altKey;
      init.shiftKey = e.shiftKey;
      init.metaKey = e.metaKey;
    }
    let replay;
    try {
      replay = new e.constructor(e.type, init);
    } catch (_) {
      replay = new Event(e.type, init);
    }
    node.dispatchEvent(replay);
  };
  for (const marker of _moduleMarkers.concat(_registered)) {
    for (const spec of marker.interactions || []) {
      document.addEventListener(spec.event, async (e) => {
        const node = _interactionNode(e, spec);
        if (!node || _interactionReplay.has(node) ||
            window.__gofastr.loadedModules?.[marker.name]) return;
        e.preventDefault();
        try {
          await loadModule(marker.name);
          _interactionReplay.add(node);
          try { _replayInteraction(e, node); }
          finally { _interactionReplay.delete(node); }
        } catch (_) {
          _warnModuleUnavailable(marker.name);
        }
      });
    }
  }
  function _scanForModules(root) {
    const scope = root && root.querySelectorAll ? root : document;
    const idleQueue = [];
    for (const m of _moduleMarkers.concat(_registered)) {
      const { name, selector, idle } = m;
      // rpc-stub owns static-export clicks. The marker table is shared by all
      // compositions, so skip this one entry instead of fetching dead code.
      if (name === 'rpc' && document.__fuiStaticDispatch) continue;
      // Skip if the module is already loaded, its own internal scanner
      // takes care of newly inserted DOM via the MutationObserver.
      const lm = window.__gofastr.loadedModules;
      if (lm && own(lm, name) && lm[name]) continue;
      // Test the scope node ITSELF as well as its descendants: a
      // lazily-mounted widget root appended to <body> carries root
      // markers (data-cui-drag-dismiss) on the node handed to us.
      if (!(scope.matches?.(selector) || scope.querySelector(selector))) continue;
      if (idle) {
        idleQueue.push(name);
      } else {
        loadModule(name).catch(() => {});
      }
    }
    if (idleQueue.length) _scheduleIdleModules(idleQueue);
  }
  // Phase 5 idle fallback (ROADMAP §8). Modules tagged `idle: true` in
  // `_moduleMarkers` ship after FCP via requestIdleCallback so they
  // never compete with the user's first interaction. Safari < 16.2 and
  // Firefox < 55 lack rIC, fall back to setTimeout(0) which still
  // runs after the current task settles.
  function _scheduleIdleModules(names) {
    const rIC = window.requestIdleCallback || ((fn) => setTimeout(fn, 0));
    rIC(() => {
      for (const n of names) loadModule(n).catch(() => {});
    });
  }
  // Re-scan after SPA-nav swaps content. Two phases:
  //
  //  1. Marker scan: modules that AREN'T loaded yet get fetched when
  //     their marker appears in the freshly-swapped content. (Fresh
  //     page brings new feature → load on demand.)
  //
  //  2. Per-module rescan: modules that ARE loaded re-run their
  //     scanner against the new DOM. Modules opt in by registering
  //     a function on `window.__gofastr._moduleScanners[name]`; the
  //     contract is "wire any new elements inside `root`, idempotent
  //     against already-wired elements". This is how SSR-inlined
  //     toast stacks on the new page get their TTL timers armed,
  //     without it, `_initToasts` would have run only once at module
  //     load before that DOM existed.
  window.addEventListener('gofastr:navigate', () => {
    const G = window.__gofastr;
    // The idle-loaded activelink module reads this at load: only a
    // document that already navigated client-side has a server mark
    // for it to clear (src/activelink.js).
    if (G) G._navigated = true;
    _scanForModules(document);
    // Task A: re-inject aria-live onto any new signal nodes from the swapped page.
    _injectSignalAria();
    if (G && G._moduleScanners) {
      for (const name in G._moduleScanners) {
        if (G.loadedModules && G.loadedModules[name]) {
          try { G._moduleScanners[name](document); } catch (_) {}
        }
      }
    }
  });

  // Close any open modal widgets on SPA navigation. Toasts/panels
  // (non-backdrop'd widgets) survive, they're page-independent
  // UI like build-progress banners.
  window.addEventListener('gofastr:navigate', () => {
    const G = window.__gofastr;
    if (!G || !G._modalStack) return;
    for (const name of [...G._modalStack]) G.closeWidget(name);
  });

  const _bootstrapComponentCSS = () => {
    const G = window.__gofastr;
    if (!G?.scanAndLoadCSS) return;
    // Seed _pendingLinks with names already covered by the SSR
    // bundle, so the on-demand scanner doesn't redundantly load
    // per-component sheets. The names live on the bundle's
    // data-cui-bundle attribute (a stable contract), not parsed
    // from the URL: a <link> to the bundle, or the <style> a page
    // whose head fits the first round trip carries it in.
    document.head.querySelectorAll('[data-cui-bundle]').forEach((l) => {
      // A bundle link that failed (head sheets have settled by the time
      // this runs) covers nothing: leave its names to the scanner. A
      // failed sheet is an empty one (the browser still gives it a
      // CSSStyleSheet), and a bundle is never empty.
      if (l.tagName === 'LINK') try { if (!l.sheet || !l.sheet.cssRules.length) return; } catch (_) {}
      const names = (l.getAttribute('data-cui-bundle') || '').split(',');
      for (const n of names) if (n) G._pendingLinks.add(n);
    });
    G.scanAndLoadCSS(docEl);
    G.scheduleIdleLoads();
  };

  // Event listeners attach unconditionally, they fire only when the
  // matching event happens, so installing them before the DOM is parsed
  // is safe. An earlier arrangement gated them inside
  // `if (document.readyState === 'loading')`, which silently disabled
  // them when runtime.js loaded after DOMContentLoaded (late injection,
  // fast parse, dynamic re-init).

  // Disclosure behaviour — the aria-expanded mirror, Escape-to-close,
  // the focus containment, the close-on-navigate — lives in
  // framework/headless's headless-disclosure module (a registered
  // behaviour, loaded on the details[data-hui-disclosure] marker);
  // the kernel holds none of it.

  // Task A: auto-inject aria-live onto signal nodes so screen readers
  // announce dynamic updates. Restricted to TEXT-mode nodes (the default
  // when data-cui-signal-mode is absent or "text"): attr-mode and
  // html-mode bindings must NOT receive role=status because:
  //  - attr-mode: injects into element attributes (e.g. <a href=…>),
  //    not text, role=status on an <a> is invalid ARIA.
  //  - html-mode: swaps innerHTML of island wrappers; treating the
  //    entire region as a live region causes a storm of announcements
  //    on every island update. Those regions use their own role/aria.
  // Runs at boot and after SPA navigation.
  const _injectSignalAria = () => {
    document.querySelectorAll('[data-cui-signal]').forEach((node) => {
      const mode = node.getAttribute('data-cui-signal-mode') || 'text';
      if (mode !== 'text') return;
      if (!node.getAttribute('role')) node.setAttribute('role', 'status');
      if (!node.getAttribute('aria-live')) node.setAttribute('aria-live', 'polite');
      if (!node.getAttribute('aria-atomic')) node.setAttribute('aria-atomic', 'true');
    });
  };
  // Initial-pass hooks: these scan the CURRENT DOM, so they have
  // to wait until the document is at least parsed.
  // _bootstrapComponentCSS scans existing markers; _scanForModules
  // dispatches demand-load modules (headless behaviours among them —
  // the disclosure module does its own aria-expanded sync for
  // server-rendered <details>).
  // _runMountActions fires component actions marked data-action-mount once,
  // right after hydration. Component clientJS handlers (data-action) only run
  // on user events (click/input/change/submit); a server-rendered island that
  // must populate itself on load, an entity list fetching its rows, a detail
  // view fetching one record, a relation <select> fetching its options, opts
  // in by carrying data-action-mount="<actionName>" on a node inside a
  // [data-component]. Re-runs on SPA nav so a swapped-in page repopulates.
  // Each element this pass fires is marked (_cuiM): markup inserted
  // outside a navigation (an overlay, an island swap, widget chrome) is
  // the actionloader module's, which fires only unmarked elements, so the
  // swap a navigation's pass already fired is not fired again when the
  // mutation observer hands the same nodes over a microtask later.
  const _runMountActions = (root) => {
    const G = window.__gofastr;
    if (!G || !G.trigger) return;
    const scope = root || document;
    for (const el of scope.querySelectorAll('[data-action-mount]')) {
      const action = el.getAttribute('data-action-mount');
      el._cuiM = 1;
      if (!action) continue;
      const name = closestAttr(el, 'data-component') ?? closestAttr(el, 'data-widget');
      if (!name) continue;
      G.trigger(name, action, collectParams(el));
    }
  };
  window.addEventListener('gofastr:navigate', () => _runMountActions(document));

  const _initialPass = () => {
    _bootstrapComponentCSS();
    _scanForModules(document);
    // Intercepting routes are rare, so their module is demand-loaded off
    // the manifest: no intercepting route, no bytes, no listeners.
    if (Array.isArray(window.__gofastr_routes) &&
        window.__gofastr_routes.some((r) => r.intercept)) loadModule('intercept');
    // Same pattern for route preload: any route declaring a preload mode
    // loads the prefetch machinery; a manifest without one costs nothing.
    if (Array.isArray(window.__gofastr_routes) &&
        window.__gofastr_routes.some((r) => r.preload)) loadModule('preload');
    // Deferred outlets: any route in the manifest carrying a
    // `deferred` list loads the parallel-parts machinery. The address
    // walk that decides whether the ENVELOPE module boot-loads too
    // lives in src/parts.js (module-side: a plain page never runs it).
    if (Array.isArray(window.__gofastr_routes) &&
        window.__gofastr_routes.some((r) => r.deferred && r.deferred.length)) loadModule('parts');
    // Compiled server actions: the manifest names each screen's action
    // hash; the loader module fetches per-screen scripts on navigation.
    // Pages without actions load nothing.
    if (window.__gofastr_actions &&
        Object.keys(window.__gofastr_actions).length) loadModule('actionloader');
    // Active-link highlighting is cosmetic post-nav work carved out of
    // core (level-1 budget): the idle-loaded module applies aria-current
    // on load and keeps it fresh across SPA navs. Skipped in the embed
    // composition, nav is absent there, and typeof on an undeclared
    // identifier is the one safe probe for a fragment symbol.
    if (typeof loadPage === 'function') {
      (window.requestIdleCallback || ((fn) => setTimeout(fn, 150)))(
        () => { loadModule('activelink').catch(() => {}); });
    }
    _injectSignalAria();
    _runMountActions(document);
  };
  // A deferred runtime.js runs at readyState 'interactive' but before
  // the deferred action scripts after it, so it waits for
  // DOMContentLoaded too, or mounts fire at handlers not registered yet.
  // Nothing injects runtime.js after parsing, the one spot where a
  // deferred tag would miss that event.
  if (document.readyState === 'loading' || document.currentScript?.defer) {
    document.addEventListener('DOMContentLoaded', _initialPass);
  } else {
    _initialPass();
  }

})();
