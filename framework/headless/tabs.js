// headless-tabs: the behaviour module for this package's tab strips.
// Selection stays the kernel's signal contract (data-fui-signal-set on
// the tab, data-active mirrored on the wrapper, CSS lights the panel);
// the module owns the keyboard contract (one tab is roving-tabindex 0;
// ArrowLeft/ArrowRight are RTL-aware and select on focus; Home/End
// jump; Tab leaves the strip), the data-state mirror for ports, and
// the vacate stash's restore-on-first-show with live-node moves after
// that. It replaces the retired core-ui/runtime tabs module.
(function () {
  'use strict';
  const NAME = 'headless-tabs';
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

  const wired = new WeakSet();   // strips with an observer installed
  const live = new WeakMap();    // panel -> DocumentFragment of its vacated live nodes
  const parsed = new WeakMap();  // stash script -> decoded {index: html}

  function stashMap(wrapper) {
    for (const s of wrapper.querySelectorAll('script[data-hui-tabs-stash]')) {
      let m = parsed.get(s);
      if (!m) {
        try { m = JSON.parse(s.textContent || '{}'); } catch (_) { m = {}; }
        parsed.set(s, m);
      }
      return m;
    }
    return null;
  }

  function apply(wrapper) {
    const idx = parseInt(wrapper.getAttribute('data-active') || '0', 10);
    if (wrapper.hasAttribute('data-hui-tabs-state')) {
      for (const t of wrapper.querySelectorAll('[role="tab"]')) {
        const on = t.getAttribute('data-fui-signal-set') === wrapper.getAttribute('data-fui-signal') + ':' + idx ||
          t.closest('[data-hui-tabs]') === wrapper && t.getAttribute('aria-selected') === 'true';
        t.setAttribute('data-state', on ? 'active' : 'inactive');
        t.setAttribute('aria-selected', on ? 'true' : 'false');
        t.setAttribute('tabindex', on ? '0' : '-1');
      }
    }
    if (!wrapper.hasAttribute('data-hui-tabs-vacate')) return;
    const stash = stashMap(wrapper);
    for (const panel of wrapper.querySelectorAll('[role="tabpanel"]')) {
      const i = panel.id ? panel.id.replace(/-panel-([0-9]+)$/, '$2') : '';
      const m = panel.id && /-panel-\d+$/.test(panel.id)
        ? panel.id.slice(panel.id.lastIndexOf('-panel-') + 7) : '';
      const mine = m !== '' && parseInt(m, 10) === idx;
      if (mine && !panel.childNodes.length && stash && stash[m]) {
        // First show: restore from the stash through the same
        // insertion pipeline html-mode signal regions use, then drop
        // the script — it is dead weight once every entry it held has
        // been read (the live-node moves own the panels from here).
        panel.innerHTML = stash[m];
        delete stash[m];
        if (Object.keys(stash).length === 0) {
          for (const s of wrapper.querySelectorAll('script[data-hui-tabs-stash]')) s.remove();
        }
        if (NS.scanAndLoadCSS) { try { NS.scanAndLoadCSS(panel); } catch (_) {} }
      } else if (mine && live.has(panel)) {
        // Re-show: move the panel's own live nodes back.
        const frag = live.get(panel);
        panel.appendChild(frag);
        live.delete(panel);
      } else if (!mine && panel.childNodes.length) {
        // Hide: park the live nodes in a detached fragment so anything
        // the runtime swapped in survives the re-show.
        const frag = document.createDocumentFragment();
        while (panel.firstChild) frag.appendChild(panel.firstChild);
        live.set(panel, frag);
      }
    }
  }

  function wire(w) {
    if (wired.has(w) || !w.isConnected) return;
    wired.add(w);
    const obs = new MutationObserver(function () { apply(w); });
    obs.observe(w, { attributes: true, attributeFilter: ['data-active'] });
    apply(w);
  }

  function scan(scope) {
    for (const w of within(scope || document, '[data-hui-tabs]')) wire(w);
  }

  // ─── the keyboard contract ───────────────────────────────────────

  document.addEventListener('keydown', function (e) {
    const tab = e.target && e.target.closest && e.target.closest('[data-hui-tabs] [role="tab"]');
    if (!tab) return;
    const wrapper = tab.closest('[data-hui-tabs]');
    if (!wrapper) return;
    const tabs = Array.from(wrapper.querySelectorAll('[role="tab"]')).filter(function (t) {
      return t.getAttribute('aria-disabled') !== 'true';
    });
    if (tabs.length === 0) return;
    const idx = tabs.indexOf(tab);
    const rtl = getComputedStyle(tab).direction === 'rtl';

    if (e.key === 'ArrowRight' || e.key === 'ArrowLeft') {
      const forward = (e.key === 'ArrowRight') !== rtl;
      e.preventDefault();
      const next = tabs[(idx + (forward ? 1 : -1) + tabs.length) % tabs.length];
      next.focus();
      next.click();
      return;
    }
    if (e.key === 'Home' || e.key === 'End') {
      e.preventDefault();
      const next = e.key === 'Home' ? tabs[0] : tabs[tabs.length - 1];
      next.focus();
      next.click();
      return;
    }
  });

  // A click selects: the signal write is the kernel's (the anchor's
  // data-fui-signal-set); the module updates the roving tabindex and
  // the aria mirror so the strip stays coherent even before the signal
  // fanout lands.
  document.addEventListener('click', function (e) {
    const tab = e.target && e.target.closest && e.target.closest('[data-hui-tabs] [role="tab"]');
    if (!tab) return;
    const wrapper = tab.closest('[data-hui-tabs]');
    if (!wrapper) return;
    for (const t of wrapper.querySelectorAll('[role="tab"]')) {
      const on = t === tab;
      t.setAttribute('aria-selected', on ? 'true' : 'false');
      t.setAttribute('tabindex', on ? '0' : '-1');
      if (wrapper.hasAttribute('data-hui-tabs-state')) {
        t.setAttribute('data-state', on ? 'active' : 'inactive');
      }
    }
  });

  // Focus follows the roving tabindex: focusing a tab with the arrows
  // updates it; Tab leaves naturally (nothing to do).
  document.addEventListener('focusin', function (e) {
    const tab = e.target && e.target.closest && e.target.closest('[data-hui-tabs] [role="tab"]');
    if (!tab) return;
    const wrapper = tab.closest('[data-hui-tabs]');
    if (!wrapper) return;
    for (const t of wrapper.querySelectorAll('[role="tab"]')) {
      const on = t === tab;
      t.setAttribute('tabindex', on ? '0' : '-1');
    }
  });

  scan(document);
  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;
})();
