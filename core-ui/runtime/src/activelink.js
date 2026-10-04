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
  const update = (path) => {
    for (const link of document.querySelectorAll('nav a')) {
      const href = link.getAttribute('href');
      if (!href) continue; // server-managed (MatchPath, dynamic), hands off
      if (link.hasAttribute('data-cui-activelink-skip')) continue;
      let active = href === path;
      if (!active && link.hasAttribute('data-cui-match-prefix')) {
        // The attribute's VALUE is the prefix when non-empty — the
        // sidebar emits its MatchPath there, and the owned section can
        // differ from the href (an overview link deep in a section that
        // still owns the section root). Empty value keeps the href as
        // the prefix, the original opt-in spelling.
        const attrPrefix = (link.getAttribute('data-cui-match-prefix') || '').trim();
        const hrefPath = (attrPrefix !== '' ? attrPrefix : href).split('?')[0].split('#')[0];
        const pathOnly = (path || '').split('?')[0].split('#')[0];
        // Match on SEGMENT boundaries, and accept the canonical
        // no-trailing-slash href (/docs) as well as the trailing-slash
        // form (/docs/), apps register /docs, so requiring the slash
        // left the ordinary case permanently dark. /docs-old shares a
        // text prefix with /docs but not a segment, so it stays out.
        // "/" is never used as a prefix, otherwise every nav link
        // would match every page.
        const hrefBase = hrefPath.endsWith('/') ? hrefPath.slice(0, -1) : hrefPath;
        if (hrefBase !== '' && (pathOnly === hrefBase || pathOnly.startsWith(hrefBase + '/'))) {
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
        const grp = link.closest('[data-hui-sidebar-group], details');
        const nav = link.closest('nav');
        if (grp && nav && nav.contains(grp)) {
          if (grp.tagName === 'DETAILS') grp.open = true;
          else grp.setAttribute('open', '');
        }
      } else if (link.classList.contains('active') || link.hasAttribute('data-cui-activelink') || link.hasAttribute('data-cui-match-prefix')) {
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
        // must not strip them.
        link.removeAttribute('aria-current');
        link.classList.remove('active');
      }
    }
  };

  G._updateActiveLink = update;
  window.addEventListener('gofastr:navigate', (e) => {
    update((e.detail && e.detail.path) || location.pathname + location.search);
  });
  // Correct the highlight for wherever the page is NOW, SSR covered the
  // initial URL, but a navigation may have happened before idle load.
  update(location.pathname + location.search);

  (G.loadedModules ||= {}).activelink = true;
})();
