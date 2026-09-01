// Carousel runtime module, wires Prev/Next buttons, pagination
// dots, ArrowLeft/Right keyboard nav, and optional AutoRotate.
//
// Layout-side: the track is `display: flex; overflow-x: auto;
// scroll-snap-type: x mandatory;` so the user can also drag/swipe
// natively. The runtime drives scrollLeft to advance programmatically.
//
// AutoRotate pauses on:
//   - prefers-reduced-motion: reduce
//   - mouseover or focuswithin on the carousel
//   - Page Visibility = hidden (background tab)
(function () {
  'use strict';

  // Every carousel with a live autorotate interval. Detached elements
  // cannot be found through the document, so teardown walks THIS set,
  // not querySelectorAll — see the gofastr:navigate handler below.
  const rotating = new Set();

  function track(carousel) {
    return carousel.querySelector('[data-fui-carousel-track]');
  }

  function slides(carousel) {
    return Array.from(carousel.querySelectorAll('[data-fui-carousel-slide]'));
  }

  function dots(carousel) {
    return Array.from(carousel.querySelectorAll('[data-fui-carousel-dot]'));
  }

  function visibleCount(carousel) {
    const v = getComputedStyle(carousel).getPropertyValue('--ui-carousel-cols');
    const n = parseInt(v, 10);
    return Number.isFinite(n) && n > 0 ? n : 1;
  }

  function currentIndex(carousel) {
    const tr = track(carousel);
    if (!tr) return 0;
    const sl = slides(carousel);
    if (sl.length === 0) return 0;
    const left = tr.scrollLeft;
    // Pick the slide whose offset is closest to scrollLeft.
    let best = 0, bestDist = Infinity;
    for (let i = 0; i < sl.length; i++) {
      const d = Math.abs(sl[i].offsetLeft - tr.offsetLeft - left);
      if (d < bestDist) { bestDist = d; best = i; }
    }
    return best;
  }

  function scrollTo(carousel, idx) {
    const tr = track(carousel);
    const sl = slides(carousel);
    if (!tr || !sl[idx]) return;
    const x = sl[idx].offsetLeft - tr.offsetLeft;
    tr.scrollTo({ left: x, behavior: 'smooth' });
  }

  function step(carousel, delta) {
    const sl = slides(carousel);
    if (sl.length === 0) return;
    const loop = carousel.getAttribute('data-fui-carousel-loop') === 'true';
    const cur = currentIndex(carousel);
    const visible = visibleCount(carousel);
    const max = Math.max(0, sl.length - visible);
    let next = cur + delta;
    if (loop) {
      const n = max + 1;
      next = ((next % n) + n) % n;
    } else {
      if (next < 0) next = 0;
      if (next > max) next = max;
    }
    scrollTo(carousel, next);
  }

  function updateDotsAndArrows(carousel) {
    const sl = slides(carousel);
    if (sl.length === 0) return;
    const cur = currentIndex(carousel);
    const visible = visibleCount(carousel);
    const max = Math.max(0, sl.length - visible);
    const loop = carousel.getAttribute('data-fui-carousel-loop') === 'true';
    dots(carousel).forEach(function (d, i) {
      if (i === cur) d.setAttribute('aria-current', 'true');
      else d.removeAttribute('aria-current');
    });
    if (!loop) {
      const prev = carousel.querySelector('[data-fui-carousel-prev]');
      const next = carousel.querySelector('[data-fui-carousel-next]');
      if (prev) prev.disabled = cur === 0;
      if (next) next.disabled = cur >= max;
    }
  }

  // Per-carousel wiring.
  function attach(carousel) {
    if (carousel.dataset.fuiCarouselBound === '1') return;
    carousel.dataset.fuiCarouselBound = '1';

    const tr = track(carousel);
    if (tr) {
      tr.addEventListener('scroll', function () {
        // Debounce-ish via rAF.
        if (carousel._fuiScrollRaf) return;
        carousel._fuiScrollRaf = requestAnimationFrame(function () {
          carousel._fuiScrollRaf = 0;
          updateDotsAndArrows(carousel);
        });
      });
    }

    // Click delegation, Prev/Next + dots scoped to this carousel.
    carousel.addEventListener('click', function (ev) {
      const prev = ev.target.closest('[data-fui-carousel-prev]');
      if (prev) { ev.preventDefault(); step(carousel, -1); return; }
      const next = ev.target.closest('[data-fui-carousel-next]');
      if (next) { ev.preventDefault(); step(carousel, 1); return; }
      const dot = ev.target.closest('[data-fui-carousel-dot]');
      if (dot) {
        ev.preventDefault();
        const i = parseInt(dot.getAttribute('data-fui-carousel-dot') || '0', 10);
        scrollTo(carousel, i);
        return;
      }
    });

    // Keyboard, when the carousel or any descendant has focus.
    carousel.addEventListener('keydown', function (ev) {
      if (ev.target && /^(INPUT|TEXTAREA|SELECT)$/.test(ev.target.tagName)) return;
      if (ev.key === 'ArrowLeft')  { ev.preventDefault(); step(carousel, -1); return; }
      if (ev.key === 'ArrowRight') { ev.preventDefault(); step(carousel, 1); return; }
    });

    // AutoRotate.
    const rotateMs = parseInt(carousel.getAttribute('data-fui-carousel-autorotate') || '0', 10);
    if (rotateMs > 0) {
      const prefersReduced = window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
      if (!prefersReduced) {
        let timer = null;
        // userPaused tracks pointer/focus interaction. visibilitychange
        // returning the tab to foreground must NOT auto-resume when the
        // user has actively hovered or focused the carousel, that
        // would yank a slide out from under their pointer.
        let userPaused = false;
        // dead is set by teardown for carousels that left the document:
        // start() must refuse afterwards, otherwise the document-level
        // visibilitychange listener would re-arm the interval on a
        // detached subtree the next time the tab regains focus.
        let dead = false;
        function start() {
          if (dead || timer || userPaused) return;
          timer = setInterval(function () {
            if (document.visibilityState === 'hidden') return;
            step(carousel, 1);
          }, rotateMs);
        }
        function stop() { if (timer) { clearInterval(timer); timer = null; } }
        const onVis = function () {
          if (document.visibilityState === 'hidden') stop(); else start();
        };
        // kill fully disarms a carousel that left the DOM: interval,
        // document listener, and registry entry. Called by teardown.
        function kill() {
          dead = true;
          stop();
          document.removeEventListener('visibilitychange', onVis);
          rotating.delete(carousel);
        }
        carousel._fuiCarouselKill = kill;
        rotating.add(carousel);
        carousel.addEventListener('mouseenter', function () { userPaused = true; stop(); });
        carousel.addEventListener('mouseleave', function () { userPaused = false; start(); });
        carousel.addEventListener('focusin', function () { userPaused = true; stop(); });
        carousel.addEventListener('focusout', function (ev) {
          if (!carousel.contains(ev.relatedTarget)) { userPaused = false; start(); }
        });
        document.addEventListener('visibilitychange', onVis);
        start();
      }
    }

    updateDotsAndArrows(carousel);

    // Virtual-scroll hydration. Slides with data-fui-carousel-defer
    // are placeholders; their content lives in a sibling
    // <script type='application/json' data-fui-carousel-deferred-for=…>
    // map. IntersectionObserver swaps in the real HTML the first time
    // a placeholder enters the track viewport. Once hydrated the
    // observer stops watching the slide.
    hydrateVirtual(carousel);
  }

  function hydrateVirtual(carousel) {
    if (carousel.__fuiCarouselHydrate) return;
    carousel.__fuiCarouselHydrate = true;
    const id = carousel.getAttribute('id');
    if (!id) return;
    const manifestEl = carousel.querySelector(
      'script[type="application/json"][data-fui-carousel-deferred-for="' + id + '"]'
    );
    if (!manifestEl) return;
    let manifest;
    try { manifest = JSON.parse(manifestEl.textContent || '{}'); } catch (_) { return; }
    if (!manifest || typeof manifest !== 'object') return;
    if (typeof IntersectionObserver === 'undefined') {
      // No observer support, hydrate everything upfront so the
      // carousel is at least functional.
      for (const k of Object.keys(manifest)) {
        const ph = carousel.querySelector('[data-fui-carousel-defer="' + k + '"]');
        if (ph) { ph.innerHTML = manifest[k]; ph.removeAttribute('data-fui-carousel-defer'); }
      }
      return;
    }
    const root = track(carousel);
    if (!root) return;
    const io = new IntersectionObserver(function (entries) {
      for (const entry of entries) {
        if (!entry.isIntersecting) continue;
        const ph = entry.target;
        const idx = ph.getAttribute('data-fui-carousel-defer');
        if (idx == null) continue;
        const html = manifest[idx];
        if (html != null) {
          ph.innerHTML = html;
          ph.removeAttribute('data-fui-carousel-defer');
          ph.removeAttribute('style');
        }
        io.unobserve(ph);
      }
    }, {
      root: root,
      // Read-ahead: one full track-width ahead means the next-pinged
      // slide is decoded before the user reaches it.
      rootMargin: '0px ' + (root.clientWidth || 600) + 'px 0px ' + (root.clientWidth || 600) + 'px',
      threshold: 0.01,
    });
    carousel.querySelectorAll('[data-fui-carousel-defer]').forEach(function (ph) {
      io.observe(ph);
    });
  }

  function scan(root) {
    const scope = root && root.querySelectorAll ? root : document;
    // The MutationObserver scanner path passes each INSERTED node as
    // root; a carousel inserted as that node itself (island swap) is
    // not its own descendant, so test the root before scanning below it.
    if (scope !== document && scope.matches && scope.matches('[data-fui-carousel]')) attach(scope);
    scope.querySelectorAll('[data-fui-carousel]').forEach(attach);
  }
  scan(document);
  // Teardown for autorotate carousels that left the document. The DOM
  // swap runs BEFORE gofastr:navigate dispatches (runtime.js: swapAtSlot
  // -> finishNav), so by the time any listener runs the previous page's
  // carousels are detached and invisible to document.querySelectorAll.
  // Walking the registry by isConnected is the only reliable signal —
  // and it must NOT touch carousels that are still connected: one that
  // lives in a shared layout layer outlives the swap and must keep
  // rotating (scan() skips it below, its fuiCarouselBound guard makes a
  // stop-then-rescan a permanent stop).
  function pruneDetached() {
    rotating.forEach(function (c) {
      if (!c.isConnected && typeof c._fuiCarouselKill === 'function') {
        c._fuiCarouselKill();
      }
    });
  }
  window.addEventListener('gofastr:navigate', function () {
    pruneDetached();
    scan(document);
  });
  // Same prune + wire for island/RPC swaps: core's MutationObserver
  // re-runs every loaded module's scanner over inserted subtrees, which
  // is both the teardown trigger for replaced carousels and the only
  // wiring path for carousels arriving outside a full SPA navigation.
  window.__gofastr = window.__gofastr || {};
  window.__gofastr._moduleScanners = window.__gofastr._moduleScanners || {};
  window.__gofastr._moduleScanners.carousel = function (root) {
    pruneDetached();
    scan(root);
  };
  window.__gofastr = window.__gofastr || {};
  window.__gofastr.carousel = { rescan: scan };
  // Loader contract (runtime.js loadModule): a module announces itself
  // by setting loadedModules[name]; the MutationObserver scanner loop
  // ONLY runs scanners whose flag is set. carousel.js never set it, so
  // every _moduleScanners registration was dead code and
  // loadModule('carousel') could not resolve from the cached flag.
  (window.__gofastr.loadedModules ||= {}).carousel = true;
})();
