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
  // Exposed on the namespace so the widgets module (which reads
  // shortcut combos inside mountWidget) can share the same impl
  // without duplicating logic.
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
  // Hoist onto the namespace once it exists so widgets.js can use it.
  // The window.__gofastr = { ... } assignment below picks it up via
  // a property bag — see _parseCombo.

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
    // CSRF: forward the page's <meta name="csrf-token"> via the
    // X-CSRF-Token header. A JSON/multipart RPC body can't carry the
    // urlencoded `_csrf` field the auth.CSRF middleware parses, so the
    // header is the only channel that works for these requests. Mirrors
    // toggleaction.js / optimisticaction.js — see core-ui/ARCHITECTURE.md.
    const csrfMeta = document.querySelector('meta[name="csrf-token"]');
    if (csrfMeta) {
      const tok = csrfMeta.getAttribute('content');
      if (tok) headers['X-CSRF-Token'] = tok;
    }
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
      const r = await fetch(resolvedPath, { method, headers, body: body || undefined, signal: ctl.signal, credentials: 'same-origin' });
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
        // Await the toasts module. If it fails to load (deploy mid-
        // flight, CDN 5xx, network hiccup) we still need to show the
        // user something — a silently dropped "Save failed" toast is
        // a real bug.
        window.__gofastr._dispatchToastHeader(toastHeader);
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
  // Also handles kiln-emitted data-kiln-tool buttons + plain forms with
  // a relative `action` attribute; kiln-built pages rely on the same
  // generic dispatcher.
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
      // Kiln dispatch: data-kiln-tool buttons fire a /kiln/tool/<name>
      // POST with the data-kiln-args body. Scoped to kiln-rendered
      // pages (body.kiln-app) or any subtree explicitly opted in via
      // data-fui-trusted — otherwise stored-XSS inside user-content
      // could carry a data-kiln-tool attribute and CSRF as the
      // logged-in user.
      const legacy = e.target.closest('[data-kiln-tool]');
      if (legacy && (document.body.classList.contains('kiln-app') ||
                     legacy.closest('[data-fui-trusted]'))) {
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
      // Kiln dispatch: data-kiln-tool form submits. Scoped to
      // kiln-rendered pages (body.kiln-app) or data-fui-trusted
      // subtrees, same as the button delegator above.
      if (form.hasAttribute('data-kiln-tool') &&
          (document.body.classList.contains('kiln-app') ||
           form.closest('[data-fui-trusted]'))) {
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
      if (!action || action.match(/^https?:\/\//)) return;
      const enctype = (form.getAttribute('enctype') || '').toLowerCase();
      const wantsJSON = enctype === 'application/json';
      const explicitSPA = form.hasAttribute('data-fui-spa');
      // Safe-by-default: urlencoded / multipart / unspecified-enctype
      // forms submit the browser-native way (preserves Set-Cookie,
      // Location-follow, file uploads, default password-manager UX).
      // Opt INTO the SPA path with data-fui-spa or enctype="application/json"
      // when you actually want fetch-and-swap behavior.
      if (!wantsJSON && !explicitSPA) return;
      e.preventDefault();
      const wantsForm = enctype === 'application/x-www-form-urlencoded' ||
                        enctype === 'multipart/form-data';
      const fd = new FormData(form);
      let body, headers;
      if (wantsJSON) {
        const obj = {}; fd.forEach((v, k) => { obj[k] = v; });
        body = JSON.stringify(obj);
        headers = { 'Content-Type': 'application/json' };
      } else if (wantsForm) {
        if (enctype === 'multipart/form-data') {
          body = fd;
          headers = {}; // browser sets Content-Type with boundary
        } else {
          const params = new URLSearchParams();
          fd.forEach((v, k) => params.append(k, v));
          body = params;
          headers = { 'Content-Type': 'application/x-www-form-urlencoded' };
        }
      } else {
        // data-fui-spa with no enctype → default to urlencoded so
        // r.ParseForm() works on the server side.
        const params = new URLSearchParams();
        fd.forEach((v, k) => params.append(k, v));
        body = params;
        headers = { 'Content-Type': 'application/x-www-form-urlencoded' };
      }
      try {
        const resp = await fetch(action, {
          method: form.getAttribute('method') || 'POST',
          headers,
          body,
          redirect: 'follow',
          credentials: 'same-origin',
        });
        if (resp.redirected && resp.url) {
          // Hard navigation. (Previously this had a `typeof navigate
          // === 'function'` branch trying to use a free identifier
          // `navigate`, which never resolved — the SPA navigator is on
          // window.__gofastr.navigate. We explicitly do NOT call SPA
          // nav here: a form-intercept Location follow lands on a
          // server-rendered page that may not be in this app's SPA
          // route table, and the hard nav also rebuilds the SSE
          // connection cleanly. Documented behaviour.)
          window.location.assign(resp.url);
          return;
        }
      } catch (_) {}
    });

    // Debounced input-driven RPC: a form with
    // data-fui-rpc-trigger="input" fires its RPC each time an input
    // inside it changes, after a debounce window. Useful for
    // type-ahead search where the server is the source of truth for
    // filtered results (see core-ui/ARCHITECTURE.md — search is an
    // island state change, not a route).
    document.addEventListener('input', (e) => {
      // Open any focused combobox so typing makes the listbox visible
      // without requiring an ArrowDown press first. The RPC response
      // arrives after a debounce; we want the listbox open the moment
      // the first character lands, so the user sees options swap in.
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
  // The eager click delegator (installed below) awaits this readiness
  // Promise before calling openWidget. openWidget reads
  // _widgetCatalog[name] and silently bails if absent, so a click that
  // arrives before the catalog returns must wait for entries to be
  // populated. We set the Promise up immediately and stash the resolver
  // so the .then() of the catalog fetch (which runs after the namespace
  // is assigned further down) can settle it. Stash on the IIFE-local
  // bag below; the namespace assignment at __gofastr = { … } would
  // otherwise wipe direct assignments here.
  let _widgetCatalogResolve;
  const _widgetCatalogReady = new Promise((resolve) => { _widgetCatalogResolve = resolve; });

  fetch('/__gofastr/widgets?page=' + encodeURIComponent(location.pathname),
        { headers: { 'X-Gofastr-Widget-Discovery': '1' } })
    .then((r) => (r.ok ? r.json() : null))
    .then(async (list) => {
      if (!Array.isArray(list)) return;
      // The widget runtime now ships as a split module. Make sure it's
      // loaded before iterating mounts — covers the case where no
      // [data-fui-widget] marker is present in initial HTML (the
      // marker scanner wouldn't have fired) but server-side
      // registration says there are widgets to mount.
      if (list.length > 0) {
        try { await window.__gofastr.loadModule('widgets'); } catch (_) {}
      }
      const tryMount = () => {
        if (!window.__gofastr || !window.__gofastr.mountWidget) {
          setTimeout(tryMount, 0);
          return;
        }
        // Stash every widget's payload so openWidget can retrieve a
        // hidden one on demand. Also resolve _widgetCatalogReadyResolve
        // so the eager click delegator can proceed.
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

        // Eager click delegator (installed at boot, see below) is
        // awaiting this Promise — resolve so queued clicks unblock now
        // that the catalog is populated.
        _widgetCatalogResolve();
      };
      tryMount();
    })
    .catch(() => { _widgetCatalogResolve(); });

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
    /** Reject dangerous schemes when a signal value is about to be
        written into a URL-bearing HTML attribute (href / src / action
        / xlink:href / formaction). Returns true when the value MUST
        be discarded. Allows http(s), mailto, tel, relative paths,
        same-page anchors, and data:image/* (used for inline blob
        previews). Rejects javascript:, vbscript:, and other data:
        payloads.

        This is the runtime-side guard against signal-bound `href` on
        Lightbox AllowDownload + any other widget that mirrors an
        attacker-controllable signal into a click-triggered attribute.
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
      const trimmed = String(value || '').replace(/[\s -]+/g, '').toLowerCase();
      if (trimmed.startsWith('javascript:')) return true;
      if (trimmed.startsWith('vbscript:')) return true;
      if (trimmed.startsWith('data:')) {
        // Allow data:image/* only; everything else (data:text/html,
        // data:application/javascript, etc.) is rejected. NOTE: this
        // intentionally allows data:image/svg+xml — an SVG in an <img>
        // src (the only sink signal-bound `src`/`href` reaches here)
        // renders inertly and does NOT execute its scripts. SVG only runs
        // script when loaded as a *document* (iframe/object/navigation),
        // which is not a signal-URL sink. See AI_TEST_AUDIT.md (pass 3).
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

    // --- Router API ---

    /** Programmatically navigate to a path */
    navigate(path, { replace = false } = {}) {
      if (path === currentPath) return;
      // Security: reject attacker-controllable schemes BEFORE
      // touching the URL bar. Server-rendered data-fui-push-state
      // attributes (e.g. on a combobox option) and signal-bound
      // hrefs are the trust boundary; navigate() is the choke point
      // for all programmatic SPA navigation, so the guard lives
      // here. Reuses the same gate as Lightbox AllowDownload etc.
      if (this._isUnsafeSignalUrl('href', path)) {
        console.warn('[gofastr] navigate refused unsafe URL:', path);
        return;
      }
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
          window.__gofastr._toastOrFallback(result.message);
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

    /** parseCombo helper used by the widgets module's keyboard-shortcut
        scanners (data-fui-shortcut-click, data-fui-shortcut-focus). */
    _parseCombo: parseCombo,

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
          // The html escape hatch is for TRUSTED HTML *strings* only.
          // Non-string values (e.g. the auto-built dispatchRPC error
          // object {ok:false,status,text}) carry untrusted server-error
          // text; JSON.stringify does NOT HTML-escape, so routing it
          // through innerHTML would execute reflected markup. Render
          // non-strings as text (mirrors text-mode below).
          if (typeof value === 'string') {
            node.innerHTML = value;
          } else {
            node.textContent = (value == null) ? '' : JSON.stringify(value);
          }
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
          let v = String(value ?? '');
          // URL-bearing attrs (href / src / action / xlink:href /
          // formaction): reject dangerous schemes (javascript:,
          // vbscript:, data: except data:image/*). Stops a signal-
          // driven anchor (e.g. Lightbox AllowDownload) from
          // executing arbitrary JS when an attacker controls the
          // signal value via a query-string deeplink param.
          if (window.__gofastr._isUnsafeSignalUrl(attr, v)) v = '';
          node.setAttribute(attr, v);
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
    // the inline renderer if the toasts module isn't available.
    _toastOrFallback(cfg) {
      this.loadModule('toasts')
        .then(() => { try { this.toast(cfg); } catch (_) {} })
        .catch(() => { try { this._fallbackToast(cfg); } catch (_) {} });
    },

    // _fallbackToast renders an unstyled-but-visible toast notice when
    // the toasts module can't load. No TTL, no animation, no hover
    // pause — just a labelled live region the user can read and
    // dismiss with the × button. Uses textContent throughout (no
    // innerHTML) so a malicious title can't inject script.
    _fallbackToast(cfg) {
      if (!cfg || !cfg.title) return null;
      let container = document.querySelector('[data-fui-toast-fallback]');
      if (!container) {
        container = document.createElement('div');
        container.setAttribute('data-fui-toast-fallback', '');
        container.setAttribute('role', 'region');
        container.setAttribute('aria-label', 'Notifications (degraded)');
        container.style.cssText = 'position:fixed;top:1rem;right:1rem;z-index:2147483600;display:grid;gap:0.5rem;max-width:min(360px,calc(100vw - 2rem));pointer-events:auto;';
        document.body.appendChild(container);
      }
      const variant = cfg.variant || 'info';
      const isAssertive = variant === 'warning' || variant === 'danger';
      const item = document.createElement('div');
      item.setAttribute('role', isAssertive ? 'alert' : 'status');
      item.setAttribute('aria-live', isAssertive ? 'assertive' : 'polite');
      item.style.cssText = 'background:#1f2937;color:#fff;padding:0.75rem 1rem;border-radius:6px;font-family:system-ui,sans-serif;font-size:0.9rem;box-shadow:0 4px 12px rgba(0,0,0,0.2);display:flex;gap:0.75rem;align-items:flex-start;';
      const text = document.createElement('div');
      text.style.cssText = 'flex:1;';
      const title = document.createElement('strong');
      title.style.cssText = 'display:block;';
      title.textContent = cfg.title;
      text.appendChild(title);
      if (cfg.body) {
        const body = document.createElement('div');
        body.style.cssText = 'margin-top:0.25rem;opacity:0.9;';
        body.textContent = cfg.body;
        text.appendChild(body);
      }
      const dismiss = document.createElement('button');
      dismiss.type = 'button';
      dismiss.setAttribute('aria-label', 'Dismiss notification');
      dismiss.style.cssText = 'background:none;border:0;color:inherit;font-size:1.2rem;cursor:pointer;line-height:1;padding:0 0.25rem;';
      dismiss.textContent = '×';
      dismiss.addEventListener('click', () => { item.remove(); });
      item.appendChild(text);
      item.appendChild(dismiss);
      container.appendChild(item);
      return item;
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
        // Screen group optimization: if both paths share a screen group,
        // only swap the inner content, preserving the layout shell.
        const groupEl = findCommonScreenGroup(prevPath || currentPath, path);
        if (groupEl) {
          swapScreenGroupContent(groupEl, cached.html);
        } else {
          swapMainContent(cached.html);
        }
        updateActiveLink(path);
        window.scrollTo(0, 0);
        window.dispatchEvent(new CustomEvent('gofastr:navigate', { detail: { path, prevPath, cached: true } }));
        return;
      }

      const resp = await fetch(path, {
        headers: { 'X-Gofastr-Navigate': '1' },
      });
      if (!resp.ok) throw new Error(`HTTP ${resp.status}`);

      // X-Gofastr-Location signals "server policy redirected this
      // partial — go nav to the new URL instead of trying to swap
      // the empty body in place." Set by uihost on a Redirect policy
      // outcome. The fetch above won't see a 303 (we deliberately use
      // 200 + header to survive redirect:'follow').
      const redirectTo = resp.headers.get('X-Gofastr-Location');
      if (redirectTo) {
        // pushState was already called by the click handler with the
        // requested path; replace it with the redirect destination so
        // the URL bar matches what we're about to load.
        try { history.replaceState(null, '', redirectTo); } catch (_) {}
        currentPath = redirectTo;
        _pendingNav.delete(path);
        document.documentElement.removeAttribute('aria-busy');
        return loadPage(redirectTo);
      }

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
      // Screen group optimization: preserve layout shell for sibling nav.
      const groupEl = findCommonScreenGroup(prevPath || currentPath, path);
      if (groupEl) {
        swapScreenGroupContent(groupEl, body);
      } else {
        swapMainContent(body);
      }
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

  // --- Screen group awareness ---
  // When navigating between siblings inside the same data-fui-screen-group,
  // only swap the group's inner <main> content, preserving the layout shell.
  const findCommonScreenGroup = (fromPath, toPath) => {
    const groups = document.querySelectorAll('[data-fui-screen-group]');
    // Pick the DEEPEST matching group — for nested screen groups the
    // inner group's layout shell is what should survive sibling-nav,
    // not the outer one. We compare by prefix length: longer prefix
    // → more specific → wins.
    let best = null;
    let bestLen = -1;
    for (const g of groups) {
      const prefix = g.getAttribute('data-fui-screen-group');
      if (prefix && fromPath.startsWith(prefix) && toPath.startsWith(prefix)) {
        if (prefix.length > bestLen) {
          best = g;
          bestLen = prefix.length;
        }
      }
    }
    return best;
  };

  const swapScreenGroupContent = (groupEl, html) => {
    const main = groupEl.querySelector('[role="main"]') ?? groupEl.querySelector('main');
    if (main) {
      main.innerHTML = html;
      if (window.__gofastr?.scanAndLoadCSS) window.__gofastr.scanAndLoadCSS(main);
    }
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
      const target = e.target.closest(`[data-action-type="${eventType}"], [data-action-${eventType}]`);
      if (!target) return;

      const action = target.getAttribute(`data-action-${eventType}`) || target.getAttribute('data-action');
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
      // Demand-load split runtime modules whose marker attributes show
      // up in injected subtrees (RPC innerHTML replacement, signal
      // swaps, island updates). Without this, dynamically-inserted
      // fileupload zones / popover triggers / toast stacks would never
      // load their module and behave as dead DOM.
      _scanForModules(node);
      // And re-run scanners of modules that ARE loaded so they wire
      // any newly-inserted elements (toast TTL, fileupload drop zones).
      const G = window.__gofastr;
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

  // === EAGER WIDGET DELEGATORS =========================================
  // The data-fui-open click handler, data-fui-toast click handler, and
  // popstate listener used to live inside the /__gofastr/widgets
  // catalog fetch's .then() callback. That meant on a slow network the
  // very first click on an open trigger had no handler to receive it
  // — the catalog hadn't returned yet, so the .then() hadn't run.
  //
  // We install them here at boot, before the catalog fetch. Each
  // handler awaits loadModule('widgets') (via the openWidget stub on
  // __gofastr) so it works regardless of whether the catalog has
  // resolved. Idempotent via document.__fuiOpenDispatch.
  function _installEagerWidgetDelegators() {
    if (document.__fuiOpenDispatch) return;
    document.__fuiOpenDispatch = true;
    document.addEventListener('click', (e) => {
      // Toast trigger: data-fui-toast='<json>' fires a client toast.
      const toastBtn = e.target.closest && e.target.closest('[data-fui-toast]');
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
      const btn = e.target.closest && e.target.closest('[data-fui-open]');
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
      (async () => {
        // The widgets module + catalog must both be ready before
        // openWidget can find the entry. Awaiting both here keeps the
        // click responsive even on a cold-cache page where the user
        // clicked faster than /__gofastr/widgets returned.
        await window.__gofastr.loadModule('widgets').catch(() => {});
        await _widgetCatalogReady;
        await window.__gofastr.openWidget(name, { params: overrides, pushUrl: true });
        if (anchorPref !== null) {
          await window.__gofastr.loadModule('popover');
          window.__gofastr._anchorPopover(name, btn, anchorPref || 'bottom');
        }
      })();
    });
  }
  _installEagerWidgetDelegators();

  // === DRAG-TO-DISMISS (bottom-sheet style) ============================
  // Pointer-driven drag-to-close for widgets whose Definition opts in
  // via DragDismiss (data-fui-drag-dismiss="true" on the widget root,
  // data-fui-drag-handle="true" on the visible handle bar). Drag is
  // only initiated from the handle so taps inside the panel content
  // (scrolling, form input) don't accidentally dismiss the sheet.
  //
  // Thresholds: close on >80px downward distance OR >0.5px/ms downward
  // velocity. Snap back otherwise. data-fui-dragging is mirrored onto
  // the widget root while the gesture is active (CSS suppresses entrance
  // animation and transitions so the live transform isn't fought).
  function _installDragDismiss() {
    if (document.__fuiDragDismissDispatch) return;
    document.__fuiDragDismissDispatch = true;
    const DISTANCE_THRESHOLD = 80;
    const VELOCITY_THRESHOLD = 0.5; // px per ms
    let active = null;
    document.addEventListener('pointerdown', (e) => {
      if (active) return;
      const handle = e.target && e.target.closest && e.target.closest('[data-fui-drag-handle="true"]');
      if (!handle) return;
      const widget = handle.closest('[data-fui-drag-dismiss="true"]');
      if (!widget) return;
      const name = widget.getAttribute('data-fui-widget') || '';
      // Only primary pointer (left mouse / single touch).
      if (e.button !== undefined && e.button > 0) return;
      active = {
        widget, name, pointerId: e.pointerId,
        startY: e.clientY, startTime: Date.now(),
        lastY: e.clientY, lastTime: Date.now(),
      };
      widget.setAttribute('data-fui-dragging', 'true');
      try { widget.setPointerCapture(e.pointerId); } catch (_) {}
    }, true);
    document.addEventListener('pointermove', (e) => {
      if (!active || e.pointerId !== active.pointerId) return;
      const dy = Math.max(0, e.clientY - active.startY);
      active.widget.style.transform = 'translateY(' + dy + 'px)';
      active.lastY = e.clientY;
      active.lastTime = Date.now();
    }, true);
    function finishDrag(close) {
      const w = active.widget;
      const name = active.name;
      try { w.releasePointerCapture(active.pointerId); } catch (_) {}
      w.removeAttribute('data-fui-dragging');
      if (close && name) {
        const G = window.__gofastr;
        if (G && typeof G.closeWidget === 'function') {
          try { G.closeWidget(name); } catch (_) {}
        }
        // Clear transform AFTER close so the panel doesn't briefly
        // snap back before unmount.
        setTimeout(() => { try { w.style.transform = ''; } catch (_) {} }, 0);
      } else {
        // Snap back to the resting position.
        w.style.transform = '';
      }
      active = null;
    }
    document.addEventListener('pointerup', (e) => {
      if (!active || e.pointerId !== active.pointerId) return;
      const dy = Math.max(0, e.clientY - active.startY);
      const dt = Math.max(1, Date.now() - active.startTime);
      const velocity = dy / dt;
      const shouldClose = dy > DISTANCE_THRESHOLD || velocity > VELOCITY_THRESHOLD;
      finishDrag(shouldClose);
    }, true);
    document.addEventListener('pointercancel', (e) => {
      if (!active || e.pointerId !== active.pointerId) return;
      finishDrag(false);
    }, true);
  }
  _installDragDismiss();

  if (!window.__fuiDeepLinkPopstate) {
    window.__fuiDeepLinkPopstate = true;
    window.addEventListener('popstate', () => {
      setTimeout(() => {
        const G = window.__gofastr;
        if (G && typeof G._syncDeepLinks === 'function') G._syncDeepLinks();
      }, 0);
    });
  }

  // === DEMAND-LOAD SCANNERS ===========================================
  // Each split module has a marker attribute that, when found in the
  // DOM, triggers a load. Scanners run after DOMContentLoaded + after
  // every SPA-nav swap (`gofastr:navigate`) + when the MutationObserver
  // sees newly inserted DOM.
  const _moduleMarkers = [
    // Copy-to-clipboard delegated handler. Loaded when any
    // [data-fui-copy-text-from] button is on the page (or arrives via
    // SPA-nav). The src/copy.js module installs a single document-level
    // listener that handles every button.
    { name: 'copy',       selector: '[data-fui-copy-text-from]' },
    { name: 'fileupload', selector: '[data-fui-fileupload]' },
    { name: 'popover',    selector: '[data-fui-popover-anchor]' },
    { name: 'menu',       selector: '[data-fui-menu]' },
    { name: 'toasts',     selector: '[data-fui-toast-stack],[data-fui-toast]' },
    // SSE: background event stream. Idle-loaded — never blocks first
    // interaction; the channel only carries push updates, not user
    // actions. See ROADMAP §8 Phase 5.
    { name: 'sse',        selector: 'meta[name="gofastr-sse"]', idle: true },
    // Widgets: any SSR-inlined widget element or any data-fui-open
    // trigger button anywhere on the page. The catalog auto-mount
    // path explicitly awaits loadModule('widgets') too, so this
    // scanner just covers the marker-on-page path. Idle-loaded —
    // SSR-inlined widget chrome is already on the page; mounting is
    // hydration not first paint. See ROADMAP §8 Phase 5.
    { name: 'widgets',    selector: '[data-fui-widget],[data-fui-open]', idle: true },
    // Combobox: any WAI-ARIA combobox + listbox pair. The module
    // handles keyboard nav, click-to-pick, outside-click close, and
    // updates aria-expanded + aria-activedescendant.
    { name: 'combobox',   selector: '[role="combobox"]' },
    // Tree: any WAI-ARIA tree. The module handles roving tabindex,
    // arrow-key nav, type-ahead, and toggle clicks that flip
    // aria-expanded + show/hide child <ul role="group">.
    { name: 'tree',       selector: '[role="tree"]' },
    // InfiniteScroll: wrappers with the marker attribute. The module
    // attaches an IntersectionObserver to each [data-fui-infinite-
    // sentinel] inside and POSTs to data-fui-infinite-scroll.
    { name: 'infinitescroll', selector: '[data-fui-infinite-scroll]' },
    // Banner: dismissible inline-alert support. The module runs the
    // localStorage-backed hide pass for already-dismissed banners and
    // wires the delegated click handler for the X button.
    { name: 'banner',         selector: '[data-fui-banner-dismiss]' },
    // Slider: mirrors <input type="range"> value into the associated
    // <output> on input events. Loaded only when ShowValue=true (the
    // mirror marker is on the input then).
    { name: 'slider',         selector: '[data-fui-slider-mirror]' },
    // NumberInput: wires the +/- step buttons of framework/ui.NumberInput
    // to the associated <input type="number">.
    { name: 'numberinput',    selector: '[data-fui-number-step]' },
    // TextArea autogrow: applies the same auto-resize handler the
    // widget runtime uses for textareas anywhere on the page.
    { name: 'textarea',       selector: 'textarea[data-fui-autogrow]' },
    // MultiSelect: chip rendering for checked options + chip removal.
    { name: 'multiselect',    selector: '[data-fui-multiselect-chips]' },
    // FileDropzone: filename display + optional image preview strip.
    { name: 'dropzone',       selector: '[data-fui-comp="ui-dropzone"]' },
    // RangeSlider: cross-clamp min/max thumbs + optional value mirror.
    { name: 'rangeslider',    selector: 'input[data-fui-range-slider]' },
    // TagInput: commit on Enter/comma, backspace removes last, chip ×.
    { name: 'taginput',       selector: '[data-fui-tag-input]' },
    // AnimatedCounter: IntersectionObserver-driven tick on first view.
    { name: 'animatedcounter', selector: '[data-fui-animated-counter]' },
    // TableOfContents: harvest h2/h3 from target region + active-section tracking.
    { name: 'toc',             selector: '[data-fui-toc]' },
    // ScrollSpy: generic IntersectionObserver section tracking for any nav with in-page anchors.
    { name: 'scrollspy',       selector: '[data-fui-scrollspy]' },
    // OptimisticAction: SSR-declared success state flips on click, RPC fires underneath, rolls back on non-2xx.
    { name: 'optimisticaction', selector: '[data-fui-comp="ui-optimistic-action"]' },
    // ToggleAction: three-state mutex toggle (idle ↔ committed with optional untoggle, mutually exclusive within data-toggle-group).
    { name: 'toggleaction', selector: '[data-fui-comp="ui-toggle-action"]' },
    // NetworkRetryBanner: persistent banner gated by RPC-failure threshold / SSE silence. Health-check retry.
    { name: 'networkretrybanner', selector: '[data-fui-comp="ui-network-retry-banner"]' },
    // SortableList: HTML5 drag + keyboard reorder. POSTs new order on commit.
    { name: 'sortablelist',    selector: '[data-fui-sortable]' },
    // Shortcut: page-level (non-widget) data-fui-shortcut-focus +
    // data-fui-shortcut-click bindings.
    { name: 'shortcut',        selector: '[data-fui-shortcut-focus],[data-fui-shortcut-click]' },
    // Lightbox: arrow-nav across gallery siblings + image preloading.
    { name: 'lightbox',        selector: '[data-fui-comp="ui-lightbox"][data-fui-lightbox]' },
    // Carousel: prev/next, dots, ArrowLeft/Right, optional AutoRotate.
    { name: 'carousel',        selector: '[data-fui-carousel]' },
    // ThemeToggle: dark/light/auto cycle button + pill sync.
    { name: 'themeswitch',     selector: '[data-fui-theme-toggle]' },
    // BackToTop: scroll-past-threshold reveal + smooth scroll.
    { name: 'backtotop',       selector: '[data-fui-back-to-top]' },
    // ConditionalField: show/hide content based on another field's value.
    { name: 'conditionalfield', selector: '[data-fui-comp="ui-conditional-field"]' },
    // PasswordInput: show/hide toggle for password fields.
    { name: 'passwordinput',   selector: '[data-fui-comp="ui-password-input"]' },
    // SearchInput: clear button visibility + input clearing.
    { name: 'searchinput',     selector: '[data-fui-comp="ui-search-input"]' },
    // FormRepeater: serializes field values into RPC add/remove clicks.
    { name: 'formrepeater',    selector: '[data-fui-comp="ui-form-repeater"]' },
  ];
  function _scanForModules(root) {
    const scope = root && root.querySelectorAll ? root : document;
    const idleQueue = [];
    for (const m of _moduleMarkers) {
      // Skip if the module is already loaded — its own internal scanner
      // takes care of newly inserted DOM via the MutationObserver.
      if (window.__gofastr.loadedModules && window.__gofastr.loadedModules[m.name]) continue;
      if (!scope.querySelector(m.selector)) continue;
      if (m.idle) {
        idleQueue.push(m.name);
      } else {
        loadModule(m.name).catch(() => {});
      }
    }
    if (idleQueue.length) _scheduleIdleModules(idleQueue);
  }
  // Phase 5 idle fallback (ROADMAP §8). Modules tagged `idle: true` in
  // `_moduleMarkers` ship after FCP via requestIdleCallback so they
  // never compete with the user's first interaction. Safari < 16.2 and
  // Firefox < 55 lack rIC — fall back to setTimeout(0) which still
  // runs after the current task settles.
  function _scheduleIdleModules(names) {
    const rIC = window.requestIdleCallback || ((fn) => setTimeout(fn, 0));
    rIC(() => {
      for (const n of names) loadModule(n).catch(() => {});
    });
  }
  // Re-scan after SPA-nav swaps content. Two phases:
  //
  //  1. Marker scan — modules that AREN'T loaded yet get fetched when
  //     their marker appears in the freshly-swapped content. (Fresh
  //     page brings new feature → load on demand.)
  //
  //  2. Per-module rescan — modules that ARE loaded re-run their
  //     scanner against the new DOM. Modules opt in by registering
  //     a function on `window.__gofastr._moduleScanners[name]`; the
  //     contract is "wire any new elements inside `root`, idempotent
  //     against already-wired elements". This is how SSR-inlined
  //     toast stacks on the new page get their TTL timers armed —
  //     without it, `_initToasts` would have run only once at module
  //     load before that DOM existed.
  window.addEventListener('gofastr:navigate', () => {
    _scanForModules(document);
    const G = window.__gofastr;
    if (G && G._moduleScanners) {
      for (const name in G._moduleScanners) {
        if (G.loadedModules && G.loadedModules[name]) {
          try { G._moduleScanners[name](document); } catch (_) {}
        }
      }
    }
  });

  // Close any open modal widgets on SPA navigation. Toasts/panels
  // (non-backdrop'd widgets) survive — they're page-independent
  // UI like build-progress banners.
  window.addEventListener('gofastr:navigate', () => {
    const G = window.__gofastr;
    if (!G || !G._modalStack) return;
    for (const name of [...G._modalStack]) G.closeWidget(name);
  });

  // Re-fetch the widget catalog after SPA-nav so page-scoped widgets
  // registered with .Pages("/route") become available when the user
  // arrives via partial-fetch (instead of a full page load).
  //
  // Without this, the boot-time catalog only contains widgets visible
  // on the initial path; clicking a data-fui-open trigger for a
  // page-scoped widget elsewhere silently bails because the entry is
  // missing from _widgetCatalog.
  //
  // The fetch is idempotent — entries are MERGED into the catalog
  // (existing entries from boot don't get overwritten unless the
  // server returns a changed version). Non-hidden widgets that
  // aren't already mounted are mounted now. Then _syncDeepLinks runs
  // so the URL's modal/drawer query params open the right surface.
  window.addEventListener('gofastr:navigate', (e) => {
    const path = (e && e.detail && e.detail.path) || location.pathname;
    fetch('/__gofastr/widgets?page=' + encodeURIComponent(path),
          { headers: { 'X-Gofastr-Widget-Discovery': '1' } })
      .then((r) => (r.ok ? r.json() : null))
      .then(async (list) => {
        if (!Array.isArray(list) || list.length === 0) return;
        const G = window.__gofastr;
        if (!G) return;
        // Make sure the widgets module is loaded — the initial page
        // may have had no widgets, so loadModule('widgets') was never
        // triggered and mountWidget isn't on the namespace yet.
        try { await G.loadModule('widgets'); } catch (_) { return; }
        G._widgetCatalog = G._widgetCatalog || {};
        G._widgetDeepLinks = G._widgetDeepLinks || {};
        for (const item of list) {
          const cfg = item.cfg;
          const prev = G._widgetCatalog[cfg.name];
          G._widgetCatalog[cfg.name] = item;
          if (cfg.deepLinkKey && cfg.deepLinkValue && !prev) {
            const idx = G._widgetDeepLinks;
            (idx[cfg.deepLinkKey] = idx[cfg.deepLinkKey] || []).push({
              value: cfg.deepLinkValue,
              name: cfg.name,
              params: cfg.deepLinkParams || [],
            });
          }
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

  // Mirror details.open → summary aria-expanded for screen readers.
  // Native <summary> reports as "button" without an expanded state.
  // Helper hoisted out of any branch so both the initial-pass and
  // the toggle-listener forms can use it.
  const _mirrorDisclosure = (d) => {
    const s = d.querySelector(':scope > summary');
    if (s) s.setAttribute('aria-expanded', d.open ? 'true' : 'false');
  };

  // Event listeners attach unconditionally — they fire only when
  // the matching event happens, so installing them before DOM is
  // parsed is safe. The previous arrangement gated these inside
  // `if (document.readyState === 'loading')`, which silently
  // disabled them when runtime.js loaded after DOMContentLoaded
  // (late injection, fast parse, dynamic re-init). Esc-to-close,
  // aria-expanded mirroring, and menu-focus-on-open are all
  // load-bearing for keyboard + AT users; they must run regardless
  // of script-load timing.

  // Focus trap via `inert`: when a disclosure opts in with
  // data-fui-disclosure-trap (used for mobile drawer / full-sheet
  // popovers), set `inert` on every body child that's NOT the
  // ancestor chain of the drawer. Tab walking is naturally confined
  // because inert removes elements from the focus order + the AT
  // tree. Cleared on close, so the rest of the page returns to life.
  //
  // _inertNeighbors records what we toggled so removal is exact —
  // we can't just "remove inert from everything" because some
  // hosts ship their own inert state.
  const _inertNeighbors = new WeakMap();
  const _applyDisclosureTrap = (d, open) => {
    if (open) {
      // Find body-level ancestor of d; we make every OTHER body
      // child inert.
      let bodyChild = d;
      while (bodyChild.parentElement && bodyChild.parentElement !== document.body) {
        bodyChild = bodyChild.parentElement;
      }
      if (bodyChild.parentElement !== document.body) return; // not in body
      const made = [];
      for (const sib of document.body.children) {
        if (sib === bodyChild) continue;
        if (sib.hasAttribute('inert')) continue; // don't touch existing
        sib.setAttribute('inert', '');
        made.push(sib);
      }
      _inertNeighbors.set(d, made);
    } else {
      const made = _inertNeighbors.get(d);
      if (!made) return;
      for (const sib of made) sib.removeAttribute('inert');
      _inertNeighbors.delete(d);
    }
  };

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
      // Focus-trap opt-in: confine focus to the drawer subtree via
      // `inert` on siblings. Wired both ways so the trap clears on
      // close (including auto-close on SPA nav).
      if (d.hasAttribute('data-fui-disclosure-trap')) {
        _applyDisclosureTrap(d, d.open);
      }
    }
  }, true); // capture phase — toggle doesn't bubble

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

  // Initial-pass hooks: these scan the CURRENT DOM, so they have
  // to wait until the document is at least parsed. updateActiveLink
  // marks server-rendered nav links; _bootstrapComponentCSS scans
  // existing markers; _scanForModules dispatches demand-load
  // modules; the disclosure pass syncs aria-expanded on every
  // server-rendered <details data-fui-disclosure>.
  const _initialPass = () => {
    updateActiveLink(location.pathname);
    _bootstrapComponentCSS();
    _scanForModules(document);
    for (const d of document.querySelectorAll('details[data-fui-disclosure]')) {
      _mirrorDisclosure(d);
    }
  };
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', _initialPass);
  } else {
    _initialPass();
  }

  window.G=window.__gofastr;
})();
