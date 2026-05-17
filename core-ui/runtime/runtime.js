// GoFastr Core-UI Runtime v0.4 — ES2020+
(() => {
  'use strict';

  // -----------------------------------------------------------------------
  // Component handler registry
  // -----------------------------------------------------------------------
  const handlers = {};

  // -----------------------------------------------------------------------
  // State store — compiled Go components share state through this
  // -----------------------------------------------------------------------
  const state = {};

  // parseCombo turns a string like "Mod+Shift+k" into a normalized
  // shortcut spec the keydown handlers compare against. Mod is the
  // OS-appropriate primary modifier (Cmd on Mac, Ctrl elsewhere).
  function parseCombo(s) {
    const out = { key: '', mod: false, shift: false, alt: false };
    s.split('+').forEach((p) => {
      const t = p.trim().toLowerCase();
      if (t === 'mod' || t === 'cmd' || t === 'ctrl') out.mod = true;
      else if (t === 'shift') out.shift = true;
      else if (t === 'alt' || t === 'option') out.alt = true;
      else out.key = t;
    });
    return out;
  }

  // -----------------------------------------------------------------------
  // Router: known routes from screen registration
  // -----------------------------------------------------------------------
  const routes = new Map(); // path → { title, preload }
  let currentPath = location.pathname + location.search;

  // -----------------------------------------------------------------------
  // Module-level RPC dispatcher — installed ONCE at script load.
  //
  // Why module-level: islands fire RPCs from anywhere on the page, not
  // just inside a mounted widget. The model (see core-ui/ARCHITECTURE.md)
  // requires `data-fui-rpc` to work without any widget setup. Each
  // mounted widget still has its own RPC handler so widget-scoped
  // close/reset behavior keeps working — but the global path is the
  // baseline, always available.
  //
  // Response semantics:
  //   - body  → broadcast to data-fui-rpc-signal (text or JSON)
  //   - X-Gofastr-Push-State header → apply via history.pushState,
  //     update currentPath. NO re-fetch — URL update only.
  //
  // X-FUI-Widget header is set when the button lives inside a
  // data-fui-widget context, omitted otherwise. The server doesn't
  // require it.
  // -----------------------------------------------------------------------
  // Per-signal abort controllers so rapid clicks targeting the same
  // signal-bound region don't race. Each new dispatch aborts the
  // previous in-flight one — last-click wins by the time the runtime
  // sees the response, not by network arrival order. This is what
  // pagination spam-click protection needs: 10 clicks ending on page
  // 1 must settle on page 1, not whichever response landed last.
  const _rpcInFlight = new Map(); // signal name → AbortController

  async function dispatchRPC(node) {
    const path = node.getAttribute('data-fui-rpc');
    const method = (node.getAttribute('data-fui-rpc-method') || 'POST').toUpperCase();
    const responseSignal = node.getAttribute('data-fui-rpc-signal');
    const closeOnSuccess = node.hasAttribute('data-fui-rpc-close');
    const resetOnSuccess = node.hasAttribute('data-fui-rpc-reset') && node.tagName === 'FORM';

    // Abort any in-flight dispatch for this signal. The previous
    // fetch will reject with AbortError; we ignore that branch below.
    if (responseSignal) {
      const prev = _rpcInFlight.get(responseSignal);
      if (prev) prev.abort();
    }
    const ctl = new AbortController();
    if (responseSignal) _rpcInFlight.set(responseSignal, ctl);
    let body = node.getAttribute('data-fui-rpc-body');
    let resolvedPath = path;
    let bodyIsFormData = false;
    if (!body && node.tagName === 'FORM') {
      const fd = new FormData(node);
      // For GET, encode form data as the query string of the RPC
      // path. POST/PUT/PATCH send as JSON body so the server reads
      // r.Body. This matches normal HTML form semantics.
      if (method === 'GET') {
        const params = new URLSearchParams();
        fd.forEach((v, k) => { if (v != null) params.set(k, String(v)); });
        const qs = params.toString();
        if (qs) {
          resolvedPath = path + (path.includes('?') ? '&' : '?') + qs;
        }
      } else if (node.enctype === 'multipart/form-data' || node.querySelector('input[type="file"]')) {
        // Forms with files OR an explicit multipart enctype need to
        // ship as multipart/form-data so File objects survive. fetch
        // sets the right Content-Type (with boundary) automatically
        // when body is a FormData instance.
        body = fd;
        bodyIsFormData = true;
      } else {
        const obj = {}; fd.forEach((v, k) => { obj[k] = v; });
        body = JSON.stringify(obj);
      }
    }
    const widgetEl = node.closest('[data-fui-widget]');
    const headers = {};
    if (widgetEl) headers['X-FUI-Widget'] = widgetEl.getAttribute('data-fui-widget') || '';
    if (body && !bodyIsFormData) headers['Content-Type'] = 'application/json';
    // Optional pre-flight confirm — useful for destructive RPCs
    // (delete, revoke, drop). The user gets a native browser confirm
    // dialog with the supplied message; cancel aborts the dispatch.
    const confirmMsg = node.getAttribute('data-fui-confirm');
    if (confirmMsg && typeof window.confirm === 'function') {
      if (!window.confirm(confirmMsg)) return;
    }

    // Disable the trigger during the in-flight request — but only
    // when we DON'T have abort-dedup via a signal. Signal-based RPCs
    // (pagination buttons, etc.) need the user to be able to click
    // again instantly; the AbortController guarantees only the last
    // click's response reaches setSignal.
    const wantDisable = !responseSignal && (node.tagName === 'BUTTON' || node.tagName === 'INPUT');
    if (wantDisable) node.disabled = true;
    try {
      const r = await fetch(resolvedPath, { method, headers, body: body || undefined, signal: ctl.signal });
      if (!r.ok) {
        const txt = await r.text();
        if (responseSignal) window.__gofastr.setSignal(responseSignal, { ok: false, status: r.status, text: txt });
        return;
      }
      // URL state update (no re-fetch). Either the server hands us the
      // canonical URL via X-Gofastr-Push-State, or the triggering
      // element declares it via data-fui-push-state. Header takes
      // precedence so the server can override.
      const pushState = r.headers.get('X-Gofastr-Push-State')
        || node.getAttribute('data-fui-push-state');
      if (pushState) {
        try {
          history.pushState(null, '', pushState);
          currentPath = location.pathname + location.search;
        } catch (_) {}
      }
      // X-Gofastr-Toast header fires toasts on success — same
      // contract as the widget-scoped dispatchRPC. The server emits a
      // JSON array of ToastTrigger objects (single object tolerated);
      // each fires through __gofastr.toast().
      const toastHeader = r.headers.get('X-Gofastr-Toast');
      if (toastHeader) {
        // Await the toasts module — the header may arrive before the
        // user has hovered any toast trigger, so we may need to fetch
        // it on the fly. Errors swallowed so a missing module doesn't
        // break the response handling.
        window.__gofastr.loadModule('toasts').then(() => {
          try {
            const parsed = JSON.parse(toastHeader);
            const arr = Array.isArray(parsed) ? parsed : [parsed];
            for (const cfg of arr) window.__gofastr.toast(cfg);
          } catch (_) {}
        }).catch(() => {});
      }
      const ct = r.headers.get('content-type') || '';
      const data = ct.indexOf('application/json') >= 0 ? await r.json() : await r.text();
      if (responseSignal) window.__gofastr.setSignal(responseSignal, data);
      // Widget-scoped helpers (close/reset) — only valid when inside a widget.
      if (closeOnSuccess && widgetEl && widgetEl.__fuiDismiss) widgetEl.__fuiDismiss();
      if (resetOnSuccess) node.reset();
      // Post-success primitives — declared on the trigger so app code
      // never has to ship JS for "show 'Done ✓' on the button" or
      // "scroll to the new content". Idempotent (afterText only sets
      // once via data-fui-rpc-after-done="1").
      if (!node.dataset.fuiRpcAfterDone) {
        const afterText = node.getAttribute('data-fui-rpc-after-text');
        if (afterText !== null) node.textContent = afterText;
        if (node.hasAttribute('data-fui-rpc-after-disable')) {
          node.setAttribute('aria-disabled', 'true');
          if ('disabled' in node) node.disabled = true;
        }
        node.dataset.fuiRpcAfterDone = '1';
      }
      const scrollSel = node.getAttribute('data-fui-rpc-scroll-to');
      if (scrollSel) {
        const target = document.querySelector(scrollSel);
        if (target) Promise.resolve().then(() => {
          try { target.scrollIntoView({behavior: 'smooth', block: 'nearest'}); }
          catch (_) {}
        });
      }
    } catch (err) {
      // Swallow AbortError — it just means a newer dispatch superseded
      // us before the response arrived. Any other error propagates.
      if (err && err.name !== 'AbortError') throw err;
    } finally {
      // Clear the in-flight slot only if WE are still the latest
      // dispatch — a later click may have replaced us, in which case
      // _rpcInFlight already holds its controller.
      if (responseSignal && _rpcInFlight.get(responseSignal) === ctl) {
        _rpcInFlight.delete(responseSignal);
      }
      // Re-enable unless data-fui-rpc-after-disable wanted a sticky
      // disabled state (e.g. "Revealed ✓" demo button).
      const sticky = node.hasAttribute('data-fui-rpc-after-disable') && node.dataset.fuiRpcAfterDone === '1';
      if (!sticky && wantDisable) node.disabled = false;
    }
  }

  // Per-form debounce timers for data-fui-rpc-trigger="input".
  const inputDebounceTimers = new WeakMap();

  // Global click+submit dispatcher — installed once at module load.
  // Catches data-fui-rpc on any element NOT inside a widget. Widget
  // scopes have their own handler that intercepts first.
  //
  // Also handles legacy data-kiln-tool buttons + plain forms with a
  // relative `action` attribute, kept here because kiln-built pages
  // rely on the same generic dispatcher.
  if (!document.__fuiGlobalDispatch) {
    document.__fuiGlobalDispatch = true;
    document.addEventListener('click', async (e) => {
      // Skip if inside a widget — that widget's handler owns the click.
      if (e.target.closest('[data-fui-widget]')) return;
      const btn = e.target.closest('[data-fui-rpc]');
      if (btn && btn.tagName !== 'FORM') {
        e.preventDefault();
        await dispatchRPC(btn);
        return;
      }
      // Legacy: data-kiln-tool buttons fire a /kiln/tool/<name> POST
      // with the data-kiln-args body. Kept for kiln-built pages.
      const legacy = e.target.closest('[data-kiln-tool]');
      if (legacy) {
        e.preventDefault();
        const tool = legacy.getAttribute('data-kiln-tool');
        const args = legacy.getAttribute('data-kiln-args') || '';
        try {
          await fetch('/kiln/tool/' + tool, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: args,
          });
        } catch (_) {}
      }
    });
    document.addEventListener('submit', async (e) => {
      const form = e.target.closest('form');
      if (!form || form.closest('[data-fui-widget]')) return;
      if (form.hasAttribute('data-fui-rpc')) {
        e.preventDefault();
        await dispatchRPC(form);
        return;
      }
      // Legacy: data-kiln-tool form submits.
      if (form.hasAttribute('data-kiln-tool')) {
        e.preventDefault();
        const tool = form.getAttribute('data-kiln-tool');
        const fd = new FormData(form);
        const obj = {}; fd.forEach((v, k) => { obj[k] = v; });
        try {
          await fetch('/kiln/tool/' + tool, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(obj),
          });
        } catch (_) {}
        return;
      }
      // Plain forms with a relative `action` post via fetch (so
      // server-rendered forms work without a full page reload).
      const action = form.getAttribute('action');
      if (action && !action.match(/^https?:\/\//)) {
        e.preventDefault();
        const fd = new FormData(form);
        const obj = {}; fd.forEach((v, k) => { obj[k] = v; });
        try {
          await fetch(action, {
            method: form.getAttribute('method') || 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(obj),
          });
        } catch (_) {}
      }
    });

    // Debounced input-driven RPC: a form with
    // data-fui-rpc-trigger="input" fires its RPC each time an input
    // inside it changes, after a debounce window. Useful for
    // type-ahead search where the server is the source of truth for
    // filtered results (see core-ui/ARCHITECTURE.md — search is an
    // island state change, not a route).
    document.addEventListener('input', (e) => {
      const form = e.target.closest('form[data-fui-rpc][data-fui-rpc-trigger="input"]');
      if (!form) return;
      // Skip if inside a widget — widget owns its own input handling.
      if (form.closest('[data-fui-widget]')) return;
      const ms = parseInt(form.getAttribute('data-fui-rpc-debounce-ms') || '250', 10) || 250;
      const prev = inputDebounceTimers.get(form);
      if (prev) clearTimeout(prev);
      inputDebounceTimers.set(form, setTimeout(() => {
        inputDebounceTimers.delete(form);
        dispatchRPC(form);
      }, ms));
    });
  }

  const registerRoutes = (routeList) => {
    if (!Array.isArray(routeList)) return;
    for (const r of routeList) {
      routes.set(r.path ?? r.Path, {
        title: r.title ?? r.Title ?? '',
        preload: r.preload ?? r.Preload ?? false,
      });
    }
  };

  // Hydrate routes + catalog from inline <script type="application/json">
  // blocks the SSR emits. The browser treats them as inert data (not
  // executable), so they pass strict CSP. Reading happens before
  // first paint of any non-trivial component because runtime.js is
  // injected at the end of <body>, after the JSON blocks in <head>.
  const _readInlineJSON = (id) => {
    const el = document.getElementById(id);
    if (!el) return null;
    try { return JSON.parse(el.textContent || 'null'); }
    catch (_) { return null; }
  };
  if (!window.__gofastr_routes) {
    const r = _readInlineJSON('gofastr-routes');
    if (r) window.__gofastr_routes = r;
  }
  if (!window.__gofastr_catalog) {
    const c = _readInlineJSON('gofastr-catalog');
    if (c) window.__gofastr_catalog = c;
  }

  // Bootstrap routes from injected data
  if (Array.isArray(window.__gofastr_routes)) {
    registerRoutes(window.__gofastr_routes);
  }

  // Auto-discover registered widgets. The framework runtime is loaded
  // once per page (via /__gofastr/runtime.js); each Mount(r, def) on
  // the server registers in a process-global map; this fetch picks the
  // list up and mounts every widget. 404 means no widgets registered
  // — silently skip (the runtime works for plain pages too).
  // Per-page scoped widget discovery — apps that constrain widgets
  // to specific routes via .Pages / .PagesPrefix / .PagesMatch get
  // a filtered catalog. Widgets with no Routes declared appear on
  // every page (the backwards-compatible default).
  fetch('/__gofastr/widgets?page=' + encodeURIComponent(location.pathname),
        { headers: { 'X-Gofastr-Widget-Discovery': '1' } })
    .then((r) => (r.ok ? r.json() : null))
    .then((list) => {
      if (!Array.isArray(list)) return;
      const tryMount = () => {
        if (!window.__gofastr || !window.__gofastr.mountWidget) {
          setTimeout(tryMount, 0);
          return;
        }
        // Stash every widget's payload so openWidget can retrieve a
        // hidden one on demand.
        window.__gofastr._widgetCatalog = window.__gofastr._widgetCatalog || {};
        // Build a deep-link index: key -> [{value, name, params}, ...]
        // so URL parsing on boot / popstate is O(1) per registered key.
        window.__gofastr._widgetDeepLinks = window.__gofastr._widgetDeepLinks || {};
        for (const item of list) {
          window.__gofastr._widgetCatalog[item.cfg.name] = item;
          const cfg = item.cfg;
          if (cfg.deepLinkKey && cfg.deepLinkValue) {
            const idx = window.__gofastr._widgetDeepLinks;
            (idx[cfg.deepLinkKey] = idx[cfg.deepLinkKey] || []).push({
              value: cfg.deepLinkValue,
              name: cfg.name,
              params: cfg.deepLinkParams || [],
            });
          }
          if (item.hidden) continue; // open later via openWidget(name)
          // Non-hidden widgets auto-mount at boot. Chrome HTML is
          // fetched lazily from cfg.chromePath so the registry stays
          // small; if the page already SSR-inlined this widget (root
          // element exists in DOM), mountWidget short-circuits to a
          // hydrate-only path. Either way, the result is a wired
          // widget root.
          window.__gofastr._mountByName(item.cfg.name);
        }
        // Open any widget whose deep link matches the current URL. Pure
        // post-hydration — there's a single-frame window where the page
        // paints without the modal. SSR pre-rendering is a future
        // optimization; correctness (refresh / share / back-button) is
        // already covered by this open-on-boot pass.
        window.__gofastr._syncDeepLinks();

        // Global click delegation for data-fui-open buttons. Bound
        // ONCE per document (idempotent flag). Buttons can carry
        // data-fui-deeplink="user_id=42&tab=profile" to seed the
        // widget's signals from per-click values (e.g. row clicks).
        if (!document.__fuiOpenDispatch) {
          document.__fuiOpenDispatch = true;
          document.addEventListener('click', (e) => {
            // Toast trigger: data-fui-toast='<json>' fires a client
            // toast. Cheaper than data-fui-rpc when no server work is
            // needed (form-validation hint, copy-to-clipboard ack, …).
            const toastBtn = e.target.closest('[data-fui-toast]');
            if (toastBtn) {
              e.preventDefault();
              window.__gofastr.loadModule('toasts').then(() => {
                try {
                  const cfg = JSON.parse(toastBtn.getAttribute('data-fui-toast'));
                  window.__gofastr.toast(cfg);
                } catch (_) {}
              }).catch(() => {});
              return;
            }
            const btn = e.target.closest('[data-fui-open]');
            if (!btn) return;
            const name = btn.getAttribute('data-fui-open');
            if (!name) return;
            e.preventDefault();
            const raw = btn.getAttribute('data-fui-deeplink') || '';
            const overrides = {};
            if (raw) {
              for (const pair of raw.split('&')) {
                if (!pair) continue;
                const eq = pair.indexOf('=');
                if (eq < 0) continue;
                overrides[decodeURIComponent(pair.slice(0, eq))] =
                  decodeURIComponent(pair.slice(eq + 1));
              }
            }
            const anchorPref = btn.getAttribute('data-fui-popover-anchor');
            Promise.resolve(
              window.__gofastr.openWidget(name, { params: overrides, pushUrl: true })
            ).then(async () => {
              if (anchorPref !== null) {
                // Make sure the popover module is loaded before we
                // call the anchor function — protects users who click
                // before the idle scan / hover-prefetch warmed it.
                await window.__gofastr.loadModule('popover');
                window.__gofastr._anchorPopover(name, btn, anchorPref || 'bottom');
              }
            });
          });
        }

        // Browser back/forward: re-sync widgets against the URL. The
        // SPA navigation path (popstate handler near the bottom of this
        // file) already swaps <main>; this listener focuses on widget
        // open/close state, which is independent of <main>'s content.
        if (!window.__fuiDeepLinkPopstate) {
          window.__fuiDeepLinkPopstate = true;
          window.addEventListener('popstate', () => {
            // Defer so this fires AFTER the main popstate handler has
            // (potentially) swapped <main> — otherwise we'd open a
            // widget against a stale DOM.
            setTimeout(() => window.__gofastr._syncDeepLinks(), 0);
          });
        }
      };
      tryMount();
    })
    .catch(() => {});

  // -----------------------------------------------------------------------
  // Screen cache — stores rendered screens for instant back-navigation.
  // -----------------------------------------------------------------------
  const screenCache = new Map(); // path → { html, title, timestamp }
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
    screenCache.set(path, { html, title, timestamp: Date.now() });
  };

  // Cache the initial page so back-navigation to it works instantly.
  // Route through cacheScreen() so the LRU cap is enforced uniformly.
  const initialMain = document.querySelector('[role="main"]') ?? document.querySelector('main');
  if (initialMain) {
    cacheScreen(location.pathname, initialMain.innerHTML, document.title);
  }

  // -----------------------------------------------------------------------
  // Public API (what compiled JS calls)
  // -----------------------------------------------------------------------
  window.__gofastr = {
    /** Register event handlers for a component */
    register(id, events) {
      handlers[id] = events;
    },

    /** Trigger an event on a component */
    trigger(id, event, params) {
      handlers[id]?.[event]?.(params);
    },

    handlers,

    // --- Router API ---

    /** Programmatically navigate to a path */
    navigate(path, { replace = false } = {}) {
      if (path === currentPath) return;
      if (replace) {
        history.replaceState(null, '', path);
      } else {
        history.pushState(null, '', path);
      }
      loadPage(path);
    },

    /** Register routes dynamically */
    registerRoutes,

    /** Get current path */
    get currentPath() { return currentPath; },

    // --- State helpers (compiled Go code uses these) ---

    getState(key, defaultVal) {
      return state[key] ?? defaultVal;
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

    /** Legacy toast — kept as a forwarding shim so older callers
        (string-only arg) continue to work. The real implementation
        is the cfg-object version defined below; it owns the stack
        widget + lifecycle. */

    /** Fetch partial HTML from server and inject into selector */
    async fetchPage(url, selector) {
      const r = await fetch(url, { headers: { 'X-Gofastr-Partial': '1' } });
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
        if (key && state[key] !== undefined) {
          el.value = state[key];
        }
      });
    },

    /** Call a server action and handle the response */
    async serverAction(action, params = {}) {
      return this._serverActionFor('', action, params);
    },

    /** Call a server action for a specific component */
    async _serverActionFor(componentId, action, params = {}) {
      const sessionCookie = document.cookie.match(/gofastr-session=([^;]+)/);
      const session = sessionCookie ? sessionCookie[1] : '';
      const resp = await fetch('/__gofastr/action', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ action, params, session, componentId }),
      });
      if (resp.ok) {
        const result = await resp.json();
        if (result.message) {
          window.__gofastr.loadModule('toasts').then(() => {
            window.__gofastr.toast(result.message);
          }).catch(() => {});
        }
        return result;
      }
      return null;
    },

    /** loadCSS is a no-op kept for external callers that still invoke
     * window.__gofastr.loadCSS(path). The per-screen chunk endpoint
     * (/__gofastr/css/<path>) now returns 410 GONE — declare CSS per
     * component via registry.RegisterStyle and the runtime loads
     * /__gofastr/comp/<name>.css from the SSR-emitted <link>. */
    loadCSS(_screenPath) { /* no-op */ },

    // Component CSS — three modes share _pendingLinks + data-fui-style dedup.
    // See core-ui/ARCHITECTURE.md for the model. Catalog seeded by /__gofastr/catalog.js.
    _pendingLinks: new Set(),
    loadComponentCSS(name) {
      if (!name || this._pendingLinks.has(name)) return;
      if (document.querySelector('link[data-fui-style="' + name + '"]')) return;
      const e = (window.__gofastr_catalog || {})[name];
      if (!e) return;
      this._pendingLinks.add(name);
      const link = document.createElement('link');
      link.rel = 'stylesheet';
      link.href = e.stylePath + (e.version ? '?v=' + e.version : '');
      link.setAttribute('data-fui-style', name);
      link.id = 'fui-css-' + name;
      document.head.appendChild(link);
    },
    scanAndLoadCSS(root) {
      if (!root) return;
      const html = root.outerHTML || root.innerHTML;
      if (typeof html === 'string' && html.indexOf('data-fui-comp') < 0) return;
      if (!root.querySelectorAll) return;
      root.querySelectorAll('[data-fui-comp]').forEach((el) => {
        this.loadComponentCSS(el.getAttribute('data-fui-comp'));
      });
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
    // Widgets (core-ui/widget) — overlay UIs that mount on top of any
    // page. mountWidget is the runtime entrypoint used by per-widget
    // bootstrap scripts. The host (Go) builds the WidgetDef → emits a
    // tiny init script that calls __gofastr.mountWidget(cfg, chrome).
    // All DOM/SSE/RPC plumbing lives here, in the framework runtime.
    // -----------------------------------------------------------------

    /** Internal widget-state registry. Idempotent: a widget mounted
        twice with the same name is a no-op. */
    _widgets: {},
    _signals: {},

    /** Names of currently-mounted modal (backdrop'd) widgets, oldest
        at index 0. Drives body-scroll lock + the Tab focus trap so a
        modal opened from inside another modal traps Tab to itself
        rather than to the outer one. */
    _modalStack: [],

    /** Tracks split runtime modules already loaded. The loader checks
        this map before injecting a <script>; modules set their own
        entry to true on load. */
    loadedModules: {},

    /** Load a split runtime module by name (e.g. "fileupload",
        "popover"). Returns a cached Promise that resolves once the
        module's IIFE has executed. Safe to call concurrently — the
        first call wins, all callers await the same fetch. */
    loadModule,

    /** Selector for focusable elements inside a modal — used by the
        initial-focus pass and the Tab focus trap. */
    _focusSel: 'a[href],button:not([disabled]):not([aria-disabled="true"]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])',

    /** Push a value into a named signal and reflect it into all
        [data-fui-signal="<name>"] DOM nodes. Mode is read from the
        node's data-fui-signal-mode attr ("text" default, "html",
        "attr"+data-fui-signal-attr). */
    setSignal(name, value) {
      let s = this._signals[name];
      if (!s) { s = this._signals[name] = { value: undefined, listeners: [] }; }
      s.value = value;
      for (const fn of s.listeners) {
        try { fn(value); } catch (_) {}
      }
      document.querySelectorAll('[data-fui-signal="' + name + '"]').forEach((node) => {
        const mode = node.getAttribute('data-fui-signal-mode') || 'text';
        if (mode === 'html') {
          node.innerHTML = (typeof value === 'string') ? value : (value == null ? '' : JSON.stringify(value));
          window.__gofastr.scanAndLoadCSS(node);
          // Wire any toast items the freshly-swapped HTML brought in.
          // Awaits the toasts module — when an island-driven update
          // injects a toast for the first time, the module loads,
          // then _initToasts runs against the new content.
          if (node.querySelector && node.querySelector('[data-fui-toast-id]')) {
            window.__gofastr.loadModule('toasts').then(() => {
              window.__gofastr._initToasts(node);
            }).catch(() => {});
          }
        } else if (mode === 'attr') {
          const attr = node.getAttribute('data-fui-signal-attr') || 'value';
          node.setAttribute(attr, String(value ?? ''));
        } else {
          if (value == null) node.textContent = '';
          else if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') node.textContent = String(value);
          else node.textContent = JSON.stringify(value);
        }
        // After-update hook: brief flash to signal the value changed.
        // Useful for headers/badges where the user might miss an
        // update otherwise. Duration overridable via
        // data-fui-flash-duration-ms; default 600ms.
        if (node.hasAttribute('data-fui-flash-on-update')) {
          const dur = parseInt(node.getAttribute('data-fui-flash-duration-ms') || '600', 10);
          node.classList.remove('fui-flash');
          // Force reflow so the next add re-runs the animation.
          // eslint-disable-next-line no-unused-expressions
          node.offsetWidth;
          node.classList.add('fui-flash');
          setTimeout(() => node.classList.remove('fui-flash'), dur);
        }
        // After-update hook: scroll a container to bottom so streaming
        // chat logs / live tails surface new content without manual
        // scrolling. Opt-in via data-fui-scroll-bottom-on-update on
        // the signal node itself or the resolved selector target.
        if (node.hasAttribute('data-fui-scroll-bottom-on-update')) {
          const sel = node.getAttribute('data-fui-scroll-bottom-on-update');
          const target = sel ? node.querySelector(sel) || document.querySelector(sel) || node : node;
          // Defer to end of microtask so the new innerHTML lays out first.
          Promise.resolve().then(() => { try { target.scrollTop = target.scrollHeight; } catch (_) {} });
        }
      });
    },

    /** Read the current value of a named signal. */
    signal(name) {
      return this._signals[name]?.value;
    },

    // Toast stack runtime (__gofastr.toast, _initToasts, _dismissToast,
    // _toastTimers, _toastSeq) lives in the split-runtime toasts module
    // at core-ui/runtime/src/toasts.js. The module self-registers
    // those on window.__gofastr when it loads. Core code that calls
    // them (the click delegator for data-fui-toast, the X-Gofastr-Toast
    // header dispatch in dispatchRPC) awaits loadModule('toasts')
    // first so the very first toast on a cold cache still fires.

    /** Mount a hidden widget by name. Looks up the entry stashed by
        the auto-discovery fetch and delegates to mountWidget. No-op
        if the widget is already mounted (mountWidget is idempotent).

        opts:
          params: { [key]: value } — extra values mirrored into named
                  signals after mount (deep-link seeding).
          pushUrl: when true (and the widget is deep-linked), update
                  the browser URL via pushState so refresh / share /
                  back-button stay consistent.
    */
    async openWidget(name, opts) {
      const entry = this._widgetCatalog && this._widgetCatalog[name];
      if (!entry) return;
      const o = opts || {};
      const params = o.params || {};
      await this._mountByName(name);
      // Seed signals from URL params or click overrides — bound to the
      // declared deepLinkParams so untrusted query values can't write
      // arbitrary signal keys.
      const declared = entry.cfg.deepLinkParams || [];
      if (declared.length) {
        const url = new URL(window.location.href);
        for (const k of declared) {
          const v = (k in params) ? params[k] : url.searchParams.get(k);
          if (v != null) this.setSignal(k, v);
        }
      }
      if (o.pushUrl && entry.cfg.deepLinkKey && entry.cfg.deepLinkValue) {
        this._deepLinkPushUrl(entry.cfg, params);
      }
    },

    /** Lazy chrome cache: name → Promise<HTMLString>. Each widget's
        chrome is fetched once per page session and reused. */
    _chromeCache: {},

    /** Mount a widget by name. Resolves the chrome HTML — preferring
        an SSR-inlined root if one already exists in the DOM, otherwise
        lazily fetching cfg.chromePath. Idempotent across both. */
    async _mountByName(name) {
      const entry = this._widgetCatalog && this._widgetCatalog[name];
      if (!entry) return;
      if (this._widgets[name]) return; // already mounted (hydrate or click)
      const cfg = entry.cfg;
      // SSR-inline path: server already put the chrome in the page.
      const existing = document.querySelector('[data-fui-widget="' + CSS.escape(name) + '"]');
      if (existing) {
        this.mountWidget(cfg, null, existing);
        return;
      }
      // Lazy fetch path: hit cfg.chromePath, cache only successful
      // responses so a transient failure (server restart, offline,
      // 5xx) doesn't poison the cache for the rest of the session.
      const path = cfg.chromePath || ('/core-ui/widget/' + name + '/chrome');
      if (!this._chromeCache[name]) {
        this._chromeCache[name] = (async () => {
          try {
            const r = await fetch(path);
            if (!r.ok) throw new Error('chrome fetch ' + r.status);
            return await r.text();
          } catch (err) {
            // Drop the cache entry so a retry on the next openWidget
            // call attempts a fresh fetch.
            delete this._chromeCache[name];
            throw err;
          }
        })();
      }
      let html = '';
      try { html = await this._chromeCache[name]; } catch (_) {}
      if (html) this.mountWidget(cfg, html);
    },

    /** Dismiss a mounted widget by name. */
    closeWidget(name) {
      const w = this._widgets[name];
      if (w && typeof w.dismiss === 'function') w.dismiss();
    },

    // _anchorPopover lives in the split-runtime popover module
    // (core-ui/runtime/src/popover.js). It self-registers onto
    // window.__gofastr when the module loads. Core code that needs
    // it (the data-fui-open click delegator) awaits
    // loadModule('popover') first.

    /** Update window.location with a widget's deep-link params via
        pushState — no fetch, no reload. Strips the previous deep-link
        key on the same URL key so reopening from URL A to URL B works
        cleanly. */
    _deepLinkPushUrl(cfg, params) {
      const url = new URL(window.location.href);
      url.searchParams.set(cfg.deepLinkKey, cfg.deepLinkValue);
      for (const k of cfg.deepLinkParams || []) {
        if (k in params) url.searchParams.set(k, params[k]);
      }
      if (url.href !== window.location.href) {
        history.pushState(null, '', url.pathname + url.search + url.hash);
      }
    },

    /** Strip a widget's deep-link params from the URL via pushState.
        Called from dismiss() when the closed widget was opened via
        deep-link OR was URL-bound at boot. */
    _deepLinkStripUrl(cfg) {
      const url = new URL(window.location.href);
      let touched = false;
      if (url.searchParams.get(cfg.deepLinkKey) === cfg.deepLinkValue) {
        url.searchParams.delete(cfg.deepLinkKey);
        touched = true;
      }
      for (const k of cfg.deepLinkParams || []) {
        if (url.searchParams.has(k)) { url.searchParams.delete(k); touched = true; }
      }
      if (touched) {
        const s = url.searchParams.toString();
        history.pushState(null, '', url.pathname + (s ? '?' + s : '') + url.hash);
      }
    },

    /** Compare current URL to the deep-link index; mount any widget
        whose deep-link is now in the URL, dismiss any whose deep-link
        is no longer present. Called on boot and on popstate. */
    _syncDeepLinks() {
      const idx = this._widgetDeepLinks || {};
      const url = new URL(window.location.href);
      // Open any matches not yet mounted.
      for (const key in idx) {
        const got = url.searchParams.get(key);
        for (const entry of idx[key]) {
          const mounted = !!this._widgets[entry.name];
          if (got === entry.value && !mounted) {
            // No pushUrl: the URL already carries the deep link; we'd
            // just be replacing it with itself.
            this.openWidget(entry.name, { pushUrl: false });
          } else if (got !== entry.value && mounted) {
            // Widget is mounted but the URL no longer wants it open.
            this.closeWidget(entry.name);
          }
        }
      }
    },

    /** Mount a widget. cfg is the registry metadata. The chrome can
        arrive three ways:
          - SSR-inlined: existingEl is the root already in the DOM
            (preferred — no construction, hydrate only).
          - Lazy fetch: chromeHTML is the rendered HTML to insert.
          - Both null: skip (mountWidget is a no-op).
        Idempotent — a widget already mounted (same cfg.name) is a
        no-op. */
    mountWidget(cfg, chromeHTML, existingEl) {
      const NS = this;
      if (NS._widgets[cfg.name]) return; // already mounted
      NS._widgets[cfg.name] = { cfg };

      // Stylesheet
      if (!document.querySelector('link[data-fui-style="' + cfg.name + '"]')) {
        const link = document.createElement('link');
        link.rel = 'stylesheet';
        link.href = cfg.stylePath;
        link.setAttribute('data-fui-style', cfg.name);
        document.head.appendChild(link);
      }

      // Backdrop + chrome. For SSR-inlined widgets the backdrop may
      // already be a sibling of the root; otherwise we create one.
      let backdrop = null;
      if (cfg.backdrop) {
        backdrop = document.querySelector('[data-fui-backdrop="' + CSS.escape(cfg.name) + '"]');
        if (!backdrop) {
          backdrop = document.createElement('div');
          backdrop.className = 'fui-backdrop overlay-backdrop';
          backdrop.setAttribute('data-fui-backdrop', cfg.name);
          document.body.appendChild(backdrop);
        }
      }
      let widgetEl;
      if (existingEl) {
        widgetEl = existingEl;
        // Strip the `hidden` attribute that the SSR layer uses to
        // keep click-to-open widgets out of the layout until needed.
        widgetEl.removeAttribute('hidden');
      } else if (chromeHTML) {
        const tmp = document.createElement('div');
        tmp.innerHTML = chromeHTML;
        widgetEl = tmp.firstElementChild;
        document.body.appendChild(widgetEl);
      } else {
        // Neither path supplied — nothing to mount.
        delete NS._widgets[cfg.name];
        return;
      }
      NS._widgets[cfg.name].root = widgetEl;
      NS._widgets[cfg.name].backdrop = backdrop;
      NS._widgets[cfg.name].hydrated = !!existingEl;
      NS.scanAndLoadCSS(widgetEl);

      // Backdrop'd widgets are modal — lock body scroll, focus first
      // focusable, push onto the modal stack so nested modals unwind
      // correctly. Non-backdrop widgets (panels, toasts, banners) do
      // none of this.
      const isModal = !!cfg.backdrop;
      const previousFocus = isModal ? document.activeElement : null;
      if (isModal) {
        if (NS._modalStack.length === 0) document.body.style.overflow = 'hidden';
        NS._modalStack.push(cfg.name);
        // Defer focus by a microtask so any post-mount DOM updates
        // (signal seed, slot innerHTML swaps) finish first.
        Promise.resolve().then(() => {
          const focusables = widgetEl.querySelectorAll(NS._focusSel);
          if (focusables.length > 0) focusables[0].focus();
        });
      }

      function dismiss() {
        // SSR-inlined widgets stay in the DOM across open/close —
        // we just hide them with the `hidden` attribute instead of
        // detaching, so reopening doesn't require a fresh fetch and
        // the chrome stays available for the next deep-link hit.
        const wasHydrated = NS._widgets[cfg.name]?.hydrated;
        const outsideHandler = NS._widgets[cfg.name]?.outsideHandler;
        const anchorResize = NS._widgets[cfg.name]?.anchorResize;
        const anchorScroll = NS._widgets[cfg.name]?.anchorScroll;
        const anchorTrigger = NS._widgets[cfg.name]?.anchorTrigger;
        if (outsideHandler) {
          document.removeEventListener('click', outsideHandler);
        }
        if (anchorResize) {
          window.removeEventListener('resize', anchorResize);
        }
        if (anchorScroll) {
          window.removeEventListener('scroll', anchorScroll, { capture: true });
        }
        if (anchorTrigger) {
          anchorTrigger.classList.remove('is-popover-trigger-active');
          anchorTrigger.removeAttribute('data-fui-popover-trigger');
        }
        // Clear anchor-driven inline positioning so a re-open from a
        // non-anchor trigger picks up the chrome's default placement.
        if (widgetEl && widgetEl.style) {
          widgetEl.style.left = '';
          widgetEl.style.top = '';
          widgetEl.style.right = '';
          widgetEl.style.bottom = '';
          widgetEl.style.position = '';
          widgetEl.style.removeProperty('--ui-popover-arrow-x');
          widgetEl.style.removeProperty('--ui-popover-arrow-y');
          widgetEl.removeAttribute('data-fui-popover-side');
        }
        if (wasHydrated && widgetEl) {
          widgetEl.setAttribute('hidden', '');
        } else if (widgetEl?.parentNode) {
          widgetEl.parentNode.removeChild(widgetEl);
        }
        if (backdrop?.parentNode) backdrop.parentNode.removeChild(backdrop);
        delete NS._widgets[cfg.name];
        if (NS._popoverStack && NS._popoverStack.length) {
          const pIdx = NS._popoverStack.indexOf(cfg.name);
          if (pIdx >= 0) NS._popoverStack.splice(pIdx, 1);
        }
        if (isModal) {
          const idx = NS._modalStack.indexOf(cfg.name);
          if (idx >= 0) NS._modalStack.splice(idx, 1);
          if (NS._modalStack.length === 0) document.body.style.overflow = '';
          if (previousFocus && typeof previousFocus.focus === 'function') {
            try { previousFocus.focus(); } catch (_) {}
          }
        }
        // If this widget owns its deep-link in the URL, strip the
        // params on close so refresh/share land back on the parent
        // page. _syncDeepLinks (popstate) calls closeWidget which
        // reaches us; in that case the URL was already updated by the
        // browser — _deepLinkStripUrl is a no-op.
        if (cfg.deepLinkKey && cfg.deepLinkValue) NS._deepLinkStripUrl(cfg);
      }
      NS._widgets[cfg.name].dismiss = dismiss;

      // Initial state hydration — only when the widget actually has
      // signals (the registry omits statePath otherwise so a modal
      // with zero signals doesn't pay for an empty round-trip on
      // every open). We apply each value only if a signal hasn't
      // already been written by the deep-link seed path; the seed
      // path runs synchronously after mountWidget while this fetch
      // resolves later, and we don't want it to clobber URL-derived
      // values with the SignalFunc default.
      if (cfg.statePath) {
        fetch(cfg.statePath, { headers: { 'X-FUI-Widget': cfg.name } })
          .then((r) => (r.ok ? r.json() : {}))
          .then((state) => {
            for (const k in state) {
              const existing = NS._signals[k];
              if (existing && existing.value !== undefined) continue;
              NS.setSignal(k, state[k]);
            }
          })
          .catch(() => {});
      }

      // SSE bindings
      const seenStreams = {};
      for (const b of cfg.sse || []) {
        if (!seenStreams[b.path]) {
          try {
            const es = new EventSource(b.path);
            seenStreams[b.path] = es;
            // Track SSE connection state on document.body so any
            // widget can style itself off the html-level class. The
            // browser auto-reconnects on transient drops; we toggle
            // the class on each open/error transition.
            es.addEventListener('open', () => {
              window.__fuiSSEReady = true;
              document.body.classList.remove('fui-sse-down');
              document.body.classList.add('fui-sse-up');
            });
            es.addEventListener('error', () => {
              document.body.classList.remove('fui-sse-up');
              document.body.classList.add('fui-sse-down');
            });
          } catch (_) {
            seenStreams[b.path] = null;
          }
        }
        const es = seenStreams[b.path];
        if (!es) continue;
        if (b.reload) {
          es.addEventListener(b.event, (ev) => {
            if (b.match) {
              let payload = {};
              try { payload = JSON.parse(ev.data) || {}; } catch (_) {}
              for (const k in b.match) {
                if (String(payload[k]) !== String(b.match[k])) return;
              }
            }
            setTimeout(() => location.reload(), 200);
          });
          continue;
        }
        if (b.refetch) {
          es.addEventListener(b.event, () => {
            fetch(cfg.statePath, { headers: { 'X-FUI-Widget': cfg.name } })
              .then((r) => (r.ok ? r.json() : null))
              .then((state) => {
                if (state && b.signal in state) NS.setSignal(b.signal, state[b.signal]);
              })
              .catch(() => {});
          });
        } else {
          es.addEventListener(b.event, (ev) => {
            let payload;
            try { payload = JSON.parse(ev.data); } catch (_) { payload = ev.data; }
            NS.setSignal(b.signal, payload);
          });
        }
      }

      async function dispatchRPC(node) {
        const path = node.getAttribute('data-fui-rpc');
        const method = (node.getAttribute('data-fui-rpc-method') || 'POST').toUpperCase();
        const responseSignal = node.getAttribute('data-fui-rpc-signal');
        const closeOnSuccess = node.hasAttribute('data-fui-rpc-close');
        const resetOnSuccess = node.hasAttribute('data-fui-rpc-reset') && node.tagName === 'FORM';
        let body = node.getAttribute('data-fui-rpc-body');
        let bodyIsFormData = false;
        if (!body && node.tagName === 'FORM') {
          const fd = new FormData(node);
          if (node.enctype === 'multipart/form-data' || node.querySelector('input[type="file"]')) {
            body = fd;
            bodyIsFormData = true;
          } else {
            const obj = {}; fd.forEach((v, k) => { obj[k] = v; });
            body = JSON.stringify(obj);
          }
        }
        const headers = { 'X-FUI-Widget': cfg.name };
        if (body && !bodyIsFormData) headers['Content-Type'] = 'application/json';
        if (node.tagName === 'BUTTON' || node.tagName === 'INPUT') node.disabled = true;
        try {
          const r = await fetch(path, { method, headers, body: body || undefined });
          if (!r.ok) {
            const txt = await r.text();
            if (responseSignal) NS.setSignal(responseSignal, { ok: false, status: r.status, text: txt });
            return;
          }
          // X-Gofastr-Toast header fires toasts on success. The server
          // emits a JSON-encoded array of ToastTrigger objects (single
          // object also tolerated for hand-set headers); the runtime
          // dispatches each into the client toast stack. Awaits the
          // toasts module so a header can fire even on the first
          // RPC of a cold cache.
          const toastHeader = r.headers.get('X-Gofastr-Toast');
          if (toastHeader) {
            NS.loadModule('toasts').then(() => {
              try {
                const parsed = JSON.parse(toastHeader);
                const arr = Array.isArray(parsed) ? parsed : [parsed];
                for (const cfg of arr) NS.toast(cfg);
              } catch (_) {}
            }).catch(() => {});
          }
          const ct = r.headers.get('content-type') || '';
          const data = ct.indexOf('application/json') >= 0 ? await r.json() : await r.text();
          if (responseSignal) NS.setSignal(responseSignal, data);
          if (closeOnSuccess) dismiss();
          if (resetOnSuccess) node.reset();
        } finally {
          if (node.tagName === 'BUTTON' || node.tagName === 'INPUT') node.disabled = false;
        }
      }

      // Click-to-fill: any clickable element with
      // data-fui-fill-input="<selector>" copies a value into the
      // matching input/textarea on click, focuses it, and fires
      // an input event so any validity wiring re-syncs. The value
      // defaults to the button's textContent; override with
      // data-fui-fill-text="<explicit text>" for cases where the
      // button label and the prompt should differ.
      widgetEl.addEventListener('click', (e) => {
        const btn = e.target.closest('[data-fui-fill-input]');
        if (!btn || !widgetEl.contains(btn)) return;
        const sel = btn.getAttribute('data-fui-fill-input');
        if (!sel) return;
        const target = widgetEl.querySelector(sel) || document.querySelector(sel);
        if (!target) return;
        e.preventDefault();
        const explicit = btn.getAttribute('data-fui-fill-text');
        target.value = explicit !== null ? explicit : btn.textContent.trim();
        target.dispatchEvent(new Event('input', { bubbles: true }));
        try { target.focus(); target.select?.(); } catch (_) {}
      });

      // Keyboard-shortcut click: data-fui-shortcut-click="<combo>"
      // simulates a click on the matching keypress when no input is
      // focused (so typing 'y' inside the chat input still types).
      // Useful for binary-decision UIs (Approve/Reject). Document-
      // level delegation so elements added later via signal updates
      // (plan cards re-rendered on chat_html refetch) still bind.
      // Idempotent guard avoids double-binding across widget mounts.
      if (!document.__fuiShortcutClick) {
        document.__fuiShortcutClick = true;
        document.addEventListener('keydown', (e) => {
          const a = document.activeElement;
          if (a && (a.tagName === 'INPUT' || a.tagName === 'TEXTAREA' || a.isContentEditable)) return;
          if (e.isComposing) return;
          const targets = document.querySelectorAll('[data-fui-shortcut-click]');
          for (const el of targets) {
            const combo = el.getAttribute('data-fui-shortcut-click') || '';
            if (!combo) continue;
            const match = parseCombo(combo);
            if (!match.key) continue;
            if (e.key.toLowerCase() !== match.key) continue;
            if (match.mod && !(e.metaKey || e.ctrlKey)) continue;
            if (match.shift && !e.shiftKey) continue;
            if (match.alt && !e.altKey) continue;
            e.preventDefault();
            try { el.click(); } catch (_) {}
            return;
          }
        });
      }

      // Keyboard-shortcut focus: any element with
      // data-fui-shortcut-focus="Mod+k" (or any combo per parseCombo
      // below) is focused on the matching keypress, regardless of
      // where the keystroke originated. Mod = Cmd on Mac, Ctrl else.
      widgetEl.querySelectorAll('[data-fui-shortcut-focus]').forEach((el) => {
        const combo = el.getAttribute('data-fui-shortcut-focus') || '';
        if (!combo) return;
        const match = parseCombo(combo);
        document.addEventListener('keydown', (e) => {
          if (!match.key) return;
          if (e.key.toLowerCase() !== match.key) return;
          if (match.mod && !(e.metaKey || e.ctrlKey)) return;
          if (match.shift && !e.shiftKey) return;
          if (match.alt && !e.altKey) return;
          // Skip when the user is mid-IME composition.
          if (e.isComposing) return;
          e.preventDefault();
          try { el.focus(); el.select?.(); } catch (_) {}
        });
      });

      // Live elapsed-time counters: any element with
       // data-fui-tick-elapsed=<unix-ms> gets its text rewritten each
       // animation frame as 'Ns', '1.2s', '12s' relative to that
       // start time. Used by the panel for pending tool-call rows so
       // a stuck tool is visible without waiting for the result.
       const tickElapsed = () => {
         widgetEl.querySelectorAll('[data-fui-tick-elapsed]').forEach((el) => {
           const start = parseInt(el.getAttribute('data-fui-tick-elapsed'), 10);
           if (!start) return;
           const ms = Date.now() - start;
           let txt;
           if (ms < 1000) txt = ms + 'ms';
           else if (ms < 10000) txt = (ms / 1000).toFixed(1) + 's';
           else txt = Math.round(ms / 1000) + 's';
           el.textContent = txt;
         });
       };
       tickElapsed();
       setInterval(tickElapsed, 200);

      // Textareas marked data-fui-autogrow size their height to fit
      // their content, capped by CSS max-height. Clears inline height
      // before measuring so growth + shrinkage both work after edits
      // and after form.reset().
      widgetEl.querySelectorAll('textarea[data-fui-autogrow]').forEach((ta) => {
        const grow = () => {
          ta.style.height = 'auto';
          ta.style.height = ta.scrollHeight + 'px';
        };
        ta.addEventListener('input', grow);
        // form.reset() doesn't fire input; pick it up next frame.
        const form = ta.form;
        if (form) form.addEventListener('reset', () => requestAnimationFrame(grow));
        // Initial sync (for value pre-set server-side or by autofill).
        Promise.resolve().then(grow);
      });

      // Enter-to-submit on textareas inside data-fui-submit-on-enter
      // forms. Shift+Enter still inserts a newline. Skips submission
      // when form is invalid (HTML5 :required handles the no-op feel).
      // Persist input drafts: data-fui-persist-storage='<key>' on
       // an input/textarea saves its value to localStorage on input
       // and restores it on mount. Cleared on form reset (after a
       // successful send) so a fresh send doesn't immediately
       // re-restore the same text.
      widgetEl.querySelectorAll('[data-fui-persist-storage]').forEach((el) => {
        const key = el.getAttribute('data-fui-persist-storage');
        if (!key) return;
        try {
          const saved = window.localStorage.getItem(key);
          if (saved && !el.value) {
            el.value = saved;
            el.dispatchEvent(new Event('input', { bubbles: true }));
          }
        } catch (_) {}
        el.addEventListener('input', () => {
          try { window.localStorage.setItem(key, el.value); } catch (_) {}
        });
        const form = el.form;
        if (form) form.addEventListener('reset', () => {
          try { window.localStorage.removeItem(key); } catch (_) {}
        });
      });

      // Copy-from: any clickable element with
      // data-fui-copy-text-from='<selector>' copies the matching
      // element's textContent to the system clipboard. The button
      // gets a brief 'copied!' affordance via the .fui-copied
      // class for 1.2s.
      widgetEl.addEventListener('click', (e) => {
        const btn = e.target.closest('[data-fui-copy-text-from]');
        if (!btn || !widgetEl.contains(btn)) return;
        const sel = btn.getAttribute('data-fui-copy-text-from');
        if (!sel) return;
        const target = widgetEl.querySelector(sel) || document.querySelector(sel);
        if (!target) return;
        e.preventDefault();
        const text = (target.innerText || target.textContent || '').trim();
        const flash = () => {
          btn.classList.add('fui-copied');
          setTimeout(() => btn.classList.remove('fui-copied'), 1200);
        };
        if (navigator.clipboard && navigator.clipboard.writeText) {
          navigator.clipboard.writeText(text).then(flash, flash);
        } else {
          // Fallback for older browsers / lacking permissions.
          try {
            const ta = document.createElement('textarea');
            ta.value = text;
            document.body.appendChild(ta);
            ta.select();
            document.execCommand('copy');
            ta.remove();
            flash();
          } catch (_) {}
        }
      });

      // Char counter: any element with data-fui-charcount-source
       // gets its textContent updated to "<n> chars" of the matching
       // input/textarea on every input. Useful for length-aware
       // prompts (LLM context budget, character limits).
      widgetEl.querySelectorAll('[data-fui-charcount-source]').forEach((el) => {
        const sel = el.getAttribute('data-fui-charcount-source');
        if (!sel) return;
        const src = widgetEl.querySelector(sel) || document.querySelector(sel);
        if (!src) return;
        const sync = () => { el.textContent = src.value.length + ' chars'; };
        src.addEventListener('input', sync);
        const form = src.form;
        if (form) form.addEventListener('reset', () => requestAnimationFrame(sync));
        sync();
      });

      // Esc clears any input/textarea opted in via
      // data-fui-clear-on-esc; fires an input event so validity
      // wiring re-syncs (Send button disables again, etc).
      widgetEl.querySelectorAll('[data-fui-clear-on-esc]').forEach((el) => {
        el.addEventListener('keydown', (e) => {
          if (e.key !== 'Escape' || !el.value) return;
          e.preventDefault();
          e.stopPropagation();
          el.value = '';
          el.dispatchEvent(new Event('input', { bubbles: true }));
        });
      });

      const enterForms = widgetEl.querySelectorAll('form[data-fui-submit-on-enter]');
      const isEnter = (e) => (e.key === 'Enter' || e.code === 'Enter' || e.keyCode === 13);
      enterForms.forEach((form) => {
        form.querySelectorAll('textarea').forEach((ta) => {
          // keydown to call preventDefault before the browser inserts \n.
          ta.addEventListener('keydown', (e) => {
            if (!isEnter(e) || e.shiftKey || e.isComposing) return;
            e.preventDefault();
            e.stopPropagation();
            if (typeof form.requestSubmit === 'function') {
              form.requestSubmit();
            } else {
              form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
            }
          });
          // Belt-and-suspenders: keypress can also fire the char event
          // in some contexts (older keyboard APIs / synthesized events).
          ta.addEventListener('keypress', (e) => {
            if (!isEnter(e) || e.shiftKey) return;
            e.preventDefault();
            e.stopPropagation();
          });
        });
      });

      // Form-validity → button-disabled wiring. Any FORM marked
       // data-fui-disable-when-invalid keeps its inner submit buttons
       // disabled while form.checkValidity() is false. Pairs with HTML5
       // input attributes (required, pattern, …) so the framework
       // doesn't reinvent validation.
      const validityForms = widgetEl.querySelectorAll('form[data-fui-disable-when-invalid]');
      validityForms.forEach((form) => {
        const sync = () => {
          const ok = form.checkValidity();
          form.querySelectorAll('button[type="submit"], input[type="submit"]').forEach((btn) => {
            btn.disabled = !ok;
          });
        };
        form.addEventListener('input', sync);
        form.addEventListener('change', sync);
        // form.reset() empties values but the 'reset' handler runs
        // BEFORE the values clear; re-sync next frame so validity
        // reflects the cleared state.
        form.addEventListener('reset', () => {
          requestAnimationFrame(sync);
        });
        // Initial state.
        Promise.resolve().then(sync);
      });

      // Widget-scoped click + submit. The click handler only fires
      // for button-like targets (BUTTON, INPUT, A) — skipping FORM
      // matches lets clicks on form descendants (radios, checkboxes,
      // text inputs) reach their default browser behavior. The form's
      // submit handler owns POSTing the form on Apply.
      widgetEl.addEventListener('click', async (e) => {
        const btn = e.target.closest('[data-fui-rpc]');
        if (btn && widgetEl.contains(btn) && btn.tagName !== 'FORM') {
          e.preventDefault();
          await dispatchRPC(btn);
          return;
        }
        const closeBtn = e.target.closest('[data-fui-action="close"]');
        if (closeBtn && widgetEl.contains(closeBtn)) {
          e.preventDefault();
          dismiss();
        }
      });
      widgetEl.addEventListener('submit', async (e) => {
        const form = e.target.closest('form[data-fui-rpc]');
        if (form && widgetEl.contains(form)) {
          e.preventDefault();
          await dispatchRPC(form);
        }
      });

      if (cfg.closeOnClick && backdrop) backdrop.addEventListener('click', dismiss);
      // closeOnEscape is handled globally on the modal stack (see the
      // single document-level handler in the boot section). Per-widget
      // listeners would all fire on a single Escape and close every
      // open modal at once instead of just the topmost. The cfg flag
      // is recorded on the _widgets entry so the global handler can
      // honour an opt-out (panel widgets, banners, etc.).
      if (isModal) NS._widgets[cfg.name].closeOnEscape = !!cfg.closeOnEscape;

      // Non-modal popover dismissal — separate from the modal stack so
      // focus trap / scroll lock semantics stay backdrop-gated. A
      // floating panel that declares closeOnEscape or closeOnClickOutside
      // is pushed onto _popoverStack; document-level handlers (boot
      // section) close the topmost on Escape / outside-click.
      if (!isModal && (cfg.closeOnEscape || cfg.closeOnClick)) {
        NS._widgets[cfg.name].closeOnEscape = !!cfg.closeOnEscape;
        NS._widgets[cfg.name].closeOnClickOutside = !!cfg.closeOnClick;
        (NS._popoverStack ||= []).push(cfg.name);
        // Track this widget's outside-click handler so dismiss() can
        // detach it. Defer attachment by a microtask so the click that
        // opened us doesn't itself trigger an immediate close — the
        // open-click bubbles to document AFTER mountWidget returns.
        if (cfg.closeOnClick) {
          const outsideHandler = (e) => {
            if (widgetEl.contains(e.target)) return;
            // The trigger button that opened us is allowed to toggle —
            // ignore clicks on any element with data-fui-open targeting
            // this widget.
            const trigger = e.target.closest('[data-fui-open="' + cfg.name + '"]');
            if (trigger) return;
            dismiss();
          };
          NS._widgets[cfg.name].outsideHandler = outsideHandler;
          setTimeout(() => document.addEventListener('click', outsideHandler), 0);
        }
      }

      // Global click+submit dispatcher (idempotent across widgets).
      // Handles agent-rendered page buttons + plain forms + legacy
      // data-kiln-tool attributes.
      if (!document.__fuiGlobalDispatch) {
        document.__fuiGlobalDispatch = true;
        document.addEventListener('click', async (e) => {
          if (e.target.closest('[data-fui-widget]')) return;
          const fuiBtn = e.target.closest('[data-fui-rpc]');
          // Same FORM-skip as the widget-scoped handler — the global
          // submit listener below owns POSTing forms.
          if (fuiBtn && fuiBtn.tagName !== 'FORM') { e.preventDefault(); await dispatchRPC(fuiBtn); return; }
          const legacy = e.target.closest('[data-kiln-tool]');
          if (legacy) {
            e.preventDefault();
            const tool = legacy.getAttribute('data-kiln-tool');
            const args = legacy.getAttribute('data-kiln-args') || '';
            try {
              await fetch('/kiln/tool/' + tool, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: args,
              });
            } catch (_) {}
          }
        });
        document.addEventListener('submit', async (e) => {
          const form = e.target.closest('form');
          if (!form || form.closest('[data-fui-widget]')) return;
          if (form.hasAttribute('data-fui-rpc')) {
            e.preventDefault();
            await dispatchRPC(form);
            return;
          }
          if (form.hasAttribute('data-kiln-tool')) {
            e.preventDefault();
            const tool = form.getAttribute('data-kiln-tool');
            const fd = new FormData(form);
            const obj = {}; fd.forEach((v, k) => { obj[k] = v; });
            try {
              await fetch('/kiln/tool/' + tool, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(obj),
              });
            } catch (_) {}
            return;
          }
          const action = form.getAttribute('action');
          if (action && !action.match(/^https?:\/\//)) {
            e.preventDefault();
            const fd = new FormData(form);
            const obj = {}; fd.forEach((v, k) => { obj[k] = v; });
            try {
              await fetch(action, {
                method: form.getAttribute('method') || 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(obj),
              });
            } catch (_) {}
          }
        });
      }
    },
  };

  // -----------------------------------------------------------------------
  // Helpers
  // -----------------------------------------------------------------------
  const closestAttr = (el, attr) => {
    const node = el.closest(`[${attr}]`);
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
    // Exact match
    if (routes.has(clean)) return true;
    // Try dynamic route patterns (e.g., /products/:slug)
    const parts = clean.split('/').filter(Boolean);
    for (const [pattern] of routes) {
      if (!pattern.includes(':')) continue;
      const patParts = pattern.split('/').filter(Boolean);
      if (patParts.length !== parts.length) continue;
      let match = true;
      for (let i = 0; i < patParts.length; i++) {
        if (patParts[i].startsWith(':')) continue; // dynamic segment
        if (patParts[i] !== parts[i]) { match = false; break; }
      }
      if (match) return true;
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
    let t = document.getElementById('fui-nav-toast');
    if (!t) {
      t = document.createElement('div');
      t.id = 'fui-nav-toast';
      t.className = 'fui-nav-toast';
      t.setAttribute('role', 'alert');
      document.body.appendChild(t);
    }
    t.textContent = msg;
    t.classList.add('is-visible');
    clearTimeout(t._fuiTimer);
    t._fuiTimer = setTimeout(() => t.classList.remove('is-visible'), 4000);
  };

  /** Fetch page, swap <main>. Caches for instant back-nav. */
  const loadPage = async (path) => {
    // Drop redundant in-flight nav to the same URL (10 clicks → 1 fetch).
    if (_pendingNav.has(path)) return;
    _pendingNav.add(path);
    const prevPath = currentPath;
    currentPath = path;
    // Surface "I heard you" feedback to assistive tech and screen
    // readers while the fetch is in flight. The CSS hook can show a
    // progress strip via [aria-busy="true"] on documentElement.
    document.documentElement.setAttribute('aria-busy', 'true');

    try {
      const cached = getCachedScreen(path);
      if (cached) {
        // Title first so SR + browser-history see the new title
        // before pushState fires (the click handler does pushState).
        document.title = cached.title;
        announceRoute(cached.title);
        swapMainContent(cached.html);
        updateActiveLink(path);
        window.scrollTo(0, 0);
        window.dispatchEvent(new CustomEvent('gofastr:navigate', { detail: { path, prevPath, cached: true } }));
        return;
      }

      const resp = await fetch(path, {
        headers: { 'X-Gofastr-Navigate': '1' },
      });
      if (!resp.ok) throw new Error(`HTTP ${resp.status}`);

      const html = await resp.text();

      // Compute title BEFORE swapping content so document.title is
      // already correct when AT or extensions observe the new state.
      let title, body, partial = resp.headers.get('X-Gofastr-Partial') === 'true';
      if (partial) {
        title = resp.headers.get('X-Gofastr-Title') || document.title;
        body = html;
      } else {
        const doc = new DOMParser().parseFromString(html, 'text/html');
        const nm = doc.querySelector('main');
        title = doc.querySelector('title')?.textContent || document.title;
        body = nm?.innerHTML ?? '';
      }
      document.title = title;
      announceRoute(title);
      swapMainContent(body);
      cacheScreen(path, body, title);

      updateActiveLink(path);
      window.scrollTo(0, 0);
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
      document.documentElement.removeAttribute('aria-busy');
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

  const swapMainContent = (html) => {
    const main = document.querySelector('[role="main"]') ?? document.querySelector('main');
    if (main) {
      main.innerHTML = html;
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
    if (anchor.target === '_blank') return;
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
    history.pushState(null, '', fullPath);
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
  });

  // Event delegation: [data-action]
  document.addEventListener('click', (e) => {
    const target = e.target.closest('[data-action]');
    if (!target) return;

    const action = target.getAttribute('data-action');
    const componentId = closestAttr(e.target, 'data-component')
      ?? closestAttr(e.target, 'data-widget');

    if (componentId && action) {
      e.preventDefault();
      window.__gofastr.trigger(componentId, action, collectParams(target));
    }
  });

  // Event delegation: [data-action-type]
  for (const eventType of ['input', 'change', 'submit']) {
    document.addEventListener(eventType, (e) => {
      const target = e.target.closest(`[data-action-type="${eventType}"]`);
      if (!target) return;

      const action = target.getAttribute('data-action');
      if (!action) return;

      const componentId = closestAttr(e.target, 'data-component')
        ?? closestAttr(e.target, 'data-widget');

      if (componentId) {
        e.preventDefault();
        const params = { ...collectParams(target), value: e.target.value ?? '', eventType };
        window.__gofastr.trigger(componentId, action, params);
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

  const hydrate = (componentId) => {
    if (hydrated.has(componentId)) return;
    hydrated.add(componentId);

    const el = document.querySelector(`[data-widget="${componentId}"]`)
      ?? document.querySelector(`[data-component="${componentId}"]`);
    if (!el) return;

    const scriptSrc = el.getAttribute('data-behavior');
    if (scriptSrc) {
      const script = document.createElement('script');
      script.src = scriptSrc;
      document.head.appendChild(script);
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
  // demand when <meta name="gofastr-sse"> is present on the page.
  // The module self-installs an EventSource and reflects "island"
  // events into matching [data-island] regions. Reconnect lives in
  // the module too.

  // FileUpload runtime has moved to its own demand-loaded module at
  // /__gofastr/runtime/fileupload.js. Core ships the loader + the
  // page-scan trigger below; the actual drag/drop wiring + filename
  // preview ships only when the page contains a [data-fui-fileupload]
  // zone (or when a `data-fui-prefetch="fileupload"` trigger is
  // hovered, whichever comes first).
  //
  // The legacy `window.__fuiWireFileUploads` is preserved by the
  // module itself for back-compat with external callers.

  // === MODULE LOADER ===================================================
  // loadModule(name) returns a cached Promise that resolves once the
  // named split-runtime module is loaded. Multiple callers for the
  // same name share one fetch. Modules self-register by setting
  // window.__gofastr.loadedModules[name] = true; the loader polls that
  // flag while the <script> downloads.
  //
  // Cache-busting: the host SSRs the per-module hash into a JSON
  // manifest under <script id="gofastr-runtime-modules">. The loader
  // reads it once; if a name is missing from the manifest, we fall
  // back to an un-versioned URL (works in dev, may pollute caches in
  // prod — the manifest is the source of truth).
  const _moduleManifest = (() => {
    try {
      const el = document.getElementById('gofastr-runtime-modules');
      if (!el) return {};
      return JSON.parse(el.textContent || '{}');
    } catch (_) { return {}; }
  })();
  const _modulePromises = {};
  function loadModule(name) {
    if (window.__gofastr.loadedModules && window.__gofastr.loadedModules[name]) {
      return Promise.resolve();
    }
    if (_modulePromises[name]) return _modulePromises[name];
    _modulePromises[name] = new Promise((resolve, reject) => {
      const v = _moduleManifest[name] || '';
      const url = '/__gofastr/runtime/' + name + '.js' + (v ? '?v=' + v : '');
      const s = document.createElement('script');
      s.src = url;
      s.async = false;
      s.onload = () => resolve();
      s.onerror = () => {
        // Drop the cached promise so a retry fires a fresh request.
        delete _modulePromises[name];
        reject(new Error('failed to load runtime module: ' + name));
      };
      document.head.appendChild(s);
    });
    return _modulePromises[name];
  }
  // Hover/focus prefetch: any element with data-fui-prefetch="<name>"
  // kicks off the module fetch as soon as the user hovers or
  // keyboard-focuses it. By the time they click, the module is
  // resolved. Capture-phase + once-per-element so we don't churn on
  // every mouse move.
  const _prefetchAttempted = new WeakSet();
  function _prefetch(e) {
    const node = e.target && e.target.closest && e.target.closest('[data-fui-prefetch]');
    if (!node || _prefetchAttempted.has(node)) return;
    _prefetchAttempted.add(node);
    const names = (node.getAttribute('data-fui-prefetch') || '').split(/\s+/).filter(Boolean);
    for (const n of names) { loadModule(n).catch(() => {}); }
  }
  document.addEventListener('pointerover', _prefetch, { capture: true, passive: true });
  document.addEventListener('focusin', _prefetch, { capture: true });

  // === DEMAND-LOAD SCANNERS ===========================================
  // Each split module has a marker attribute that, when found in the
  // DOM, triggers a load. Scanners run after DOMContentLoaded + after
  // every SPA-nav swap (`gofastr:navigate`) + when the MutationObserver
  // sees newly inserted DOM.
  const _moduleMarkers = [
    { name: 'fileupload', selector: '[data-fui-fileupload]' },
    { name: 'popover',    selector: '[data-fui-popover-anchor]' },
    { name: 'menu',       selector: '[data-fui-menu]' },
    { name: 'toasts',     selector: '[data-fui-toast-stack],[data-fui-toast]' },
    { name: 'sse',        selector: 'meta[name="gofastr-sse"]' },
  ];
  function _scanForModules(root) {
    const scope = root && root.querySelectorAll ? root : document;
    for (const m of _moduleMarkers) {
      // Skip if the module is already loaded — its own internal scanner
      // takes care of newly inserted DOM via the MutationObserver.
      if (window.__gofastr.loadedModules && window.__gofastr.loadedModules[m.name]) continue;
      if (scope.querySelector(m.selector)) loadModule(m.name).catch(() => {});
    }
  }
  // Re-scan after SPA-nav swaps content; modules that are already
  // loaded re-run their own scanner via the gofastr:navigate hook
  // they install themselves.
  window.addEventListener('gofastr:navigate', () => _scanForModules(document));

  // Close any open modal widgets on SPA navigation. Toasts/panels
  // (non-backdrop'd widgets) survive — they're page-independent
  // UI like build-progress banners.
  window.addEventListener('gofastr:navigate', () => {
    // Re-wire any already-loaded module that exposes a scanner. The
    // fileupload module sets window.__gofastr.scanFileUploads on
    // load; other split modules follow the same convention.
    setTimeout(() => {
      const G = window.__gofastr;
      if (!G) return;
      if (typeof G.scanFileUploads === 'function') G.scanFileUploads(document);
    }, 0);
    const G = window.__gofastr;
    if (!G || !G._modalStack) return;
    for (const name of [...G._modalStack]) G.closeWidget(name);
  });

  const _bootstrapComponentCSS = () => {
    const G = window.__gofastr;
    if (!G?.scanAndLoadCSS) return;
    // Seed _pendingLinks with names already covered by the SSR
    // bundle link, so the on-demand scanner doesn't redundantly load
    // per-component sheets. The names live on the bundle <link>'s
    // data-fui-bundle attribute (a stable contract), not parsed
    // from the URL.
    document.head.querySelectorAll('link[data-fui-bundle]').forEach((l) => {
      const names = (l.getAttribute('data-fui-bundle') || '').split(',');
      for (const n of names) if (n) G._pendingLinks.add(n);
    });
    G.scanAndLoadCSS(document.documentElement);
    G.scheduleIdleLoads();
  };

  if (document.readyState === 'loading') {

    // Hydration-on-SSR: every fresh page render runs through here on
    // load. Apply aria-current to the matching nav link so the active
    // state is visible without SPA-style routing. Server-rendered
    // pages can also embed aria-current themselves; this just fills
    // the gap when they don't.
    document.addEventListener('DOMContentLoaded', () => {
      updateActiveLink(location.pathname);
    });
    document.addEventListener('DOMContentLoaded', _bootstrapComponentCSS);

    // Demand-load split runtime modules: scan the page for known
    // marker attributes (data-fui-fileupload, etc.) and kick off
    // loadModule() for whichever ones are present. Each module
    // self-wires when it loads — core just decides whether to fetch.
    document.addEventListener('DOMContentLoaded', () => _scanForModules(document));

    // Mirror details.open → summary aria-expanded for screen readers.
    // Native <summary> reports as "button" without an expanded state.
    // We run it once at boot for every disclosure, plus on every
    // toggle event thereafter.
    const _mirrorDisclosure = (d) => {
      const s = d.querySelector(':scope > summary');
      if (s) s.setAttribute('aria-expanded', d.open ? 'true' : 'false');
    };
    document.addEventListener('DOMContentLoaded', () => {
      for (const d of document.querySelectorAll('details[data-fui-disclosure]')) {
        _mirrorDisclosure(d);
      }
    });
    // 'toggle' fires on every open/close. Delegate at document level
    // so dynamically-inserted disclosures are covered.
    document.addEventListener('toggle', (e) => {
      const d = e.target;
      if (d && d.tagName === 'DETAILS' && d.hasAttribute('data-fui-disclosure')) {
        _mirrorDisclosure(d);
        // Menu disclosure (data-fui-menu): on open, focus the first
        // menuitem so keyboard users land inside the panel without an
        // extra Tab. The native <summary> retains visible focus until
        // the user moves it with ArrowDown.
        if (d.open && d.hasAttribute('data-fui-menu')) {
          const first = d.querySelector('[role="menuitem"]:not([aria-disabled="true"])');
          if (first) first.focus();
        }
      }
    }, true); // capture phase — toggle doesn't bubble

    // Escape close — fires once per Escape and closes the topmost
    // dismissable surface. Modals take priority (focus trap + backdrop
    // semantics), then non-modal popovers. Both stacks are LIFO so
    // nested surfaces unwind in the order they were opened. Per-widget
    // listeners would all fire simultaneously and close every open
    // surface at once instead of just the topmost.
    document.addEventListener('keydown', (e) => {
      if (e.key !== 'Escape') return;
      const G = window.__gofastr;
      if (G._modalStack && G._modalStack.length > 0) {
        const topName = G._modalStack[G._modalStack.length - 1];
        const top = G._widgets[topName];
        if (top && top.closeOnEscape) {
          e.stopPropagation();
          G.closeWidget(topName);
          return;
        }
      }
      if (G._popoverStack && G._popoverStack.length > 0) {
        const topName = G._popoverStack[G._popoverStack.length - 1];
        const top = G._widgets[topName];
        if (top && top.closeOnEscape) {
          e.stopPropagation();
          G.closeWidget(topName);
        }
      }
    });

    // Modal Tab focus trap. When any backdrop'd widget is open, Tab
    // (and Shift+Tab) cycle focus within the topmost modal rather
    // than escaping to the surrounding page. Runs in capture so it
    // beats the menu/disclosure handlers.
    document.addEventListener('keydown', (e) => {
      if (e.key !== 'Tab') return;
      const G = window.__gofastr;
      if (!G._modalStack || G._modalStack.length === 0) return;
      const topName = G._modalStack[G._modalStack.length - 1];
      const root = G._widgets[topName]?.root;
      if (!root) return;
      const focusables = Array.from(root.querySelectorAll(G._focusSel))
        .filter((el) => el.offsetParent !== null || el === document.activeElement);
      if (focusables.length === 0) return;
      const first = focusables[0], last = focusables[focusables.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault(); last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault(); first.focus();
      } else if (!root.contains(document.activeElement)) {
        // Focus escaped (e.g. user clicked outside, then pressed Tab).
        // Pull it back to the first focusable in the modal.
        e.preventDefault(); first.focus();
      }
    }, true);

    // Menu keyboard navigation (roving focus, Home/End, Tab close,
    // type-ahead) ships in core-ui/runtime/src/menu.js — loaded on
    // demand when the page contains a [data-fui-menu] element. The
    // "focus first menuitem on open" 4-liner above stays in core
    // since it's integrated with the disclosure toggle handler.

    // Escape closes any open <details data-fui-disclosure>. Native
    // <details> only handles Escape when the summary itself has
    // focus; this extends it to "Escape anywhere on the page". An
    // open modal widget takes precedence — its own CloseOnEscape
    // handler runs and we defer so a single Escape doesn't close
    // both.
    document.addEventListener('keydown', (e) => {
      if (e.key !== 'Escape') return;
      const G = window.__gofastr;
      if (G && G._modalStack && G._modalStack.length > 0) return;
      for (const d of document.querySelectorAll('details[data-fui-disclosure][open]')) {
        // Only refocus the summary if focus is already inside this
        // disclosure — otherwise we'd yank focus away from whatever
        // the user was actually doing in main content.
        const wasInside = d.contains(document.activeElement);
        d.removeAttribute('open');
        if (wasInside) d.querySelector('summary')?.focus();
      }
    });
  } else {
    // Document already past parsing — run the same hooks the
    // DOMContentLoaded branch installs. SSE connection is handled
    // by the module loader (the marker scanner detects
    // <meta name="gofastr-sse"> and idle-loads the sse module).
    _scanForModules(document);
    _bootstrapComponentCSS();
  }

  window.G=window.__gofastr;
})();
