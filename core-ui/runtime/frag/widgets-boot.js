// widgets-boot.js (spec fragment `widgets-boot`, boot class; deps: kernel).
// Owns: the /__gofastr/widgets catalog fetch + auto-mount pass, the
// _widgetCatalog readiness Promise, and the eager open/toast click
// delegators that must be installed before the catalog resolves.

  // Auto-discover registered widgets. The framework runtime is loaded
  // once per page (via /__gofastr/runtime.js); each Mount(r, def) on
  // the server registers in a process-global map; this fetch picks the
  // list up and mounts every widget. 404 means no widgets registered,
  // silently skip (the runtime works for plain pages too).
  // Per-page scoped widget discovery, apps that constrain widgets
  // to specific routes via .Pages / .PagesPrefix / .PagesMatch get
  // a filtered catalog. Widgets with no Routes declared appear on
  // every page (the backwards-compatible default).
  // The eager click delegator (installed below) awaits this readiness
  // Promise before calling openWidget. openWidget reads
  // _widgetCatalog[name] and silently bails if absent, so a click that
  // arrives before the catalog returns must wait for entries to be
  // populated. We set the Promise up immediately and stash the resolver
  // so the .then() of the catalog fetch (which runs after the namespace
  // is assigned further down) can settle it. Stash on the IIFE-local
  // bag below; the namespace assignment at __gofastr = { … } would
  // otherwise wipe direct assignments here.
  let _wcr;
  const _wready = new Promise((resolve) => { _wcr = resolve; });

  // Widget catalog fetch. The live endpoint is session-gated and per-page
  // scoped (?page= filters widgets to the current route). A serverless
  // export never composes widgets-boot, the `static` composition omits it
  // and rpc-stub intercepts data-fui-open clicks, so this fetch only ever
  // runs in the live (full) composition.
  fetch('/__gofastr/widgets?page=' + encodeURIComponent(location.pathname),
        { headers: { 'X-Gofastr-Widget-Discovery': '1' } })
    .then((r) => (r.ok ? r.json() : null))
    .then(async (list) => {
      if (!Array.isArray(list)) { _wcr(); return; }
      // The widget runtime now ships as a split module. Make sure it's
      // loaded before iterating mounts, covers the case where no
      // [data-fui-widget] marker is present in initial HTML (the
      // marker scanner wouldn't have fired) but server-side
      // registration says there are widgets to mount.
      if (list.length > 0) {
        try { await window.__gofastr.loadModule('widgets'); } catch (_) {}
      }
      const tryMount = () => {
        if (!window.__gofastr || !window.__gofastr.mountWidget) {
          setTimeout(tryMount, 0);
          return;
        }
        // Stash every widget's payload so openWidget can retrieve a
        // hidden one on demand. Also settle _wready (via _wcr) so the
        // eager click delegator can proceed.
        window.__gofastr._widgetCatalog = window.__gofastr._widgetCatalog || {};
        for (const item of list) {
          window.__gofastr._widgetCatalog[item.cfg.name] = item;
          if (item.hidden) continue; // open later via openWidget(name)
          // Non-hidden widgets auto-mount at boot. Chrome HTML is
          // fetched lazily from cfg.chromePath so the registry stays
          // small; if the page already SSR-inlined this widget (root
          // element exists in DOM), mountWidget short-circuits to a
          // hydrate-only path. Either way, the result is a wired
          // widget root.
          window.__gofastr._mountByName(item.cfg.name);
        }
        // Open any widget whose deep link matches the current URL. Pure
        // post-hydration, there's a single-frame window where the page
        // paints without the modal. SSR pre-rendering is a future
        // optimization; correctness (refresh / share / back-button) is
        // already covered by this open-on-boot pass.
        window.__gofastr._syncDeepLinks();

        // Eager click delegator (installed at boot, see below) is
        // awaiting this Promise, resolve so queued clicks unblock now
        // that the catalog is populated.
        _wcr();
      };
      tryMount();
    })
    .catch(() => { _wcr(); });

  // === EAGER WIDGET DELEGATORS =========================================
  // The data-fui-open click handler, data-fui-toast click handler, and
  // popstate listener used to live inside the /__gofastr/widgets
  // catalog fetch's .then() callback. That meant on a slow network the
  // very first click on an open trigger had no handler to receive it,
  // the catalog hadn't returned yet, so the .then() hadn't run.
  //
  // We install them here at boot, before the catalog fetch. Each
  // handler awaits loadModule('widgets') (via the openWidget stub on
  // __gofastr) so it works regardless of whether the catalog has
  // resolved. Idempotent via document.__fuiOpenDispatch.
  function _installEagerWidgetDelegators() {
    if (document.__fuiOpenDispatch) return;
    document.__fuiOpenDispatch = true;
    document.addEventListener('click', (e) => {
      // Toast trigger: data-fui-toast='<json>' fires a client toast.
      const toastBtn = e.target.closest && e.target.closest('[data-fui-toast]');
      if (toastBtn) {
        e.preventDefault();
        window.__gofastr.loadModule('toasts').then(() => {
          try {
            const cfg = JSON.parse(toastBtn.getAttribute('data-fui-toast'));
            window.__gofastr.toast(cfg);
          } catch (_) {}
        }).catch(() => {});
        return;
      }
      const btn = e.target.closest && e.target.closest('[data-fui-open]');
      if (!btn) return;
      // The live catalog path mounts the widget module; RPC controls inside
      // the mounted chrome await src/rpc.js in their scoped listeners.
      const name = btn.getAttribute('data-fui-open');
      if (!name) return;
      e.preventDefault();
      const raw = btn.getAttribute('data-fui-deeplink') || '';
      const overrides = {};
      if (raw) {
        for (const pair of raw.split('&')) {
          if (!pair) continue;
          const eq = pair.indexOf('=');
          if (eq < 0) continue;
          overrides[decodeURIComponent(pair.slice(0, eq))] =
            decodeURIComponent(pair.slice(eq + 1));
        }
      }
      const anchorPref = btn.getAttribute('data-fui-popover-anchor');
      (async () => {
        // The widgets module + catalog must both be ready before
        // openWidget can find the entry. Awaiting both here keeps the
        // click responsive even on a cold-cache page where the user
        // clicked faster than /__gofastr/widgets returned.
        await window.__gofastr.loadModule('widgets').catch(() => {});
        await _wready;
        await window.__gofastr.openWidget(name, { params: overrides, pushUrl: true });
        if (anchorPref !== null) {
          await window.__gofastr.loadModule('popover');
          window.__gofastr._anchorPopover(name, btn, anchorPref || 'bottom');
        }
      })();
    });
  }
  _installEagerWidgetDelegators();
