// GoFastr Core-UI Runtime v0.4 — ES2020+
(() => {
  'use strict';

  // OS hint on <html data-fui-os="mac|other"> so SSR-rendered
  // shortcut hints (framework/ui.ShortcutHint) can display
  // platform-correct mod-key glyphs (⌘ on Mac, Ctrl elsewhere)
  // without per-component JS. Detection is best-effort; functional
  // shortcut matching does not depend on this (parseCombo accepts
  // both metaKey and ctrlKey when Mod is required).
  try {
    const ua = (navigator.userAgentData && navigator.userAgentData.platform) ||
               navigator.platform || '';
    document.documentElement.setAttribute(
      'data-fui-os',
      /Mac|iPhone|iPad|iPod/.test(ua) ? 'mac' : 'other'
    );
  } catch (_) { /* SSR / non-browser */ }

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
      if (toastHeader && window.__gofastr.toast) {
        try {
          const parsed = JSON.parse(toastHeader);
          const arr = Array.isArray(parsed) ? parsed : [parsed];
          for (const cfg of arr) window.__gofastr.toast(cfg);
        } catch (_) {}
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
      // Open any focused combobox so typing makes the listbox visible
      // without requiring an ArrowDown press first. We can't wait for
      // the RPC response — by then the listbox already has options but
      // aria-expanded stays false and CSS keeps the panel hidden.
      const combo = e.target && e.target.closest && e.target.closest('[role="combobox"]');
      if (combo) {
        const lbId = combo.getAttribute('aria-controls');
        const lb = lbId ? document.getElementById(lbId) : null;
        if (lb) {
          combo.setAttribute('aria-expanded', 'true');
          lb.removeAttribute('hidden');
        }
      }
      const form = e.target.closest('form[data-fui-rpc][data-fui-rpc-trigger="input"]');
      if (!form) return;
      // Note: this used to skip forms inside [data-fui-widget] under the
      // theory that the widget would own its own input handling — but no
      // widget-scoped input-trigger handler exists (only general-purpose
      // ones for char-count, autogrow, etc.), so the skip stranded any
      // combobox / typeahead inside a widget surface (e.g. CommandPalette).
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
              try {
                const cfg = JSON.parse(toastBtn.getAttribute('data-fui-toast'));
                window.__gofastr.toast(cfg);
              } catch (_) {}
              e.preventDefault();
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
            ).then(() => {
              if (anchorPref !== null) {
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
          window.__gofastr.toast(result.message);
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
          // Pure no-op for non-toast signals — the querySelector finds
          // nothing and returns early.
          if (node.querySelector && node.querySelector('[data-fui-toast-id]')) {
            window.__gofastr._initToasts(node);
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

    /** Toast TTL bookkeeping: id -> { timer, remaining, startedAt }. */
    _toastTimers: {},

    /** Wire freshly-swapped toast items inside `root`: schedule auto
        dismiss via data-fui-toast-ttl-ms, pause on hover/focus, resume
        on leave, click-to-dismiss via [data-fui-toast-dismiss]. The
        same toast HTML re-rendered should NOT reset existing timers
        — we key by toast id and skip already-known ones. */
    _initToasts(root) {
      const NS = this;
      const items = root.querySelectorAll('[data-fui-toast-id]');
      // Track which ids are present in the current DOM. Anything in
      // _toastTimers but absent here was dismissed — cancel its timer.
      const present = {};
      items.forEach((item) => {
        const id = item.getAttribute('data-fui-toast-id');
        present[id] = true;
        if (NS._toastTimers[id]) return; // already wired
        const ttl = parseInt(item.getAttribute('data-fui-toast-ttl-ms') || '0', 10);
        if (ttl > 0) {
          const rec = { remaining: ttl, startedAt: Date.now(), timer: 0 };
          NS._toastTimers[id] = rec;
          const arm = () => {
            rec.startedAt = Date.now();
            rec.timer = setTimeout(() => NS._dismissToast(item, id), rec.remaining);
          };
          const pause = () => {
            if (!rec.timer) return;
            clearTimeout(rec.timer);
            rec.timer = 0;
            rec.remaining -= Date.now() - rec.startedAt;
            if (rec.remaining < 100) rec.remaining = 100;
          };
          item.addEventListener('mouseenter', pause);
          item.addEventListener('focusin', pause);
          item.addEventListener('mouseleave', arm);
          item.addEventListener('focusout', arm);
          arm();
        }
        item.addEventListener('click', (e) => {
          if (e.target.closest('[data-fui-toast-dismiss]')) {
            e.preventDefault();
            NS._dismissToast(item, id);
          }
        });
      });
      // Cancel timers for toasts that are no longer in the DOM
      // (server-side dismissal / replacement).
      for (const id in NS._toastTimers) {
        if (!present[id]) {
          clearTimeout(NS._toastTimers[id].timer);
          delete NS._toastTimers[id];
        }
      }
    },

    /** Add the leave class, remove from DOM after the CSS animation
        finishes. Idempotent: a second dismiss on a leaving toast is
        a no-op. */
    _dismissToast(item, id) {
      if (!item || item.classList.contains('is-leaving')) return;
      item.classList.add('is-leaving');
      const rec = this._toastTimers[id];
      if (rec) { clearTimeout(rec.timer); delete this._toastTimers[id]; }
      // Read duration from the computed style so theme overrides apply.
      // Fall back to 200ms if unset / unparseable.
      const cs = getComputedStyle(item);
      const ms = parseFloat(cs.animationDuration) * 1000 || 200;
      setTimeout(() => { if (item.parentNode) item.parentNode.removeChild(item); }, ms);
    },

    /** Counter for client-generated toast ids — unique within a page. */
    _toastSeq: 0,

    /** Push a toast onto a stack. cfg = { variant, title, body, ttl, stack }
        OR a bare string treated as { title: <string>, ttl: 4000 } for
        ergonomic ad-hoc calls.

        variant: info|success|warning|danger|neutral (default info)
        title:   required string
        body:    optional supporting text
        ttl:     milliseconds; 0 (or omitted with cfg-object) = persistent.
                 String-arg shorthand defaults to a 4s auto-dismiss.
        stack:   name of the toast-stack widget to push into; defaults
                 to the first stack present in the DOM (auto-creates a
                 plain top-right container if none is mounted).
        Returns the new item's id. */
    toast(cfg) {
      if (cfg == null) return null;
      // String shorthand: __gofastr.toast("Saved") → info toast, 4s ttl.
      if (typeof cfg === 'string') cfg = { title: cfg, ttl: 4000 };
      if (!cfg.title) return null;
      // Locate the target stack container. Explicit cfg.stack wins,
      // otherwise pick the first stack on the page. If no stack is
      // mounted (apps that haven't registered a preset.ToastStack),
      // auto-create one and append it to <body> — toasts always fire.
      let container = null;
      if (cfg.stack) {
        container = document.querySelector(
          '[data-fui-toast-stack="' + CSS.escape(cfg.stack) + '"]');
      }
      if (!container) {
        container = document.querySelector('[data-fui-toast-stack]');
      }
      if (!container) {
        container = document.createElement('div');
        container.className = 'ui-toast-stack';
        container.setAttribute('data-fui-comp', 'ui-toast-stack');
        container.setAttribute('data-fui-toast-stack', '__auto');
        container.style.cssText = 'position:fixed;top:1rem;right:1rem;z-index:2147483600;display:grid;gap:0.5rem;pointer-events:none;max-width:min(360px,calc(100vw - 2rem));';
        document.body.appendChild(container);
        if (this.scanAndLoadCSS) this.scanAndLoadCSS(container);
      }

      const id = 't' + (++this._toastSeq);
      const variant = cfg.variant || 'info';
      const isAssertive = variant === 'warning' || variant === 'danger';
      const role = isAssertive ? 'alert' : 'status';
      const live = isAssertive ? 'assertive' : 'polite';
      const glyph = ({success:'✓', warning:'!', danger:'✕', info:'i', neutral:'•'})[variant] || 'i';

      const ttl = parseInt(cfg.ttl || 0, 10);
      const item = document.createElement('div');
      item.className = 'ui-toast-stack__item';
      item.setAttribute('data-fui-toast-id', id);
      if (ttl > 0) item.setAttribute('data-fui-toast-ttl-ms', String(ttl));

      const wrap = document.createElement('div');
      wrap.className = 'ui-notification ui-notification--' + variant;
      wrap.setAttribute('data-fui-comp', 'ui-notification');
      wrap.setAttribute('role', role);
      wrap.setAttribute('aria-live', live);

      // Use textContent on the title/body to insulate from XSS — cfg
      // values may originate from server responses or page scripts;
      // the runtime never interprets them as HTML.
      const icon = document.createElement('span');
      icon.className = 'ui-notification__icon';
      icon.setAttribute('aria-hidden', 'true');
      icon.textContent = glyph;

      const text = document.createElement('div');
      text.className = 'ui-notification__text';
      const titleEl = document.createElement('strong');
      titleEl.className = 'ui-notification__title';
      titleEl.textContent = cfg.title;
      text.appendChild(titleEl);
      if (cfg.body) {
        const bodyEl = document.createElement('p');
        bodyEl.className = 'ui-notification__body';
        bodyEl.textContent = cfg.body;
        text.appendChild(bodyEl);
      }

      const dismiss = document.createElement('button');
      dismiss.type = 'button';
      dismiss.className = 'ui-notification__dismiss';
      dismiss.setAttribute('aria-label', 'Dismiss notification');
      dismiss.setAttribute('data-fui-toast-dismiss', '');
      dismiss.textContent = '×';

      wrap.appendChild(icon);
      wrap.appendChild(text);
      wrap.appendChild(dismiss);
      item.appendChild(wrap);
      container.appendChild(item);

      // The notification's `data-fui-comp="ui-notification"` marker is
      // emitted on the wrap element. Trigger the CSS catalog scanner
      // so the per-component stylesheet loads (no-op if already
      // loaded once this session).
      if (this.scanAndLoadCSS) this.scanAndLoadCSS(item);

      // Reuse the existing TTL/hover-pause/click-dismiss wiring so
      // header-driven and JS-driven toasts behave identically.
      this._initToasts(container);
      return id;
    },

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

    /** Position a freshly-opened popover-style widget next to its
        trigger element. The preferred placement is the value of the
        trigger's `data-fui-popover-anchor` attribute — one of
        "top", "bottom", "left", "right", or "auto" (= bottom-first
        with fallback). When the preferred side would overflow the
        viewport, the algorithm tries the opposite side and finally
        clamps inside an 8px viewport margin.

        Side-effects on top of position:

         - Adds `data-fui-popover-trigger="<name>"` and `class:
           is-popover-trigger-active` on the trigger so the
           originating button can highlight while its popover is
           open. Cleared on dismiss.
         - Adds `data-fui-popover-side="top|bottom|left|right"` on
           the widget root reflecting the chosen side (post-flip).
           CSS uses it to position the arrow / pointer.
         - Repositions on `window.resize` until dismissed. */
    _anchorPopover(name, trigger, preferred) {
      const NS = this;
      const widget = NS._widgets[name];
      if (!widget || !widget.root) return;
      const root = widget.root;
      const pref = (preferred || 'auto').toLowerCase();
      // If we were already anchored to a different trigger (popover
      // re-opened from a sibling), clear the previous trigger's
      // active state + resize listener before rebinding.
      const prevTrigger = widget.anchorTrigger;
      if (prevTrigger && prevTrigger !== trigger) {
        prevTrigger.classList.remove('is-popover-trigger-active');
        prevTrigger.removeAttribute('data-fui-popover-trigger');
      }
      if (widget.anchorResize) {
        window.removeEventListener('resize', widget.anchorResize);
        widget.anchorResize = null;
      }
      if (widget.anchorScroll) {
        window.removeEventListener('scroll', widget.anchorScroll, { capture: true });
        widget.anchorScroll = null;
      }
      // Mark trigger as the currently-active source.
      trigger.classList.add('is-popover-trigger-active');
      trigger.setAttribute('data-fui-popover-trigger', name);
      const place = () => {
        const gap = 10; // gap >= arrow-size so the pointer fits cleanly
        const margin = 8;
        const tr = trigger.getBoundingClientRect();
        // Set the anchored marker BEFORE measuring so the chrome's
        // border + shadow + max-inline-size are reflected in the
        // bounding rect — without this the measurement is from the
        // un-styled chrome and the placement misses by a few pixels.
        if (!root.hasAttribute('data-fui-popover-side')) {
          root.setAttribute('data-fui-popover-side', 'bottom');
        }
        // Reset overrides so we measure the widget at its natural size.
        root.style.left = '';
        root.style.top = '';
        root.style.right = '';
        root.style.bottom = '';
        const wr = root.getBoundingClientRect();
        const vw = window.innerWidth;
        const vh = window.innerHeight;
        const order = (pref === 'auto')
          ? ['bottom', 'top', 'right', 'left']
          : [pref, 'bottom', 'top', 'right', 'left'].filter((s, i, a) => a.indexOf(s) === i);
        let x = tr.left;
        let y = tr.bottom + gap;
        let chosen = order[0];
        for (const side of order) {
          if (side === 'bottom') {
            x = tr.left;
            y = tr.bottom + gap;
            if (y + wr.height <= vh - margin) { chosen = 'bottom'; break; }
          } else if (side === 'top') {
            x = tr.left;
            y = tr.top - gap - wr.height;
            if (y >= margin) { chosen = 'top'; break; }
          } else if (side === 'right') {
            x = tr.right + gap;
            y = tr.top;
            if (x + wr.width <= vw - margin) { chosen = 'right'; break; }
          } else if (side === 'left') {
            x = tr.left - gap - wr.width;
            y = tr.top;
            if (x >= margin) { chosen = 'left'; break; }
          }
          chosen = side;
        }
        // Clamp into the viewport.
        x = Math.max(margin, Math.min(x, vw - wr.width - margin));
        y = Math.max(margin, Math.min(y, vh - wr.height - margin));
        root.style.position = 'fixed';
        root.style.left = x + 'px';
        root.style.top = y + 'px';
        root.style.right = 'auto';
        root.style.bottom = 'auto';
        root.setAttribute('data-fui-popover-side', chosen);
        // Arrow offset — distance from popover's anchored edge to
        // the center of the trigger, so the arrow always sits below
        // the originating button regardless of clamping.
        if (chosen === 'top' || chosen === 'bottom') {
          const arrowX = (tr.left + tr.width / 2) - x;
          root.style.setProperty('--ui-popover-arrow-x', Math.max(12, Math.min(arrowX, wr.width - 12)) + 'px');
        } else {
          const arrowY = (tr.top + tr.height / 2) - y;
          root.style.setProperty('--ui-popover-arrow-y', Math.max(12, Math.min(arrowY, wr.height - 12)) + 'px');
        }
      };
      place();
      // Reposition on viewport resize AND on scroll — the popover is
      // position:fixed, so without these the trigger moves under the
      // page scroll while the popover stays glued to the viewport.
      // Listeners run via requestAnimationFrame so we get one place()
      // per frame even on a furious wheel-spin. capture:true picks up
      // scroll events from ANY ancestor (overflow:auto containers)
      // without listing them explicitly. passive:true preserves
      // smooth scrolling.
      let rafPending = false;
      const schedulePlace = () => {
        if (rafPending) return;
        rafPending = true;
        requestAnimationFrame(() => {
          rafPending = false;
          place();
        });
      };
      const onResize = schedulePlace;
      const onScroll = schedulePlace;
      window.addEventListener('resize', onResize);
      window.addEventListener('scroll', onScroll, { passive: true, capture: true });
      widget.anchorResize = onResize;
      widget.anchorScroll = onScroll;
      widget.anchorTrigger = trigger;
    },

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
          // dispatches each into the client toast stack.
          const toastHeader = r.headers.get('X-Gofastr-Toast');
          if (toastHeader && NS.toast) {
            try {
              const parsed = JSON.parse(toastHeader);
              const arr = Array.isArray(parsed) ? parsed : [parsed];
              for (const cfg of arr) NS.toast(cfg);
            } catch (_) {}
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

      // (data-fui-copy-text-from handler is now globally delegated
      // at the document level — see _wireCopyHandler() below — so it
      // works for buttons outside any widget context too.)

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

  // SSE Island Support
  const connectSSE = () => {
    const sseUrl = document.querySelector('meta[name="gofastr-sse"]')?.getAttribute('content');
    if (!sseUrl) return;

    const source = new EventSource(sseUrl);

    source.addEventListener('island', (event) => {
      try {
        const { island, html } = JSON.parse(event.data);
        if (island === undefined || html === undefined) return;
        const el = document.querySelector(`[data-island="${island}"]`);
        if (!el) return;
        el.innerHTML = html;
        el.classList.add('island-updated');
        setTimeout(() => el.classList.remove('island-updated'), 1000);
      } catch { /* ignore malformed SSE */ }
    });

    source.onerror = () => {
      source.close();
      setTimeout(connectSSE, 3000);
    };
  };

  // FileUpload runtime — wire every [data-fui-fileupload] zone in a
  // subtree. Idempotent: zones already wired carry a __fuiWired flag.
  // Reads dropped files into the inner <input type="file">, renders
  // a filename + size summary (plus an image thumbnail for the first
  // picked image) into the embedded .ui-fileupload__filename element.
  function _wireFileUploads(root) {
    const scope = root && root.querySelectorAll ? root : document;
    const zones = scope.querySelectorAll('[data-fui-fileupload]');
    for (const zone of zones) {
      if (zone.__fuiWired) continue;
      zone.__fuiWired = true;
      const input = zone.querySelector('input[type="file"]');
      if (!input) continue;
      const filename = zone.querySelector('.ui-fileupload__filename');

      const fmtBytes = (n) => {
        if (n < 1024) return n + ' B';
        if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB';
        return (n / (1024 * 1024)).toFixed(2) + ' MB';
      };
      const render = () => {
        if (!filename) return;
        filename.innerHTML = '';
        const files = input.files;
        if (!files || files.length === 0) return;
        filename.classList.add('is-populated');
        // Optional thumbnail — use the first IMAGE in the picked set
        // (not necessarily files[0], which could be a non-image like
        // a doc submitted alongside screenshots). Keeps payload small
        // (don't load N data URLs at once for 50 photos).
        const firstImage = Array.from(files).find(f => f.type && f.type.startsWith('image/'));
        if (firstImage) {
          const img = document.createElement('img');
          img.className = 'ui-fileupload__thumb';
          img.alt = '';
          const reader = new FileReader();
          reader.onload = (e) => { img.src = e.target.result; };
          reader.readAsDataURL(firstImage);
          filename.appendChild(img);
        }
        // Filenames list
        const list = document.createElement('ul');
        list.className = 'ui-fileupload__list';
        for (const f of files) {
          const li = document.createElement('li');
          li.textContent = f.name + ' · ' + fmtBytes(f.size);
          list.appendChild(li);
        }
        filename.appendChild(list);
      };
      input.addEventListener('change', render);
      // Initial render for SSR-restored states (some browsers
      // restore input.files on back-nav).
      render();

      const onEnter = (e) => {
        e.preventDefault();
        zone.closest('[data-fui-comp="ui-fileupload"]')?.classList.add('is-dragover');
      };
      const onLeave = (e) => {
        e.preventDefault();
        // dragleave fires when moving to a child — guard via relatedTarget.
        if (zone.contains(e.relatedTarget)) return;
        zone.closest('[data-fui-comp="ui-fileupload"]')?.classList.remove('is-dragover');
      };
      const onDrop = (e) => {
        e.preventDefault();
        zone.closest('[data-fui-comp="ui-fileupload"]')?.classList.remove('is-dragover');
        const files = e.dataTransfer && e.dataTransfer.files;
        if (!files || files.length === 0) return;
        if (input.disabled) return;
        // Assign via DataTransfer so the input's `files` becomes the
        // dropped list — required because input.files is read-only
        // except via a DataTransfer object.
        const dt = new DataTransfer();
        for (const f of files) {
          if (!input.multiple && dt.items.length > 0) break;
          dt.items.add(f);
        }
        input.files = dt.files;
        input.dispatchEvent(new Event('change', { bubbles: true }));
      };
      zone.addEventListener('dragenter', onEnter);
      zone.addEventListener('dragover', onEnter);
      zone.addEventListener('dragleave', onLeave);
      zone.addEventListener('drop', onDrop);
    }
  }
  // Make it available for the SPA-nav swap path.
  window.__fuiWireFileUploads = _wireFileUploads;

  // -----------------------------------------------------------------------
  // Globally-delegated copy-to-clipboard handler. Any element with
  // `data-fui-copy-text-from='<selector>'` copies the matching element's
  // textContent on click — works inside or outside a widget context.
  //
  // Feedback channels:
  //   - Adds `.fui-copied` to the button for 1.2s (CSS swaps inner
  //     `.ui-copy-btn__label` ↔ `.ui-copy-btn__copied` spans).
  //   - If the button has a sibling/ancestor `[data-fui-copy-status]`,
  //     writes the configured text into it (polite aria-live region).
  //   - If `data-fui-copy-toast` is set, dispatches a toast via
  //     `window.__gofastr.toast({...})` (a JSON config) so callers can
  //     opt into success/error toasts without per-button JS.
  // -----------------------------------------------------------------------
  document.addEventListener('click', (e) => {
    const btn = e.target && e.target.closest && e.target.closest('[data-fui-copy-text-from]');
    if (!btn) return;
    const sel = btn.getAttribute('data-fui-copy-text-from');
    if (!sel) return;
    const target = document.querySelector(sel);
    if (!target) return;
    e.preventDefault();
    const text = (target.innerText || target.textContent || '').trim();
    const flash = () => {
      btn.classList.add('fui-copied');
      setTimeout(() => btn.classList.remove('fui-copied'), 1200);
    };
    const announce = () => {
      const root = btn.parentElement || btn;
      const status = root.querySelector('[data-fui-copy-status]')
        || btn.querySelector('[data-fui-copy-status]');
      if (!status) return;
      const msg = btn.getAttribute('data-fui-copy-announce') || 'Copied';
      status.textContent = '';
      setTimeout(() => { status.textContent = msg; }, 30);
    };
    const fireToast = () => {
      const raw = btn.getAttribute('data-fui-copy-toast');
      if (!raw) return;
      try {
        const cfg = JSON.parse(raw);
        if (window.__gofastr && window.__gofastr.toast) {
          window.__gofastr.toast(cfg);
        }
      } catch (_) { /* malformed JSON: ignore */ }
    };
    const success = () => { flash(); announce(); fireToast(); };
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(success, success);
    } else {
      // Fallback for older browsers / lacking permissions.
      try {
        const ta = document.createElement('textarea');
        ta.value = text;
        document.body.appendChild(ta);
        ta.select();
        document.execCommand('copy');
        ta.remove();
        success();
      } catch (_) { /* deliberately silent — copy is best-effort */ }
    }
  });

  // -----------------------------------------------------------------------
  // Infinite scroll wiring.
  //
  // Every [data-fui-infinite-scroll] wrapper gets an IntersectionObserver
  // attached to its child [data-fui-infinite-sentinel]. When the sentinel
  // intersects the viewport (with rootMargin from data-fui-infinite-root-
  // margin, default "200px"), the runtime POSTs to the wrapper's
  // data-fui-infinite-scroll URL with the current cursor in the form
  // body. The response HTML is appended to the items container.
  //
  // Response semantics:
  //   - Body: HTML fragment, appended verbatim into the items container.
  //   - X-Gofastr-Infinite-Cursor: next cursor token. Empty/missing →
  //     end-of-feed, sentinel removed, observer disconnected.
  //
  // aria-busy on the wrapper toggles true → false across each fetch so
  // screen readers can detect loading state. The wrapper's role should
  // be "feed" (set by the SSR component) for proper announcement.
  //
  // No-JS fallback: the SSR component renders a <noscript><form action=
  // "<rpcPath>"> "Load more" button alongside the sentinel.
  // -----------------------------------------------------------------------
  function _wireInfiniteScroll(root) {
    if (typeof IntersectionObserver === 'undefined') return;
    const wrappers = (root === document
      ? document.querySelectorAll('[data-fui-infinite-scroll]')
      : (root.matches && root.matches('[data-fui-infinite-scroll]')
          ? [root, ...root.querySelectorAll('[data-fui-infinite-scroll]')]
          : root.querySelectorAll('[data-fui-infinite-scroll]')));
    wrappers.forEach((wrap) => {
      if (wrap.__fuiInfiniteWired) return;
      wrap.__fuiInfiniteWired = true;
      const sentinel = wrap.querySelector('[data-fui-infinite-sentinel]');
      if (!sentinel) return;
      const path = wrap.getAttribute('data-fui-infinite-scroll');
      if (!path) return;
      const itemsSel = wrap.getAttribute('data-fui-infinite-items') || '[data-fui-infinite-items]';
      const items = wrap.querySelector(itemsSel) || wrap;
      const rootMargin = wrap.getAttribute('data-fui-infinite-root-margin') || '200px';
      let cursor = wrap.getAttribute('data-fui-infinite-cursor') || '';
      let inFlight = false;
      let exhausted = false;

      const fetchMore = async () => {
        if (inFlight || exhausted) return;
        inFlight = true;
        wrap.setAttribute('aria-busy', 'true');
        try {
          const body = new URLSearchParams();
          if (cursor) body.set('cursor', cursor);
          const r = await fetch(path, {
            method: 'POST',
            headers: { 'Accept': 'text/html', 'Content-Type': 'application/x-www-form-urlencoded' },
            body: body.toString(),
            credentials: 'same-origin',
          });
          if (!r.ok) {
            // Soft-fail; the user can scroll up and try again. No state mutation.
            return;
          }
          const html = await r.text();
          if (html) {
            const tmp = document.createElement('template');
            tmp.innerHTML = html;
            items.appendChild(tmp.content);
            if (window.__gofastr?.scanAndLoadCSS) {
              window.__gofastr.scanAndLoadCSS(items);
            }
          }
          const next = r.headers.get('X-Gofastr-Infinite-Cursor') || '';
          if (next === '') {
            // End of feed.
            exhausted = true;
            observer.disconnect();
            sentinel.remove();
          } else {
            cursor = next;
            wrap.setAttribute('data-fui-infinite-cursor', next);
          }
        } catch (_) {
          /* network / abort — keep sentinel, allow retry on next intersection */
        } finally {
          inFlight = false;
          wrap.setAttribute('aria-busy', 'false');
        }
        // After a fetch lands, the IntersectionObserver won't re-fire
        // if the sentinel was already in view and stays in view (new
        // items get inserted ABOVE the sentinel, so its viewport
        // intersection is unchanged). Chase the next page if needed.
        if (!exhausted) {
          requestAnimationFrame(() => requestAnimationFrame(() => {
            const r2 = sentinel.getBoundingClientRect();
            const vh = window.innerHeight || document.documentElement.clientHeight;
            // Convert rootMargin to pixels for the manual check — same
            // semantics as IntersectionObserver's rootMargin.
            const margin = parseInt(rootMargin, 10) || 0;
            const inView = r2.top < vh + margin && r2.bottom > -margin;
            if (inView) fetchMore();
          }));
        }
      };

      const observer = new IntersectionObserver((entries) => {
        for (const e of entries) {
          if (e.isIntersecting) {
            fetchMore();
            break;
          }
        }
      }, { rootMargin });
      observer.observe(sentinel);
    });
  }
  window.__fuiWireInfiniteScroll = _wireInfiniteScroll;

  // Close any open modal widgets on SPA navigation. Toasts/panels
  // (non-backdrop'd widgets) survive — they're page-independent
  // UI like build-progress banners.
  window.addEventListener('gofastr:navigate', () => {
    // Re-wire file uploads on the new page content.
    setTimeout(() => _wireFileUploads(document), 0);
    // Re-wire infinite scroll on the new page content.
    setTimeout(() => _wireInfiniteScroll(document), 0);
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
    document.addEventListener('DOMContentLoaded', connectSSE);

    // Hydration-on-SSR: every fresh page render runs through here on
    // load. Apply aria-current to the matching nav link so the active
    // state is visible without SPA-style routing. Server-rendered
    // pages can also embed aria-current themselves; this just fills
    // the gap when they don't.
    document.addEventListener('DOMContentLoaded', () => {
      updateActiveLink(location.pathname);
    });
    document.addEventListener('DOMContentLoaded', _bootstrapComponentCSS);

    // FileUpload: wire drag/drop on every [data-fui-fileupload] zone
    // and render a filename + size preview into the embedded
    // .ui-fileupload__filename element after every change. Native
    // <input type="file"> is the source of truth; the runtime just
    // forwards dropped File objects into it so the form-POST flow
    // and the picker flow share one code path.
    document.addEventListener('DOMContentLoaded', _wireFileUploads);
    document.addEventListener('DOMContentLoaded', () => _wireInfiniteScroll(document));

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

    // Menu keyboard nav. Lives at document level so newly-inserted
    // menus pick it up for free. Handles: ArrowDown/Up roving focus,
    // Home/End, Tab (close + escape), printable-key type-ahead. Esc
    // is owned by the data-fui-disclosure handler below.
    let _menuTypeBuf = '', _menuTypeAt = 0;
    document.addEventListener('keydown', (e) => {
      const item = e.target && e.target.closest && e.target.closest('[role="menuitem"]');
      if (!item) return;
      const panel = item.closest('[role="menu"]');
      if (!panel) return;
      const items = Array.from(panel.querySelectorAll('[role="menuitem"]:not([aria-disabled="true"])'));
      if (items.length === 0) return;
      const idx = items.indexOf(item);
      const move = (to) => {
        e.preventDefault();
        items[(to + items.length) % items.length].focus();
      };
      if (e.key === 'ArrowDown') return move(idx + 1);
      if (e.key === 'ArrowUp')   return move(idx - 1);
      if (e.key === 'Home')      return move(0);
      if (e.key === 'End')       return move(items.length - 1);
      if (e.key === 'Tab') {
        // Close the surrounding disclosure so focus escapes naturally.
        const d = panel.closest('details[data-fui-disclosure]');
        if (d) d.removeAttribute('open');
        return; // do NOT preventDefault — let Tab move focus
      }
      // Type-ahead: a printable single-character key jumps to the
      // next item whose label starts with the accumulated prefix.
      if (e.key.length === 1 && !e.ctrlKey && !e.metaKey && !e.altKey) {
        const now = Date.now();
        if (now - _menuTypeAt > 800) _menuTypeBuf = '';
        _menuTypeAt = now;
        _menuTypeBuf += e.key.toLowerCase();
        for (let i = 1; i <= items.length; i++) {
          const cand = items[(idx + i) % items.length];
          const label = (cand.textContent || '').trim().toLowerCase();
          if (label.startsWith(_menuTypeBuf)) {
            e.preventDefault();
            cand.focus();
            return;
          }
        }
      }
    });

    // Combobox keyboard nav. Listens at document level so dynamically
    // populated listboxes (data-fui-rpc returns options) pick it up
    // for free. Per the WAI-ARIA Combobox 1.2 pattern:
    //
    //   ArrowDown — open listbox + highlight first option; if open,
    //               move highlight to next option (wraps)
    //   ArrowUp   — open listbox + highlight last option; if open,
    //               move highlight to previous option (wraps)
    //   Enter     — select the highlighted option: fill input with
    //               its data-value (or textContent), close listbox
    //   Escape    — close listbox; second Esc clears input
    //   Tab       — close listbox; let Tab move focus naturally
    //
    // aria-activedescendant reflects the highlighted option's id.
    // The runtime tags the highlighted option with .is-active so CSS
    // can style it without rewriting class lists.
    const _comboHighlight = (lb, opt) => {
      lb.querySelectorAll('[role="option"].is-active').forEach((o) => {
        o.classList.remove('is-active');
      });
      if (opt) {
        opt.classList.add('is-active');
        const input = document.querySelector('[role="combobox"][aria-controls="' + lb.id + '"]');
        if (input) input.setAttribute('aria-activedescendant', opt.id || '');
      }
    };
    const _comboClose = (input, lb) => {
      input.setAttribute('aria-expanded', 'false');
      input.setAttribute('aria-activedescendant', '');
      if (lb) {
        lb.setAttribute('hidden', '');
        lb.querySelectorAll('[role="option"].is-active').forEach((o) => o.classList.remove('is-active'));
      }
    };
    const _comboOpen = (input, lb) => {
      if (!lb) return;
      input.setAttribute('aria-expanded', 'true');
      lb.removeAttribute('hidden');
    };
    const _comboPick = (input, lb, opt) => {
      if (!opt) return;
      const val = opt.getAttribute('data-value') || (opt.textContent || '').trim();
      input.value = val;
      input.dispatchEvent(new Event('change', { bubbles: true }));
      _comboClose(input, lb);
    };
    document.addEventListener('keydown', (e) => {
      const input = e.target && e.target.closest && e.target.closest('[role="combobox"]');
      if (!input) return;
      const lbId = input.getAttribute('aria-controls');
      if (!lbId) return;
      const lb = document.getElementById(lbId);
      if (!lb) return;
      const options = Array.from(lb.querySelectorAll('[role="option"]:not([aria-disabled="true"])'));
      const activeId = input.getAttribute('aria-activedescendant');
      const activeIdx = options.findIndex((o) => o.id === activeId);
      const isOpen = input.getAttribute('aria-expanded') === 'true';
      switch (e.key) {
        case 'ArrowDown': {
          if (options.length === 0) return;
          e.preventDefault();
          if (!isOpen) { _comboOpen(input, lb); _comboHighlight(lb, options[0]); return; }
          const next = options[(activeIdx + 1 + options.length) % options.length];
          _comboHighlight(lb, next);
          return;
        }
        case 'ArrowUp': {
          if (options.length === 0) return;
          e.preventDefault();
          if (!isOpen) { _comboOpen(input, lb); _comboHighlight(lb, options[options.length - 1]); return; }
          const prev = options[(activeIdx - 1 + options.length) % options.length];
          _comboHighlight(lb, prev);
          return;
        }
        case 'Home': {
          if (!isOpen || options.length === 0) return;
          e.preventDefault();
          _comboHighlight(lb, options[0]);
          return;
        }
        case 'End': {
          if (!isOpen || options.length === 0) return;
          e.preventDefault();
          _comboHighlight(lb, options[options.length - 1]);
          return;
        }
        case 'Enter': {
          if (!isOpen) return;
          if (activeIdx < 0) return;
          e.preventDefault();
          _comboPick(input, lb, options[activeIdx]);
          return;
        }
        case 'Escape': {
          if (isOpen) { e.preventDefault(); _comboClose(input, lb); return; }
          if (input.value) { e.preventDefault(); input.value = ''; input.dispatchEvent(new Event('input', { bubbles: true })); return; }
          return;
        }
        case 'Tab': {
          if (isOpen) _comboClose(input, lb);
          return;
        }
      }
    });
    // Click on an option picks it. Delegate so RPC-injected options work.
    document.addEventListener('click', (e) => {
      const opt = e.target && e.target.closest && e.target.closest('[role="option"]');
      if (!opt || opt.getAttribute('aria-disabled') === 'true') return;
      const lb = opt.closest('[role="listbox"]');
      if (!lb || !lb.id) return;
      const input = document.querySelector('[role="combobox"][aria-controls="' + lb.id + '"]');
      if (!input) return;
      e.preventDefault();
      _comboPick(input, lb, opt);
    });
    // Auto-open the listbox when the input receives focus and the
    // server has populated options. Auto-close on outside click.
    document.addEventListener('focusin', (e) => {
      const input = e.target && e.target.closest && e.target.closest('[role="combobox"]');
      if (!input) return;
      const lbId = input.getAttribute('aria-controls');
      const lb = lbId ? document.getElementById(lbId) : null;
      if (!lb) return;
      if (lb.querySelector('[role="option"]')) _comboOpen(input, lb);
    });
    document.addEventListener('click', (e) => {
      // Close any open combobox whose input + listbox the click missed.
      for (const input of document.querySelectorAll('[role="combobox"][aria-expanded="true"]')) {
        const lbId = input.getAttribute('aria-controls');
        const lb = lbId ? document.getElementById(lbId) : null;
        if (input.contains(e.target) || (lb && lb.contains(e.target))) continue;
        _comboClose(input, lb);
      }
    });

    // -------------------------------------------------------------
    // TreeView: clicking a [data-fui-tree-toggle] button toggles its
    // parent treeitem's aria-expanded. Pairs with the keyboard handler
    // below — both routes flip the same attribute so the visual chevron
    // rotation + the screen-reader announcement + the child <ul>
    // visibility all stay in sync.
    document.addEventListener('click', (e) => {
      const toggle = e.target && e.target.closest && e.target.closest('[data-fui-tree-toggle]');
      if (!toggle) return;
      const item = toggle.closest('[role="treeitem"]');
      if (!item) return;
      const current = item.getAttribute('aria-expanded');
      if (current === null) return; // leaf — nothing to toggle
      const next = current === 'true' ? 'false' : 'true';
      item.setAttribute('aria-expanded', next);
      // Show/hide the child group container.
      const group = item.querySelector(':scope > [role="group"]');
      if (group) {
        if (next === 'true') group.removeAttribute('hidden');
        else group.setAttribute('hidden', '');
      }
    });

    // -------------------------------------------------------------
    // TreeView keyboard nav (WAI-ARIA tree pattern).
    //
    // Listens at document level so RPC-injected children pick it up.
    //
    //   ArrowDown — focus next visible treeitem
    //   ArrowUp   — focus previous visible treeitem
    //   ArrowRight — if collapsed: expand; if expanded: focus first child
    //   ArrowLeft  — if expanded: collapse; if collapsed: focus parent
    //   Home / End — focus first / last visible treeitem
    //   Enter / Space — toggle expand; click the row's primary anchor
    //   Type-ahead — focus the next treeitem whose label starts with
    //                the typed prefix (800ms reset window)
    //
    // Expansion fires the existing data-fui-rpc on the treeitem's
    // toggle button so server-side lazy-loaded children come back via
    // the signal swap path. The runtime then sets aria-expanded=true,
    // un-hides the child <ul role="group">, and (if the child set
    // arrived via signal) the swap auto-loads any new comp CSS.
    // -------------------------------------------------------------
    const _treeRows = (tree) =>
      Array.from(tree.querySelectorAll('[role="treeitem"]'))
        .filter((n) => {
          // Skip hidden treeitems (collapsed branches).
          let cur = n.parentElement;
          while (cur && cur !== tree) {
            if (cur.hasAttribute && cur.hasAttribute('hidden')) return false;
            cur = cur.parentElement;
          }
          return true;
        });
    const _treeFocus = (tree, item) => {
      tree.querySelectorAll('[role="treeitem"][tabindex="0"]').forEach((n) => n.setAttribute('tabindex', '-1'));
      item.setAttribute('tabindex', '0');
      item.focus();
    };
    let _treeTypeBuf = '', _treeTypeAt = 0;
    document.addEventListener('keydown', (e) => {
      const item = e.target && e.target.closest && e.target.closest('[role="treeitem"]');
      if (!item) return;
      const tree = item.closest('[role="tree"]');
      if (!tree) return;
      const rows = _treeRows(tree);
      const idx = rows.indexOf(item);
      if (idx < 0) return;
      const move = (to) => {
        e.preventDefault();
        _treeFocus(tree, rows[Math.max(0, Math.min(rows.length - 1, to))]);
      };
      const expanded = item.getAttribute('aria-expanded');
      const isLeaf = expanded === null;
      switch (e.key) {
        case 'ArrowDown': return move(idx + 1);
        case 'ArrowUp':   return move(idx - 1);
        case 'Home':      return move(0);
        case 'End':       return move(rows.length - 1);
        case 'ArrowRight': {
          if (isLeaf) return;
          if (expanded === 'false') {
            e.preventDefault();
            const toggle = item.querySelector(':scope > .tree__row [data-fui-tree-toggle], :scope > [data-fui-tree-toggle]');
            if (toggle) toggle.click();
            else item.setAttribute('aria-expanded', 'true');
            return;
          }
          // Already expanded — focus first child.
          const firstChild = item.querySelector(':scope > [role="group"] > [role="treeitem"]');
          if (firstChild) { e.preventDefault(); _treeFocus(tree, firstChild); }
          return;
        }
        case 'ArrowLeft': {
          if (!isLeaf && expanded === 'true') {
            e.preventDefault();
            const toggle = item.querySelector(':scope > .tree__row [data-fui-tree-toggle], :scope > [data-fui-tree-toggle]');
            if (toggle) toggle.click();
            else item.setAttribute('aria-expanded', 'false');
            return;
          }
          // Already collapsed (or leaf) — focus parent treeitem.
          const parent = item.parentElement && item.parentElement.closest && item.parentElement.closest('[role="treeitem"]');
          if (parent) { e.preventDefault(); _treeFocus(tree, parent); }
          return;
        }
        case 'Enter':
        case ' ': {
          e.preventDefault();
          if (!isLeaf) {
            const toggle = item.querySelector(':scope > .tree__row [data-fui-tree-toggle], :scope > [data-fui-tree-toggle]');
            if (toggle) toggle.click();
            else item.setAttribute('aria-expanded', expanded === 'true' ? 'false' : 'true');
          } else {
            // Click the primary anchor / button inside the row.
            const link = item.querySelector(':scope > .tree__row a, :scope > .tree__row button, :scope > a, :scope > button');
            if (link) link.click();
          }
          return;
        }
      }
      if (e.key.length === 1 && !e.ctrlKey && !e.metaKey && !e.altKey) {
        const now = Date.now();
        if (now - _treeTypeAt > 800) _treeTypeBuf = '';
        _treeTypeAt = now;
        _treeTypeBuf += e.key.toLowerCase();
        for (let i = 1; i <= rows.length; i++) {
          const cand = rows[(idx + i) % rows.length];
          const label = (cand.textContent || '').trim().toLowerCase();
          if (label.startsWith(_treeTypeBuf)) {
            e.preventDefault();
            _treeFocus(tree, cand);
            return;
          }
        }
      }
    });

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
    connectSSE();
    _bootstrapComponentCSS();
    _wireInfiniteScroll(document);
  }

  window.G=window.__gofastr;
})();
