// GoFastr runtime module — InfiniteScroll
//
// Wires every [data-fui-infinite-scroll] wrapper:
//   - IntersectionObserver on the inner [data-fui-infinite-sentinel]
//   - On intersection, POSTs to data-fui-infinite-scroll with the
//     current cursor (URL-encoded body), appends the HTML response
//     into the items container, and advances the cursor from the
//     X-Gofastr-Infinite-Cursor response header.
//   - Empty/missing cursor header = end-of-feed → sentinel removed,
//     observer disconnected.
//   - aria-busy toggles true → false across each fetch.
//   - <noscript>"Load more" fallback ships with the SSR shell so
//     non-JS users get the same data.
//
// Drain loop: after each fetch lands, if the sentinel is STILL in
// view (new items were inserted ABOVE it, so its viewport
// intersection is unchanged), we manually call fetchMore again.
// IntersectionObserver doesn't re-fire on its own in that case.
//
// Re-wires after SPA navigation (`gofastr:navigate`) via the
// _moduleScanners hook so swapped-in pages get their wrappers wired.
(() => {
  'use strict';

  function wireScroll(root) {
    if (typeof IntersectionObserver === 'undefined') return;
    const scope = root && root.querySelectorAll ? root : document;
    const wrappers = scope.matches && scope.matches('[data-fui-infinite-scroll]')
      ? [scope, ...scope.querySelectorAll('[data-fui-infinite-scroll]')]
      : scope.querySelectorAll('[data-fui-infinite-scroll]');
    wrappers.forEach((wrap) => {
      if (wrap.__fuiInfiniteWired) return;
      wrap.__fuiInfiniteWired = true;
      const sentinel = wrap.querySelector('[data-fui-infinite-sentinel]');
      if (!sentinel) return;
      const path = wrap.getAttribute('data-fui-infinite-scroll');
      if (!path) return;
      const itemsSel = wrap.getAttribute('data-fui-infinite-items') ||
        '[data-fui-infinite-items]';
      const items = wrap.querySelector(itemsSel) || wrap;
      const rootMargin = wrap.getAttribute('data-fui-infinite-root-margin') || '200px';
      let cursor = wrap.getAttribute('data-fui-infinite-cursor') || '';
      let inFlight = false;
      let exhausted = false;

      const fetchMore = async () => {
        if (inFlight || exhausted) return;
        inFlight = true;
        wrap.setAttribute('aria-busy', 'true');
        try {
          const body = new URLSearchParams();
          if (cursor) body.set('cursor', cursor);
          const r = await fetch(path, {
            method: 'POST',
            headers: {
              'Accept': 'text/html',
              'Content-Type': 'application/x-www-form-urlencoded',
            },
            body: body.toString(),
            credentials: 'same-origin',
          });
          if (!r.ok) return;
          const html = await r.text();
          if (html) {
            const tmp = document.createElement('template');
            tmp.innerHTML = html;
            items.appendChild(tmp.content);
            if (window.__gofastr && window.__gofastr.scanAndLoadCSS) {
              window.__gofastr.scanAndLoadCSS(items);
            }
          }
          const next = r.headers.get('X-Gofastr-Infinite-Cursor') || '';
          if (next === '') {
            exhausted = true;
            observer.disconnect();
            sentinel.remove();
          } else {
            cursor = next;
            wrap.setAttribute('data-fui-infinite-cursor', next);
          }
        } catch (_) {
          /* network / abort — keep sentinel, allow retry on next intersection */
        } finally {
          inFlight = false;
          wrap.setAttribute('aria-busy', 'false');
        }
        // After a fetch lands, IntersectionObserver won't re-fire if
        // the sentinel was already in view and stays in view (items
        // get inserted ABOVE it, so its intersection is unchanged).
        // Manually re-check after layout settles.
        if (!exhausted) {
          requestAnimationFrame(() => requestAnimationFrame(() => {
            const r2 = sentinel.getBoundingClientRect();
            const vh = window.innerHeight || document.documentElement.clientHeight;
            const margin = parseInt(rootMargin, 10) || 0;
            const inView = r2.top < vh + margin && r2.bottom > -margin;
            if (inView) fetchMore();
          }));
        }
      };

      const observer = new IntersectionObserver((entries) => {
        for (const e of entries) {
          if (e.isIntersecting) {
            fetchMore();
            break;
          }
        }
      }, { rootMargin });
      observer.observe(sentinel);
    });
  }

  window.__gofastr = window.__gofastr || {};
  window.__gofastr.scanInfiniteScroll = wireScroll;
  // Back-compat alias for the monolithic-runtime name.
  window.__fuiWireInfiniteScroll = wireScroll;
  // Re-wire after SPA navigation so freshly-swapped pages get wired.
  ((window.__gofastr._moduleScanners ||= {})).infinitescroll = wireScroll;
  (window.__gofastr.loadedModules ||= {}).infinitescroll = true;
  wireScroll(document);
})();
