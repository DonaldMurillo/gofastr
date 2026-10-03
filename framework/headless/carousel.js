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
  function slidesOf(root) {
    return Array.from(root.querySelectorAll('[data-hui-carousel-track] > [aria-label]'));
  }
  function currentOf(root) {
    const slides = slidesOf(root);
    for (let i = 0; i < slides.length; i++) {
      if (slides[i].getAttribute('aria-current') === 'true') return i;
    }
    return 0;
  }

  // setActive marks exactly one slide and its dot, scrolls the track,
  // and re-says the status through the server's sentence shape.
  function setActive(root, idx) {
    const slides = slidesOf(root);
    if (slides.length === 0) return;
    idx = ((idx % slides.length) + slides.length) % slides.length;
    for (let i = 0; i < slides.length; i++) {
      const on = i === idx;
      if (on) slides[i].setAttribute('aria-current', 'true');
      else slides[i].removeAttribute('aria-current');
    }
    for (const dot of root.querySelectorAll('[data-hui-carousel-goto]')) {
      if (parseInt(dot.getAttribute('data-hui-carousel-goto'), 10) === idx) {
        dot.setAttribute('aria-current', 'true');
      } else {
        dot.removeAttribute('aria-current');
      }
    }
    const track = trackOf(root);
    if (track) {
      const sl = slides[idx];
      if (sl && track.scrollLeft !== sl.offsetLeft - track.offsetLeft) {
        try { sl.scrollIntoView({ block: 'nearest', inline: 'start', behavior: REDUCED && REDUCED.matches ? 'auto' : 'smooth' }); } catch (_) {}
      }
      // The status sentence: the format and total travel on the
      // -fmt hook ("<sentence>|<total>"); the words stay the server's.
      const pack = track.getAttribute('data-hui-carousel-status-fmt');
      if (pack) {
        const bar = pack.indexOf('|');
        if (bar > 0) {
          const fmt = pack.slice(0, bar);
          const total = pack.slice(bar + 1);
          track.setAttribute('data-hui-carousel-status',
            fmt.replace(/\{n\}/g, String(idx + 1)).replace(/\{total\}/g, total));
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
    setActive(root, currentOf(root) + 1);
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

  function clampStep(root, idx) {
    const n = slidesOf(root).length;
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
    else if (e.key === 'End') to = slidesOf(root).length - 1;
    if (to >= 0) {
      e.preventDefault();
      setActive(root, to);
    }
  });

  // ─── the arrival pass ────────────────────────────────────────────

  function scan(root) {
    const scope = root && root.querySelectorAll ? root : document;
    for (const el of within(scope, '[data-hui-carousel]')) {
      armRotation(el);
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
