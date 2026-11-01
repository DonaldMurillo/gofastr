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
