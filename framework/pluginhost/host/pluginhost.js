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
 * Adapter contract (see pluginhost.BrokerRegistration in Go):
 *
 *   window.__gofastrPluginHost.register(name, {
 *     manifest: { entry, isolation, sandbox, capabilities, minHeight, schema, title },
 *     config:   { …plugin blob… },
 *     onEvent:  function (method, params, api) { … }
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
   * resolveTokens enumerates the --* custom properties the host emits on
   * :root / html and reads each RESOLVED value via getComputedStyle. Cross-
   * origin stylesheets throw on cssRules access; those are wrapped in
   * try/catch and skipped. The resulting map is bridged verbatim into the
   * frame (§7), which writes its own :root block from it.
   */
  function resolveTokens() {
    var names = {};
    var sheets = document.styleSheets;
    for (var i = 0; i < sheets.length; i++) {
      var rules;
      try {
        rules = sheets[i].cssRules;
      } catch (e) {
        // Cross-origin sheet (SecurityError) — skip, never bridge foreign CSS.
        continue;
      }
      if (!rules) continue;
      for (var j = 0; j < rules.length; j++) {
        var rule = rules[j];
        if (!rule || rule.type !== 1) continue; // STYLE_RULE
        var sel = rule.selectorText || "";
        // Only :root / html scoped custom properties bridge across the frame.
        if (sel.indexOf(":root") === -1 && sel.indexOf("html") === -1) continue;
        var decls = rule.style;
        if (!decls) continue;
        for (var k = 0; k < decls.length; k++) {
          var prop = decls[k];
          if (prop.indexOf("--") === 0) names[prop] = true;
        }
      }
    }
    var cs = getComputedStyle(document.documentElement);
    var tokens = {};
    for (var name in names) {
      if (!Object.prototype.hasOwnProperty.call(names, name)) continue;
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

  // Same-origin-collapsing sandbox tokens: these hand the framed document
  // access back to the host origin (DOM/cookies/storage) and are stripped
  // UNCONDITIONALLY. Keep in sync with Go's sameOriginCollapsingTokens.
  var SAME_ORIGIN_COLLAPSING = { "allow-same-origin": true };

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
        if (!tok || SAME_ORIGIN_COLLAPSING[tok] || seen[tok]) continue;
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

  function createIframe(marker, manifest) {
    var entry = (manifest && manifest.entry) || "";
    var frame = document.createElement("iframe");
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
      ready: false,
      focused: false,
      lastMetric: null,
      theme: null,
      observer: null,
      api: null
    };
  }

  // Host → frame request expecting a response (resolves on matched id).
  function request(st, method, params, timeoutMs) {
    return new Promise(function (resolve, reject) {
      var id = "h-" + (++reqCounter);
      var timer = setTimeout(function () {
        if (st.pending[id]) {
          delete st.pending[id];
          reject({ code: "E_TIMEOUT", message: "request " + method + " timed out" });
        }
      }, timeoutMs || RESPONSE_TIMEOUT_MS);
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
      // Host → frame request → Promise (5s timeout).
      request: function (method, params, timeoutMs) {
        return request(st, method, params, timeoutMs);
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
    // Clear any pending requests so nothing leaks.
    for (var id in st.pending) {
      if (Object.prototype.hasOwnProperty.call(st.pending, id)) {
        clearTimeout(st.pending[id].timer);
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
