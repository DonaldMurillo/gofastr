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
  // and rpc-stub intercepts data-cui-open clicks, so this fetch only ever
  // runs in the live (full) composition.
  fetch('/__gofastr/widgets?page=' + encodeURIComponent(location.pathname),
        { headers: { 'X-Gofastr-Widget-Discovery': '1' } })
    .then((r) => (r.ok ? r.json() : null))
    .then(async (list) => {
      if (!Array.isArray(list)) { _wcr(); return; }
      // The widget runtime now ships as a split module. Make sure it's
      // loaded before iterating mounts, covers the case where no
      // [data-cui-widget] marker is present in initial HTML (the
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
        // Open any widget whose deep link matches the current URL. On
        // a full page load the host has usually SSR-inlined that
        // chrome (framework/uihost injectWidgetSSR) and _mountByName
        // hydrates it here; this pass is what covers SPA navigations
        // and popstate, where no fresh HTML arrives.
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
  // The data-cui-open click handler, data-cui-toast click handler, and
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
      // Toast trigger: data-cui-toast='<json>' fires a client toast.
      const toastBtn = e.target.closest && e.target.closest('[data-cui-toast]');
      if (toastBtn) {
        e.preventDefault();
        // The header path's dispatcher: it loads the module, and a
        // page with no stack (NS.toast answers null) or a module that
        // fails to load still shows the toast in the kernel's
        // fallback region instead of nothing.
        try {
          const cfg = JSON.parse(toastBtn.getAttribute('data-cui-toast'));
          window.__gofastr._toastOrFallback(cfg);
        } catch (_) {}
        return;
      }
      const btn = e.target.closest && e.target.closest('[data-cui-open]');
      if (!btn) return;
      // The live catalog path mounts the widget module; RPC controls inside
      // the mounted chrome await src/rpc.js in their scoped listeners.
      const name = btn.getAttribute('data-cui-open');
      if (!name) return;
      e.preventDefault();
      const raw = btn.getAttribute('data-cui-deeplink') || '';
      const overrides = {};
      if (raw) {
        // Degrade-don't-throw: a malformed percent escape throws URIError
        // AFTER preventDefault, which would consume the open click; a bad
        // pair is skipped (the selector-guard family's containment).
        for (const pair of raw.split('&')) {
          if (!pair) continue;
          const eq = pair.indexOf('=');
          if (eq < 0) continue;
          try {
            overrides[decodeURIComponent(pair.slice(0, eq))] =
              decodeURIComponent(pair.slice(eq + 1));
          } catch (_) {}
        }
      }
      const anchorPref = btn.getAttribute('data-cui-popover-anchor');
      (async () => {
        // The widgets module + catalog must both be ready before
        // openWidget can find the entry. Awaiting both here keeps the
        // click responsive even on a cold-cache page where the user
        // clicked faster than /__gofastr/widgets returned.
        await window.__gofastr.loadModule('widgets').catch(() => {});
        await _wready;
        // btn rides along so openWidget can read data-cui-ctx (#321):
        // the trigger's context keys the chrome fetch + cache. Read in
        // the module, not here: core bytes are the scarce ones.
        await window.__gofastr.openWidget(name, { params: overrides, pushUrl: true, btn });
        if (anchorPref !== null) {
          await window.__gofastr.loadModule('popover');
          window.__gofastr._anchorPopover(name, btn, anchorPref || 'bottom');
        }
      })();
    });
  }
  _installEagerWidgetDelegators();

  // Re-fetch the widget catalog after SPA-nav so page-scoped widgets
  // registered with .Pages("/route") become available when the user
  // arrives via partial-fetch (instead of a full page load).
  //
  // Without this, the boot-time catalog only contains widgets visible
  // on the initial path; clicking a data-cui-open trigger for a
  // page-scoped widget elsewhere silently bails because the entry is
  // missing from _widgetCatalog.
  //
  // Owned here, not by boot: boot is in every composition, and the
  // static one has no live endpoint to ask (widgets-boot-static keeps
  // its own navigate pass against the dumped catalog).
  //
  // The fetch is idempotent, entries are MERGED into the catalog
  // (existing entries from boot don't get overwritten unless the
  // server returns a changed version). Non-hidden widgets that
  // aren't already mounted are mounted now. Then _syncDeepLinks runs
  // so the URL's modal/drawer query params open the right surface.
  window.addEventListener('gofastr:navigate', (e) => {
    const path = (e && e.detail && e.detail.path) || location.pathname;
    fetch('/__gofastr/widgets?page=' + encodeURIComponent(path),
          { headers: { 'X-Gofastr-Widget-Discovery': '1' } })
      .then((r) => (r.ok ? r.json() : null))
      .then(async (list) => {
        if (!Array.isArray(list) || list.length === 0) return;
        const G = window.__gofastr;
        if (!G) return;
        // Make sure the widgets module is loaded, the initial page
        // may have had no widgets, so loadModule('widgets') was never
        // triggered and mountWidget isn't on the namespace yet.
        try { await G.loadModule('widgets'); } catch (_) { return; }
        G._widgetCatalog = G._widgetCatalog || {};
        for (const item of list) {
          const cfg = item.cfg;
          G._widgetCatalog[cfg.name] = item;
          // Auto-mount non-hidden widgets that aren't already on the
          // page. Hidden widgets (Modal / Drawer / Popover) stay
          // hidden until openWidget is called from a trigger.
          if (item.hidden) continue;
          if (G._mountByName) G._mountByName(cfg.name);
        }
        if (G._syncDeepLinks) G._syncDeepLinks();
      })
      .catch(() => { /* navigation succeeded; missing catalog is non-fatal */ });
  });
