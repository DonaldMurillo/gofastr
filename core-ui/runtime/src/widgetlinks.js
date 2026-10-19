// URL push/strip helpers for widgets that opt into deep linking.
(function () {
  'use strict';
  const G = window.__gofastr;

  G._deepLinkOpen = function (cfg, o) {
    const params = o.params || {};
    const declared = cfg.deepLinkParams || [];
    if (declared.length) {
      const url = new URL(window.location.href);
      for (const k of declared) {
        // Own-property check: `in` walks the prototype chain, and a
        // param literally named "constructor" would pass it.
        const fromCaller = Object.prototype.hasOwnProperty.call(params, k);
        const v = fromCaller ? params[k] : url.searchParams.get(k);
        // Values read off the query string are attacker-supplied.
        // Flagging them here is what lets setSignal's html render mode
        // treat them as text instead of markup.
        if (v != null) G.setSignal(k, v, fromCaller ? undefined : { untrusted: true });
      }
    }
    if (o.pushUrl && cfg.deepLinkKey && cfg.deepLinkValue) {
      // Skip if dismissed while the module loaded (cold cache).
      const stillMounted = Object.prototype.hasOwnProperty.call(G._widgets, cfg.name) && G._widgets[cfg.name];
      if (!stillMounted) return;
      G._deepLinkPushUrl(cfg, params);
    }
  };

  G._deepLinkPushUrl = function (cfg, params) {
    const url = new URL(location.href);
    url.searchParams.set(cfg.deepLinkKey, cfg.deepLinkValue);
    for (const key of cfg.deepLinkParams || []) {
      // Own-property check: `in` walks the prototype chain, and a
      // param literally named "constructor" would pass it.
      if (Object.prototype.hasOwnProperty.call(params, key)) url.searchParams.set(key, params[key]);
    }
    if (url.href !== location.href) G._pushURL(url.pathname + url.search + url.hash);
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
    G._pushURL(url.pathname + (query ? '?' + query : '') + url.hash);
  };

  // Pure function exports on the namespace, no DOM to rescan; just
  // the loader's loaded flag.
  (G.loadedModules ||= {}).widgetlinks = true;
})();
