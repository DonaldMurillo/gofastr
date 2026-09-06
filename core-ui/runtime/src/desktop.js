// GoFastr runtime module, desktop bridge
//
// The browser half of battery/desktop's typed bridge. Loaded on
// demand via __gofastr.loadModule('desktop'); like the ws module it
// has no DOM marker; the host injects window.__gofastr_desktop at
// document start, and the generated per-host bridge.js layers typed
// namespaces on top of `call`:
//
//   __gofastr.desktop.clipboard.writeText({text})  // -> Promise
//   __gofastr.desktop.on('menu_file_new', fn)    // native events
//
// In a plain browser __gofastr_desktop is absent, `available` is
// false, and every `call` rejects {code:'unsupported'} without a
// network request.
//
// Nothing here logs payloads. A listener that throws is reported with
// the event NAME only, never the payload.
(() => {
  'use strict';
  window.__gofastr = window.__gofastr || {};
  const NS = window.__gofastr;

  // Same anchored name class the server validates; checked before a
  // name can touch the URL (the loadModule name-gate posture in
  // boot.js: a "../../x" token must not normalize onto another route).
  const NAME = /^[a-z][A-Za-z0-9_]*$/;

  const available = () => !!window.__gofastr_desktop;

  function mkErr(code, message) {
    const e = new Error(message);
    e.code = code;
    return e;
  }
  // The id of the window this page lives in: the host marker's window
  // field, "main" when absent (the first window, or a plain browser).
  // Sent on every call so the server can tell which window is asking
  // (windows.broadcast excludes it, windows.self answers it). A claim
  // by the page, not an identity.
  const windowID = () => (window.__gofastr_desktop && window.__gofastr_desktop.window) || 'main';

  // The page's CSRF token, when the app ships one (the _csrf helper
  // shape from rpc.js). The desktop chokepoint defends itself without
  // it; forwarding keeps a BFF-posture app working unchanged.
  function csrf(headers) {
    const token = document.querySelector('meta[name="csrf-token"]')?.content;
    if (token) headers['X-CSRF-Token'] = token;
    headers['X-Gofastr-Window'] = windowID();
    return headers;
  }

  function call(cap, method, input) {
    return new Promise((resolve, reject) => {
      if (!available()) return reject(mkErr('unsupported', 'desktop host is not available'));
      if (!NAME.test(cap) || !NAME.test(method)) {
        return reject(mkErr('invalid_input', 'invalid capability or method name'));
      }
      fetch('/__gofastr/desktop/call/' + cap + '/' + method, {
        method: 'POST',
        headers: csrf({ 'Content-Type': 'application/json' }),
        body: JSON.stringify(input ?? {}),
        credentials: 'same-origin'
      }).then((res) =>
        res.text().then((text) => {
          let body = null;
          try { body = text ? JSON.parse(text) : null; } catch (_) { body = null; }
          if (res.ok && body && body.ok) return resolve(body.result);
          const err = body && body.error;
          reject(mkErr(err?.code || 'internal', err?.message || 'internal error'));
        })
      ).catch(() => reject(mkErr('internal', 'internal error')));
    });
  }

  // A borderless window drags through the page. The request rides the
  // WebView's own script message channel (window.webkit.messageHandlers),
  // NOT the HTTP bridge: the native side needs the mouse-down that is
  // still the current event, which only holds on that channel. In a
  // plain browser there is no handler and startDrag is a no-op.
  const DRAG_MSG = '{"type":"drag"}';
  function startDrag() {
    const w = window.webkit;
    const h = w && w.messageHandlers && w.messageHandlers.gofastr;
    if (h) try { h.postMessage(DRAG_MSG); } catch (_) { /* handler gone */ }
  }

  // Mousedown on an element carrying data-fui-window-drag, or inside
  // one, starts a window drag: the drag-handle pattern for borderless
  // windows. Delegated, so runtime-swapped islands keep working.
  if (typeof document !== 'undefined' && document.addEventListener) {
    document.addEventListener('mousedown', (e) => {
      const t = e.target;
      if (t && t.closest && t.closest('[data-fui-window-drag]')) startDrag();
    });
  }

  // The relaunch redirect: report where the page is after every
  // client-side navigation (the gofastr:navigate event the router
  // dispatches after a swap; boot-embed uses the same event), and once
  // at load for the entry path. A claim like the window header: the
  // server keeps only the main window's report and validates it both
  // ways. Fire-and-forget; a rejected report is never worth a console
  // error.
  function reportPath(p) {
    if (available() && typeof p === 'string' && p.charAt(0) === '/') {
      call('window', 'setPath', { path: p }).catch(() => {});
    }
  }
  reportPath(location.pathname + location.search);
  window.addEventListener('gofastr:navigate', (e) => {
    reportPath((e.detail && e.detail.path) || location.pathname + location.search);
  });

  // Native events arrive through _dispatch (Window.Eval from the Go
  // side), never through the SSE bus: sse.js only carries island
  // frames. A listener that throws must not break the dispatch loop or
  // the caller.
  const listeners = new Map();
  function on(name, fn) {
    if (typeof fn !== 'function') return;
    let set = listeners.get(name);
    if (!set) { set = new Set(); listeners.set(name, set); }
    set.add(fn);
  }
  function off(name, fn) {
    const set = listeners.get(name);
    if (set) set.delete(fn);
  }
  function _dispatch(name, payload) {
    const set = listeners.get(name);
    if (!set) return;
    for (const fn of Array.from(set)) {
      try { fn(payload); } catch (_) { console.error('desktop listener failed', name); }
    }
  }

  NS.desktop = {
    available,
    // The id of the window this page lives in ("main" when the marker
    // carries none).
    windowID: windowID(),
    call,
    on,
    off,
    _dispatch,
    // Set by the generated bridge.js once it loads.
    manifest: null
  };

  // The window namespace. The generated bridge.js REPLACES
  // NS.desktop['window'] with the typed capability methods once it
  // loads, so the accessor below merges startDrag into whatever
  // arrives: app code sees one namespace before and after bridge.js.
  let winNS = { startDrag };
  Object.defineProperty(NS.desktop, 'window', {
    configurable: true,
    enumerable: true,
    get() { return winNS; },
    set(v) {
      winNS = (v && typeof v === 'object') ? Object.assign(v, { startDrag }) : { startDrag };
    }
  });
  (NS.loadedModules ||= {}).desktop = true;
})();
