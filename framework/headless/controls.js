// headless-controls: the behaviour module for the stateful form
// controls this package renders — the animated counter, the number
// stepper, the slider's output mirror and the range pair's cross-clamp
// and output sentence. Loaded by the kernel when one of its markers is
// on the page, at boot, on insertion, or after a client navigation.
//
// Nothing here owns a value: the counter's number belongs to the
// signals kernel, each input's value to the input, and the module
// only steps inside the bounds the server rendered, mirrors what a
// thumb shows, and animates toward a final value that is already the
// SSR text. No sentence is said here: the range pair's output is
// re-formatted through the sentence the component rendered into the
// output's hook, and reduced motion keeps every number where the
// server put it.
(function () {
  'use strict';
  const NAME = 'headless-controls';
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

  // ─── counter animation ───────────────────────────────────────────

  // The final value is already the SSR text; the animation only walks
  // a display from the recorded start to it. Reduced motion, or a
  // non-numeric either end, leaves the SSR text untouched.
  function animateCounter(root) {
    const from = parseFloat(root.getAttribute('data-hui-counter-from'));
    const value = root.querySelector('[data-fui-signal]');
    if (!value || !Number.isFinite(from)) return;
    const to = parseFloat(value.textContent);
    if (!Number.isFinite(to) || to === from) return;
    if (REDUCED && REDUCED.matches) return;
    let ms = parseFloat(root.getAttribute('data-hui-counter-ms'));
    if (!Number.isFinite(ms) || ms <= 0) ms = 800;
    const t0 = performance.now();
    function frame(now) {
      const k = Math.min(1, (now - t0) / ms);
      const eased = 1 - Math.pow(1 - k, 3);
      value.textContent = String(Math.round(from + (to - from) * eased));
      if (k < 1) requestAnimationFrame(frame);
      else value.textContent = String(to);
    }
    requestAnimationFrame(frame);
  }

  // ─── number stepper ──────────────────────────────────────────────

  // Steps the input inside the bounds its own attributes declare, then
  // reports the change as a real input event so every pipeline that
  // watches the control sees it. The declared bounds are the server's;
  // contradictory props are refused at render, never repaired here.
  function step(btn, dir) {
    const id = btn.getAttribute('data-hui-number-input-for');
    const input = id && document.getElementById(id);
    if (!input || input.disabled) return;
    const stepAttr = parseFloat(input.getAttribute('step') || '1');
    const delta = (Number.isFinite(stepAttr) && stepAttr > 0 ? stepAttr : 1) * dir;
    const cur = parseFloat(input.value);
    let next = Number.isFinite(cur) ? cur + delta : delta;
    const min = parseFloat(input.getAttribute('min'));
    const max = parseFloat(input.getAttribute('max'));
    if (Number.isFinite(min) && next < min) next = min;
    if (Number.isFinite(max) && next > max) next = max;
    if (Number.isFinite(stepAttr) && stepAttr > 0 && (input.getAttribute('step') || '').indexOf('.') === -1) {
      next = Math.round(next);
    }
    input.value = String(next);
    input.dispatchEvent(new Event('input', { bubbles: true }));
    input.dispatchEvent(new Event('change', { bubbles: true }));
  }

  // ─── slider output mirror ────────────────────────────────────────

  function syncSlider(input) {
    const root = input.closest('[data-hui-slider]');
    const out = root && root.querySelector('[data-hui-slider-output]');
    if (out) out.textContent = input.value;
  }

  // ─── range pair ──────────────────────────────────────────────────

  // Cross-clamps the thumb the reader just moved and re-formats the
  // output through the sentence the component rendered into the
  // output's hook, %s for %s, so a translated page keeps its own
  // words live.
  function syncPair(root, moved) {
    const low = root.querySelector('[data-hui-range-slider-low]');
    const high = root.querySelector('[data-hui-range-slider-high]');
    const out = root.querySelector('[data-hui-range-slider-output]');
    if (!low || !high) return;
    if (parseFloat(low.value) > parseFloat(high.value)) {
      if (moved === low) low.value = high.value;
      else high.value = low.value;
    }
    if (out) {
      const fmt = out.getAttribute('data-hui-range-slider-output') || '';
      out.textContent = fmt.replace('%s', low.value).replace('%s', high.value);
    }
  }

  // ─── delegated listeners ─────────────────────────────────────────

  document.addEventListener('click', function (e) {
    const t = e.target;
    if (!t || !t.closest) return;
    const dec = t.closest('[data-hui-number-input-decrement]');
    if (dec) { step(dec, -1); return; }
    const inc = t.closest('[data-hui-number-input-increment]');
    if (inc) step(inc, 1);
  });

  document.addEventListener('input', function (e) {
    const t = e.target;
    if (!t || !t.closest) return;
    if (t.matches('input[type="range"][data-hui-range-slider-low],input[type="range"][data-hui-range-slider-high]')) {
      const root = t.closest('[data-hui-range-slider]');
      if (root) syncPair(root, t);
      return;
    }
    if (t.matches('input[type="range"]') && t.closest('[data-hui-slider]')) syncSlider(t);
  });

  // ─── the arrival pass ────────────────────────────────────────────

  function scan(root) {
    const scope = root && root.querySelectorAll ? root : document;
    for (const el of within(scope, '[data-hui-counter-animate]')) animateCounter(el);
    for (const el of within(scope, '[data-hui-slider-output]')) {
      const input = el.closest('[data-hui-slider]');
      const range = input && input.querySelector('input[type="range"]');
      if (range) el.textContent = range.value;
    }
    const seen = new Set();
    for (const el of within(scope, '[data-hui-range-slider]')) {
      if (seen.has(el)) continue;
      seen.add(el);
      syncPair(el, null);
    }
  }

  scan(document);
  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;
})();
