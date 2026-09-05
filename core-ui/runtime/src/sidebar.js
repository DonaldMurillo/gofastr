// Sidebar collapsible-rail runtime.
//
// Applies the persisted compact state before interaction, keeps the collapse
// button's accessible name/state synchronized, toggles button-dialect groups,
// and rescans after SPA swaps.
//
// Server-owned collapse state: a root WITHOUT data-fui-sidebar-storage
// carries a server-rendered data-collapsed value. setup() only runs for
// storage-carrying roots, so hydration never overwrites the server's state
// from localStorage, and setCollapsed() skips its write when no key exists,
// so a stale local value can never be written back either.
//
// Storage keys are namespaced and component-encoded (the banner.js dismissKey
// shape): "gofastr.sidebar-collapse." + encodeURIComponent(key). Injected
// markup carrying a data-fui-sidebar-storage value that names another
// feature's key can therefore only ever touch this module's own namespace.
// A legacy raw-key entry (the pre-namespace spelling) is migrated once on
// the read side: value copied to the encoded key, raw entry removed.
(() => {
  'use strict';
  const G = window.__gofastr;
  const wired = new WeakSet();

  const STORAGE_PREFIX = 'gofastr.sidebar-collapse.';

  const labelFor = (button, collapsed) => {
    const custom = button.getAttribute(collapsed
      ? 'data-fui-sidebar-expand-label'
      : 'data-fui-sidebar-collapse-label');
    if (custom) return custom;
    return collapsed ? 'Expand navigation' : 'Collapse navigation';
  };

  const setCollapsed = (root, collapsed, persist) => {
    root.setAttribute('data-collapsed', collapsed ? 'true' : 'false');
    const button = root.querySelector('[data-fui-sidebar-collapse]');
    if (button) {
      button.setAttribute('aria-expanded', collapsed ? 'false' : 'true');
      button.setAttribute('aria-label', labelFor(button, collapsed));
    }
    if (!persist) return;
    const key = root.getAttribute('data-fui-sidebar-storage');
    if (!key) return;
    // Guard spelled at the sink: literal prefix + whole-operand
    // encodeURIComponent, so an attribute-borne key can only name an entry
    // inside this module's namespace.
    try { localStorage.setItem(STORAGE_PREFIX + encodeURIComponent(key), collapsed ? 'true' : 'false'); } catch (_) {}
  };

  const setup = (root) => {
    if (wired.has(root)) return;
    wired.add(root);
    const key = root.getAttribute('data-fui-sidebar-storage');
    let collapsed = false;
    if (key) {
      try {
        // The key is namespaced and component-encoded at every sink so
        // markup injected after boot cannot name an arbitrary origin key;
        // state stored under the pre-namespace raw spelling is not read.
        const stored = localStorage.getItem(STORAGE_PREFIX + encodeURIComponent(key));
        collapsed = stored === 'true';
      } catch (_) {}
    }
    setCollapsed(root, collapsed, false);
  };

  // Button-dialect groups (SidebarGroupMarkup: button): the toggle button
  // owns aria-expanded; the element it names via aria-controls owns hidden.
  const toggleGroup = (button) => {
    // Resolve the panel first and bail before touching aria-expanded:
    // with a broken or absent aria-controls (hand-rolled host markup),
    // flipping the button's state while the panel never moves would
    // desync the two permanently.
    const panelId = button.getAttribute('aria-controls');
    const panel = panelId ? document.getElementById(panelId) : null;
    if (!panel) return;
    const expanded = button.getAttribute('aria-expanded') !== 'true';
    button.setAttribute('aria-expanded', expanded ? 'true' : 'false');
    if (expanded) panel.removeAttribute('hidden');
    else panel.setAttribute('hidden', '');
  };

  const scan = (scope) => {
    const root = scope?.querySelectorAll ? scope : document;
    if (root.matches?.('[data-fui-sidebar][data-fui-sidebar-storage]')) setup(root);
    root.querySelectorAll('[data-fui-sidebar][data-fui-sidebar-storage]').forEach(setup);
  };

  document.addEventListener('click', (event) => {
    const groupButton = event.target.closest?.('[data-fui-sidebar-group-toggle]');
    if (groupButton) {
      event.preventDefault();
      toggleGroup(groupButton);
      return;
    }
    const button = event.target.closest?.('[data-fui-sidebar-collapse]');
    if (!button) return;
    const root = button.closest('[data-fui-sidebar]');
    if (!root) return;
    event.preventDefault();
    setCollapsed(root, root.getAttribute('data-collapsed') !== 'true', true);
  });

  scan(document);
  window.addEventListener('gofastr:navigate', () => scan(document));
  new MutationObserver((records) => {
    for (const record of records) {
      for (const node of record.addedNodes) {
        if (node.nodeType === 1) scan(node);
      }
    }
  }).observe(document.documentElement, { childList: true, subtree: true });

  (G.loadedModules ||= {}).sidebar = true;
})();
