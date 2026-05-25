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
(() => {
  'use strict';
  window.__gofastr = window.__gofastr || {};

  // Per-banner state. Keys are the wrap DOM nodes. Each entry holds
  // { failureCount, sseTimer, healthInFlight } — module-globals would
  // collapse onto whichever banner happened to be set up last.
  const stateByBanner = new WeakMap();
  // Iteration helper: keep a Set of registered banners to walk for the
  // global reportFailure / reportRecovery calls. (WeakMap isn't
  // iterable on its own.)
  const banners = new Set();

  const getState = (banner) => {
    let s = stateByBanner.get(banner);
    if (!s) {
      s = { failureCount: 0, sseTimer: null, healthInFlight: false };
      stateByBanner.set(banner, s);
    }
    return s;
  };

  const show = (banner) => { banner.removeAttribute('hidden'); };
  const hide = (banner) => {
    banner.setAttribute('hidden', '');
    banner.removeAttribute('data-state');
  };

  const reportFailureOn = (banner) => {
    const s = getState(banner);
    s.failureCount++;
    const threshold = parseInt(banner.getAttribute('data-fui-network-retry-threshold') || '3', 10);
    if (s.failureCount >= threshold) show(banner);
  };
  const reportRecoveryOn = (banner) => {
    const s = getState(banner);
    s.failureCount = 0;
    hide(banner);
  };

  const reportFailure = () => { banners.forEach(reportFailureOn); };
  const reportRecovery = () => { banners.forEach(reportRecoveryOn); };

  const checkHealthOn = (banner) => {
    const s = getState(banner);
    if (s.healthInFlight) return Promise.resolve(false); // re-entrancy guard
    const url = banner.getAttribute('data-fui-network-retry-health');
    if (!url) return Promise.resolve(false);
    s.healthInFlight = true;
    banner.setAttribute('data-state', 'checking');
    return fetch(url, { method: 'GET', credentials: 'same-origin' })
      .then((res) => {
        s.healthInFlight = false;
        if (res.ok) {
          reportRecoveryOn(banner);
          return true;
        }
        banner.setAttribute('data-state', 'down');
        return false;
      })
      .catch(() => {
        s.healthInFlight = false;
        banner.setAttribute('data-state', 'down');
        return false;
      });
  };

  const checkHealth = () => {
    const ps = [];
    banners.forEach((b) => ps.push(checkHealthOn(b)));
    return Promise.all(ps);
  };

  const setupBanner = (banner) => {
    if (banner.__fuiBannerBound) return;
    banner.__fuiBannerBound = true;
    banners.add(banner);
    getState(banner); // initialize state slot

    const retry = banner.querySelector('[data-fui-network-retry-button]');
    if (retry) {
      retry.addEventListener('click', () => { checkHealthOn(banner); });
    }
    const silenceMs = parseInt(banner.getAttribute('data-fui-network-retry-sse-silence') || '0', 10);
    if (silenceMs > 0) {
      const s = getState(banner);
      // Clear any prior interval first — defensive against duplicate
      // setup calls (e.g. a rescan that bypasses the bound guard).
      if (s.sseTimer) { clearInterval(s.sseTimer); s.sseTimer = null; }
      s.sseTimer = setInterval(() => {
        const st = window.__gofastr.sseStatus;
        if (!st || !st.lastEventAt) return;
        if (Date.now() - st.lastEventAt > silenceMs) show(banner);
      }, Math.max(1000, Math.floor(silenceMs / 4)));
    }
  };

  const scan = (root) => {
    const scope = root && root.querySelectorAll ? root : document;
    for (const b of scope.querySelectorAll('[data-fui-comp="ui-network-retry-banner"]')) {
      setupBanner(b);
    }
    // Demo wiring: the /components/networkretrybanner page renders
    // two test buttons that drive the public API manually. Bind here
    // so the demo works without inline onclick (strict CSP blocks it).
    for (const btn of scope.querySelectorAll('[data-fui-network-retry-demo-trigger]')) {
      if (btn.__fuiDemoBound) continue;
      btn.__fuiDemoBound = true;
      btn.addEventListener('click', reportFailure);
    }
    for (const btn of scope.querySelectorAll('[data-fui-network-retry-demo-recover]')) {
      if (btn.__fuiDemoBound) continue;
      btn.__fuiDemoBound = true;
      btn.addEventListener('click', reportRecovery);
    }
  };

  requestAnimationFrame(() => scan(document));
  document.addEventListener('gofastr:navigate', () => {
    // Disconnect old banners' timers — their DOM is about to be
    // replaced. Walk the WeakMap-keyed Set; banners not in the new
    // DOM tree get GC'd as the Set is cleared.
    banners.forEach((b) => {
      const s = stateByBanner.get(b);
      if (s && s.sseTimer) { clearInterval(s.sseTimer); s.sseTimer = null; }
    });
    banners.clear();
    requestAnimationFrame(() => scan(document));
  });

  window.__gofastr.networkStatus = {
    reportFailure,
    reportRecovery,
    checkHealth,
  };
})();
