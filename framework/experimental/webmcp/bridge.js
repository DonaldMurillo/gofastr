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
  var mc = document.modelContext || navigator.modelContext;
  if (!mc || typeof mc.registerTool !== 'function') return;
  // The placeholder is a QUOTED JSON string; JSON.parse (unlike an
  // object literal) keeps "__proto__" an ordinary own key and accepts
  // duplicates, so no declarable schema can break or poison the bridge.
  var tools = JSON.parse(__GOFASTR_WEBMCP_TOOLS__);

  function dispatch(t, input) {
    return Promise.resolve().then(function () {
      var u = new URL(t.path, location.origin);
      var opts = {
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
            var v = input[k];
            if (v === null || v === undefined) return;
            u.searchParams.set(k, typeof v === 'object' ? JSON.stringify(v) : String(v));
          });
        }
      } else {
        // Unsafe methods carry the app's CSRF token the same way the
        // core runtime's RPCs do (double-submit: header must match the
        // cookie). Read at dispatch time; the meta tag may render
        // after this script parses.
        var meta = document.querySelector('meta[name="csrf-token"]');
        if (meta && meta.content) opts.headers['X-CSRF-Token'] = meta.content;
        opts.headers['Content-Type'] = 'application/json';
        opts.body = JSON.stringify(input === undefined ? {} : input);
      }
      return fetch(u.pathname + u.search, opts).then(function (res) {
        return res.text().then(function (text) {
          return { content: [{ type: 'text', text: text }], isError: !res.ok };
        });
      });
    }).catch(function (err) {
      // Terminal: covers fetch rejection AND body-read failure, so the
      // agent always gets a structured isError result, never a throw.
      return { content: [{ type: 'text', text: 'request failed: ' + err }], isError: true };
    });
  }

  tools.forEach(function (t) {
    // A failure here — sync throw or async rejection — means the tool
    // is silently absent from getTools() (e.g. the app's own page JS
    // already registered the name). Scream so the collision is
    // debuggable, and keep registering the remaining tools either way.
    var warn = function (err) {
      console.warn('gofastr webmcp: registerTool(' + t.name + ') failed: ' + err);
    };
    try {
      var p = mc.registerTool({
        name: t.name,
        title: t.title || undefined,
        description: t.description,
        inputSchema: t.inputSchema,
        execute: function (input) { return dispatch(t, input); }
      });
      if (p && typeof p.catch === 'function') p.catch(warn);
    } catch (err) {
      warn(err);
    }
  });
})();
