// signals.js: signal store + binding (spec fragment `signals`, marker class; deps: kernel).
// Owns: setSignal, signal binding/broadcast, the SSR seed read + application,
// the signal-set/inc/toggle read-modify-write path, aria injection.
  // isReservedSignalKey rejects the JS object keys that, when used as a
  // dynamic property name on the _signals store, mutate the store's
  // prototype chain instead of creating an own data property:
  //   store["__proto__"] = {…}   // invokes the __proto__ setter
  //   store["constructor"]/["prototype"] // shadow built-ins
  // A seed (full-load or partial) carrying such a key would re-parent the
  // _signals object, every not-yet-set signal name would then resolve
  // through the attacker object (cross-signal confusion) and setSignal
  // would mutate the shared prototype. Seed keys are server-controlled
  // today; this is advisory-recommended defense-in-depth (strip
  // __proto__/constructor/prototype before merging). Used by all three
  // seed-merge loops (boot seed + mergeSeedFromDOM page/global).
  const isReservedSignalKey = (k) =>
    k === '__proto__' || k === 'constructor' || k === 'prototype';


  // signals namespace members. _signals is the signal store; setSignal is the
  // write path every signal mutation flows through (prototype-pollution guard,
  // html/attr/text render modes, after-update flash + scroll-to-bottom hooks).
  // MUST run before the seed application below, which reads window.__gofastr._signals.
  Object.assign(window.__gofastr, {
    _signals: {},

    /** Read the current value of a named signal. Returns undefined for
        unset signals. Used by data-fui-signal-inc and data-fui-signal-toggle
        to read-modify-write without an RPC round-trip. */
    getSignal(name) {
      const s = own(this._signals, name) ? this._signals[name] : undefined;
      return s ? s.value : undefined;
    },
    /** Push a value into a named signal and reflect it into all
        [data-fui-signal="<name>"] DOM nodes. Mode is read from the
        node's data-fui-signal-mode attr ("text" default, "html",
        "attr"+data-fui-signal-attr). */
    setSignal(name, value, opts) {
      // Prototype pollution: the reserved-key guard used to live only in
      // the three seed-merge loops, but attribute-controlled keys enter
      // HERE, data-fui-signal-set/-inc/-toggle, and fetched-JSON keys
      // from poll.js and widgets.js. `__proto__:POLLUTED` re-parents the
      // store, and because getSignal is `s ? s.value : undefined` EVERY
      // unset signal then reads back the attacker's value. Pure data
      // corruption, so no CSP stops it; the guard belongs at the write.
      if (isReservedSignalKey(name)) {
        console.warn('[gofastr] refused reserved signal name:', name);
        return;
      }
      let s = own(this._signals, name) ? this._signals[name] : undefined;
      if (!s) { s = this._signals[name] = { value: undefined, listeners: [] }; }
      // opts.untrusted marks a value that came from the URL (the
      // deep-link seed in widgets.js). Recorded on the signal so the
      // html render mode below refuses to treat it as markup, and reset
      // whenever the same signal is next set from a trusted source.
      s.untrusted = !!(opts && opts.untrusted);
      s.value = value;
      for (const fn of s.listeners) {
        try { fn(value); } catch (_) {}
      }
      // Escape the signal name before it enters the selector, a name
      // containing selector metacharacters (e.g. '"]') would otherwise
      // produce an invalid selector and querySelectorAll would THROW,
      // taking setSignal (and every listener it drives) down with it.
      // Same shape as sse.js:76.
      document.querySelectorAll('[data-fui-signal="' + CSS.escape(String(name)) + '"]').forEach((node) => {
        const mode = node.getAttribute('data-fui-signal-mode') || 'text';
        if (mode === 'html') {
          // The html escape hatch is for TRUSTED HTML *strings* only.
          // On a non-2xx response dispatchRPC broadcasts the auto-built
          // error object {ok:false,status,text} into the signal. That
          // object is NOT trusted HTML, and applying it here (via
          // innerHTML OR textContent) would either execute reflected
          // markup or overwrite the existing trusted region with a
          // JSON blob, corrupting the UI on every failed RPC. The
          // documented optimistic-UI invariant, "a failed delete
          // leaves the row/list unchanged", depends on this no-op.
          // Text-mode nodes below still render a human-readable
          // "Error: …" string, so failure feedback is not lost.
          if (typeof value !== 'string') return;
          // A value seeded from location.search is attacker-supplied by
          // construction, `?x=<img onerror=…>` on any page carrying an
          // html-mode binding of `x`. Render it as text instead. The
          // value still reaches the node; it just stops being markup.
          if (s.untrusted) { node.textContent = value; return; }
          node.innerHTML = value;
          window.__gofastr.scanAndLoadCSS(node);
          // Wire any toast items the freshly-swapped HTML brought in.
          // Awaits the toasts module, when an island-driven update
          // injects a toast for the first time, the module loads,
          // then _initToasts runs against the new content.
          if (node.querySelector && node.querySelector('[data-fui-toast-id]')) {
            window.__gofastr.loadModule('toasts').then(() => {
              window.__gofastr._initToasts(node);
            }).catch(() => {});
          }
        } else if (mode === 'attr') {
          const attr = node.getAttribute('data-fui-signal-attr') || 'value';
          // The attribute NAME is developer-supplied and server-
          // rendered, so the allow-list that keeps a signal out of
          // `srcdoc` / `style` / `on*` lives in Go, at the emitters
          // (core-ui/store.SignalAttrAllowed), refusing to render the
          // binding beats warning about it after it shipped, and costs
          // the runtime no bytes. The value guard below still runs on
          // every client-side update.
          let v = String(value ?? '');
          // URL-bearing attrs (href / src / action / xlink:href /
          // formaction): reject dangerous schemes (javascript:,
          // vbscript:, data: except data:image/*). Stops a signal-
          // driven anchor (e.g. Lightbox AllowDownload) from
          // executing arbitrary JS when an attacker controls the
          // signal value via a query-string deeplink param.
          if (window.__gofastr._isUnsafeSignalUrl(attr, v)) v = '';
          node.setAttribute(attr, v);
          // Tabs (framework/ui.Tabs): when the wrapper's data-active
          // index changes, mirror it into aria-selected on the strip's
          // role=tab buttons, CSS keys the visual highlight off
          // data-active, but assistive tech reads aria-selected.
          if (attr === 'data-active') {
            node.querySelectorAll('[role="tab"][data-fui-tab-index]').forEach((b) => {
              b.setAttribute('aria-selected', String(b.getAttribute('data-fui-tab-index') === v));
            });
          }
        } else {
          // Task B: when the value is an error object from dispatchRPC
          // ({ok:false, status, text}), render it as a human-readable
          // string instead of raw JSON so users see "Error: 500" rather
          // than {"ok":false,"status":500,"text":"..."}.
          if (value != null && typeof value === 'object' && value.ok === false) {
            const s = value.status ? String(value.status) : 'unknown';
            const t = value.text ? String(value.text).substring(0, 200) : '';
            node.textContent = 'Error: ' + s + (t ? ' \u2014 ' + t : '');
          } else if (value == null) {
            node.textContent = '';
          } else if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') {
            node.textContent = String(value);
          } else {
            node.textContent = JSON.stringify(value);
          }
        }
        // After-update hook: brief flash to signal the value changed.
        // Useful for headers/badges where the user might miss an
        // update otherwise. Duration overridable via
        // data-fui-flash-duration-ms; default 600ms.
        // Task D: skip the flash when the user prefers reduced motion.
        if (node.hasAttribute('data-fui-flash-on-update')) {
          const prefersReduced = window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
          if (!prefersReduced) {
            const dur = parseInt(node.getAttribute('data-fui-flash-duration-ms') || '600', 10);
            node.classList.remove('fui-flash');
            // Force reflow so the next add re-runs the animation.
            // eslint-disable-next-line no-unused-expressions
            node.offsetWidth;
            node.classList.add('fui-flash');
            setTimeout(() => node.classList.remove('fui-flash'), dur);
          }
        }
        // After-update hook: scroll a container to bottom so streaming
        // chat logs / live tails surface new content without manual
        // scrolling. Opt-in via data-fui-scroll-bottom-on-update on
        // the signal node itself or the resolved selector target.
        if (node.hasAttribute('data-fui-scroll-bottom-on-update')) {
          const sel = node.getAttribute('data-fui-scroll-bottom-on-update');
          const target = sel ? node.querySelector(sel) || document.querySelector(sel) || node : node;
          // Defer to end of microtask so the new innerHTML lays out first.
          Promise.resolve().then(() => { try { target.scrollTop = target.scrollHeight; } catch (_) {} });
        }
      });
    },

    /** Read the current value of a named signal. */
    signal(name) {
      return (own(this._signals, name) ? this._signals[name] : undefined)?.value;
    },
  });

  // Client signal mutations are core behavior. Keep this delegated listener
  // outside RPC so tabs, counters, and toggles work before any network module
  // loads. Widget roots retain their own event ownership.
  document.addEventListener('click', (e) => {
    if (e.target.closest('[data-fui-widget]')) return;
    const node = e.target.closest('[data-fui-signal-set],[data-fui-signal-inc],[data-fui-signal-toggle]');
    if (!node) return;
    e.preventDefault();
    const G = window.__gofastr;

    const set = node.getAttribute('data-fui-signal-set');
    if (set) {
      const sep = set.indexOf(':');
      if (sep > 0) G.setSignal(set.substring(0, sep), set.substring(sep + 1));
    }

    const inc = node.getAttribute('data-fui-signal-inc');
    if (inc) {
      const sep = inc.indexOf(':');
      const name = sep > 0 ? inc.substring(0, sep) : inc;
      const delta = sep > 0 ? Number(inc.substring(sep + 1)) : 1;
      G.setSignal(name, (Number(G.getSignal(name)) || 0) + delta);
    }

    const toggle = node.getAttribute('data-fui-signal-toggle');
    if (toggle) {
      const current = G.getSignal(toggle);
      G.setSignal(toggle, !current || current === 'false' || current === '0');
    }
  });


  // Apply the SSR signal seed (stashed above) to the signal store BEFORE
  // hydration. Existing in-memory values win (the seed never clobbers a
  // value already mutated on the client, relevant for app-global slices
  // across SPA navigations); fresh names are created with no listeners.
  if (window.__gofastr_signals_seed) {
    const store = window.__gofastr._signals;
    const seed = window.__gofastr_signals_seed;
    for (const k in seed) {
      if (!Object.prototype.hasOwnProperty.call(seed, k)) continue;
      if (isReservedSignalKey(k)) continue;
      if (store[k]) {
        store[k].value = seed[k];
      } else {
        store[k] = { value: seed[k], listeners: [] };
      }
    }
  }
