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
  // method -> handler, for inbound host → widget notifications.
  var notificationHandlers = Object.create(null);
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
    notificationHandlers[method] = handler;
  }

  // --- Inbound dispatch (host → widget) --------------------------------------

  function runHandler(method, params) {
    var h = notificationHandlers[method];
    if (typeof h !== "function") return; // unknown method → ignored, not thrown
    try { h(params || {}); } catch (e) {
      if (typeof console !== "undefined" && console.error) {
        console.error("[mcpapp] handler for " + method + " threw", e);
      }
    }
  }

  function handleNotification(method, params) {
    runHandler(method, params);
    // ui/resource-teardown is the host saying this widget is going away.
    // The app's registered handler runs first (it may still post a final
    // notification); after it, every request still in flight can never be
    // answered — fail them instead of leaking until each timer fires.
    if (method === "ui/resource-teardown") {
      rejectOutstanding({ code: "E_TEARDOWN", message: "widget torn down" });
    }
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
      // Host → widget REQUESTS (method + id) are not part of this
      // client's surface; dropping them is the ignore-don't-throw rule.
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
    // hostCapabilities, hostInfo, hostContext — and only then sends the
    // ui/notifications/initialized notification.
    connect: function (appCapabilities, timeoutMs) {
      return request("ui/initialize", { appCapabilities: appCapabilities || {} }, timeoutMs)
        .then(function (result) {
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
    // Host → widget notification handler (method → handler);
    // re-registering a method overwrites. Notifications with no
    // registered handler are ignored, never thrown.
    on: register,
    // Named registrars for the host → widget notifications.
    onToolInput: function (handler) { register("ui/notifications/tool-input", handler); },
    onToolInputPartial: function (handler) { register("ui/notifications/tool-input-partial", handler); },
    onToolResult: function (handler) { register("ui/notifications/tool-result", handler); },
    onToolCancelled: function (handler) { register("ui/notifications/tool-cancelled", handler); },
    onHostContextChanged: function (handler) { register("ui/notifications/host-context-changed", handler); },
    onResourceTeardown: function (handler) { register("ui/resource-teardown", handler); },
    onMessage: function (handler) { register("notifications/message", handler); }
  };

  window.addEventListener("message", onWindowMessage);
  // The widget document is going away — outstanding requests can never
  // get a response; fail them now instead of leaking until timeout.
  window.addEventListener("pagehide", function () {
    rejectOutstanding({ code: "E_TEARDOWN", message: "page hidden" });
  });
})();
