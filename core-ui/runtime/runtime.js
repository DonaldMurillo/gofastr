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

  // -----------------------------------------------------------------------
  // Router: known routes from screen registration
  // -----------------------------------------------------------------------
  const routes = new Map(); // path → { title, preload }
  let currentPath = location.pathname;

  const registerRoutes = (routeList) => {
    if (!Array.isArray(routeList)) return;
    for (const r of routeList) {
      routes.set(r.path ?? r.Path, {
        title: r.title ?? r.Title ?? '',
        preload: r.preload ?? r.Preload ?? false,
      });
    }
  };

  // Bootstrap routes from injected data
  if (Array.isArray(window.__gofastr_routes)) {
    registerRoutes(window.__gofastr_routes);
  }

  // -----------------------------------------------------------------------
  // Screen cache — stores rendered screens for instant back-navigation.
  // -----------------------------------------------------------------------
  const screenCache = new Map(); // path → { html, title, timestamp }
  const MAX_CACHE_SIZE = 20;

  const cacheScreen = (path, html, title) => {
    if (screenCache.size >= MAX_CACHE_SIZE) {
      const oldest = screenCache.keys().next().value;
      screenCache.delete(oldest);
    }
    screenCache.set(path, { html, title, timestamp: Date.now() });
  };

  // Cache the initial page so back-navigation to it works instantly
  const initialMain = document.querySelector('[role="main"]') ?? document.querySelector('main');
  if (initialMain) {
    screenCache.set(location.pathname, {
      html: initialMain.innerHTML,
      title: document.title,
      timestamp: Date.now(),
    });
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

    /** Show a toast notification */
    toast(msg) {
      document.querySelector('.gofastr-toast')?.remove();
      const t = document.createElement('div');
      t.className = 'gofastr-toast';
      t.setAttribute('role', 'status');
      t.setAttribute('aria-live', 'polite');
      t.textContent = msg;
      t.style.cssText = 'position:fixed;bottom:24px;right:24px;background:#10B981;color:white;padding:12px 24px;border-radius:8px;font-weight:600;box-shadow:0 4px 12px rgba(0,0,0,0.15);z-index:9999;transition:opacity 0.3s;font-family:system-ui,sans-serif;';
      document.body.appendChild(t);
      setTimeout(() => {
        t.style.opacity = '0';
        setTimeout(() => t.remove(), 300);
      }, 2000);
    },

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

    /** Load CSS for a screen path if not already loaded */
    loadCSS(screenPath) {
      const makeId = (p) => 'gofastr-css-' + p.replace(/[^a-zA-Z0-9]/g, '-');
      // Build parent paths (closest first, then up to root)
      const parents = [];
      let p = screenPath;
      while (p !== '/' && p.includes('/')) {
        p = p.substring(0, p.lastIndexOf('/')) || '/';
        parents.push(p);
      }
      // Try parent paths first (they have registered CSS), skip dynamic sub-routes
      for (const path of parents) {
        const linkId = makeId(path);
        if (document.getElementById(linkId)) return;
      }
      // Load only the closest parent that isn't already loaded
      for (const path of parents) {
        const linkId = makeId(path);
        const link = document.createElement('link');
        link.id = linkId;
        link.rel = 'stylesheet';
        link.href = '/__gofastr/css' + path;
        link.onerror = () => link.remove();
        document.head.appendChild(link);
        return;
      }
      // If screenPath itself is a root or known route, load it
      if (screenPath === '/' || parents.length === 0) {
        const linkId = makeId(screenPath);
        if (!document.getElementById(linkId)) {
          const link = document.createElement('link');
          link.id = linkId;
          link.rel = 'stylesheet';
          link.href = '/__gofastr/css' + screenPath;
          document.head.appendChild(link);
        }
      }
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
        } else if (mode === 'attr') {
          const attr = node.getAttribute('data-fui-signal-attr') || 'value';
          node.setAttribute(attr, String(value ?? ''));
        } else {
          if (value == null) node.textContent = '';
          else if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') node.textContent = String(value);
          else node.textContent = JSON.stringify(value);
        }
      });
    },

    /** Read the current value of a named signal. */
    signal(name) {
      return this._signals[name]?.value;
    },

    /** Mount a widget. cfg comes from the per-widget bootstrap;
        chromeHTML is the host-rendered slot DOM as an HTML string.
        Idempotent — a widget already mounted (same cfg.name) is a
        no-op. */
    mountWidget(cfg, chromeHTML) {
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

      // Backdrop + chrome
      let backdrop = null;
      if (cfg.backdrop) {
        backdrop = document.createElement('div');
        backdrop.className = 'fui-backdrop';
        backdrop.setAttribute('data-fui-backdrop', cfg.name);
        document.body.appendChild(backdrop);
      }
      const tmp = document.createElement('div');
      tmp.innerHTML = chromeHTML;
      const widgetEl = tmp.firstElementChild;
      document.body.appendChild(widgetEl);
      NS._widgets[cfg.name].root = widgetEl;
      NS._widgets[cfg.name].backdrop = backdrop;

      function dismiss() {
        if (widgetEl?.parentNode) widgetEl.parentNode.removeChild(widgetEl);
        if (backdrop?.parentNode) backdrop.parentNode.removeChild(backdrop);
        delete NS._widgets[cfg.name];
      }
      NS._widgets[cfg.name].dismiss = dismiss;

      // Initial state hydration
      fetch(cfg.statePath, { headers: { 'X-FUI-Widget': cfg.name } })
        .then((r) => (r.ok ? r.json() : {}))
        .then((state) => {
          for (const k in state) NS.setSignal(k, state[k]);
        })
        .catch(() => {});

      // SSE bindings
      const seenStreams = {};
      for (const b of cfg.sse || []) {
        if (!seenStreams[b.path]) {
          try {
            const es = new EventSource(b.path);
            seenStreams[b.path] = es;
            es.addEventListener('open', () => { window.__fuiSSEReady = true; });
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
        let body = node.getAttribute('data-fui-rpc-body');
        if (!body && node.tagName === 'FORM') {
          const fd = new FormData(node);
          const obj = {}; fd.forEach((v, k) => { obj[k] = v; });
          body = JSON.stringify(obj);
        }
        const headers = { 'X-FUI-Widget': cfg.name };
        if (body) headers['Content-Type'] = 'application/json';
        if (node.tagName === 'BUTTON' || node.tagName === 'INPUT') node.disabled = true;
        try {
          const r = await fetch(path, { method, headers, body: body || undefined });
          if (!r.ok) {
            const txt = await r.text();
            if (responseSignal) NS.setSignal(responseSignal, { ok: false, status: r.status, text: txt });
            return;
          }
          const ct = r.headers.get('content-type') || '';
          const data = ct.indexOf('application/json') >= 0 ? await r.json() : await r.text();
          if (responseSignal) NS.setSignal(responseSignal, data);
        } finally {
          if (node.tagName === 'BUTTON' || node.tagName === 'INPUT') node.disabled = false;
        }
      }

      // Widget-scoped click + submit
      widgetEl.addEventListener('click', async (e) => {
        const btn = e.target.closest('[data-fui-rpc]');
        if (btn && widgetEl.contains(btn)) {
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
      if (cfg.closeOnEscape) {
        document.addEventListener('keydown', (e) => {
          if (e.key === 'Escape' && document.body.contains(widgetEl)) dismiss();
        });
      }

      // Global click+submit dispatcher (idempotent across widgets).
      // Handles agent-rendered page buttons + plain forms + legacy
      // data-kiln-tool attributes.
      if (!document.__fuiGlobalDispatch) {
        document.__fuiGlobalDispatch = true;
        document.addEventListener('click', async (e) => {
          if (e.target.closest('[data-fui-widget]')) return;
          const fuiBtn = e.target.closest('[data-fui-rpc]');
          if (fuiBtn) { e.preventDefault(); await dispatchRPC(fuiBtn); return; }
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

  const isKnownRoute = (path) => {
    const clean = path.split('?')[0].split('#')[0];
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

  const getCachedScreen = (path) => screenCache.get(path);

  /** Fetch page, swap <main>. Caches for instant back-nav. */
  const loadPage = async (path) => {
    const prevPath = currentPath;
    currentPath = path;

    try {
      const cached = getCachedScreen(path);
      if (cached) {
        swapMainContent(cached.html);
        document.title = cached.title;
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

      if (resp.headers.get('X-Gofastr-Partial') === 'true') {
        swapMainContent(html);
        const title = resp.headers.get('X-Gofastr-Title') || document.title;
        document.title = title;
        cacheScreen(path, html, title);
        window.__gofastr.loadCSS(path);
      } else {
        const doc = new DOMParser().parseFromString(html, 'text/html');
        const nm = doc.querySelector('main');
        if (nm) swapMainContent(nm.innerHTML);
        const title = doc.querySelector('title')?.textContent ?? document.title;
        cacheScreen(path, nm?.innerHTML ?? '', title);
        document.title = title;
      }
      updateActiveLink(path);
      window.scrollTo(0, 0);
      if (Array.isArray(window.__gofastr_routes)) {
        const cur = window.__gofastr_routes.find(r => r.path === path);
        if (cur?.cssChunk) window.__gofastr.loadCSS(path);
      }
      window.dispatchEvent(new CustomEvent('gofastr:navigate', { detail: { path, prevPath, cached: false } }));
    } catch (err) {
      console.error('[gofastr] Nav failed:', err);
      location.href = path;
    }
  };

  const swapMainContent = (html) => {
    const main = document.querySelector('[role="main"]') ?? document.querySelector('main');
    if (main) main.innerHTML = html;
  };

  const updateActiveLink = (path) => {
    const navLinks = document.querySelectorAll('nav a');
    for (const link of navLinks) {
      const href = link.getAttribute('href');
      if (href === path) {
        link.setAttribute('aria-current', 'page');
        link.classList.add('active');
      } else {
        link.removeAttribute('aria-current');
        link.classList.remove('active');
      }
    }
  };

  // Intercept internal link clicks
  document.addEventListener('click', (e) => {
    const anchor = e.target.closest('a[href]');
    if (!anchor) return;
    const href = anchor.getAttribute('href');
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    if (!isInternalLink(href)) return;
    if (anchor.target === '_blank') return;
    if (!isKnownRoute(href)) return;

    e.preventDefault();
    history.pushState(null, '', href);
    loadPage(href);
  });

  // Handle browser back/forward
  window.addEventListener('popstate', () => {
    const path = location.pathname;
    if (path !== currentPath) {
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

  window.addEventListener('gofastr:navigate', () => { closeAllOverlays(); });

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', connectSSE);
  } else {
    connectSSE();
  }

  // Overlay manager: Dialog, Sheet, Drawer — all use a full-screen backdrop wrapper.
  // The backdrop (ov) covers the viewport. The content (.dialog/.sheet/.drawer) is a child.
  // Click on backdrop → close. Click on content → does NOT close.
  // Escape → close topmost. Tab → trapped inside topmost overlay.
  const overlayCache={};
  const overlayStack=[];
  const focusSel='button,[href],input,select,textarea,[tabindex]:not([tabindex="-1"])';
  // Close all open overlays (used on navigation).
  const closeAllOverlays=()=>{
    [...overlayStack].forEach(ov=>ov.remove());
    overlayStack.length=0;
    document.body.style.overflow='';
  };

  // Inject built-in overlay CSS on first use (apps can override via theme).
  let _overlayCSSInjected=false;
  /** Strip <script> tags and event handler attributes from HTML to mitigate XSS */
  const _sanitizeHTML=(html)=>{
    return html
      .replace(/<script\b[^<]*(?:<\/script>)?/gi,'')
      .replace(/\bon\w+\s*=\s*(["'][^"']*["']|\S+)/gi,'');
  };

  const _injectOverlayCSS=()=>{
    if(_overlayCSSInjected)return;
    _overlayCSSInjected=true;
    const s=document.createElement('style');
    s.setAttribute('data-gofastr-overlays','');
    s.textContent=`
[data-overlay]{box-sizing:border-box}
.overlay-backdrop{position:fixed;inset:0;display:flex;z-index:1000;background:rgba(0,0,0,0.5);transition:opacity 0.3s}
.backdrop-closing{opacity:0}
.dialog-overlay{align-items:center;justify-content:center}
.dialog{position:relative;background:var(--surface,#fff);border-radius:var(--radius-lg,12px);padding:var(--spacing-xl,24px);max-width:90vw;width:480px;transition:transform 0.2s,opacity 0.2s}
.dialog.dialog-opening,.dialog.dialog-closing{transform:scale(0.95);opacity:0}
.sheet-backdrop{align-items:flex-end;justify-content:center}
.sheet{position:relative;background:var(--surface,#fff);border-radius:var(--radius-lg,12px) var(--radius-lg,12px) 0 0;padding:var(--spacing-lg,16px);max-height:70vh;overflow-y:auto;width:100%;max-width:100%;transition:transform 0.3s}
.sheet.sheet-opening,.sheet.sheet-closing{transform:translateY(100%)}
.sheet-handle{width:40px;height:4px;background:var(--border,#E5E7EB);border-radius:2px;margin:0 auto 8px}
.drawer-backdrop{align-items:stretch;justify-content:flex-start}
.drawer{position:relative;background:var(--surface,#fff);width:320px;max-width:85vw;height:100vh;overflow-y:auto;padding:var(--spacing-xl,24px);transition:transform 0.3s}
.drawer.drawer-opening,.drawer.drawer-closing{transform:translateX(-100%)}
.overlay-close{position:absolute;top:8px;right:8px;background:none;border:none;font-size:24px;cursor:pointer;color:var(--text-muted,#6B7280);line-height:1;padding:4px 8px;border-radius:4px}
.overlay-close:hover{background:var(--background,#F9FAFB)}
.dialog-actions{display:flex;gap:8px;justify-content:flex-end;margin-top:16px}
.dialog-cancel-btn{padding:8px 16px;border:1px solid var(--border,#E5E7EB);border-radius:var(--radius-sm,4px);background:transparent;color:var(--text,#1F2937);cursor:pointer;font-size:14px}
.dialog-cancel-btn:hover{background:var(--background,#F9FAFB)}
.confirm-btn{padding:8px 16px;border:none;border-radius:var(--radius-sm,4px);background:var(--primary,#4F46E5);color:#fff;cursor:pointer;font-size:14px;font-weight:600}
.confirm-btn:hover{opacity:0.9}
`;
    document.head.appendChild(s);
  };

  const _pendingOverlays=new Set();
  window.__gofastr.openOverlay=async(type,path)=>{
    const key=type+':'+path;
    if(_pendingOverlays.has(key)) return;
    _pendingOverlays.add(key);
    _injectOverlayCSS();
    try {
    let html;
    if(overlayCache[path]){
      html=overlayCache[path];
    } else {
      const resp=await fetch(path,{headers:{'X-Gofastr-Navigate':'1'}});
      if(!resp.ok) return;
      html=await resp.text();
      html=_sanitizeHTML(html);
      overlayCache[path]=html;
    }
    // Hydrate cached HTML with current client state
    const cartCount=window.__gofastr.getState('cart-count',0);
    let hydrated=html;
    if(cartCount>0){
      const items=Array.from({length:cartCount},(_,i)=>'<li>Cart item '+(i+1)+'</li>').join('');
      hydrated=hydrated.replace(/Your cart is empty\./,'<ul>'+items+'</ul>');
      hydrated=hydrated.replace(/\d+\s*items?/g,cartCount+' item'+(cartCount!==1?'s':''));
      hydrated=hydrated.replace(/(<span[^>]*cart-badge[^>]*>)([\s\S]*?)(<\/span>)/,'$1'+cartCount+' items$3');
    }
    const isSheet=type==='sheet';
    const isDrawer=type==='drawer';
    // All types: full-screen backdrop with content child inside
    const backdrop=document.createElement('div');
    backdrop.setAttribute('data-overlay','');
    const cb='<button class="overlay-close" aria-label="Close" data-overlay-close>\u00d7</button>';
    if(isDrawer){
      backdrop.className='overlay-backdrop drawer-backdrop';
      backdrop.innerHTML=`<nav class="drawer drawer-opening">${hydrated}<button class="drawer-close-btn" data-overlay-close style='width:100%;margin-top:1rem;padding:0.5rem;text-align:center;background:transparent;border:1px solid var(--border);border-radius:4px;cursor:pointer'>Close</button>${cb}</nav>`;
    } else if(isSheet){
      backdrop.className='overlay-backdrop sheet-backdrop';
      backdrop.innerHTML=`<div class="sheet sheet-opening"><div class="sheet-handle"></div>${hydrated}<button class="sheet-close-btn cta-button" data-overlay-close style="width:100%;margin-top:0.5rem">Close</button>${cb}</div>`;
    } else {
      backdrop.className='overlay-backdrop dialog-overlay';
      backdrop.innerHTML=`<div class="dialog dialog-opening">${hydrated}${cb}</div>`;
    }
    document.body.appendChild(backdrop);
    document.body.style.overflow='hidden';
    // Force reflow so the browser paints the "opening" state, then remove
    // the class to trigger the CSS transition (slide-in / fade-in).
    backdrop.offsetHeight;
    const content=backdrop.querySelector('.dialog,.drawer,.sheet');
    if(content) content.classList.remove('dialog-opening','sheet-opening','drawer-opening');
    const f=backdrop.querySelectorAll(focusSel);
    if(f.length>0)f[0].focus();
    overlayStack.push(backdrop);
    return backdrop;
    } finally { _pendingOverlays.delete(key); }
  };
  window.__gofastr.closeOverlay=(ov)=>{
    if(!ov)ov=overlayStack[overlayStack.length-1];
    if(!ov)return;
    // Add closing animation class to the content element, not the backdrop
    const content=ov.querySelector('.dialog,.drawer,.sheet');
    if(content){
      if(content.classList.contains('dialog'))content.classList.add('dialog-closing');
      else if(content.classList.contains('drawer'))content.classList.add('drawer-closing');
      else if(content.classList.contains('sheet'))content.classList.add('sheet-closing');
    }
    ov.classList.add('backdrop-closing');
    setTimeout(()=>{
      ov.remove();
      document.body.style.overflow='';
      const i=overlayStack.indexOf(ov);
      if(i>-1)overlayStack.splice(i,1);
      if(overlayStack.length>0)document.body.style.overflow='hidden';
    },300);
  };
  document.addEventListener('click',(e)=>{
    if(e.target.matches('[data-overlay-close]')){
      window.__gofastr.closeOverlay(e.target.closest('[data-overlay]'));
      return;
    }
    // Only clicking the backdrop itself (not content inside it) should close
    if(e.target.matches('.overlay-backdrop'))window.__gofastr.closeOverlay(e.target);
  });
  document.addEventListener('keydown',(e)=>{
    if(e.key==='Escape'&&overlayStack.length>0)window.__gofastr.closeOverlay();
    if(e.key==='Tab'&&overlayStack.length>0){
      const top=overlayStack[overlayStack.length-1],f=top.querySelectorAll(focusSel);
      if(!f.length)return;
      if(e.shiftKey&&document.activeElement===f[0]){e.preventDefault();f[f.length-1].focus();}
      else if(!e.shiftKey&&document.activeElement===f[f.length-1]){e.preventDefault();f[0].focus();}
    }
  });
  window.G=window.__gofastr;
})();
