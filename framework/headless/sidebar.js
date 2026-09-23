// headless-sidebar: the behaviour module for this package's sidebars.
// Ported from the retired core-ui/runtime sidebar module, byte-for-byte
// in contract: the storage key is namespaced and component-encoded at
// every sink (markup injected after boot cannot name an arbitrary
// origin key); a sidebar with no storage key is server-owned and this
// module never writes for it; the collapse BUTTON carries the
// data-hui-sidebar-toggle hook and its two labels ride beside it as
// data-hui-sidebar-collapse-label / -expand-label (the words come from
// the markup — the component resolved them — and this module carries
// no sentence of its own); the root's data-hui-sidebar-collapse names
// the collapse MODE and is never a click target; the button-dialect
// group toggle owns aria-expanded while the panel it names via
// aria-controls owns hidden. The mobile drawer is the widget
// runtime's (Requires("widgets")).
(function () {
  'use strict';
  const NAME = 'headless-sidebar';
  const G = window.__gofastr = window.__gofastr || {};
  if (G.loadedModules && Object.prototype.hasOwnProperty.call(G.loadedModules, NAME)) return;
  G.loadedModules = G.loadedModules || {};
  G.loadedModules[NAME] = true;
  const wired = new WeakSet();

  const STORAGE_PREFIX = 'gofastr.sidebar-collapse.';

  const labelFor = (button, collapsed) => {
    // The words come from the markup (the component resolved them
    // through its strings); null means the button carries none and
    // its existing accessible name is left alone.
    const custom = button.getAttribute(collapsed
      ? 'data-hui-sidebar-expand-label'
      : 'data-hui-sidebar-collapse-label');
    return custom || null;
  };

  const setCollapsed = (root, collapsed, persist) => {
    root.setAttribute('data-collapsed', collapsed ? 'true' : 'false');
    const button = root.querySelector('[data-hui-sidebar-toggle]');
    if (button) {
      button.setAttribute('aria-expanded', collapsed ? 'false' : 'true');
      const label = labelFor(button, collapsed);
      if (label !== null) button.setAttribute('aria-label', label);
    }
    if (!persist) return;
    const key = root.getAttribute('data-hui-sidebar-storage');
    if (!key) return;
    // Guard spelled at the sink: literal prefix + whole-operand
    // encodeURIComponent, so an attribute-borne key can only name an entry
    // inside this module's namespace.
    try { localStorage.setItem(STORAGE_PREFIX + encodeURIComponent(key), collapsed ? 'true' : 'false'); } catch (_) {}
  };

  const setup = (root) => {
    if (wired.has(root)) return;
    wired.add(root);
    // The variant and collapse hooks declare the posture: an
    // off-canvas sidebar has no inline collapse to restore, and the
    // none collapse is the server's (the primitive's contract).
    const variant = root.getAttribute('data-hui-sidebar-variant');
    const collapse = root.getAttribute('data-hui-sidebar-collapse');
    if (variant === 'off-canvas' || collapse === 'none') { wired.add(root); return; }
    const key = root.getAttribute('data-hui-sidebar-storage');
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
    if (root.matches?.('[data-hui-sidebar][data-hui-sidebar-storage]')) setup(root);
    root.querySelectorAll('[data-hui-sidebar][data-hui-sidebar-storage]').forEach(setup);
  };

  document.addEventListener('click', (event) => {
    const groupButton = event.target.closest?.('[data-hui-sidebar-group-toggle]');
    if (groupButton) {
      event.preventDefault();
      toggleGroup(groupButton);
      return;
    }
    // Only the toggle BUTTON is a collapse control. The root's
    // data-hui-sidebar-collapse names the collapse mode, and a link
    // click inside the nav is navigation, not a collapse: matching the
    // root's attribute here made every click inside the sidebar
    // collapse it (and swallowed the link's default).
    const button = event.target.closest?.('[data-hui-sidebar-toggle]');
    if (!button) return;
    const root = button.closest('[data-hui-sidebar]');
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

})();
