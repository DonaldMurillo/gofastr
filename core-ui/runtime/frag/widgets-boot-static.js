// widgets-boot-static.js (spec fragment `widgets-boot-static`, boot class;
// deps: kernel). The static counterpart of widgets-boot — MUTUALLY EXCLUSIVE
// with it (never compose both). Composed into the `static` bundle in place
// of widgets-boot; serves the same role (catalog fetch + auto-mount pass +
// eager open/toast delegators) against the DUMPED catalog the static
// exporter writes.
//
// Why a separate fragment instead of a runtime branch on data-fui-static:
// the composition IS the switch (kernel+rpc-stub+signals+nav+widgets-boot-
// static+boot vs. kernel+rpc+signals+nav+widgets-boot+boot). The static
// exporter (framework/static.Builder.dumpWidgetAssets) writes
// /__gofastr/widgets.json — the unfiltered every-widget catalog — and the
// per-widget chrome HTML at /core-ui/widget/<name>/chrome. The live
// endpoint /__gofastr/widgets?page=… is session-gated and ABSENT on a
// serverless host, so fetching it would 404. This fragment fetches the
// dumped file; the rest of the catalog processing is identical to
// widgets-boot, and openWidget resolves a hidden widget's chrome against
// the static tree (the chrome fetch in src/widgets.js works unchanged
// because the static exporter dumps those files too).
//
// Duplication vs. sharing with widgets-boot: this file mirrors
// widgets-boot.js line-for-line except for the catalog URL (no ?page=
// filter — the dumped catalog is already the unfiltered every-widget
// list). The rpc/rpc-stub pair already duplicates _sameOrigin/_originOK
// for the same reason: mutually-exclusive fragments are not a shared
// code surface. Pulling the eager delegators into a third shared
// fragment would (a) change the `full` composition and risk drift in
// data-fui-open behaviour there, and (b) require either a runtime
// branch on data-fui-static (the exact silent _staticMode trap the
// composition switch was introduced to avoid) or a third composition
// entry in fragments.go for zero functional gain. Duplicating keeps
// `full` byte-identical and keeps the static composition honest.
//
// data-fui-open / data-fui-toast / data-fui-deeplink are still OWNED by
// widgets-boot (see fragments.go) — this fragment cross-references them
// the way rpc-stub cross-references the rpc family. The attrdoc gate
// treats cross-references as non-transferable; that holds here.

  // Auto-discover registered widgets from the dumped catalog. The
  // exporter (framework/static.Builder.dumpWidgetAssets) calls
  // widget.ServeWidgetList with no page filter, so this is the
  // unfiltered every-widget list. 404 / empty array means no widgets
  // were registered — silently skip (the runtime works for plain
  // pages too).
  //
  // The eager click delegator (installed below) awaits this readiness
  // Promise before calling openWidget. openWidget reads
  // _widgetCatalog[name] and silently bails if absent, so a click that
  // arrives before the catalog returns must wait for entries to be
  // populated. We set the Promise up immediately and stash the resolver
  // so the .then() of the catalog fetch can settle it. Stash on the
  // IIFE-local bag below; the namespace assignment at __gofastr = { … }
  // would otherwise wipe direct assignments here.
  let _wcr;
  const _wready = new Promise((resolve) => { _wcr = resolve; });

  // Catalog fetch — the dumped file. The live endpoint is session-gated
  // and absent on a serverless host; the static exporter dumps the
  // canonical registry shape to /__gofastr/widgets.json. Processing is
  // identical to widgets-boot.
  fetch('/__gofastr/widgets.json',
        { headers: { 'X-Gofastr-Widget-Discovery': '1' } })
    .then((r) => (r.ok ? r.json() : null))
    .then(async (list) => {
      if (!Array.isArray(list)) { _wcr(); return; }
      // The widget runtime ships as a split module. Make sure it's
      // loaded before iterating mounts — covers the case where no
      // [data-fui-widget] marker is present in initial HTML (the
      // marker scanner wouldn't have fired) but the catalog says there
      // are widgets to mount.
      if (list.length > 0) {
        try { await window.__gofastr.loadModule('widgets'); } catch (_) {}
      }
      const tryMount = () => {
        if (!window.__gofastr || !window.__gofastr.mountWidget) {
          setTimeout(tryMount, 0);
          return;
        }
        // Stash every widget's payload so openWidget can retrieve a
        // hidden one on demand. Also resolve _widgetCatalogReadyResolve
        // so the eager click delegator can proceed.
        window.__gofastr._widgetCatalog = window.__gofastr._widgetCatalog || {};
        for (const item of list) {
          window.__gofastr._widgetCatalog[item.cfg.name] = item;
          if (item.hidden) continue; // open later via openWidget(name)
          // Non-hidden widgets auto-mount at boot. Chrome HTML is
          // fetched lazily from cfg.chromePath — the static exporter
          // dumps the same bytes, so the fetch resolves against the
          // static tree.
          window.__gofastr._mountByName(item.cfg.name);
        }
        // Open any widget whose deep link matches the current URL.
        window.__gofastr._syncDeepLinks();

        // Eager click delegator (installed at boot, see below) is
        // awaiting this Promise — resolve so queued clicks unblock now
        // that the catalog is populated.
        _wcr();
      };
      tryMount();
    })
    .catch(() => { _wcr(); });

  // === EAGER WIDGET DELEGATORS =========================================
  // Identical to widgets-boot's _installEagerWidgetDelegators (the
  // capability being restored). Installed here at boot, before the
  // catalog fetch, so a click on an open trigger before the catalog
  // resolves is queued (via _wready) rather than lost. Idempotent via
  // document.__fuiOpenDispatch — the same flag widgets-boot uses; the
  // two fragments are never composed together so the flag stays honest.
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
      // openWidget resolves against the dumped catalog; chrome HTML
      // comes from the per-widget static file the exporter dumps.
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
        // clicked faster than /__gofastr/widgets.json returned.
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
