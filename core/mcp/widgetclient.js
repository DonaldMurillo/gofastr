/*!
 * mcp/widgetclient.js — widget-side client for MCP Apps (GoFastr).
 *
 * Plain JavaScript (IIFE, no imports, no external URLs). Runs INSIDE the
 * host-rendered sandboxed iframe of an MCP App and speaks the MCP Apps
 * widget protocol (ext-apps, spec 2026-01-26): JSON-RPC 2.0 over
 * postMessage, no extra framing. GoFastr serves the widget side of MCP
 * Apps and is not a host; this script is what an app author loads
 * instead of hand-rolling the postMessage RPC.
 *
 * Security invariants (mirror of pluginhost's frame client — do NOT
 * weaken):
 *   - Messages are accepted only when event.source === window.parent.
 *     We do NOT check event.origin: this frame IS an opaque origin (its
 *     origin string is literally "null"), so origin-string checks are a
 *     trap in both directions; the window identity is the gate.
 *   - Posts use targetOrigin "*" — the frame is opaque-origin, so a
 *     concrete targetOrigin is the wrong tool; the source check is the
 *     gate.
 *
 * Usage (inside the widget document, after this script loads):
 *
 *   var app = window.__gofastrMcpApp;
 *   app.connect({ availableDisplayModes: ["inline"] }).then(function (host) {
 *     // host.protocolVersion / hostCapabilities / hostInfo / hostContext
 *   });
 *   app.callTool({ name: "search", arguments: { q: "hi" } }).then(...);
 *   app.onToolResult(function (params) { ... });
 *   app.sizeChanged({ width: 320, height: 240 });
 *
 * Theme: the client applies the host's theme signals (hostContext.theme,
 * hostContext.styles) to the document root on connect and on every
 * host-context-changed, so author CSS can use var(--color-…) and
 * :root[data-theme="dark"]. A widget invents no palette of its own.
 */
(function () {
  'use strict';

  // --- Protocol constants (pinned by widgetclient_test.go) -------------------

  var JSON_RPC_VERSION = "2.0";    // the envelope; there is no other framing
  var RESPONSE_TIMEOUT_MS = 30000; // request → response default; tools/call
                                   // turns can be slow — pass an explicit
                                   // timeoutMs when they are slower still
  var MAX_INFLIGHT = 64;           // pending-request bound

  // id -> {resolve, reject, timer} for OUR outbound (widget → host)
  // requests. Null-proto so a "__proto__"/"constructor" id looks up as
  // undefined instead of a truthy Object.prototype member.
  var pending = Object.create(null);
  // method -> handler, for inbound host → widget traffic: the
  // notifications, plus the ui/resource-teardown request handler.
  var inboundHandlers = Object.create(null);
  var reqCounter = 0;

  function message(method, params, id) {
    var msg = { jsonrpc: JSON_RPC_VERSION, method: method };
    if (params) msg.params = params;
    if (id !== undefined) msg.id = id;
    return msg;
  }

  function postToHost(msg) {
    // The widget frame is an opaque origin; targetOrigin MUST be "*". The
    // real gate is the event.source === window.parent check on both sides.
    window.parent.postMessage(msg, "*");
  }

  // A non-positive / NaN / infinite timeoutMs is not a timeout — fall back
  // to the protocol default instead of handing garbage to setTimeout.
  function validTimeout(timeoutMs) {
    return (typeof timeoutMs === "number" && isFinite(timeoutMs) && timeoutMs > 0)
      ? timeoutMs
      : RESPONSE_TIMEOUT_MS;
  }

  // Fail every outstanding request (host teardown, pagehide) so nothing
  // hangs; responses arriving after this are dropped by id.
  function rejectOutstanding(err) {
    for (var id in pending) {
      if (Object.prototype.hasOwnProperty.call(pending, id)) {
        clearTimeout(pending[id].timer);
        pending[id].reject(err);
      }
    }
    pending = Object.create(null);
  }

  function request(method, params, timeoutMs) {
    if (Object.keys(pending).length >= MAX_INFLIGHT) {
      // Reject BEFORE posting: a widget that ignores the rejection would
      // otherwise stack unbounded entries waiting to time out.
      return Promise.reject({
        code: "E_SATURATED",
        message: "request map saturated at " + MAX_INFLIGHT + " in flight: " + method
      });
    }
    timeoutMs = validTimeout(timeoutMs);
    return new Promise(function (resolve, reject) {
      var id = ++reqCounter;
      var timer = setTimeout(function () {
        if (pending[id]) {
          delete pending[id];
          reject({ code: "E_TIMEOUT", message: "request " + method + " timed out" });
        }
      }, timeoutMs);
      pending[id] = { resolve: resolve, reject: reject, timer: timer };
      try {
        postToHost(message(method, params, id));
      } catch (e) {
        // A non-structured-cloneable params postMessage throw must clean
        // up the pending entry + timer, or it lingers to timeout and
        // counts toward MAX_INFLIGHT.
        clearTimeout(timer);
        delete pending[id];
        reject({ code: "E_SEND", message: "request " + method + " not sendable: " + String(e && e.message || e) });
      }
    });
  }

  function notify(method, params) {
    postToHost(message(method, params));
  }

  function register(method, handler) {
    inboundHandlers[method] = handler;
  }

  // --- Host context / theme -----------------------------------------------

  // The widget-side theming convention (spec 2026-01-26, HostContext; the
  // position doc is framework/docs/content/agent-host.md): a widget
  // consumes the host's theme signals and invents no palette of its own.
  // The spec's theme signals are:
  //
  //   theme                "light" | "dark"
  //   styles.variables     CSS custom properties with standardized names
  //                        (--color-*, --font-*, …), whose values hosts
  //                        SHOULD write with light-dark()
  //   styles.css.fonts     @font-face / @import CSS the view injects
  //
  // The client merges the ui/initialize result's hostContext and every
  // ui/notifications/host-context-changed partial update (the spec says
  // merge, field by field) into the running state below, then applies it
  // to the document root:
  //
  //   theme           → <html data-theme="…"> + color-scheme, which is
  //                     what makes the host's light-dark() variable
  //                     values resolve on the correct side
  //   styles.variables→ inline custom properties on the root, inherited
  //                     by all author markup; names a later update no
  //                     longer lists are removed
  //   styles.css.fonts→ one <style data-mcpapp-fonts> element, replaced
  //                     (never stacked) when the host sends new font CSS,
  //                     removed when an update ships none
  //
  // Widgets have no styling of their own to undo here: a widget that
  // ignores theming simply does not reference the variables or the
  // data-theme selector, and the applied state is inert markup.
  var hostContextState = Object.create(null);
  // Custom properties the last applyHostTheme set on the root: the
  // diff base for removing ones a later update no longer lists.
  var appliedThemeVars = Object.create(null);

  function mergeHostContext(partial) {
    if (!partial || typeof partial !== "object") return;
    for (var k in partial) {
      if (Object.prototype.hasOwnProperty.call(partial, k)) {
        hostContextState[k] = partial[k];
      }
    }
  }

  function applyHostTheme() {
    if (typeof document === "undefined") return;
    var root = document.documentElement;
    var theme = hostContextState.theme;
    if (theme === "light" || theme === "dark") {
      root.setAttribute("data-theme", theme);
      root.style.colorScheme = theme;
    }
    var styles = hostContextState.styles;
    if (!styles || typeof styles !== "object") return;
    var vars = styles.variables;
    var seen = Object.create(null);
    if (vars && typeof vars === "object") {
      for (var name in vars) {
        if (!Object.prototype.hasOwnProperty.call(vars, name)) continue;
        var value = vars[name];
        // Standardized variable names all start with "--"; anything else
        // (or a non-string value) is not a theme signal, skip it.
        if (name.indexOf("--") === 0 && typeof value === "string") {
          root.style.setProperty(name, value);
          seen[name] = true;
        }
      }
    }
    // Theme application is state, not a pile: what the previous context
    // set and this one no longer lists comes off the root.
    for (var prev in appliedThemeVars) {
      if (!seen[prev]) root.style.removeProperty(prev);
    }
    appliedThemeVars = seen;

    var css = styles.css;
    var fonts = (css && typeof css === "object" && typeof css.fonts === "string") ? css.fonts : "";
    var el = document.querySelector("style[data-mcpapp-fonts]");
    if (fonts) {
      if (!el) {
        el = document.createElement("style");
        el.setAttribute("data-mcpapp-fonts", "");
        (document.head || root).appendChild(el);
      }
      el.textContent = fonts;
    } else if (el) {
      el.remove();
    }
  }

  // applyHostContext merges a full or partial HostContext into the
  // running state and applies its theme signals to the document. Called
  // by connect(), by the host-context-changed dispatch, and by author
  // code that wants to re-apply (or seed, in local dev) the state.
  // Theme application must never break message dispatch, so it is
  // guarded like a handler: log, don't throw.
  function applyHostContext(partial) {
    mergeHostContext(partial);
    try {
      applyHostTheme();
    } catch (e) {
      if (typeof console !== "undefined" && console.error) {
        console.error("[mcpapp] applying host theme threw", e);
      }
    }
  }
  // --- Inbound dispatch (host → widget) --------------------------------------

  // A JSON-RPC response to a host request. Results here are always {}
  // or a plain error object, so the post cannot throw DataCloneError.
  function reply(id, result, err) {
    var msg = { jsonrpc: JSON_RPC_VERSION, id: id };
    if (err) msg.error = err;
    else msg.result = result;
    postToHost(msg);
  }

  function handleNotification(method, params) {
    // The theming convention rides the dispatch, not a registered
    // handler: the client applies host-context-changed BEFORE the app's
    // own handler runs (and even when none is registered), so the
    // document root can never miss a theme signal because author code
    // forgot a handler. Every other notification is the app's alone.
    if (method === "ui/notifications/host-context-changed") {
      applyHostContext(params);
    }
    var h = inboundHandlers[method];
    if (typeof h !== "function") return; // unknown method → ignored, not thrown
    try { h(params || {}); } catch (e) {
      if (typeof console !== "undefined" && console.error) {
        console.error("[mcpapp] handler for " + method + " threw", e);
      }
    }
  }

  // Host teardown (spec 2026-01-26: a REQUEST, not a notification — the
  // host SHOULD wait for the response before tearing the resource down,
  // to prevent data loss). The app's registered handler runs first,
  // awaited if it returns a promise; the response is posted, and only
  // then — with the response on the wire — is every outstanding request
  // failed. Same ordering as pluginhost's frame client teardown.
  function handleTeardown(msg) {
    var handler = inboundHandlers["ui/resource-teardown"];
    var run = (typeof handler === "function")
      ? function () { return handler(msg.params || {}); }
      : function () { return undefined; }; // no hook — still respond
    Promise.resolve().then(run).then(
      function () {
        reply(msg.id, {}, null);
        rejectOutstanding({ code: "E_TEARDOWN", message: "widget torn down" });
      },
      function (err) {
        reply(msg.id, null, { code: "E_HANDLER", message: String(err && err.message || err) });
        rejectOutstanding({ code: "E_TEARDOWN", message: "widget torn down" });
      }
    );
  }

  function onWindowMessage(event) {
    // Security gate: accept ONLY the parent window — NOT event.origin
    // (the sandboxed widget reports origin "null"; source identity is
    // the gate).
    var fromParent = event.source === window.parent;
    if (!fromParent) return;
    var msg = event.data;
    if (!msg || typeof msg !== "object") return;
    if (msg.jsonrpc !== JSON_RPC_VERSION) return;

    if (msg.method !== undefined) {
      // ui/resource-teardown is the ONE host → widget request on this
      // client's surface (the host waits for the response before
      // removing the resource), so it is answered, never dropped. Any
      // other request is not part of the surface; dropping it is the
      // ignore-don't-throw rule.
      if (msg.method === "ui/resource-teardown" && msg.id !== undefined) {
        handleTeardown(msg);
        return;
      }
      if (msg.id !== undefined) return;
      handleNotification(msg.method, msg.params);
      return;
    }

    // A response to one of OUR requests, correlated by id. A late
    // response — already timed out or torn down — finds no pending
    // entry and is dropped.
    var p = pending[msg.id];
    if (!p) return;
    clearTimeout(p.timer);
    delete pending[msg.id];
    if (msg.error) p.reject(msg.error);
    else p.resolve(msg.result);
  }

  // --- Public client ----------------------------------------------------------

  window.__gofastrMcpApp = {
    // Handshake (call once). Sends ui/initialize carrying the app's
    // appCapabilities, resolves with the host's result — protocolVersion,
    // hostCapabilities, hostInfo, hostContext — applies the result's
    // hostContext theme signals (see applyHostContext), and only then
    // sends the ui/notifications/initialized notification, so the host
    // considers the app ready after the document already reflects its
    // theme.
    connect: function (appCapabilities, timeoutMs) {
      return request("ui/initialize", { appCapabilities: appCapabilities || {} }, timeoutMs)
        .then(function (result) {
          if (result) applyHostContext(result.hostContext);
          notify("ui/notifications/initialized");
          return result;
        });
    },
    // Widget → host request (any spec method): params is the method's
    // spec params object, verbatim. Rejects with the host's JSON-RPC
    // error object, or one of our codes: E_TIMEOUT (no response within
    // timeoutMs, default 30s), E_SATURATED (MAX_INFLIGHT in flight),
    // E_SEND (params not cloneable), E_TEARDOWN (widget torn down or
    // page hidden).
    request: request,
    // Widget → host notification (fire-and-forget).
    notify: notify,
    // Named senders: each pins one spec method string; params pass
    // through verbatim, so the payload shape is the spec's, not ours.
    callTool: function (params, timeoutMs) { return request("tools/call", params, timeoutMs); },
    readResource: function (params, timeoutMs) { return request("resources/read", params, timeoutMs); },
    openLink: function (params, timeoutMs) { return request("ui/open-link", params, timeoutMs); },
    requestDisplayMode: function (params, timeoutMs) { return request("ui/request-display-mode", params, timeoutMs); },
    message: function (params, timeoutMs) { return request("ui/message", params, timeoutMs); },
    updateModelContext: function (params, timeoutMs) { return request("ui/update-model-context", params, timeoutMs); },
    // The widget resized itself; params carries the new size.
    sizeChanged: function (params) { notify("ui/notifications/size-changed", params); },
    // Host → widget inbound handler (method → handler);
    // re-registering a method overwrites. Inbound notifications with
    // no registered handler are ignored, never thrown.
    on: register,
    // Named registrars for the host → widget notifications.
    onToolInput: function (handler) { register("ui/notifications/tool-input", handler); },
    onToolInputPartial: function (handler) { register("ui/notifications/tool-input-partial", handler); },
    onToolResult: function (handler) { register("ui/notifications/tool-result", handler); },
    onToolCancelled: function (handler) { register("ui/notifications/tool-cancelled", handler); },
    onHostContextChanged: function (handler) { register("ui/notifications/host-context-changed", handler); },
    onMessage: function (handler) { register("notifications/message", handler); },
    // The one host → widget REQUEST: ui/resource-teardown (spec
    // 2026-01-26 — the host waits for the response before tearing the
    // resource down). The handler may return a promise; the client
    // answers after it settles — result {} on success, a JSON-RPC
    // error if it throws — then fails every outstanding request with
    // E_TEARDOWN.
    onResourceTeardown: function (handler) { register("ui/resource-teardown", handler); },
    // A copy of the merged HostContext state (initialize result plus
    // every host-context-changed update). Read-only by convention: the
    // client keeps the authoritative state for the next apply.
    hostContext: function () {
      var copy = {};
      for (var k in hostContextState) {
        if (Object.prototype.hasOwnProperty.call(hostContextState, k)) {
          copy[k] = hostContextState[k];
        }
      }
      return copy;
    },
    // Merge + apply a full or partial HostContext by hand (local dev, or
    // re-applying after author code touched the root's style).
    applyHostContext: applyHostContext
  };

  window.addEventListener("message", onWindowMessage);
  // The widget document is going away — outstanding requests can never
  // get a response; fail them now instead of leaking until timeout.
  window.addEventListener("pagehide", function () {
    rejectOutstanding({ code: "E_TEARDOWN", message: "page hidden" });
  });
})();
