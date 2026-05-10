// core-ui/widget bootstrap loader.
// Per-widget instances are produced by server.go via two substitutions:
//   __FUI_CONFIG__  → JSON object: { name, position, backdrop, … sse[] }
//   __FUI_CHROME__  → JSON-encoded HTML string for the widget chrome
//                     (host's slots already rendered server-side)
//
// Behavior:
//   1. Load the per-widget stylesheet by appending a <link>.
//   2. Append the chrome HTML to <body> (and a backdrop when configured).
//   3. Hydrate state by GET /core-ui/widget/<name>/state, populate signals.
//   4. Subscribe to each SSE binding; on event push payload into signal.
//   5. Wire data-fui-rpc click handlers to POST endpoints.
//   6. Wire data-fui-action="close" to dismiss; respect closeOnEscape +
//      closeOnClickOutside flags from config.
//
// Idempotent: a widget already mounted (same name) won't be remounted.
// CSP-safe: no inline event handlers, no inline styles. Everything runs
// from this script bundle and the styles in the linked stylesheet.
(function () {
  const cfg = __FUI_CONFIG__;
  const chrome = __FUI_CHROME__;
  const NS = (window.__fui = window.__fui || { widgets: {}, signals: {} });

  if (NS.widgets[cfg.name]) return; // already mounted
  NS.widgets[cfg.name] = { cfg };

  // ---- helpers ------------------------------------------------------
  function el(tag, attrs, html) {
    const e = document.createElement(tag);
    if (attrs) for (const k in attrs) e.setAttribute(k, attrs[k]);
    if (html != null) e.innerHTML = html;
    return e;
  }

  // signals: name -> { value, listeners[] }
  function signal(name) {
    let s = NS.signals[name];
    if (!s) {
      s = NS.signals[name] = { value: undefined, listeners: [] };
    }
    return s;
  }
  function setSignal(name, value) {
    const s = signal(name);
    s.value = value;
    for (const fn of s.listeners) {
      try { fn(value); } catch (_) {}
    }
    // Reflect into [data-fui-signal="name"] elements.
    document.querySelectorAll('[data-fui-signal="' + name + '"]').forEach((node) => {
      const mode = node.getAttribute("data-fui-signal-mode") || "text";
      if (mode === "html") node.innerHTML = stringifyForHTML(value);
      else if (mode === "attr") {
        const attr = node.getAttribute("data-fui-signal-attr") || "value";
        node.setAttribute(attr, String(value ?? ""));
      } else {
        node.textContent = stringifyForText(value);
      }
    });
  }
  function stringifyForText(v) {
    if (v == null) return "";
    if (typeof v === "string" || typeof v === "number" || typeof v === "boolean") return String(v);
    return JSON.stringify(v);
  }
  function stringifyForHTML(v) {
    if (v == null) return "";
    if (typeof v === "string") return v; // host wrote markup; trust it
    return stringifyForText(v);
  }

  NS.signal = signal;
  NS.setSignal = setSignal;

  // ---- mount stylesheet --------------------------------------------
  if (!document.querySelector('link[data-fui-style="' + cfg.name + '"]')) {
    const link = el("link", {
      rel: "stylesheet",
      href: cfg.stylePath,
      "data-fui-style": cfg.name,
    });
    document.head.appendChild(link);
  }

  // ---- mount chrome -------------------------------------------------
  let backdrop = null;
  if (cfg.backdrop) {
    backdrop = el("div", { class: "fui-backdrop", "data-fui-backdrop": cfg.name });
    document.body.appendChild(backdrop);
  }
  const root = document.createElement("div");
  root.innerHTML = chrome;
  // chrome is a single <div class="fui-widget …"> — append its first child.
  const widgetEl = root.firstElementChild;
  document.body.appendChild(widgetEl);
  NS.widgets[cfg.name].root = widgetEl;
  NS.widgets[cfg.name].backdrop = backdrop;

  function dismiss() {
    if (widgetEl && widgetEl.parentNode) widgetEl.parentNode.removeChild(widgetEl);
    if (backdrop && backdrop.parentNode) backdrop.parentNode.removeChild(backdrop);
    delete NS.widgets[cfg.name];
  }
  NS.widgets[cfg.name].dismiss = dismiss;

  // ---- initial state ------------------------------------------------
  fetch(cfg.statePath, { headers: { "X-FUI-Widget": cfg.name } })
    .then((r) => (r.ok ? r.json() : {}))
    .then((state) => {
      for (const k in state) setSignal(k, state[k]);
    })
    .catch(() => {});

  // ---- SSE bindings -------------------------------------------------
  const seenStreams = {};
  for (const b of cfg.sse || []) {
    if (!seenStreams[b.path]) {
      try {
        seenStreams[b.path] = new EventSource(b.path);
      } catch (_) {
        seenStreams[b.path] = null;
      }
    }
    const es = seenStreams[b.path];
    if (!es) continue;
    es.addEventListener(b.event, (ev) => {
      let payload;
      try { payload = JSON.parse(ev.data); } catch (_) { payload = ev.data; }
      setSignal(b.signal, payload);
    });
  }

  // ---- RPC dispatch (event delegation, scoped to the widget) -------
  widgetEl.addEventListener("click", async (e) => {
    const btn = e.target.closest("[data-fui-rpc]");
    if (btn && widgetEl.contains(btn)) {
      e.preventDefault();
      await dispatchRPC(btn);
      return;
    }
    // close action
    const closeBtn = e.target.closest('[data-fui-action="close"]');
    if (closeBtn && widgetEl.contains(closeBtn)) {
      e.preventDefault();
      dismiss();
      return;
    }
  });
  widgetEl.addEventListener("submit", async (e) => {
    const form = e.target.closest("form[data-fui-rpc]");
    if (form && widgetEl.contains(form)) {
      e.preventDefault();
      await dispatchRPC(form);
    }
  });

  // close on backdrop click
  if (cfg.closeOnClick && backdrop) {
    backdrop.addEventListener("click", dismiss);
  }
  // close on Escape
  if (cfg.closeOnEscape) {
    document.addEventListener("keydown", (e) => {
      if (e.key === "Escape" && document.body.contains(widgetEl)) dismiss();
    });
  }

  async function dispatchRPC(node) {
    const path = node.getAttribute("data-fui-rpc");
    const method = (node.getAttribute("data-fui-rpc-method") || "POST").toUpperCase();
    const responseSignal = node.getAttribute("data-fui-rpc-signal");
    let body = node.getAttribute("data-fui-rpc-body");
    if (!body && node.tagName === "FORM") {
      const fd = new FormData(node);
      const obj = {};
      fd.forEach((v, k) => { obj[k] = v; });
      body = JSON.stringify(obj);
    }
    const headers = { "X-FUI-Widget": cfg.name };
    if (body) headers["Content-Type"] = "application/json";
    if (node.tagName === "BUTTON" || node.tagName === "INPUT") node.disabled = true;
    try {
      const r = await fetch(path, { method, headers, body: body || undefined });
      if (!r.ok) {
        const txt = await r.text();
        if (responseSignal) setSignal(responseSignal, { ok: false, status: r.status, text: txt });
        return;
      }
      const ct = r.headers.get("content-type") || "";
      const data = ct.indexOf("application/json") >= 0 ? await r.json() : await r.text();
      if (responseSignal) setSignal(responseSignal, data);
    } finally {
      if (node.tagName === "BUTTON" || node.tagName === "INPUT") node.disabled = false;
    }
  }

  // expose for tests + integrators.
  NS.signal = signal;
  NS.dispatchRPC = dispatchRPC;
})();
