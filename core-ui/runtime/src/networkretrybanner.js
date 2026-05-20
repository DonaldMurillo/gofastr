// NetworkRetryBanner runtime — per-banner state via WeakMap. Exposes
// window.__gofastr.networkStatus with reportFailure() / reportRecovery()
// / checkHealth() helpers. Public API operates on EVERY mounted
// banner so app code doesn't need to know which one to address.
//
// Public API:
//   networkStatus.reportFailure()  — increment every banner's counter
//   networkStatus.reportRecovery() — reset every banner's counter + hide
//   networkStatus.checkHealth()    — ping every banner's health endpoint
//
// Loaded on-demand when a [data-fui-comp="ui-network-retry-banner"]
// element appears.
(function () {
  'use strict';
  window.__gofastr = window.__gofastr || {};

  // Per-banner state. Keys are the wrap DOM nodes. Each entry holds
  // { failureCount, sseTimer, healthInFlight } — module-globals would
  // collapse onto whichever banner happened to be set up last.
  var stateByBanner = new WeakMap();
  // Iteration helper: keep a Set of registered banners to walk for the
  // global reportFailure / reportRecovery calls. (WeakMap isn't
  // iterable on its own.)
  var banners = new Set();

  function getState(banner) {
    var s = stateByBanner.get(banner);
    if (!s) {
      s = { failureCount: 0, sseTimer: null, healthInFlight: false };
      stateByBanner.set(banner, s);
    }
    return s;
  }

  function show(banner) { banner.removeAttribute('hidden'); }
  function hide(banner) {
    banner.setAttribute('hidden', '');
    banner.removeAttribute('data-state');
  }

  function reportFailureOn(banner) {
    var s = getState(banner);
    s.failureCount++;
    var threshold = parseInt(banner.getAttribute('data-fui-network-retry-threshold') || '3', 10);
    if (s.failureCount >= threshold) show(banner);
  }
  function reportRecoveryOn(banner) {
    var s = getState(banner);
    s.failureCount = 0;
    hide(banner);
  }

  function reportFailure() {
    banners.forEach(reportFailureOn);
  }
  function reportRecovery() {
    banners.forEach(reportRecoveryOn);
  }

  function checkHealthOn(banner) {
    var s = getState(banner);
    if (s.healthInFlight) return Promise.resolve(false); // re-entrancy guard
    var url = banner.getAttribute('data-fui-network-retry-health');
    if (!url) return Promise.resolve(false);
    s.healthInFlight = true;
    banner.setAttribute('data-state', 'checking');
    return fetch(url, { method: 'GET', credentials: 'same-origin' })
      .then(function (res) {
        s.healthInFlight = false;
        if (res.ok) {
          reportRecoveryOn(banner);
          return true;
        }
        banner.setAttribute('data-state', 'down');
        return false;
      })
      .catch(function () {
        s.healthInFlight = false;
        banner.setAttribute('data-state', 'down');
        return false;
      });
  }

  function checkHealth() {
    var ps = [];
    banners.forEach(function (b) { ps.push(checkHealthOn(b)); });
    return Promise.all(ps);
  }

  function setupBanner(banner) {
    if (banner.__fuiBannerBound) return;
    banner.__fuiBannerBound = true;
    banners.add(banner);
    getState(banner); // initialize state slot

    var retry = banner.querySelector('[data-fui-network-retry-button]');
    if (retry) {
      retry.addEventListener('click', function () { checkHealthOn(banner); });
    }
    var silenceMs = parseInt(banner.getAttribute('data-fui-network-retry-sse-silence') || '0', 10);
    if (silenceMs > 0) {
      var s = getState(banner);
      // Clear any prior interval first — defensive against duplicate
      // setup calls (e.g. a rescan that bypasses the bound guard).
      if (s.sseTimer) { clearInterval(s.sseTimer); s.sseTimer = null; }
      s.sseTimer = setInterval(function () {
        var st = window.__gofastr.sseStatus;
        if (!st || !st.lastEventAt) return;
        if (Date.now() - st.lastEventAt > silenceMs) show(banner);
      }, Math.max(1000, Math.floor(silenceMs / 4)));
    }
  }

  function scan(root) {
    var scope = root && root.querySelectorAll ? root : document;
    scope.querySelectorAll('[data-fui-comp="ui-network-retry-banner"]').forEach(setupBanner);
    // Demo wiring: the /components/networkretrybanner page renders
    // two test buttons that drive the public API manually. Bind here
    // so the demo works without inline onclick (strict CSP blocks it).
    scope.querySelectorAll('[data-fui-network-retry-demo-trigger]').forEach(function (btn) {
      if (btn.__fuiDemoBound) return;
      btn.__fuiDemoBound = true;
      btn.addEventListener('click', reportFailure);
    });
    scope.querySelectorAll('[data-fui-network-retry-demo-recover]').forEach(function (btn) {
      if (btn.__fuiDemoBound) return;
      btn.__fuiDemoBound = true;
      btn.addEventListener('click', reportRecovery);
    });
  }

  requestAnimationFrame(function () { scan(document); });
  document.addEventListener('gofastr:navigate', function () {
    // Disconnect old banners' timers — their DOM is about to be
    // replaced. Walk the WeakMap-keyed Set; banners not in the new
    // DOM tree get GC'd as the Set is cleared.
    banners.forEach(function (b) {
      var s = stateByBanner.get(b);
      if (s && s.sseTimer) { clearInterval(s.sseTimer); s.sseTimer = null; }
    });
    banners.clear();
    requestAnimationFrame(function () { scan(document); });
  });

  window.__gofastr.networkStatus = {
    reportFailure: reportFailure,
    reportRecovery: reportRecovery,
    checkHealth: checkHealth,
  };
})();
