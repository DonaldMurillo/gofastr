// URL push/strip helpers for widgets that opt into deep linking.
(function () {
  'use strict';
  const G = window.__gofastr;

  G._deepLinkPushUrl = function (cfg, params) {
    const url = new URL(location.href);
    url.searchParams.set(cfg.deepLinkKey, cfg.deepLinkValue);
    for (const key of cfg.deepLinkParams || []) {
      if (key in params) url.searchParams.set(key, params[key]);
    }
    if (url.href !== location.href) history.pushState(null, '', url.pathname + url.search + url.hash);
  };

  G._deepLinkStripUrl = function (cfg) {
    const url = new URL(location.href);
    let touched = false;
    if (url.searchParams.get(cfg.deepLinkKey) === cfg.deepLinkValue) {
      url.searchParams.delete(cfg.deepLinkKey);
      touched = true;
    }
    for (const key of cfg.deepLinkParams || []) {
      if (url.searchParams.has(key)) {
        url.searchParams.delete(key);
        touched = true;
      }
    }
    if (!touched) return;
    const query = url.searchParams.toString();
    history.pushState(null, '', url.pathname + (query ? '?' + query : '') + url.hash);
  };

  // Pure function exports on the namespace — no DOM to rescan; just
  // the loader's loaded flag.
  (G.loadedModules ||= {}).widgetlinks = true;
})();
