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
  // opt in to prefix matching via data-fui-match-prefix, useful for
  // primary nav entries like "Components" (href="/components/") that
  // should light up on /components/accordion, /components/card, etc.
  // Prefix matching is OFF by default so breadcrumbs and sidebars (where
  // multiple links share a path prefix) keep their server-rendered
  // single aria-current. Non-matching links get aria-current cleared.
  // Links with NO href (server-rendered MatchPath items in a sidebar
  // where the active determination is prefix-based) are left untouched,
  // only the server has the prefix-match context for those.
  const update = (path) => {
    for (const link of document.querySelectorAll('nav a')) {
      const href = link.getAttribute('href');
      if (!href) continue; // server-managed (MatchPath, dynamic), hands off
      let active = href === path;
      if (!active && link.hasAttribute('data-fui-match-prefix')) {
        const hrefPath = href.split('?')[0].split('#')[0];
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
      } else {
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
