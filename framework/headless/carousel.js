// headless-carousel: the behaviour module for this package's
// carousels. The track is a native scrollable list (the no-script
// contract); the module owns the active slide — aria-current plus a
// class on exactly one slide, mirrored onto the dots — the prev/next
// stepping (looping when the loop flag rides the root), the
// keyboard (ArrowLeft/ArrowRight on the focused track, RTL-aware),
// the status sentence re-said through the server's words, and the
// auto-rotation with its pauses (hover, focus, hidden tab, reduced
// motion). It replaces the retired core-ui/runtime carousel module.
(function () {
  'use strict';
  const NAME = 'headless-carousel';
  const NS = window.__gofastr = window.__gofastr || {};
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;

  const REDUCED = window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)');

  function within(root, sel) {
    const out = [];
    if (root.matches && root.matches(sel)) out.push(root);
    if (root.querySelectorAll) out.push.apply(out, root.querySelectorAll(sel));
    return out;
  }

  function trackOf(root) {
    // No :scope >: the stage wrapper sits between the root and the
    // track now, and the track stays the first descendant in document
    // order (a nested carousel's track is inside a slide of this one).
    return root.querySelector('[data-hui-carousel-track]');
  }
  // slidesOf and dotsOf keep to this carousel's own parts: a carousel
  // nested in a slide carries slides and dots of its own, which a
  // descendant query would count as this one's.
  function slidesOf(root) {
    const track = trackOf(root);
    return track ? Array.from(track.children).filter(function (el) { return el.hasAttribute('aria-label'); }) : [];
  }
  function dotsOf(root) {
    return Array.from(root.querySelectorAll('[data-hui-carousel-goto]'))
      .filter(function (d) { return d.closest('[data-hui-carousel]') === root; });
  }

  // scaleOf converts layout pixels (clientWidth, scrollLeft) to the
  // visual pixels getBoundingClientRect reports: they differ under a
  // scaled or zoomed ancestor.
  function scaleOf(track) {
    const w = track.offsetWidth;
    return w ? track.getBoundingClientRect().width / w : 1;
  }
  function currentOf(root) {
    const slides = slidesOf(root);
    for (let i = 0; i < slides.length; i++) {
      if (slides[i].getAttribute('aria-current') === 'true') return i;
    }
    return 0;
  }

  // perViewOf is how many slides the track shows at once, read from
  // the layout: the sheet decides it (VisiblePerView, narrowed by a
  // container query), so the module measures instead of trusting a
  // number from the server. The track's width over one slide's pitch
  // (slide plus gap) counts a gap as no part of a slide, and only whole
  // slides count: a peeking half slide rounded up hid the last
  // position. The pixel of slack absorbs subpixel layout.
  function perViewOf(root) {
    const track = trackOf(root);
    const slides = slidesOf(root);
    if (!track || slides.length < 2) return 1;
    const a = slides[0].getBoundingClientRect();
    const b = slides[1].getBoundingClientRect();
    const pitch = Math.abs(b.left - a.left);
    if (!pitch || !a.width) return 1;
    const k = scaleOf(track);
    const per = Math.floor((track.clientWidth * k + pitch - a.width + k) / pitch);
    return Math.max(1, Math.min(slides.length, per));
  }

  // lastIndexOf is the last slide the track can bring to its start:
  // with three in view, six slides have four positions (0..3).
  function lastIndexOf(root) {
    return Math.max(0, slidesOf(root).length - perViewOf(root));
  }

  // indexAtScroll is the slide sitting at the track's start edge after
  // a scroll the module did not drive (a swipe, a trackpad, a drag of
  // the scrollbar). A track scrolled to its end reports the last
  // position even when snapping left it a pixel short.
  function indexAtScroll(root) {
    const track = trackOf(root);
    const slides = slidesOf(root);
    if (!track || slides.length === 0) return 0;
    const last = lastIndexOf(root);
    const max = track.scrollWidth - track.clientWidth;
    if (max > 0 && Math.abs(track.scrollLeft) >= max - 1) return last;
    const rtl = getComputedStyle(track).direction === 'rtl';
    const box = track.getBoundingClientRect();
    const inset = track.clientLeft * scaleOf(track);
    const edge = rtl ? box.right - inset : box.left + inset;
    let best = 0;
    let bestD = Infinity;
    for (let i = 0; i <= last; i++) {
      const r = slides[i].getBoundingClientRect();
      const d = Math.abs((rtl ? r.right : r.left) - edge);
      if (d < bestD) { bestD = d; best = i; }
    }
    return best;
  }

  // syncDots shows one dot per position, not per slide, so no dot
  // names a place the track cannot reach. The server renders a dot per
  // slide (the no-script anchors each jump to their slide); the module
  // hides the extras once it can measure, and again on every resize.
  function syncDots(root) {
    const last = lastIndexOf(root);
    for (const dot of dotsOf(root)) {
      dot.hidden = parseInt(dot.getAttribute('data-hui-carousel-goto'), 10) > last;
    }
    if (currentOf(root) > last) mark(root, last);
  }

  // setActive clamps to the reachable positions, marks the slide and
  // scrolls the track to it.
  function setActive(root, idx) {
    const slides = slidesOf(root);
    if (slides.length === 0) return;
    idx = Math.max(0, Math.min(idx, lastIndexOf(root)));
    mark(root, idx);
    const track = trackOf(root);
    const sl = slides[idx];
    if (!track || !sl) return;
    // The track scrolls itself, never scrollIntoView: that scrolls every
    // ancestor too, so a rotating carousel below the fold pulled the
    // page down to it. The slide's leading edge is its right one in RTL,
    // where scrollLeft runs negative; the delta has the right sign in
    // either direction.
    // Rects are visual pixels; scrollLeft takes layout ones.
    const rtl = getComputedStyle(track).direction === 'rtl';
    const k = scaleOf(track);
    const box = track.getBoundingClientRect();
    const r = sl.getBoundingClientRect();
    const inset = track.clientLeft * k;
    const delta = (rtl ? r.right - (box.right - inset) : r.left - (box.left + inset)) / k;
    if (Math.abs(delta) < 1) return;
    try {
      track.scrollTo({ left: track.scrollLeft + delta, behavior: REDUCED && REDUCED.matches ? 'auto' : 'smooth' });
    } catch (_) {
      track.scrollLeft += delta;
    }
  }

  // mark makes exactly one slide and its dot current and re-says the
  // status through the server's sentence shape. It never scrolls: the
  // scroll-settle path calls it for a position the track already has.
  function mark(root, idx) {
    const slides = slidesOf(root);
    if (slides.length === 0) return;
    for (let i = 0; i < slides.length; i++) {
      const on = i === idx;
      if (on) slides[i].setAttribute('aria-current', 'true');
      else slides[i].removeAttribute('aria-current');
    }
    for (const dot of dotsOf(root)) {
      if (parseInt(dot.getAttribute('data-hui-carousel-goto'), 10) === idx) {
        dot.setAttribute('aria-current', 'true');
      } else {
        dot.removeAttribute('aria-current');
      }
    }
    const track = trackOf(root);
    if (track) {
      // The status sentence: the format and total travel on the
      // -fmt hook ("<sentence>|<total>"); the words stay the server's.
      const pack = track.getAttribute('data-hui-carousel-status-fmt');
      if (pack) {
        const bar = pack.indexOf('|');
        if (bar > 0) {
          const fmt = pack.slice(0, bar);
          const total = pack.slice(bar + 1);
          track.setAttribute('data-hui-carousel-status',
            fmt.replace(/\{n\}/g, () => String(idx + 1)).replace(/\{total\}/g, () => total));
        }
      }
    }
  }

  // ─── auto-rotation ───────────────────────────────────────────────
  //
  // One timer per rotating carousel at its own cadence (4000 when the
  // rotate attribute carries none), stepping only when !paused(root).
  // Created on arm, cleared on disarm — which runs on gofastr:navigate
  // and from the detach watcher below. No shared sweep, no 1 Hz poll:
  // the retired module's shape (one interval per carousel, stopped on
  // hidden and reduced motion) is the model, with the hover/focus
  // pauses checked at each tick instead of by a listener pair.
  function cadenceOf(root) {
    const ms = parseInt(root.getAttribute('data-hui-carousel-rotate-ms'), 10);
    return Number.isFinite(ms) && ms > 0 ? ms : 4000;
  }

  function paused(root) {
    if (document.hidden) return true;
    if (REDUCED && REDUCED.matches) return true;
    if (root.matches(':hover') || root.contains(document.activeElement)) return true;
    return false;
  }

  // A Set, not a WeakSet: the detach sweep below has to walk every
  // rotating root to find the ones that left the tree, and a WeakSet
  // cannot be walked — which is how the watcher came to query the
  // DOCUMENT for connected roots and then test them for !isConnected,
  // a condition that is never true. Iterability is the fix, and it
  // costs nothing: the set holds at most one entry per live carousel.
  const rotating = new Set(); // roots with a live timer

  function stepOnce(root) {
    if (!root.isConnected || paused(root)) return;
    const next = currentOf(root) + 1;
    setActive(root, next > lastIndexOf(root) ? 0 : next);
  }

  function armRotation(root) {
    if (!root.hasAttribute('data-hui-carousel-rotate-ms')) return;
    if (rotating.has(root)) return;
    rotating.add(root);
    root.__huiTimer = setInterval(function () { stepOnce(root); }, cadenceOf(root));
  }

  function disarmRotation(root) {
    if (!rotating.has(root)) return;
    rotating.delete(root);
    if (root.__huiTimer) { clearInterval(root.__huiTimer); root.__huiTimer = null; }
  }

  // A DETACHED carousel never fires another tick we can see, but its
  // interval keeps the element (and its slides) alive: watch for the
  // detach directly, the same childList-only watcher shape the
  // disclosure trap uses, and disarm what went away. The sweep walks
  // the tracked set, not the document: a root that left the tree is
  // by definition no longer in querySelectorAll's answer, which is
  // why the previous connected-query + !isConnected sweep never fired.
  let detachWatcher = null;
  const disarmDetached = () => {
    for (const root of Array.from(rotating)) {
      if (!root.isConnected) disarmRotation(root);
    }
  };
  const syncDetachWatcher = () => {
    if (rotating.size > 0 && !detachWatcher) {
      detachWatcher = new MutationObserver(disarmDetached);
      detachWatcher.observe(document.body, { childList: true, subtree: true });
    } else if (rotating.size === 0 && detachWatcher) {
      detachWatcher.disconnect();
      detachWatcher = null;
    }
  };

  // ─── controls ────────────────────────────────────────────────────

  // clampStep bounds a step to the reachable positions, wrapping when
  // the loop flag rides the root and refusing (-1) otherwise.
  function clampStep(root, idx) {
    const n = lastIndexOf(root) + 1;
    const loop = root.hasAttribute('data-hui-carousel-loop');
    if (idx < 0 || idx >= n) return loop ? ((idx % n) + n) % n : -1;
    return idx;
  }

  document.addEventListener('click', function (e) {
    const t = e.target;
    if (!t || !t.closest) return;
    const root = t.closest('[data-hui-carousel]');
    if (!root) return;
    const prev = t.closest('[data-hui-carousel-prev]');
    const next = t.closest('[data-hui-carousel-next]');
    const dot = t.closest('[data-hui-carousel-goto]');
    if (prev) {
      e.preventDefault();
      const to = clampStep(root, currentOf(root) - 1);
      if (to >= 0) setActive(root, to);
      return;
    }
    if (next) {
      e.preventDefault();
      const to = clampStep(root, currentOf(root) + 1);
      if (to >= 0) setActive(root, to);
      return;
    }
    if (dot) {
      e.preventDefault();
      setActive(root, parseInt(dot.getAttribute('data-hui-carousel-goto'), 10));
      return;
    }
  });

  // Keyboard on the focused track: the arrows step (RTL-aware),
  // Home/End jump.
  document.addEventListener('keydown', function (e) {
    const track = e.target && e.target.closest && e.target.closest('[data-hui-carousel-track]');
    if (!track) return;
    const root = track.closest('[data-hui-carousel]');
    if (!root) return;
    if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight' && e.key !== 'Home' && e.key !== 'End') return;
    const rtl = getComputedStyle(track).direction === 'rtl';
    const cur = currentOf(root);
    let to = -1;
    if (e.key === 'ArrowRight') to = clampStep(root, cur + ((rtl) ? -1 : 1));
    else if (e.key === 'ArrowLeft') to = clampStep(root, cur + ((rtl) ? 1 : -1));
    else if (e.key === 'Home') to = 0;
    else if (e.key === 'End') to = lastIndexOf(root);
    if (to >= 0) {
      e.preventDefault();
      setActive(root, to);
    }
  });

  // ─── manual scrolling ────────────────────────────────────────────
  //
  // A swipe, a trackpad or the scrollbar moves the track with no click
  // the module sees. Scroll does not bubble, so one capturing listener
  // on the document catches every track; the mark waits for the scroll
  // to settle (scrollend where the engine has it, a short quiet timer
  // otherwise) so the dots do not flicker through every slide passed.
  function settle(track) {
    const root = track.closest('[data-hui-carousel]');
    if (root) mark(root, indexAtScroll(root));
  }
  function onScroll(e) {
    const track = e.target;
    if (!track || !track.matches || !track.matches('[data-hui-carousel-track]')) return;
    if (e.type === 'scrollend') {
      clearTimeout(track.__huiSettle);
      settle(track);
      return;
    }
    clearTimeout(track.__huiSettle);
    track.__huiSettle = setTimeout(function () { settle(track); }, 120);
  }
  document.addEventListener('scroll', onScroll, { capture: true, passive: true });
  document.addEventListener('scrollend', onScroll, { capture: true, passive: true });

  // The positions change with the track's width or a slide's (a
  // container query drops slides per view on a narrow column), so each
  // carousel's dots re-sync when its track or first slide resizes.
  const sized = new WeakSet();
  const resizer = typeof ResizeObserver === 'function'
    ? new ResizeObserver(function (entries) {
      for (const en of entries) {
        const root = en.target.closest('[data-hui-carousel]');
        if (root) syncDots(root);
      }
    })
    : null;

  // ─── the arrival pass ────────────────────────────────────────────

  function scan(root) {
    const scope = root && root.querySelectorAll ? root : document;
    for (const el of within(scope, '[data-hui-carousel]')) {
      armRotation(el);
      syncDots(el);
      if (resizer) {
        for (const box of [trackOf(el), slidesOf(el)[0]]) {
          if (box && !sized.has(box)) { sized.add(box); resizer.observe(box); }
        }
      }
    }
    syncDetachWatcher();
  }

  window.addEventListener('gofastr:navigate', function () {
    for (const root of Array.from(document.querySelectorAll('[data-hui-carousel-rotate-ms]'))) {
      disarmRotation(root);
    }
    syncDetachWatcher();
  });

  scan(document);
  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;
})();
