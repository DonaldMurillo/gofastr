// headless-toc: the behaviour module for this package's tables of
// contents. The list itself is server-rendered from explicit items —
// the module never adds, removes or fills an entry — and its only job
// is the active state: which entry's heading is in view, as
// aria-current and a class written by the observer headless-rail owns
// (declared as a requirement here, so the one observer implementation
// is loaded before this module evaluates). A missing or malformed
// target selector is a safe no-op: the links keep working, nothing is
// marked, and no inline style is ever written.
(function () {
  'use strict';
  const NAME = 'headless-toc';
  const NS = window.__gofastr = window.__gofastr || {};
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;

  function within(root, sel) {
    const out = [];
    if (root.matches && root.matches(sel)) out.push(root);
    if (root.querySelectorAll) out.push.apply(out, root.querySelectorAll(sel));
    return out;
  }

  function scan(root) {
    const scope = root && root.querySelectorAll ? root : document;
    for (const nav of within(scope, '[data-hui-toc]')) {
      if (!NS._huiRailWatch) continue;
      NS._huiRailWatch(nav, nav.getAttribute('data-hui-toc-target'), null);
    }
  }

  scan(document);
  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;
})();
