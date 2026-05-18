// Banner runtime module — dismissible inline alerts.
//
// Loaded on-demand when any [data-fui-banner-dismiss] marker is on
// the page (or arrives via SPA-nav). Two responsibilities:
//
//   1. On boot/SPA-swap: any banner with data-fui-banner-dismiss-id
//      whose dismissal was recorded in localStorage is hidden before
//      paint (we run inside DOMContentLoaded which is the runtime's
//      normal entry; for SPA swaps the rescan handler covers it).
//
//   2. Click delegation: clicking [data-fui-banner-dismiss] hides the
//      ancestor [data-fui-comp="ui-banner"]. If the dismiss button
//      carries data-fui-banner-dismiss-id, that key is written to
//      localStorage so the same banner doesn't re-appear on the next
//      page load.
//
// No server round-trip — banner dismissal is client-only. Apps that
// need server-side persistence wire that themselves via an RPC on
// the dismiss button (data-fui-rpc + a server handler).

(function () {
  'use strict';

  const STORAGE_PREFIX = 'gofastr.banner-dismiss.';

  function isDismissed(id) {
    if (!id) return false;
    try { return localStorage.getItem(STORAGE_PREFIX + id) === '1'; }
    catch (_) { return false; }
  }

  function recordDismiss(id) {
    if (!id) return;
    try { localStorage.setItem(STORAGE_PREFIX + id, '1'); }
    catch (_) { /* best-effort */ }
  }

  // Hide banners already recorded as dismissed.
  function applyPersistedDismissals(root) {
    const scope = root && root.querySelectorAll ? root : document;
    const dismissBtns = scope.querySelectorAll('[data-fui-banner-dismiss-id]');
    for (const btn of dismissBtns) {
      const id = btn.getAttribute('data-fui-banner-dismiss-id');
      if (!isDismissed(id)) continue;
      const banner = btn.closest('[data-fui-comp="ui-banner"]');
      if (banner) banner.setAttribute('hidden', '');
    }
  }

  // Single delegated click handler — survives partial-island swaps.
  document.addEventListener('click', function (ev) {
    const btn = ev.target && ev.target.closest && ev.target.closest('[data-fui-banner-dismiss]');
    if (!btn) return;
    const banner = btn.closest('[data-fui-comp="ui-banner"]');
    if (!banner) return;
    banner.setAttribute('hidden', '');
    const id = btn.getAttribute('data-fui-banner-dismiss-id');
    if (id) recordDismiss(id);
  });

  // Initial pass + SPA-nav rescan.
  applyPersistedDismissals(document);
  document.addEventListener('gofastr:navigate', function () {
    applyPersistedDismissals(document);
  });

  // Expose a rescan hook so the runtime's module-rescan loop finds us.
  window.__gofastr = window.__gofastr || {};
  window.__gofastr.banner = { rescan: applyPersistedDismissals };
})();
