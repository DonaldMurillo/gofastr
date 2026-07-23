// Sidebar collapsible-rail runtime.
//
// Applies the persisted compact state before interaction, keeps the collapse
// button's accessible name/state synchronized, and rescans after SPA swaps.
(() => {
  'use strict';
  const G = window.__gofastr;
  const wired = new WeakSet();

  const setCollapsed = (root, collapsed, persist) => {
    root.setAttribute('data-collapsed', collapsed ? 'true' : 'false');
    const button = root.querySelector('[data-fui-sidebar-collapse]');
    if (button) {
      button.setAttribute('aria-expanded', collapsed ? 'false' : 'true');
      button.setAttribute('aria-label', collapsed ? 'Expand navigation' : 'Collapse navigation');
    }
    if (!persist) return;
    const key = root.getAttribute('data-fui-sidebar-storage');
    if (!key) return;
    try { localStorage.setItem(key, collapsed ? 'true' : 'false'); } catch (_) {}
  };

  const setup = (root) => {
    if (wired.has(root)) return;
    wired.add(root);
    const key = root.getAttribute('data-fui-sidebar-storage');
    let collapsed = false;
    if (key) {
      try { collapsed = localStorage.getItem(key) === 'true'; } catch (_) {}
    }
    setCollapsed(root, collapsed, false);
  };

  const scan = (scope) => {
    const root = scope?.querySelectorAll ? scope : document;
    if (root.matches?.('[data-fui-sidebar][data-fui-sidebar-storage]')) setup(root);
    root.querySelectorAll('[data-fui-sidebar][data-fui-sidebar-storage]').forEach(setup);
  };

  document.addEventListener('click', (event) => {
    const button = event.target.closest?.('[data-fui-sidebar-collapse]');
    if (!button) return;
    const root = button.closest('[data-fui-sidebar]');
    if (!root) return;
    event.preventDefault();
    setCollapsed(root, root.getAttribute('data-collapsed') !== 'true', true);
  });

  scan(document);
  document.addEventListener('gofastr:navigate', () => scan(document));
  new MutationObserver((records) => {
    for (const record of records) {
      for (const node of record.addedNodes) {
        if (node.nodeType === 1) scan(node);
      }
    }
  }).observe(document.documentElement, { childList: true, subtree: true });

  (G.loadedModules ||= {}).sidebar = true;
})();
