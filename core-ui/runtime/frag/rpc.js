// rpc.js — RPC dispatch (spec fragment `rpc`, marker class; deps: kernel).
// Owns: dispatchRPC, the click+submit+input delegators, form encoding, CSRF
// meta read, the rpc-* response-header handling, _originOK/_sameOrigin guards.

  // -----------------------------------------------------------------------
  // Module-level RPC dispatcher — installed ONCE at script load.
  //
  // Why module-level: islands fire RPCs from anywhere on the page, not
  // just inside a mounted widget. The model (see core-ui/ARCHITECTURE.md)
  // requires `data-fui-rpc` to work without any widget setup. Each
  // mounted widget still has its own RPC handler so widget-scoped
  // close/reset behavior keeps working — but the global path is the
  // baseline, always available.
  //
  // Response semantics:
  //   - body  → broadcast to data-fui-rpc-signal (text or JSON)
  //   - X-Gofastr-Push-State header → apply via history.pushState,
  //     update currentPath. NO re-fetch — URL update only.
  //
  // X-FUI-Widget header is set when the button lives inside a
  // data-fui-widget context, omitted otherwise. The server doesn't
  // require it.
  // -----------------------------------------------------------------------
  // Per-signal abort controllers so rapid clicks targeting the same
  // signal-bound region don't race. Each new dispatch aborts the
  // previous in-flight one — last-click wins by the time the runtime
  // sees the response, not by network arrival order. This is what
  // pagination spam-click protection needs: 10 clicks ending on page
  // 1 must settle on page 1, not whichever response landed last.
  const _rpcInFlight = new Map(); // signal name → AbortController

  function _csrf(headers) {
    const token = document.querySelector('meta[name="csrf-token"]')?.content;
    if (token) headers['X-CSRF-Token'] = token;
    return headers;
  }

  // dispatchRPC is the ONE RPC dispatch implementation, shared by the
  // global click/submit delegators below AND by the widget-scoped
  // dispatcher in src/widgets.js (which calls window.__gofastr.dispatchRPC
  // with no opts — widget context is derived from the DOM). One impl means
  // every rpc-* primitive (confirm, push-state, after-*, GET-encoding,
  // abort-dedup, scroll-to) reaches both paths; the widget copy used to
  // fork and drift.
  async function dispatchRPC(node) {
    const path = node.getAttribute('data-fui-rpc');
    const method = (node.getAttribute('data-fui-rpc-method') || 'POST').toUpperCase();
    const responseSignal = node.getAttribute('data-fui-rpc-signal');
    const closeOnSuccess = node.hasAttribute('data-fui-rpc-close');
    const resetOnSuccess = node.hasAttribute('data-fui-rpc-reset') && node.tagName === 'FORM';

    // Pre-flight confirm for destructive RPCs (delete, revoke, drop).
    // MUST run before the in-flight abort/setup below: the widget path
    // used to skip this entirely (a destructive drawer delete fired
    // unconfirmed), and even on the global path a cancel left a stale
    // AbortController behind — the early return here preceded the
    // try/finally that owns _rpcInFlight, so the slot leaked and the
    // previous request was aborted for nothing.
    const confirmMsg = node.getAttribute('data-fui-confirm');
    if (confirmMsg && typeof window.confirm === 'function') {
      if (!window.confirm(confirmMsg)) return;
    }

    // Abort any in-flight dispatch for this signal. The previous fetch
    // rejects with AbortError; we ignore that branch below. Per-signal
    // dedup so rapid clicks on the same region settle on the last
    // response (pagination spam-click protection) — last-click wins by
    // the time the runtime sees the response, not by arrival order.
    if (responseSignal) {
      const prev = _rpcInFlight.get(responseSignal);
      if (prev) prev.abort();
    }
    const ctl = new AbortController();
    if (responseSignal) _rpcInFlight.set(responseSignal, ctl);

    let body = node.getAttribute('data-fui-rpc-body');
    let resolvedPath = path;
    let bodyIsFormData = false;
    if (!body && node.tagName === 'FORM') {
      const fd = new FormData(node);
      // For GET, encode the form as the query string of the RPC path.
      // append (not set) so multi-value fields survive as repeated keys
      // — the same shape the urlencoded path and r.Form[] produce. The
      // widget path used to serialize a JSON body for every method, and
      // fetch(GET, body) throws "GET/HEAD cannot have body".
      if (method === 'GET') {
        const params = new URLSearchParams();
        fd.forEach((v, k) => { if (v != null) params.append(k, String(v)); });
        const qs = params.toString();
        if (qs) resolvedPath = path + (path.includes('?') ? '&' : '?') + qs;
      } else if (node.enctype === 'multipart/form-data' || node.querySelector('input[type="file"]')) {
        // Forms with files OR an explicit multipart enctype ship as
        // multipart/form-data so File objects survive. fetch sets the
        // right Content-Type (with boundary) when body is a FormData.
        body = fd;
        bodyIsFormData = true;
      } else {
        // JSON: a repeated key becomes an array; a single occurrence
        // stays a scalar. `obj[k] = v` (last-wins) used to drop every
        // value but the last of a multi-value field. Array-when-repeated
        // matches the repeated-key shape the GET + urlencoded paths emit.
        // (FormData never yields undefined, so obj[k] === undefined is a
        // correct first-occurrence test.)
        const obj = {};
        fd.forEach((v, k) => {
          if (obj[k] === undefined) obj[k] = v;
          else if (Array.isArray(obj[k])) obj[k].push(v);
          else obj[k] = [obj[k], v];
        });
        body = JSON.stringify(obj);
      }
    }

    // Widget context — derived from the DOM so data-fui-rpc works the
    // same inside a [data-fui-widget] (the widget-scoped click/submit
    // delegator in src/widgets.js calls this with no opts) and on a
    // bare island. wentry is the containing widget's registry entry; it
    // carries the dismiss closure and pollNow.
    const widgetEl = node.closest('[data-fui-widget]');
    const widgetName = (widgetEl && widgetEl.getAttribute('data-fui-widget')) || '';
    const wentry = widgetName && window.__gofastr._widgets && window.__gofastr._widgets[widgetName];
    const headers = _csrf({});
    if (widgetName) headers['X-FUI-Widget'] = widgetName;
    if (body && !bodyIsFormData) headers['Content-Type'] = 'application/json';

    // Disable the trigger during the in-flight request — but only when
    // we DON'T have abort-dedup via a signal. Signal-based RPCs need
    // the user to be able to click again instantly; the AbortController
    // guarantees only the last click's response reaches setSignal.
    const wantDisable = !responseSignal && (node.tagName === 'BUTTON' || node.tagName === 'INPUT');
    if (wantDisable) node.disabled = true;
    node.classList.add('fui-loading');
    node.setAttribute('aria-busy', 'true');
    try {
      if (!window.__gofastr._originOK(resolvedPath)) return;
      const r = await fetch(resolvedPath, { method, headers, body: body || undefined, signal: ctl.signal, credentials: 'same-origin' });
      if (!r.ok) {
        const txt = await r.text();
        if (responseSignal) window.__gofastr.setSignal(responseSignal, { ok: false, status: r.status, text: txt });
        return;
      }
      // Server-named stale screens (X-Gofastr-Invalidate) — evicted
      // before any post-success effect. Optional: the embed composition
      // ships rpc without nav (no screen cache).
      window.__gofastr._inval?.(r);
      // URL state update (no re-fetch). X-Gofastr-Push-State header
      // takes precedence over data-fui-push-state so the server can
      // override. Both paths (global + widget) now honor this.
      const pushState = r.headers.get('X-Gofastr-Push-State')
        || node.getAttribute('data-fui-push-state');
      if (pushState) {
        try {
          history.pushState(null, '', pushState);
          currentPath = location.pathname + location.search;
        } catch (_) {}
      }
      // X-Gofastr-Toast header fires toasts on success.
      const toastHeader = r.headers.get('X-Gofastr-Toast');
      if (toastHeader) {
        window.__gofastr._dispatchToastHeader(toastHeader);
      }
      const ct = r.headers.get('content-type') || '';
      const data = ct.indexOf('application/json') >= 0 ? await r.json() : await r.text();
      if (responseSignal) window.__gofastr.setSignal(responseSignal, data);
      // Close the containing widget on success — wentry.dismiss is the
      // closure the widget runtime installs on its registry entry.
      if (closeOnSuccess && wentry && wentry.dismiss) wentry.dismiss();
      if (resetOnSuccess) node.reset();
      // Post-success primitives — "Saved ✓" / "Revealed ✓" feedback and
      // scroll-to-new-content. Idempotent via data-fui-rpc-after-done.
      if (!node.dataset.fuiRpcAfterDone) {
        const afterText = node.getAttribute('data-fui-rpc-after-text');
        if (afterText !== null) node.textContent = afterText;
        if (node.hasAttribute('data-fui-rpc-after-disable')) {
          node.setAttribute('aria-disabled', 'true');
          if ('disabled' in node) node.disabled = true;
        }
        node.dataset.fuiRpcAfterDone = '1';
      }
      const scrollSel = node.getAttribute('data-fui-rpc-scroll-to');
      if (scrollSel) {
        const target = document.querySelector(scrollSel);
        if (target) Promise.resolve().then(() => {
          try { target.scrollIntoView({behavior: 'smooth', block: 'nearest'}); }
          catch (_) {}
        });
      }
      // Polling refresh: a successful RPC likely changed server state a
      // polling widget renders, so re-fetch /state now instead of
      // waiting out the cadence. Default target is the containing widget
      // (wentry); data-fui-rpc-refresh overrides it to refresh a DIFFERENT
      // widget (e.g. a confirm modal refreshing the panel).
      const rentry = window.__gofastr._widgets && window.__gofastr._widgets[node.getAttribute('data-fui-rpc-refresh') || widgetName];
      if (rentry && rentry.pollNow) rentry.pollNow();
      // Open a widget on success (e.g. "submit form → open results drawer").
      const openWidgetName = node.getAttribute('data-fui-rpc-open');
      if (openWidgetName) {
        window.__gofastr.loadModule('widgets').then(() => {
          window.__gofastr.openWidget(openWidgetName);
        }).catch(() => {});
      }
      // SPA navigate on success — swaps <main> without full page reload.
      // force:true re-renders even when the destination IS the current
      // page: the RPC just mutated server state, so a cached copy is
      // stale by definition.
      const navigatePath = node.getAttribute('data-fui-rpc-navigate');
      if (navigatePath) {
        try {
          window.__gofastr.navigate(navigatePath, { force: true });
        } catch (_) {}
      }
    } catch (err) {
      // Swallow AbortError — a newer dispatch superseded us.
      if (err && err.name === 'AbortError') return;
      // Network error (fetch threw): write a human-readable error into
      // the signal so the user sees feedback instead of a stale value.
      if (responseSignal) {
        window.__gofastr.setSignal(responseSignal, { ok: false, status: 0, text: 'Network error \u2014 please try again' });
      }
    } finally {
      // Clear the in-flight slot only if WE are still the latest
      // dispatch — a later click may have replaced us.
      if (responseSignal && _rpcInFlight.get(responseSignal) === ctl) {
        _rpcInFlight.delete(responseSignal);
      }
      const sticky = node.hasAttribute('data-fui-rpc-after-disable') && node.dataset.fuiRpcAfterDone === '1';
      if (!sticky && wantDisable) node.disabled = false;
      node.classList.remove('fui-loading');
      node.removeAttribute('aria-busy');
    }
  }

  // Per-form debounce timers for data-fui-rpc-trigger="input".
  const _idt = new WeakMap();

  // Global click+submit dispatcher — installed once at module load.
  // Catches data-fui-rpc on any element NOT inside a widget. Widget
  // scopes have their own handler that intercepts first.
  //
  // Also handles kiln-emitted data-kiln-tool buttons + plain forms with
  // a relative `action` attribute; kiln-built pages rely on the same
  // generic dispatcher.
  if (!document.__fuiGlobalDispatch) {
    document.__fuiGlobalDispatch = true;
    document.addEventListener('click', async (e) => {
      // Skip if inside a widget — that widget's handler owns the click.
      if (e.target.closest('[data-fui-widget]')) return;
      // Client-side signal mutations — no RPC needed.
      // Lightweight local state changes (counters, toggles, tabs).
      const signalEl = e.target.closest('[data-fui-signal-set],[data-fui-signal-inc],[data-fui-signal-toggle]');
      if (signalEl) {
        e.preventDefault();
        const G = window.__gofastr;
        // Set: data-fui-signal-set="name:value"
        const set = signalEl.getAttribute('data-fui-signal-set');
        if (set) {
          const sep = set.indexOf(':');
          if (sep > 0) {
            G.setSignal(set.substring(0, sep), set.substring(sep + 1));
          }
        }
        // Increment: data-fui-signal-inc="name" or data-fui-signal-inc="name:delta"
        const inc = signalEl.getAttribute('data-fui-signal-inc');
        if (inc) {
          const sep = inc.indexOf(':');
          const n = sep > 0 ? inc.substring(0, sep) : inc;
          const delta = sep > 0 ? Number(inc.substring(sep + 1)) : 1;
          const cur = Number(G.getSignal(n)) || 0;
          G.setSignal(n, cur + delta);
        }
        // Toggle: data-fui-signal-toggle="name"
        const tog = signalEl.getAttribute('data-fui-signal-toggle');
        if (tog) {
          const cur = G.getSignal(tog);
          G.setSignal(tog, !cur || cur === 'false' || cur === '0');
        }
        return;
      }

      const btn = e.target.closest('[data-fui-rpc]');
      if (btn && btn.tagName !== 'FORM') {
        e.preventDefault();
        await dispatchRPC(btn);
        return;
      }
      // Kiln dispatch: data-kiln-tool buttons fire a /kiln/tool/<name>
      // POST with the data-kiln-args body. Scoped to kiln-rendered
      // pages (body.kiln-app) or any subtree explicitly opted in via
      // data-fui-trusted — otherwise stored-XSS inside user-content
      // could carry a data-kiln-tool attribute and CSRF as the
      // logged-in user. (_kilnOK guard + _kilnPost shared with the
      // form-submit delegator below.)
      const legacy = e.target.closest('[data-kiln-tool]');
      if (legacy && _kilnOK(legacy)) {
        e.preventDefault();
        await _kilnPost(legacy, legacy.getAttribute('data-kiln-args') || '');
      }
    });
    const _kilnOK = (el) =>
      document.body.classList.contains('kiln-app') || el.closest('[data-fui-trusted]');
    const _kilnPost = (el, body) =>
      fetch('/kiln/tool/' + el.getAttribute('data-kiln-tool'), {
        method: 'POST',
        headers: _csrf({ 'Content-Type': 'application/json' }),
        body,
      }).catch(() => {});
    document.addEventListener('submit', async (e) => {
      const form = e.target.closest('form');
      if (!form || form.closest('[data-fui-widget]')) return;
      if (form.hasAttribute('data-fui-rpc')) {
        e.preventDefault();
        await dispatchRPC(form);
        return;
      }
      // Kiln dispatch: data-kiln-tool form submits. Scoped to
      // kiln-rendered pages (body.kiln-app) or data-fui-trusted
      // subtrees, same as the button delegator above.
      if (form.hasAttribute('data-kiln-tool') && _kilnOK(form)) {
        e.preventDefault();
        const obj = {};
        new FormData(form).forEach((v, k) => { obj[k] = v; });
        await _kilnPost(form, JSON.stringify(obj));
        return;
      }
      const action = form.getAttribute('action');
      // Resolve-and-compare, never a prefix match: the old
      // /^https?:\/\// test let `//evil.example/steal` through, and the
      // form body was then fetched cross-origin.
      if (!action || !window.__gofastr._sameOrigin(action)) return;
      const enctype = (form.getAttribute('enctype') || '').toLowerCase();
      const wantsJSON = enctype === 'application/json';
      const explicitSPA = form.hasAttribute('data-fui-spa');
      // Safe-by-default: urlencoded / multipart / unspecified-enctype
      // forms submit the browser-native way (preserves Set-Cookie,
      // Location-follow, file uploads, default password-manager UX).
      // Opt INTO the SPA path with data-fui-spa or enctype="application/json"
      // when you actually want fetch-and-swap behavior.
      if (!wantsJSON && !explicitSPA) return;
      e.preventDefault();
      const wantsForm = enctype === 'application/x-www-form-urlencoded' ||
                        enctype === 'multipart/form-data';
      const fd = new FormData(form);
      let body, headers;
      if (wantsJSON) {
        const obj = {}; fd.forEach((v, k) => { obj[k] = v; });
        body = JSON.stringify(obj);
        headers = { 'Content-Type': 'application/json' };
      } else if (wantsForm) {
        if (enctype === 'multipart/form-data') {
          body = fd;
          headers = {}; // browser sets Content-Type with boundary
        } else {
          const params = new URLSearchParams();
          fd.forEach((v, k) => params.append(k, v));
          body = params;
          headers = { 'Content-Type': 'application/x-www-form-urlencoded' };
        }
      } else {
        // data-fui-spa with no enctype → default to urlencoded so
        // r.ParseForm() works on the server side.
        const params = new URLSearchParams();
        fd.forEach((v, k) => params.append(k, v));
        body = params;
        headers = { 'Content-Type': 'application/x-www-form-urlencoded' };
      }
      try {
        const resp = await fetch(action, {
          method: form.getAttribute('method') || 'POST',
          headers,
          body,
          redirect: 'follow',
          credentials: 'same-origin',
        });
        if (resp.redirected && resp.url) {
          // Hard navigation. (Previously this had a `typeof navigate
          // === 'function'` branch trying to use a free identifier
          // `navigate`, which never resolved — the SPA navigator is on
          // window.__gofastr.navigate. We explicitly do NOT call SPA
          // nav here: a form-intercept Location follow lands on a
          // server-rendered page that may not be in this app's SPA
          // route table, and the hard nav also rebuilds the SSE
          // connection cleanly. Documented behaviour.)
          window.location.assign(resp.url);
          return;
        }
      } catch (err) {
        // Never swallow this. The write may well have committed on the server,
        // so a user who sees nothing happen presses the button again.
        //
        // Inside an embed frame this is the expected path rather than a rare
        // one: boot-embed forces redirect:'error' on credentialed requests (a
        // redirect would otherwise carry the grant to whatever origin it names),
        // so a handler answering the ordinary 303-after-POST rejects here. The
        // frame cannot follow it either way — the destination is an ordinary app
        // page whose CSP refuses to be framed — so the honest outcome is a
        // visible failure, not a blank panel.
        console.error('[gofastr] form submit could not complete', err);
        const g = window.__gofastr;
        if (g && typeof g.toast === 'function') {
          g.toast({ variant: 'error', title: 'Could not complete that submission.', ttl: 6000 });
        }
      }
    });

    // Debounced input-driven RPC: a form with
    // data-fui-rpc-trigger="input" fires its RPC each time an input
    // inside it changes, after a debounce window. Useful for
    // type-ahead search where the server is the source of truth for
    // filtered results (see core-ui/ARCHITECTURE.md — search is an
    // island state change, not a route).
    document.addEventListener('input', (e) => {
      // Open any focused combobox so typing makes the listbox visible
      // without requiring an ArrowDown press first. The RPC response
      // arrives after a debounce; we want the listbox open the moment
      // the first character lands, so the user sees options swap in.
      const combo = e.target && e.target.closest && e.target.closest('[role="combobox"]');
      if (combo) {
        const lbId = combo.getAttribute('aria-controls');
        const lb = lbId ? document.getElementById(lbId) : null;
        if (lb) {
          combo.setAttribute('aria-expanded', 'true');
          lb.removeAttribute('hidden');
        }
      }
      const form = e.target.closest('form[data-fui-rpc][data-fui-rpc-trigger="input"]');
      if (!form) return;
      // Note: this used to skip forms inside [data-fui-widget] under the
      // theory that the widget would own its own input handling — but no
      // widget-scoped input-trigger handler exists (only general-purpose
      // ones for char-count, autogrow, etc.), so the skip stranded any
      // combobox / typeahead inside a widget surface (e.g. CommandPalette).
      const ms = parseInt(form.getAttribute('data-fui-rpc-debounce-ms') || '250', 10) || 250;
      const prev = _idt.get(form);
      if (prev) clearTimeout(prev);
      _idt.set(form, setTimeout(() => {
		_idt.delete(form);
        dispatchRPC(form);
      }, ms));
    });
  }

  // rpc namespace members (extracted from the kernel literal for incremental
  // assembly). _csrf/_sameOrigin/_originOK are the origin + CSRF guards every
  // runtime fetch funnels through; dispatchRPC is the shared dispatch (called
  // directly by the delegators below AND exposed on the namespace so the
  // widget-scoped dispatcher in src/widgets.js can reuse the same impl).
  Object.assign(window.__gofastr, {
    /*  _sameOrigin(u) resolves u against the current page and compares
        origins. _originOK is the refuse-and-warn wrapper every runtime
        fetch uses when its URL came from a DOM attribute or a widget
        config.

        Those attributes ARE the trust boundary: dispatchRPC attaches the
        page's CSRF token to whatever host data-fui-rpc names, and the
        response frequently lands in innerHTML. The runtime already
        treated this class as real for data-kiln-tool (gated behind
        _kilnOK); this applies the same reasoning to the rest. Same shape
        as navigate(): warn on the console, then do nothing. */
    _csrf,
    // Shared dispatch: global delegators call dispatchRPC(node); the widget
    // dispatcher calls it with {widget, dismiss, refreshDefault}.
    dispatchRPC,

    _sameOrigin(u) {
      try { return new URL(String(u ?? ''), location.href).origin === location.origin; }
      catch (_) { return false; }
    },
    _originOK(u) {
      if (this._sameOrigin(u)) return true;
      console.warn('[gofastr] refused cross-origin fetch:', u);
      return false;
    },
  });
