// headless-numbers: writes a number-mode signal binding
// (data-cui-signal-mode="number") the way the page's language
// (<html lang>) writes numbers, through Intl.NumberFormat, so a value
// the runtime changes reads like the server's i18n.Number text beside
// it: "4 821" after the French "4 820", not "4821".
//
// The signals kernel has no number mode of its own: it writes such a
// node as text. This module subscribes to each bound signal and, in a
// microtask after the kernel's write (listeners run before it), writes
// the formatted value, still inside the same task, so no frame shows
// the raw digits. Loaded by the kernel when a binding is on the page;
// the scan also rewrites any value set before the module arrived.
(function () {
  'use strict';
  const NAME = 'headless-numbers';
  const SEL = '[data-cui-signal-mode="number"]';
  const NS = window.__gofastr = window.__gofastr || {};
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;

  function numeric(v) {
    return v !== undefined && v !== null && v !== '' && typeof v !== 'object' && isFinite(v);
  }

  function write(name) {
    const v = NS.getSignal && NS.getSignal(name);
    if (!numeric(v)) return;
    const text = NS.formatNumber(+v);
    document.querySelectorAll(SEL + '[data-cui-signal="' + CSS.escape(name) + '"]').forEach(function (node) {
      node.textContent = text;
    });
  }

  // One listener per signal name, kept for the page's life: the set of
  // names is bounded by the bindings the server rendered.
  const subscribed = new Set();
  function subscribe(name) {
    if (subscribed.has(name) || !NS._signals) return;
    if (name === '__proto__' || name === 'constructor' || name === 'prototype') return;
    const own = Object.prototype.hasOwnProperty.call(NS._signals, name);
    const slot = own ? NS._signals[name] : (NS._signals[name] = { value: undefined, listeners: [] });
    subscribed.add(name);
    slot.listeners.push(function () { queueMicrotask(function () { write(name); }); });
  }

  function scan(root) {
    const scope = root && root.querySelectorAll ? root : document;
    const nodes = [];
    if (scope.matches && scope.matches(SEL)) nodes.push(scope);
    scope.querySelectorAll(SEL).forEach(function (n) { nodes.push(n); });
    for (const node of nodes) {
      const name = node.getAttribute('data-cui-signal');
      if (!name) continue;
      subscribe(name);
      write(name);
    }
  }

  scan(document);
  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;
})();
