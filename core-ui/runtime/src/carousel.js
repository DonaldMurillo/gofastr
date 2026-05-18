// Carousel runtime module — wires Prev/Next buttons, pagination
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

    // Click delegation — Prev/Next + dots scoped to this carousel.
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

    // Keyboard — when the carousel or any descendant has focus.
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
        function start() {
          if (timer) return;
          timer = setInterval(function () {
            if (document.visibilityState === 'hidden') return;
            step(carousel, 1);
          }, rotateMs);
        }
        function stop() { if (timer) { clearInterval(timer); timer = null; } }
        carousel.addEventListener('mouseenter', stop);
        carousel.addEventListener('mouseleave', start);
        carousel.addEventListener('focusin', stop);
        carousel.addEventListener('focusout', function (ev) {
          if (!carousel.contains(ev.relatedTarget)) start();
        });
        document.addEventListener('visibilitychange', function () {
          if (document.visibilityState === 'hidden') stop(); else start();
        });
        start();
      }
    }

    updateDotsAndArrows(carousel);
  }

  function scan(root) {
    const scope = root && root.querySelectorAll ? root : document;
    scope.querySelectorAll('[data-fui-carousel]').forEach(attach);
  }
  scan(document);
  document.addEventListener('gofastr:navigate', function () { scan(document); });
  window.__gofastr = window.__gofastr || {};
  window.__gofastr.carousel = { rescan: scan };
})();
