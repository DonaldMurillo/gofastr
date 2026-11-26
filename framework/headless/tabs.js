// headless-tabs: the behaviour module for this package's tab strips.
// Selection stays the kernel's signal contract (data-cui-signal-set on
// the tab, data-active mirrored on the wrapper, CSS lights the panel);
// the module owns the keyboard contract (one tab is roving-tabindex 0;
// ArrowLeft/ArrowRight are RTL-aware and select on focus; Home/End
// jump; Tab leaves the strip), the panels' hidden="until-found" (find-
// in-page or a fragment link that reveals a panel selects its tab),
// the data-state mirror for ports, and the vacate stash's restore-on-
// first-show with live-node moves after that. It replaces the retired
// core-ui/runtime tabs module.
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
    if (root.querySelectorAll) out.push(...root.querySelectorAll(sel));
    return out;
  }

  // own: the wrapper's own tabs, panels or stash. A strip nested in one
  // of its panels is another wrapper's, and a click, an arrow or a
  // resync on the outer strip must not touch it.
  function own(wrapper, sel) {
    return Array.from(wrapper.querySelectorAll(sel)).filter(function (el) {
      return el.closest('[data-hui-tabs]') === wrapper;
    });
  }

  const wired = new WeakSet();   // strips with an observer installed
  const live = new WeakMap();    // panel -> DocumentFragment of its vacated live nodes
  const parsed = new WeakMap();  // stash script -> decoded {index: html}

  function stashMap(wrapper) {
    for (const s of own(wrapper, 'script[data-hui-tabs-stash]')) {
      let m = parsed.get(s);
      if (!m) {
        try { m = JSON.parse(s.textContent || '{}'); } catch (_) { m = {}; }
        parsed.set(s, m);
      }
      return m;
    }
    return null;
  }

  // syncTabs makes the strip's own tabs say what data-active says: the
  // selected tab is aria-selected and the one roving tabindex 0, and
  // with data-hui-tabs-state the data-state mirror follows. A
  // selection can arrive with no click (setSignal, a deep link, an
  // island), so this runs on every data-active change.
  function syncTabs(wrapper) {
    const idx = String(parseInt(wrapper.getAttribute('data-active') || '0', 10));
    const state = wrapper.hasAttribute('data-hui-tabs-state');
    for (const t of own(wrapper, '[role="tab"]')) {
      const on = t.getAttribute('data-cui-tab-index') === idx;
      t.setAttribute('aria-selected', on ? 'true' : 'false');
      t.setAttribute('tabindex', on ? '0' : '-1');
      if (state) t.setAttribute('data-state', on ? 'active' : 'inactive');
    }
  }

  // syncPanels shows the selected panel and gives every other one
  // hidden="until-found" and aria-hidden, out of the tab order: the browser keeps its
  // content findable and reveals it on a match. A disabled tab's panel
  // takes plain hidden, so a search cannot open what a click cannot.
  function syncPanels(wrapper) {
    const idx = String(parseInt(wrapper.getAttribute('data-active') || '0', 10));
    const disabled = new Set();
    for (const t of own(wrapper, '[role="tab"]')) {
      if (t.getAttribute('aria-disabled') === 'true') disabled.add(t.getAttribute('data-cui-tab-index'));
    }
    for (const panel of own(wrapper, '[role="tabpanel"]')) {
      const i = panel.getAttribute('data-cui-tab-index');
      const hidden = i === idx ? null : (disabled.has(i) ? '' : 'until-found');
      if (hidden === null) {
        if (panel.hasAttribute('hidden')) panel.removeAttribute('hidden');
        if (panel.hasAttribute('aria-hidden')) panel.removeAttribute('aria-hidden');
      } else {
        if (panel.getAttribute('hidden') !== hidden) panel.setAttribute('hidden', hidden);
        // Chromium keeps an until-found panel in the accessibility tree
        // as an empty tabpanel; aria-hidden takes it out.
        if (panel.getAttribute('aria-hidden') !== 'true') panel.setAttribute('aria-hidden', 'true');
      }
      const ti = i === idx ? '0' : '-1';
      if (panel.getAttribute('tabindex') !== ti) panel.setAttribute('tabindex', ti);
    }
  }

  function apply(wrapper) {
    const idx = parseInt(wrapper.getAttribute('data-active') || '0', 10);
    syncTabs(wrapper);
    syncPanels(wrapper);
    // The kernel's data-active mirror (core-ui/runtime frag/signals.js)
    // writes aria-selected on every [role=tab][data-cui-tab-index]
    // under the wrapper whose signal changed, nested strips included:
    // each nested strip is put back from its own data-active.
    for (const inner of wrapper.querySelectorAll('[data-hui-tabs]')) syncTabs(inner);
    if (!wrapper.hasAttribute('data-hui-tabs-vacate')) return;
    const stash = stashMap(wrapper);
    for (const panel of own(wrapper, '[role="tabpanel"]')) {
      const m = panel.id && /-panel-\d+$/.test(panel.id)
        ? panel.id.slice(panel.id.lastIndexOf('-panel-') + 7) : '';
      const mine = m !== '' && parseInt(m, 10) === idx;
      if (mine && !panel.childNodes.length && stash && stash[m]) {
        // First show: restore from the stash through the same
        // insertion pipeline html-mode signal regions use, then drop
        // the script — it is dead weight once every entry it held has
        // been read (the live-node moves own the panels from here).
        panel.innerHTML = NS._html?.(stash[m]) ?? stash[m];
        delete stash[m];
        if (Object.keys(stash).length === 0) {
          for (const s of own(wrapper, 'script[data-hui-tabs-stash]')) s.remove();
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

  // settleSignal puts the strip's index back into its signal when the
  // store holds no tab of this strip. A seed past the last tab (5 on a
  // three-tab strip) or a stale value from another page is painted by
  // the server as tab 0 (headless.TabsProps.Active: out of range takes
  // 0), while the store kept 5: anything else bound to the signal, or
  // reading it, disagreed with the selected tab. An in-range value is
  // left alone, and an unset signal stays unset.
  function settleSignal(w) {
    const name = w.getAttribute('data-cui-signal');
    if (!name || typeof NS.getSignal !== 'function' || typeof NS.setSignal !== 'function') return;
    const v = NS.getSignal(name);
    if (v === undefined || v === null) return;
    const n = Number(v);
    const count = own(w, '[role="tab"]').length;
    if (String(v).trim() !== '' && Number.isInteger(n) && n >= 0 && n < count) return;
    NS.setSignal(name, parseInt(w.getAttribute('data-active') || '0', 10) || 0);
  }

  // adoptRevealed selects the panel the browser revealed before this
  // module was listening: a fragment URL on first load or a search
  // that ran before the module arrived fires beforematch with nobody
  // listening, drops the panel's hidden, and apply() would hide it
  // again. Every inactive panel ships hidden with tabindex -1, and the
  // browser's reveal drops only the hidden, so a panel without it but
  // still at -1 is the one the reader was sent to. The panel the server
  // painted active (tabindex 0) is not: a tab clicked before this module
  // arrived moved data-active away from it, and that click stands.
  function adoptRevealed(w) {
    const idx = String(parseInt(w.getAttribute('data-active') || '0', 10));
    const tabs = own(w, '[role="tab"]');
    for (const panel of own(w, '[role="tabpanel"]')) {
      const i = panel.getAttribute('data-cui-tab-index');
      if (i === idx || panel.hasAttribute('hidden') || panel.getAttribute('tabindex') !== '-1') continue;
      const tab = tabs.find(function (t) { return t.getAttribute('data-cui-tab-index') === i; });
      if (!tab || tab.getAttribute('aria-disabled') === 'true') continue;
      selectIndex(w, i);
      return;
    }
  }

  function selectIndex(wrapper, i) {
    const name = wrapper.getAttribute('data-cui-signal');
    if (name && typeof NS.setSignal === 'function') NS.setSignal(name, parseInt(i, 10));
    if (wrapper.getAttribute('data-active') !== i) wrapper.setAttribute('data-active', i);
  }

  function wire(w) {
    if (wired.has(w) || !w.isConnected) return;
    wired.add(w);
    settleSignal(w);
    adoptRevealed(w);
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
    const all = own(wrapper, '[role="tab"]');
    const tabs = all.filter(function (t) {
      return t.getAttribute('aria-disabled') !== 'true';
    });
    if (tabs.length === 0) return;
    const idx = all.indexOf(tab);
    const rtl = getComputedStyle(tab).direction === 'rtl';

    if (e.key === 'ArrowRight' || e.key === 'ArrowLeft') {
      const forward = (e.key === 'ArrowRight') !== rtl;
      e.preventDefault();
      // Walk DOM order before skipping disabled entries. The focused tab
      // may itself be disabled (server selection or a live state change).
      let i = idx;
      do { i = (i + (forward ? 1 : -1) + all.length) % all.length; }
      while (all[i].getAttribute('aria-disabled') === 'true');
      const next = all[i];
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
  // data-cui-signal-set); the module updates the roving tabindex and
  // the aria mirror so the strip stays coherent even before the signal
  // fanout lands.
  document.addEventListener('click', function (e) {
    const tab = e.target && e.target.closest && e.target.closest('[data-hui-tabs] [role="tab"]');
    if (!tab || tab.getAttribute('aria-disabled') === 'true') return;
    const wrapper = tab.closest('[data-hui-tabs]');
    if (!wrapper) return;
    for (const t of own(wrapper, '[role="tab"]')) {
      const on = t === tab;
      t.setAttribute('aria-selected', on ? 'true' : 'false');
      t.setAttribute('tabindex', on ? '0' : '-1');
      if (wrapper.hasAttribute('data-hui-tabs-state')) {
        t.setAttribute('data-state', on ? 'active' : 'inactive');
      }
    }
  });

  // The browser fires beforematch on a hidden="until-found" panel when
  // find-in-page or a fragment link is about to reveal it, then drops
  // the attribute itself. Selecting the panel's tab keeps the strip,
  // the signal and the other panels in step with what is now shown.
  document.addEventListener('beforematch', function (e) {
    const panel = e.target;
    if (!panel || !panel.matches || !panel.matches('[data-hui-tabs] [role="tabpanel"]')) return;
    const wrapper = panel.closest('[data-hui-tabs]');
    if (!wrapper) return;
    const i = panel.getAttribute('data-cui-tab-index');
    const tab = own(wrapper, '[role="tab"]').find(function (t) {
      return t.getAttribute('data-cui-tab-index') === i;
    });
    if (!tab || tab.getAttribute('aria-disabled') === 'true') return;
    selectIndex(wrapper, i);
  }, true);

  // Focus follows the roving tabindex: focusing a tab with the arrows
  // updates it; Tab leaves naturally (nothing to do).
  document.addEventListener('focusin', function (e) {
    const tab = e.target && e.target.closest && e.target.closest('[data-hui-tabs] [role="tab"]');
    if (!tab) return;
    const wrapper = tab.closest('[data-hui-tabs]');
    if (!wrapper) return;
    for (const t of own(wrapper, '[role="tab"]')) {
      const on = t === tab;
      t.setAttribute('tabindex', on ? '0' : '-1');
    }
  });

  scan(document);
  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;
})();
