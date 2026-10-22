// GoFastr runtime module, active-link highlighting.
//
// Carved from core nav (level-1 congestion-window budget): this is
// cosmetic post-navigation work. SSR renders the initial aria-current,
// so idle-loading the module leaves no visible gap, at worst the first
// SPA navigation's highlight lands a few frames late. The click→fetch→
// swap path stays in core.
(function () {
  'use strict';
  const G = window.__gofastr;

  // Links with an exact-href match get aria-current=page. A link can
  // opt in to prefix matching via data-cui-match-prefix: the VALUE,
  // when non-empty, names the section prefix (ui.Sidebar emits its
  // MatchPath there, and it can differ from the href); an empty value
  // falls back to the href itself, useful for primary nav entries like
  // "Components" (href="/components/") that should light up on
  // /components/card, /components/modal, etc.
  // Links whose current-state another party owns are left untouched,
  // the same hands-off rule as href-less links: links with NO href
  // (server-rendered MatchPath items in a sidebar where the active
  // determination is prefix-based, only the server has the prefix-match
  // context) and links carrying data-cui-activelink-skip (an
  // author-side escape hatch for a highlight owned by app code or a
  // hand-set attribute).
  // navigated is false only for the load-time sweep of a document that
  // has not navigated client-side: the server's first-paint mark on a
  // data-cui-activelink link is then still the truth, even where the
  // href is not exactly the URL (Active on /orders served at
  // /orders?page=2, or on a detail page), so the sweep leaves it.
  // Every link inside a <nav> landmark, plus links that opt in by
  // attribute wherever they sit: a header link carrying
  // data-cui-match-prefix outside a nav was silently never marked (#503).
  const LINKS = 'nav a, a[data-cui-activelink], a[data-cui-match-prefix]';
  // /about and /about/ name one page: a static export links the
  // slashed form its host serves, and a link it did not rewrite (a
  // search result, a widget's chrome) lands on the bare one.
  // A tab strip keeps its rule across the slash too, so an href's query
  // is compared after its path's bare form.
  const bare = (p) => (p.length > 1 && p.endsWith('/') ? p.slice(0, -1) : p);
  // The browser encodes Unicode in pathname while SSR may keep it in href.
  // URL.pathname gives both the same spelling without decoding %2F or %3F
  // into separators, or treating another origin's link as a local route.
  const pathKey = (p) => {
    try {
      const url = new URL(p, location.origin);
      return url.origin === location.origin ? bare(url.pathname) : p;
    } catch (_) { return bare(p); }
  };
  const key = (u) => { const i = u.indexOf('?'); return i < 0 ? pathKey(u) : pathKey(u.slice(0, i)) + u.slice(i); };
  const update = (path, navigated, scope) => {
    const pathOnly = (path || '').split('?')[0].split('#')[0];
    // Whether the link's nav (or the document, outside one) also holds
    // a link to this path WITH a query: a tab strip, whose current tab
    // the server picks.
    const tabsIn = new Map();
    const inTabStrip = (link) => {
      const box = link.closest('nav') || document;
      if (!tabsIn.has(box)) {
        tabsIn.set(box, [...box.querySelectorAll('a[href]')].some((a) => { const h = a.getAttribute('href'); return h.includes('?') && key(h).startsWith(pathKey(pathOnly) + '?'); }));
      }
      return tabsIn.get(box);
    };
    for (const link of (scope || document).querySelectorAll(LINKS)) {
      const href = link.getAttribute('href');
      if (!href) continue; // server-managed (MatchPath, dynamic), hands off
      if (link.hasAttribute('data-cui-activelink-skip')) continue;
      // A query-less href names a screen, and the URL's query is that
      // screen's in-page state (sort, page, filter), so /ask stays
      // current on /ask?page=2, as the server marks it. An href with
      // its own query (a filter tab) can only match the exact URL, and
      // so can its query-less sibling in a tab strip: the "All" tab
      // (/orders) stays dark on /orders?status=open and on its pages.
      if (href.startsWith('#')) continue;
      let active = key(href) === key(path || '') || (!href.includes('?') && pathKey(href) === pathKey(pathOnly) && !inTabStrip(link));
      if (!active && link.hasAttribute('data-cui-match-prefix')) {
        // The attribute's VALUE is the prefix when non-empty — the
        // sidebar emits its MatchPath there, and the owned section can
        // differ from the href (an overview link deep in a section that
        // still owns the section root). Empty value keeps the href as
        // the prefix, the original opt-in spelling.
        const attrPrefix = (link.getAttribute('data-cui-match-prefix') || '').trim();
        const hrefPath = (attrPrefix !== '' ? attrPrefix : href).split('?')[0].split('#')[0];
        // Match on SEGMENT boundaries, and accept the canonical
        // no-trailing-slash href (/docs) as well as the trailing-slash
        // form (/docs/), apps register /docs, so requiring the slash
        // left the ordinary case permanently dark. /docs-old shares a
        // text prefix with /docs but not a segment, so it stays out.
        // "/" is never used as a prefix, otherwise every nav link
        // would match every page.
        const hrefBase = pathKey(hrefPath);
        const currentBase = pathKey(pathOnly);
        if (hrefBase !== '/' && hrefBase !== '' && (currentBase === hrefBase || currentBase.startsWith(hrefBase + '/'))) {
          active = true;
        }
      }
      if (active) {
        link.setAttribute('aria-current', 'page');
        link.classList.add('active');
        // A collapsed group opens for its current child (DESIGN
        // "Reactive areas": a sidebar is a static area, no request).
        // The closest [data-hui-sidebar-group] or <details> ancestor,
        // either spelling a host may render; a details opens through
        // its reflected property, anything else through the attribute
        // the host's CSS keys on. Only a group INSIDE the link's own
        // <nav> is one: a <details> that wraps the whole nav (the site
        // header's phone drawer) is a menu, and opening it on load put
        // the drawer over every page.
        const nav = link.closest('nav');
        for (let group = link.parentElement; nav && group && group !== nav; group = group.parentElement) {
          if (group.tagName === 'DETAILS') group.open = true;
          else if (group.hasAttribute('data-hui-sidebar-group')) group.setAttribute('open', '');
          // The button dialect uses a sibling panel, not <details>. Open
          // every controlling ancestor, keeping its ARIA and hidden in sync.
          for (const toggle of group.children) {
            if (!toggle.hasAttribute('data-hui-sidebar-group-toggle')) continue;
            const panel = document.getElementById(toggle.getAttribute('aria-controls'));
            if (panel && group.contains(panel) && panel.contains(link)) {
              panel.hidden = false;
              toggle.setAttribute('aria-expanded', 'true');
            }
          }
        }
      } else if (link.classList.contains('active') || (navigated && link.hasAttribute('data-cui-activelink')) || link.hasAttribute('data-cui-match-prefix')) {
        // Clear what this module stamped (the class is our marker) and
        // what was HANDED to it: a data-cui-activelink link (the
        // sidebar marks every leaf so its first-paint aria-current is
        // this sweep's to move) or a data-cui-match-prefix link (the
        // sidebar emits its MatchPath there so the sweep can re-derive
        // the item) is activelink-owned, so the SSR first-paint mark on
        // it must not survive a navigation that moved elsewhere — two
        // lit entries was the bug, and the module loads idle, so the
        // navigation can land before it ever stamped .active on the
        // old link. Host-rendered navs with neither (pagination, server
        // breadcrumbs) keep owning their attributes; a runtime sweep
        // must not strip them. The data-cui-activelink handover only
        // covers navigations: with none, the server's mark stays.
        link.removeAttribute('aria-current');
        link.classList.remove('active');
      }
    }
  };

  // A rel=alternate hreflang link names this page in another locale (a
  // language switcher, ui.LanguageMenu). Chrome that outlives a swap
  // still points at the page it was rendered on, so every sweep points
  // it at the page the reader is on now, the way the server builds it
  // (i18n.PathLocales.Path): the default locale plain, the others under
  // /<tag> after the export's base. A tag outside the app's locale set
  // is an alternate on another site and is left alone.
  const alternates = () => {
    const L = window.__gofastr_locales;
    if (!L || !G._unlocalize) return;
    const b = L.base || '', p = G._unlocalize(location.pathname).slice(b.length) || '/';
    for (const a of document.querySelectorAll('a[rel~="alternate"][hreflang]')) {
      const t = a.getAttribute('hreflang').toLowerCase();
      if (t === L.default) a.setAttribute('href', b + p + location.search);
      else if (L.prefixes.includes(t)) a.setAttribute('href', b + '/' + t + (p === '/' ? '' : p) + location.search);
    }
  };

  G._updateActiveLink = update;
  window.addEventListener('gofastr:navigate', (e) => {
    update((e.detail && e.detail.path) || location.pathname + location.search, true);
    alternates();
  });
  // Widget chrome (the sidebar's phone drawer) mounts after load and is
  // served without the page's current path, and no navigation follows
  // its mount: sweep the announced root so its nav marks the page the
  // drawer opened over. Scoped to the root, so the page is not swept.
  document.addEventListener('fui:widget-open', (e) => {
    const root = e.detail && e.detail.root;
    if (root && root.querySelectorAll) update(location.pathname + location.search, true, root);
    alternates();
  });
  // Correct the highlight for wherever the page is NOW, SSR covered the
  // initial URL, but a navigation may have happened before idle load.
  // Boot's navigate listener records one in G._navigated; without it
  // the sweep only stamps matches and leaves the handed-over marks.
  update(location.pathname + location.search, !!G._navigated);
  alternates();

  (G.loadedModules ||= {}).activelink = true;
})();
