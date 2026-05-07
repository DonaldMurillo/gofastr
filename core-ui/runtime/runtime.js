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

    formatInt: (n) => String(n),
    formatFloat: (n, d) => Number(n).toFixed(d),
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

  /**
   * Fetch a page and swap the <main> content without a full reload.
   * For client-side navigation, the server returns partial HTML (just
   * the screen component content). The layout (header, footer) stays intact.
   *
   * Screen caching: after first load, the screen content is cached. Going
   * back to a cached screen restores it instantly without a network request.
   */
  const loadPage = async (path) => {
    const prevPath = currentPath;
    currentPath = path;

    try {
      // Check screen cache first (instant back-navigation)
      const cached = getCachedScreen(path);
      if (cached) {
        swapMainContent(cached.html);
        document.title = cached.title;
        updateActiveLink(path);
        window.scrollTo(0, 0);
        window.dispatchEvent(new CustomEvent('gofastr:navigate', {
          detail: { path, prevPath, cached: true },
        }));
        return;
      }

      const resp = await fetch(path, {
        headers: { 'X-Gofastr-Navigate': '1' },
      });
      if (!resp.ok) throw new Error(`HTTP ${resp.status}`);

      const html = await resp.text();

      if (resp.headers.get('X-Gofastr-Partial') === 'true') {
        // Server returned partial content — swap directly into <main>
        swapMainContent(html);
        cacheScreen(path, html, document.title);
      } else {
        // Fallback: server returned full page — parse and extract <main>
        const parser = new DOMParser();
        const doc = parser.parseFromString(html, 'text/html');
        const newMain = doc.querySelector('main');
        if (newMain) {
          swapMainContent(newMain.innerHTML);
        }
        const title = doc.querySelector('title')?.textContent ?? document.title;
        cacheScreen(path, newMain?.innerHTML ?? '', title);
        document.title = title;
      }

      // Update active nav link
      updateActiveLink(path);

      // Scroll to top
      window.scrollTo(0, 0);

      // Dispatch navigation event
      window.dispatchEvent(new CustomEvent('gofastr:navigate', {
        detail: { path, prevPath, cached: false },
      }));
    } catch (err) {
      // Fallback to full page load on error
      console.error('[gofastr] Navigation failed, falling back to full load:', err);
      location.href = path;
    }
  };

  /**
   * Swap content into the <main> element, reusing the existing layout.
   */
  const swapMainContent = (html) => {
    const main = document.querySelector('[role="main"]') ?? document.querySelector('main');
    if (main) main.innerHTML = html;
  };

  /**
   * Update the active navigation link based on current path.
   */
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

  // Intercept clicks on internal links
  document.addEventListener('click', (e) => {
    const anchor = e.target.closest('a[href]');
    if (!anchor) return;

    const href = anchor.getAttribute('href');

    // Skip if modifier keys (open in new tab/window)
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;

    // Skip external links, hashes, mailto, tel
    if (!isInternalLink(href)) return;

    // Skip if target="_blank"
    if (anchor.target === '_blank') return;

    // Only intercept known routes
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

  // -----------------------------------------------------------------------
  // Event delegation: clicks on [data-action]
  // -----------------------------------------------------------------------
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

  // -----------------------------------------------------------------------
  // Event delegation: input/change/submit on [data-action-type]
  // -----------------------------------------------------------------------
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

  // -----------------------------------------------------------------------
  // Hydration on first interaction
  // -----------------------------------------------------------------------
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

  // -----------------------------------------------------------------------
  // MutationObserver for auto-hydration
  // -----------------------------------------------------------------------
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

  // -----------------------------------------------------------------------
  // SSE Island Support
  // -----------------------------------------------------------------------
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

  // Reconnect SSE after client-side navigation
  window.addEventListener('gofastr:navigate', () => {
    // The existing EventSource is still connected to the session
    // but islands from the new page need to exist in the DOM
    // — they'll get updates naturally since SSE is session-scoped
  });

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', connectSSE);
  } else {
    connectSSE();
  }
})();
