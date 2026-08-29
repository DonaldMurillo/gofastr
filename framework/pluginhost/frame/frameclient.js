/*!
 * pluginhost/frame/frameclient.js — frame-side channel client for GoFastr
 * heavy-JS plugins.
 *
 * Plain JavaScript (IIFE, no imports, no external URLs). Runs INSIDE the
 * opaque-origin sandboxed plugin frame and mirrors the host broker
 * (host/pluginhost.js): same envelope, same source check, same
 * request/response semantics, so both directions of the plugin channel
 * share ONE contract.
 *
 * Security invariants (mirror of the broker — do NOT weaken):
 *   - Messages are accepted only when event.source === window.parent.
 *     Like the broker, we do NOT check event.origin: this frame IS an
 *     opaque origin (its origin string is literally "null"), so
 *     origin-string checks are a trap in both directions; the window
 *     identity is the gate.
 *   - Posts use targetOrigin "*" — the frame is opaque-origin, so a
 *     concrete targetOrigin is the wrong tool; the source check is the
 *     gate, exactly as in the broker's postTo.
 *
 * Usage (inside the frame document, after this script loads):
 *
 *   var client = window.__gofastrPluginFrame;
 *   client.onEvent("init", function (params) { … });
 *   client.onRequest("getState", function (params) { return {…}; });
 *   client.ready({ domReady: true }).then(function (init) { … });
 *   client.sendRequest("save", {…}).then(function (result) { … });
 */
(function () {
  'use strict';

  // --- Protocol constants (must match host/pluginhost.js; the pair is
  //     pinned by TestEnvelopeVersionsAgree) ---------------------------------
  var ENVELOPE_VERSION = 1;
  var RESPONSE_TIMEOUT_MS = 5000;  // request → response default
  var MAX_INFLIGHT = 64;           // pending-request bound, both directions

  // id -> {resolve, reject, timer} for OUR outbound (frame → host) requests.
  var pending = Object.create(null);
  // method -> handler, for inbound host → frame traffic.
  var eventHandlers = Object.create(null);
  var requestHandlers = Object.create(null);
  var reqCounter = 0;

  // ready() → init handshake state: every outstanding ready() promise
  // resolves ONCE with the first init's params; later inits only dispatch
  // through the onEvent handlers.
  var initArrived = false;
  var initParams = null;
  var initWaiters = [];

  function envelope(type, method, params, id) {
    var env = { v: ENVELOPE_VERSION, type: type, src: "plugin", result: null, error: null };
    if (id) env.id = id;
    if (method) env.method = method;
    if (params) env.params = params;
    return env;
  }

  function postToHost(env) {
    // The frame is an opaque origin; targetOrigin MUST be "*". The real
    // gate is the event.source === window.parent check on both sides (§3).
    window.parent.postMessage(env, "*");
  }

  // A non-positive / NaN / infinite timeoutMs is not a timeout — fall back
  // to the protocol default instead of handing garbage to setTimeout.
  function validTimeout(timeoutMs) {
    return (typeof timeoutMs === "number" && isFinite(timeoutMs) && timeoutMs > 0)
      ? timeoutMs
      : RESPONSE_TIMEOUT_MS;
  }

  // Fail every outstanding sendRequest (host teardown ack, pagehide) so
  // nothing hangs; responses arriving after this are dropped by id.
  function rejectOutstanding(err) {
    for (var id in pending) {
      if (Object.prototype.hasOwnProperty.call(pending, id)) {
        clearTimeout(pending[id].timer);
        pending[id].reject(err);
      }
    }
    pending = Object.create(null);
  }

  // --- Inbound dispatch (host → frame) --------------------------------------

  function reply(id, result, err) {
    var env = envelope("response", null, null, id);
    if (err) env.error = err;
    else env.result = result;
    try {
      postToHost(env);
    } catch (e) {
      // A non-structured-cloneable result (function, DOM node) throws
      // DataCloneError here; without this guard the host waits out its
      // full timeout instead of learning the handler misbehaved. Error
      // envelopes are plain strings, so the fallback always clones.
      var fallback = envelope("response", null, null, id);
      fallback.error = {
        code: "E_HANDLER",
        message: "response not cloneable: " + String(e && e.message || e)
      };
      try {
        postToHost(fallback);
      } catch (ignored) { /* parent gone — nothing left to deliver to */ }
    }
  }

  function handleRequest(msg) {
    var handler = requestHandlers[msg.method];
    if (typeof handler !== "function") {
      // Never silence: the host holds a pending entry per request id and a
      // dropped reply would hang it until its own timeout.
      reply(msg.id, null, { code: "E_NO_HANDLER", message: "no handler for " + msg.method });
      return;
    }
    // Promise.resolve().then(...): a synchronous throw in the handler
    // becomes a rejection instead of escaping the message dispatch.
    Promise.resolve().then(function () {
      return handler(msg.params || {});
    }).then(
      function (result) { reply(msg.id, result, null); },
      function (err) {
        reply(msg.id, null, { code: "E_HANDLER", message: String(err && err.message || err) });
      }
    );
  }

  function handleEvent(method, params) {
    params = params || {};
    if (method === "init") {
      if (!initArrived) {
        initArrived = true;
        initParams = params;
        var waiters = initWaiters;
        initWaiters = [];
        for (var i = 0; i < waiters.length; i++) waiters[i](params);
      }
    }
    var h = eventHandlers[method];
    if (typeof h === "function") {
      try { h(params); } catch (e) {
        if (typeof console !== "undefined" && console.error) {
          console.error("[pluginframe] onEvent handler threw", e);
        }
      }
    }
    // No handler registered → unknown events are ignored by protocol
    // rule (§4: unknown method → ignore).
  }

  // Host teardown (SPA nav): run the plugin's registered teardown handler
  // (awaited), ACK the request, THEN fail every outstanding sendRequest —
  // in that order, so the ACK is on the wire before the host removes us.
  function handleTeardown(msg) {
    var handler = requestHandlers["teardown"];
    var run = (typeof handler === "function")
      ? function () { return handler(msg.params || {}); }
      : function () { return null; }; // no plugin teardown hook — still ACK
    Promise.resolve().then(run).then(
      function (result) {
        reply(msg.id, result, null);
        rejectOutstanding({ code: "E_TEARDOWN", message: "instance torn down" });
      },
      function (err) {
        reply(msg.id, null, { code: "E_HANDLER", message: String(err && err.message || err) });
        rejectOutstanding({ code: "E_TEARDOWN", message: "instance torn down" });
      }
    );
  }

  function onMessage(event) {
    // Security mirror of the broker: accept ONLY the parent window — NOT
    // event.origin (opaque origins report "null"; source identity is the
    // gate, §3).
    var fromParent = event.source === window.parent;
    if (!fromParent) return;
    var msg = event.data;
    if (!msg || typeof msg !== "object") return;
    if (msg.v !== ENVELOPE_VERSION) return;
    if (msg.src !== "host") return;

    if (msg.type === "response") {
      var p = pending[msg.id];
      if (!p) return; // late (post-timeout / post-teardown): dropped by id
      clearTimeout(p.timer);
      delete pending[msg.id];
      if (msg.error) p.reject(msg.error);
      else p.resolve(msg.result);
      return;
    }
    if (msg.type === "request") {
      if (msg.method === "teardown") handleTeardown(msg);
      else handleRequest(msg);
      return;
    }
    if (msg.type === "event") {
      handleEvent(msg.method, msg.params);
    }
  }

  // --- Public client ----------------------------------------------------------

  window.__gofastrPluginFrame = {
    // ready → init handshake. Sends the ready event (with probes) to the
    // host and resolves with the init event's params. Resolves once; a
    // repeat init re-resolves nothing but still fires onEvent("init").
    ready: function (probes) {
      postToHost(envelope("event", "ready", { probes: probes || null }));
      return new Promise(function (resolve) {
        if (initArrived) { resolve(initParams); return; }
        initWaiters.push(resolve);
      });
    },
    // Frame → host event (fire-and-forget).
    sendEvent: function (method, params) {
      postToHost(envelope("event", method, params || {}));
    },
    // Frame → host request → Promise (default 5s timeout). SAME contract as
    // the host side: bounded in-flight map (immediate E_SATURATED reject
    // without posting), validated timeout, late responses dropped by id.
    // Our ids are prefixed "p-…" where the host's own requests are "h-…".
    sendRequest: function (method, params, timeoutMs) {
      if (Object.keys(pending).length >= MAX_INFLIGHT) {
        return Promise.reject({
          code: "E_SATURATED",
          message: "request map saturated at " + MAX_INFLIGHT + " in flight: " + method
        });
      }
      timeoutMs = validTimeout(timeoutMs);
      return new Promise(function (resolve, reject) {
        var id = "p-" + (++reqCounter);
        var timer = setTimeout(function () {
          if (pending[id]) {
            delete pending[id];
            reject({ code: "E_TIMEOUT", message: "request " + method + " timed out" });
          }
        }, timeoutMs);
        pending[id] = { resolve: resolve, reject: reject, timer: timer };
        try {
          postToHost(envelope("request", method, params || {}, id));
        } catch (e) {
          // Mirror the broker: a non-cloneable params postMessage throw
          // must clean up the pending entry + timer, or it lingers to
          // timeout and counts toward MAX_INFLIGHT.
          clearTimeout(timer);
          delete pending[id];
          reject({ code: "E_SEND", message: "request " + method + " not sendable: " + String(e && e.message || e) });
        }
      });
    },
    // Host → frame event handler (method → handler); re-registering a
    // method overwrites. Events with no registered handler are ignored.
    onEvent: function (method, handler) {
      eventHandlers[method] = handler;
    },
    // Host → frame request handler (method → handler); re-registering a
    // method overwrites. The handler receives (params) and may return a
    // value or a Promise; a method with no handler gets an E_NO_HANDLER
    // response, a throw an E_HANDLER response.
    onRequest: function (method, handler) {
      requestHandlers[method] = handler;
    }
  };

  window.addEventListener("message", onMessage);
  // The frame document is going away — outstanding sendRequests can never
  // get a response; fail them now instead of leaking until timeout.
  window.addEventListener("pagehide", function () {
    rejectOutstanding({ code: "E_TEARDOWN", message: "page hidden" });
  });
})();
