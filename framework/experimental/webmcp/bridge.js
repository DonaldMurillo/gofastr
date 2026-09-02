// GoFastr WebMCP bridge (experimental).
//
// Registers the server-declared tool manifest on the page's ModelContext
// (the WebMCP proposal, Chrome 146+ behind a flag / origin trial) and
// proxies each tool's execute() to its same-origin endpoint with the
// visitor's session credentials. No-op on browsers without the API.
//
// The spec's IDL hangs modelContext off Document; Chromium additionally
// exposes the same object on Navigator (the legacy binding), so probe
// Document first and fall back.
(function () {
  'use strict';
  // Debug state is BOUNDED and OPT-IN: the server replaces this
  // placeholder with true only for mounts that asked (WithBridgeDebug).
  // It carries feature support, registration counts, failure names, and
  // the last invocation status — never inputs, headers, or URLs.
  const debug = __GOFASTR_WEBMCP_DEBUG__;
  const dbg = debug
    ? { supported: false, attempted: 0, registered: 0, failed: [], lastStatus: '' }
    : null;
  if (dbg) window.__gofastrWebMCP = dbg;
  const mc = document.modelContext || navigator.modelContext;
  if (!mc || typeof mc.registerTool !== 'function') return;
  if (dbg) dbg.supported = true;
  // The placeholder is a QUOTED JSON string; JSON.parse (unlike an
  // object literal) keeps "__proto__" an ordinary own key and accepts
  // duplicates, so no declarable schema can break or poison the bridge.
  const tools = JSON.parse(__GOFASTR_WEBMCP_TOOLS__);

  function dispatch(t, input) {
    return Promise.resolve().then(function () {
      const u = new URL(t.path, location.origin);
      const opts = {
        method: t.method,
        credentials: 'same-origin',
        headers: { 'Accept': 'application/json', 'X-Gofastr-WebMCP': '1' }
      };
      if (t.method === 'GET') {
        // Merge input into the URL so an input key overrides a
        // baked-in query param of the same name instead of being
        // silently shadowed by it (servers read the first value).
        if (input && typeof input === 'object') {
          Object.keys(input).forEach(function (k) {
            const v = input[k];
            if (v === null || v === undefined) return;
            u.searchParams.set(k, typeof v === 'object' ? JSON.stringify(v) : String(v));
          });
        }
      } else {
        // Unsafe methods carry the app's CSRF token the same way the
        // core runtime's RPCs do (double-submit: header must match the
        // cookie). Read at dispatch time; the meta tag may render
        // after this script parses.
        const meta = document.querySelector('meta[name="csrf-token"]');
        if (meta && meta.content) opts.headers['X-CSRF-Token'] = meta.content;
        opts.headers['Content-Type'] = 'application/json';
        opts.body = JSON.stringify(input === undefined ? {} : input);
      }
      return fetch(u.pathname + u.search, opts).then(function (res) {
        if (dbg) dbg.lastStatus = res.ok ? 'ok' : 'http_' + res.status;
        return res.text().then(function (text) {
          return { content: [{ type: 'text', text: text }], isError: !res.ok };
        });
      });
    }).catch(function (err) {
      // Terminal: covers fetch rejection AND body-read failure, so the
      // agent always gets a structured isError result, never a throw.
      if (dbg) dbg.lastStatus = 'network_error';
      return { content: [{ type: 'text', text: 'request failed: ' + err }], isError: true };
    });
  }

  tools.forEach(function (t) {
    // A failure here — sync throw or async rejection — means the tool
    // is silently absent from getTools() (e.g. the app's own page JS
    // already registered the name). Scream so the collision is
    // debuggable, and keep registering the remaining tools either way.
    const warn = function (err) {
      console.warn('gofastr webmcp: registerTool(' + t.name + ') failed: ' + err);
    };
    if (dbg) dbg.attempted += 1;
    let ann;
    if (t.readOnlyHint || t.untrustedContentHint) {
      ann = {};
      if (t.readOnlyHint) ann.readOnlyHint = true;
      if (t.untrustedContentHint) ann.untrustedContentHint = true;
    }
    try {
      const p = mc.registerTool({
        name: t.name,
        title: t.title || undefined,
        description: t.description,
        inputSchema: t.inputSchema,
        annotations: ann,
        execute: function (input) { return dispatch(t, input); }
      });
      if (p && typeof p.then === 'function') {
        p.then(
          function () { if (dbg) dbg.registered += 1; },
          function (err) {
            if (dbg) dbg.failed.push(t.name);
            warn(err);
          }
        );
      } else if (dbg) {
        dbg.registered += 1;
      }
    } catch (err) {
      if (dbg) dbg.failed.push(t.name);
      warn(err);
    }
  });
})();
