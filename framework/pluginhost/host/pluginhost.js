/*!
 * pluginhost/host/pluginhost.js — generic host broker for GoFastr heavy-JS
 * plugins.
 *
 * Plain same-origin JavaScript (IIFE, no imports, no external URLs). Runs in
 * the HOST page with full privileges. It is the reusable core distilled out of
 * the wysiwyg plugin's broker: for each mount marker it creates an
 * opaque-origin sandboxed iframe, runs the versioned postMessage capability RPC
 * (protocol-v1.md §3/§4), and dispatches plugin-specific events to a
 * per-plugin adapter registered via window.__gofastrPluginHost.register.
 *
 * Security invariants (do NOT weaken — these ARE the D3 third-party guarantee):
 *   - iframe sandbox is ALWAYS "allow-scripts"; the same-origin sandbox token is
 *     never added (that would de-opaque the frame).
 *   - Messages are accepted only when event.source === iframe.contentWindow.
 *     We deliberately do NOT check event.origin: an opaque-origin frame's
 *     origin is the literal string "null", so origin-string checks are a trap.
 *
 * Protocol v1 is frozen (protocol-v1.md §3/§4). The envelope, the source check,
 * and the ready→init handshake are EXACTLY as the wysiwyg broker shipped them
 * in Phase 0.
 *
 * Frame → host requests (envelope type "request" from the frame) share that
 * one contract, both directions: the broker dispatches them to a per-method
 * handler registered via api.onRequest, with the adapter's static
 * registration.onRequest as fallback, and ALWAYS replies — result,
 * E_NO_HANDLER, or E_HANDLER. The pending map is bounded (MAX_INFLIGHT) and
 * teardown rejects stragglers with E_TEARDOWN. The frame-side counterpart is
 * frame/frameclient.js (window.__gofastrPluginFrame).
 *
 * Adapter contract (see pluginhost.BrokerRegistration in Go):
 *
 *   window.__gofastrPluginHost.register(name, {
 *     manifest: { entry, isolation, sandbox, capabilities, minHeight, schema, title },
 *     config:   { …plugin blob… },
 *     onEvent:  function (method, params, api) { … },
 *     onRequest: function (method, params, api) {
 *       // static fallback for frame → host requests with no explicit
 *       // api.onRequest handler; return a value or a Promise (a throw
 *       // becomes E_HANDLER). Neither handler exists → E_NO_HANDLER.
 *     }
 *   });
 *
 * The generic broker handles the protocol-level events itself (ready, resize,
 * focusChanged, themeApplied, metric, bootError), then invokes registration.onEvent
 * for EVERY inbound event so the adapter can handle its own methods AND mirror
 * the generic hooks under any plugin-specific names the tests read.
 */
(function () {
  'use strict';

  // --- Protocol constants (protocol-v1.md §3 — frozen) ----------------------
  var ENVELOPE_VERSION = 1;
  var RESPONSE_TIMEOUT_MS = 5000;  // request → response (§3)
  var TEARDOWN_TIMEOUT_MS = 200;   // SPA teardown ack budget (§6.9)
  var MAX_INFLIGHT = 64;           // pending-request bound, both directions

  var DEFAULT_CAPS = ["document:read", "document:write", "upload:images", "theme:read"];
  var DEFAULT_MIN_HEIGHT = "240px";
  var DEFAULT_TITLE = "Plugin";

  // Per-iframe instance state, keyed by the iframe element (framework
  // per-instance-state rule). `live` mirrors the set so we can iterate for
  // source lookup and SPA teardown — a WeakMap is not iterable.
  var states = new WeakMap();
  var live = [];
  var reqCounter = 0;

  // Registered adapters: plugin name → registration.
  var adapters = Object.create(null);

  // Expose the registry SYNCHRONOUSLY so adapter <script>s that follow this one
  // in document order can register before boot (both are parser-inserted, so
  // this runs before any adapter IIFE). boot() then scans the markers.
  window.__gofastrPluginHost = {
    register: function (name, registration) {
      if (!name || !registration || !registration.manifest) return;
      adapters[name] = registration;
    }
  };

  // --- Helpers --------------------------------------------------------------

  function csrfToken() {
    var meta = document.querySelector('meta[name="csrf-token"]');
    return meta && meta.getAttribute("content") ? meta.getAttribute("content") : null;
  }

  /**
   * THEME_TOKENS is the theme's canonical custom-property vocabulary,
   * mirrored from style.TokenNames() and pinned by a Go test
   * (token_bridge_test.go) so it cannot drift from the theme emitter.
   */
  var THEME_TOKENS = [
    "--breakpoint-2xl", "--breakpoint-lg", "--breakpoint-md", "--breakpoint-sm", "--breakpoint-xl",
    "--color-accent", "--color-background", "--color-border", "--color-border-strong",
    "--color-code-border", "--color-code-surface", "--color-code-text",
    "--color-danger", "--color-info", "--color-primary", "--color-primary-fg",
    "--color-secondary", "--color-secondary-fg", "--color-success",
    "--color-surface", "--color-surface-soft", "--color-text", "--color-text-muted",
    "--color-text-subtle", "--color-warning",
    "--duration-dropdown-enter", "--duration-fast", "--duration-normal",
    "--duration-overlay-enter", "--duration-overlay-exit", "--duration-slow",
    "--duration-toast-enter", "--duration-toast-exit",
    "--easing-ease-in", "--easing-ease-in-out", "--easing-ease-out", "--easing-spring",
    "--font-body", "--font-heading", "--font-mono",
    "--radii-full", "--radii-lg", "--radii-md", "--radii-none", "--radii-sm", "--radii-xl",
    "--shadow-lg", "--shadow-md", "--shadow-none", "--shadow-sm", "--shadow-xl",
    "--spacing-2xl", "--spacing-3xl", "--spacing-lg", "--spacing-md", "--spacing-sm",
    "--spacing-touch-target", "--spacing-xl", "--spacing-xs",
    "--text-2xl", "--text-3xl", "--text-base", "--text-lg", "--text-sm", "--text-xl", "--text-xs",
    "--tk-com", "--tk-fn", "--tk-kw", "--tk-num", "--tk-pn", "--tk-str", "--tk-type",
    "--z-dropdown", "--z-modal", "--z-popover", "--z-sticky", "--z-toast"
  ];

  /**
   * resolveTokens reads the canonical token set from computed style and
   * bridges every name with a non-empty value. The old implementation
   * DISCOVERED names by walking document.styleSheets, which had two
   * partial-palette failure modes: a stylesheet still parsing when the
   * frame boots contributed no names (the CI-only invisible-text race,
   * #271), and rule.type !== 1 skipped tokens declared only inside
   * @media / @supports. A partial palette is worse than none — a
   * bridged dark --color-text beside an unbridged --color-surface is
   * white-on-white. Reading a known vocabulary from computed style has
   * neither failure mode.
   */
  function resolveTokens() {
    var cs = getComputedStyle(document.documentElement);
    var tokens = {};
    for (var i = 0; i < THEME_TOKENS.length; i++) {
      var name = THEME_TOKENS[i];
      var val = cs.getPropertyValue(name);
      if (val == null) continue;
      val = String(val).trim();
      if (val !== "") tokens[name] = val;
    }
    return tokens;
  }

  function currentScheme() {
    return (document.documentElement.dataset &&
      document.documentElement.dataset.colorScheme) || "light";
  }

  function envelope(type, method, params, id) {
    var env = { v: ENVELOPE_VERSION, type: type, src: "host", result: null, error: null };
    if (id) env.id = id;
    if (method) env.method = method;
    if (params) env.params = params;
    return env;
  }

  function postTo(frame, env) {
    // The frame is an opaque origin; targetOrigin MUST be "*". The real gate
    // is the event.source === contentWindow check on both sides (§3).
    frame.contentWindow.postMessage(env, "*");
  }

  function cacheBust() {
    return Math.random().toString(36).slice(2, 10) + Date.now().toString(36);
  }

  // --- Per-marker wiring ----------------------------------------------------

  function parseCaps(marker, manifest) {
    var raw = marker.getAttribute("data-fui-plugin-capabilities");
    if (raw && raw.trim()) {
      var caps = raw.split(/[,\s]+/).filter(function (s) { return s.length > 0; });
      if (caps.length) return caps;
    }
    if (manifest && manifest.capabilities && manifest.capabilities.length) {
      return manifest.capabilities.slice();
    }
    return DEFAULT_CAPS.slice();
  }

  function parseDoc(marker) {
    var raw = marker.getAttribute("data-fui-plugin-doc");
    if (!raw) return null;
    try { return JSON.parse(raw); } catch (e) { return null; }
  }

  function minHeightFor(marker, manifest) {
    var raw = marker.getAttribute("data-fui-plugin-minheight");
    if (raw && raw.trim()) return raw.trim();
    if (manifest && manifest.minHeight) return manifest.minHeight;
    return DEFAULT_MIN_HEIGHT;
  }

  // Sandbox capability ALLOW-LIST. Keep in sync with Go's
  // allowedSandboxTokens (pinned by TestBrokerJSSandboxIsAllowList).
  //
  // It was a one-token deny-list that stripped only allow-same-origin, which
  // let "allow-popups-to-escape-sandbox" through — a popup the plugin opens
  // is then fully unsandboxed AND same-origin, so window.open('/admin/...')
  // is an ordinary cookie-bearing document. "allow-top-navigation" (retarget
  // the whole tab) and "allow-downloads" (write to the user's disk) passed
  // too. A deny-list has to enumerate every way out of the box and loses the
  // moment the HTML spec adds one; the manifest ships with the third-party
  // plugin, so it is attacker-influenced by construction.
  var ALLOWED_SANDBOX = {
    "allow-scripts": true,
    "allow-forms": true,
    "allow-modals": true,
    "allow-popups": true,          // a popup, still sandboxed — no escape token
    "allow-pointer-lock": true,
    "allow-orientation-lock": true,
    "allow-presentation": true
  };

  // sandboxFor is AUTHORITATIVE, not advisory: whatever the manifest carries,
  // the emitted sandbox always includes "allow-scripts" and never a
  // same-origin-collapsing token. This is the actual sink that sets the iframe
  // attribute, so the opaque-origin guarantee lives HERE, not in a Go-side
  // Validate() a plugin author might forget to call.
  function sandboxFor(manifest) {
    var tokens = (manifest && manifest.sandbox && manifest.sandbox.length)
      ? manifest.sandbox : [];
    var seen = {};
    var out = [];
    for (var i = 0; i < tokens.length; i++) {
      // The sandbox attribute is ASCII-case-insensitive and whitespace-
      // separated: lowercase + split each element so a case-variant or an
      // element with embedded whitespace can't smuggle a same-origin grant
      // past the filter (round-4 bypass).
      var parts = String(tokens[i] || "").toLowerCase().split(/\s+/);
      for (var j = 0; j < parts.length; j++) {
        var tok = parts[j];
        if (!tok || ALLOWED_SANDBOX[tok] !== true || seen[tok]) continue;
        seen[tok] = true;
        out.push(tok);
      }
    }
    if (!seen["allow-scripts"]) out.unshift("allow-scripts");
    return out.join(" ");
  }

  function titleFor(manifest) {
    return (manifest && manifest.title) || DEFAULT_TITLE;
  }

  // safeEntry requires the frame document URL to be a same-origin absolute
  // path, returning "" for anything else. Mirrors Go's validateEntry.
  //
  // The opaque-origin guarantee has two carriers: this sandbox attribute, and
  // the `Content-Security-Policy: sandbox allow-scripts` header AssetServer
  // emits for the assets IT serves. A cross-origin or scheme-bearing entry
  // escapes the second entirely — nothing the host controls emits headers for
  // someone else's document. This is the sink that sets src, so the check
  // lives HERE too, not only in a Go-side Validate() a plugin author may skip.
  function safeEntry(raw) {
    var entry = String(raw || "");
    if (!entry) return "";
    if (/[\u0000-\u001f\u007f]/.test(entry)) return "";
    // Normalise backslashes the way a browser does before reading the
    // authority: "/\\evil.example/x" is "//evil.example/x".
    var norm = entry.replace(/\\/g, "/");
    if (norm.charAt(0) !== "/") return "";   // scheme or relative path
    if (norm.charAt(1) === "/") return "";   // protocol-relative
    return entry;
  }

  function createIframe(marker, manifest) {
    var entry = safeEntry(manifest && manifest.entry);
    var frame = document.createElement("iframe");
    if (!entry) {
      // Fail closed: no src at all beats loading an off-origin document into
      // a frame the host is about to hand a capability grant.
      frame.setAttribute("sandbox", sandboxFor(manifest));
      frame.setAttribute("title", titleFor(manifest));
      marker.appendChild(frame);
      return frame;
    }
    frame.setAttribute("src", entry + (entry.indexOf("?") === -1 ? "?" : "&") + "v=" + cacheBust());
    // SECURITY: "allow-scripts" ONLY — the same-origin token is never added.
    frame.setAttribute("sandbox", sandboxFor(manifest));
    frame.setAttribute("referrerpolicy", "no-referrer");
    frame.setAttribute("title", titleFor(manifest));
    frame.style.height = minHeightFor(marker, manifest);
    frame.style.width = "100%";
    frame.style.border = "0";
    frame.style.display = "block";
    marker.appendChild(frame);
    return frame;
  }

  function createState(marker, frame, adapter) {
    return {
      marker: marker,
      frame: frame,
      adapter: adapter,
      capabilities: parseCaps(marker, adapter.manifest),
      docId: marker.getAttribute("data-fui-plugin-docid") || "demo",
      pending: {},            // id -> {resolve, reject, timer}
      requestHandlers: Object.create(null), // method -> frame→host handler
      ready: false,
      focused: false,
      lastMetric: null,
      theme: null,
      observer: null,
      api: null
    };
  }

  // A non-positive / NaN / infinite timeoutMs is not a timeout — fall back
  // to the protocol default instead of handing garbage to setTimeout (a
  // negative value fires "immediately", NaN never fires at all).
  function validTimeout(timeoutMs) {
    return (typeof timeoutMs === "number" && isFinite(timeoutMs) && timeoutMs > 0)
      ? timeoutMs
      : RESPONSE_TIMEOUT_MS;
  }

  // Host → frame request expecting a response (resolves on matched id).
  // The pending map is BOUNDED: a wedged frame that never responds would
  // otherwise grow it without limit (one timer + closure per request).
  function request(st, method, params, timeoutMs) {
    if (Object.keys(st.pending).length >= MAX_INFLIGHT) {
      return Promise.reject({
        code: "E_SATURATED",
        message: "request map saturated at " + MAX_INFLIGHT + " in flight: " + method
      });
    }
    timeoutMs = validTimeout(timeoutMs);
    return new Promise(function (resolve, reject) {
      var id = "h-" + (++reqCounter);
      var timer = setTimeout(function () {
        if (st.pending[id]) {
          delete st.pending[id];
          reject({ code: "E_TIMEOUT", message: "request " + method + " timed out" });
        }
      }, timeoutMs);
      st.pending[id] = { resolve: resolve, reject: reject, timer: timer };
      postTo(st.frame, envelope("request", method, params || {}, id));
    });
  }

  function buildApi(st) {
    var form = st.marker.closest ? st.marker.closest("form") : null;
    return {
      iframe: st.frame,
      marker: st.marker,
      form: form,
      csrfToken: csrfToken,
      // Host → frame event (fire-and-forget).
      sendEvent: function (method, params) {
        postTo(st.frame, envelope("event", method, params || {}));
      },
      // Host → frame request → Promise (5s timeout, bounded in flight).
      request: function (method, params, timeoutMs) {
        return request(st, method, params, timeoutMs);
      },
      // Frame → host request handler registration (method → handler);
      // re-registering a method overwrites. The handler receives
      // (params, api) and may return a value or a Promise.
      onRequest: function (method, handler) {
        st.requestHandlers[method] = handler;
      }
    };
  }

  function sendInit(st) {
    st.ready = true;
    st.frame.__pluginReady = true; // generic handshake signal
    var m = st.adapter.manifest || {};
    var env = envelope("event", "init", {
      doc: parseDoc(st.marker),
      markdown: null,
      tokens: resolveTokens(),
      scheme: currentScheme(),
      capabilities: st.capabilities,
      schemaVersion: m.schema || null,
      config: st.adapter.config || {}
    });
    postTo(st.frame, env);
  }

  // --- Inbound dispatch (frame → host) --------------------------------------

  function handleEvent(st, method, params) {
    params = params || {};
    // 1. Generic, protocol-level handling.
    switch (method) {
      case "ready":
        st.frame.__pluginProbes = params.probes || null; // §8a
        sendInit(st);
        break;
      case "themeApplied":
        st.theme = params;                       // {scheme, sample:{--name:value}}
        st.frame.__pluginTheme = params;
        break;
      case "resize":
        if (params.height != null) {
          // The frame reports height as a NUMBER (Math.ceil(px)). style.height
          // needs a unit — assigning a bare number is invalid CSS and silently
          // ignored, which would pin the frame at its initial height forever.
          st.frame.style.height =
            typeof params.height === "number" ? params.height + "px" : String(params.height);
        }
        break;
      case "focusChanged":
        st.focused = !!params.focused;
        break;
      case "metric":
        st.lastMetric = params;
        st.frame.__pluginLastMetric = params;    // readable by host tests
        break;
      case "bootError":
        // Frame failed to boot (e.g. a script load error inside the sandbox).
        if (typeof console !== "undefined" && console.error) {
          console.error("[pluginhost] bootError from", st.adapter && st.adapter.name, params);
        }
        break;
      default:
        // Unknown-to-platform event — leave to the adapter (forward-compat).
        break;
    }
    // 2. Plugin-specific handling + hook mirroring. The adapter sees EVERY
    //    event (incl. generic ones) so it can mirror __plugin* → plugin-named
    //    hooks the tests read, and handle its own methods (docChanged, save, …).
    if (st.adapter && typeof st.adapter.onEvent === "function") {
      try { st.adapter.onEvent(method, params, st.api); } catch (e) {
        if (typeof console !== "undefined" && console.error) {
          console.error("[pluginhost] adapter onEvent threw", e);
        }
      }
    }
  }

  // Frame → host request dispatch. An explicit handler (api.onRequest) wins;
  // the adapter's static registration.onRequest(method, params, api) is the
  // fallback for methods with no explicit handler. NEITHER exists → error
  // response, never silence: the frame holds a pending entry per request id
  // and a dropped reply would hang it until its own timeout.
  function reply(st, id, result, err) {
    // The frame may have been torn down while a handler's Promise was in
    // flight — send the response anyway so its pending map can settle. A
    // DETACHED frame has a null contentWindow: nobody can receive the reply,
    // so drop silently in exactly that case.
    if (!st.frame.contentWindow) return;
    var env = envelope("response", null, null, id);
    if (err) env.error = err;
    else env.result = result;
    try {
      postTo(st.frame, env);
    } catch (e) {
      // Detached between the liveness check and the post — nothing left to
      // deliver to.
    }
  }

  function handleRequest(st, msg) {
    var handler = st.requestHandlers[msg.method];
    var run;
    if (typeof handler === "function") {
      run = function () { return handler(msg.params || {}, st.api); };
    } else if (st.adapter && typeof st.adapter.onRequest === "function") {
      run = function () { return st.adapter.onRequest(msg.method, msg.params || {}, st.api); };
    } else {
      reply(st, msg.id, null, {
        code: "E_NO_HANDLER",
        message: "no handler for " + msg.method
      });
      return;
    }
    // Promise.resolve().then(run): a synchronous throw in the handler
    // becomes a rejection instead of escaping the message dispatch.
    Promise.resolve().then(run).then(
      function (result) { reply(st, msg.id, result, null); },
      function (err) {
        reply(st, msg.id, null, {
          code: "E_HANDLER",
          message: String(err && err.message || err)
        });
      }
    );
  }

  function onMessage(event) {
    // Find the instance this message came from. We accept ONLY messages whose
    // source is one of our iframe contentWindows — NOT event.origin (§3).
    var st = null;
    for (var i = 0; i < live.length; i++) {
      if (live[i].frame.contentWindow === event.source) { st = live[i]; break; }
    }
    if (!st) return;
    var msg = event.data;
    if (!msg || typeof msg !== "object") return;
    if (msg.v !== ENVELOPE_VERSION) return;
    if (msg.src !== "plugin") return;

    if (msg.type === "response") {
      var p = st.pending[msg.id];
      if (!p) return;
      clearTimeout(p.timer);
      delete st.pending[msg.id];
      if (msg.error) p.reject(msg.error);
      else p.resolve(msg.result);
      return;
    }
    if (msg.type === "request") {
      handleRequest(st, msg);
      return;
    }
    if (msg.type === "event") {
      handleEvent(st, msg.method, msg.params);
    }
  }

  // --- Theme sync (§7) ------------------------------------------------------

  function observeTheme(st) {
    if (typeof MutationObserver !== "function") return;
    var obs = new MutationObserver(function () {
      // data-color-scheme on <html> flipped — re-resolve and re-bridge.
      postTo(st.frame, envelope("event", "themeChanged", {
        scheme: currentScheme(),
        tokens: resolveTokens()
      }));
    });
    obs.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["data-color-scheme"]
    });
    st.observer = obs;
  }

  // --- Teardown (SPA nav — §6.9) -------------------------------------------

  function cleanup(st) {
    if (st.observer) st.observer.disconnect();
    var parent = st.frame.parentNode;
    if (parent) parent.removeChild(st.frame);
    var i = live.indexOf(st);
    if (i !== -1) live.splice(i, 1);
    states["delete"](st.frame);
    // Reject any pending requests so no promise leaks unresolved forever,
    // then clear the map — callers get E_TEARDOWN, not a hang.
    for (var id in st.pending) {
      if (Object.prototype.hasOwnProperty.call(st.pending, id)) {
        clearTimeout(st.pending[id].timer);
        st.pending[id].reject({ code: "E_TEARDOWN", message: "instance torn down" });
      }
    }
    st.pending = {};
  }

  function teardownInstance(st) {
    if (!st.ready) { cleanup(st); return; }
    request(st, "teardown", {}, TEARDOWN_TIMEOUT_MS).then(function () {
      cleanup(st);
    }, function () {
      // Timeout (≤200ms) — remove anyway, no leak.
      cleanup(st);
    });
  }

  function onNavigate() {
    var snap = live.slice(); // snapshot before mutating during iteration
    for (var i = 0; i < snap.length; i++) teardownInstance(snap[i]);
  }

  // --- Boot -----------------------------------------------------------------

  function mountMarker(marker) {
    var name = marker.getAttribute("data-fui-plugin");
    var adapter = adapters[name];
    if (!adapter) {
      if (typeof console !== "undefined" && console.warn) {
        console.warn("[pluginhost] no adapter registered for plugin '" + name + "'; mount skipped");
      }
      return;
    }
    var frame = createIframe(marker, adapter.manifest);
    var st = createState(marker, frame, adapter);
    st.api = buildApi(st);
    states.set(frame, st);
    live.push(st);
    observeTheme(st);
  }

  function boot() {
    var markers = document.querySelectorAll("[data-fui-plugin]");
    for (var i = 0; i < markers.length; i++) mountMarker(markers[i]);
    window.addEventListener("message", onMessage);
    window.addEventListener("gofastr:navigate", onNavigate);
    // Host-interaction relay (additive protocol event `hostPointerdown`, no
    // params): a pointerdown on the HOST page reaches the frame as neither a
    // pointer event nor — on iOS WebKit — a window blur, so a plugin's open
    // overlays (menus, toolbars) would have no way to notice and dismiss.
    // Relay it to every mounted frame whose element wasn't the target; frames
    // that don't handle the method ignore it by protocol rule (§4 unknown
    // method → ignore).
    document.addEventListener(
      "pointerdown",
      function (e) {
        for (var i = 0; i < live.length; i++) {
          var st = live[i];
          if (st.frame && e.target !== st.frame) {
            st.api.sendEvent("hostPointerdown", {});
          }
        }
      },
      true
    );
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", boot);
  } else {
    boot();
  }
})();
