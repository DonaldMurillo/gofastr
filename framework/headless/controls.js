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

  // formatNumber is the kernel's: a value this module writes reads like
  // the server's i18n.Number text it replaces ("٥٠" stays "٥٠" on an
  // Egyptian Arabic page).
  const formatNumber = (n) => NS.formatNumber(n);

  // valueText is an input's value written the page's way.
  function valueText(input) {
    const n = parseFloat(input.value);
    return Number.isFinite(n) ? formatNumber(n) : input.value;
  }

  function within(root, sel) {
    const out = [];
    if (root.matches && root.matches(sel)) out.push(root);
    if (root.querySelectorAll) out.push(...root.querySelectorAll(sel));
    return out;
  }

  // ─── counter animation ───────────────────────────────────────────

  // The final value is already the SSR text; the animation only walks
  // a display from the recorded start to it, written the page's way
  // (formatNumber), so the SSR text is never parsed: "4,820" reads as 4.
  // Reduced motion, or a non-numeric either end, leaves the SSR text
  // untouched. The animation never owns the number: a live write to
  // the signal (setSignal, a poll, a swap) that lands mid-walk changes
  // the text from the frame last written, and the walk stops there
  // instead of painting over it. A root animates once: a rescan of the
  // same markup (an insertion elsewhere, a navigation that kept it) is
  // not an arrival.
  const animated = new WeakSet();
  function animateCounter(root) {
    if (animated.has(root)) return;
    animated.add(root);
    const from = parseFloat(root.getAttribute('data-hui-counter-from'));
    const to = parseFloat(root.getAttribute('data-hui-counter-to'));
    const value = root.querySelector('[data-cui-signal]');
    if (!value || !Number.isFinite(from)) return;
    if (!Number.isFinite(to) || to === from) return;
    if (REDUCED && REDUCED.matches) return;
    let ms = parseFloat(root.getAttribute('data-hui-counter-ms'));
    if (!Number.isFinite(ms) || ms <= 0) ms = 800;
    const t0 = performance.now();
    let written = null;
    function frame(now) {
      if (!value.isConnected) return;
      if (written !== null && value.textContent !== written) return;
      const k = Math.min(1, (now - t0) / ms);
      const eased = 1 - Math.pow(1 - k, 3);
      written = formatNumber(k < 1 ? Math.round(from + (to - from) * eased) : to);
      value.textContent = written;
      if (k < 1) requestAnimationFrame(frame);
    }
    requestAnimationFrame(frame);
  }

  // ─── counter seed ────────────────────────────────────────────────

  // The kernel's data-cui-signal-inc steps the value the signal store
  // holds, not the markup: a counter no layer above seeded would step
  // from zero while it shows its Value. On arrival the counter hands
  // the store the value it shows (data-hui-counter-value, plain
  // digits): the fresh markup of an island swap or a navigation is the
  // number on screen, so it wins over a count stepped on the markup it
  // replaced. A root seeds once: a rescan of the same markup (an
  // insertion elsewhere, a navigation that kept it) is not an arrival,
  // and the count the reader stepped to stands. A counter whose page
  // seeds the signal itself (ui.Counter over a store.Slice) renders no
  // hook, so the page's seed stays the only one.
  const seeded = new WeakSet();
  function seedCounter(root) {
    if (seeded.has(root)) return;
    seeded.add(root);
    const n = parseFloat(root.getAttribute('data-hui-counter-value'));
    const value = root.querySelector('[data-cui-signal]');
    const name = value && value.getAttribute('data-cui-signal');
    if (!name || !Number.isFinite(n) || typeof NS.setSignal !== 'function') return;
    if (NS.getSignal(name) !== n) NS.setSignal(name, n);
  }

  // ─── number stepper ──────────────────────────────────────────────

  // Steps the input through the browser's own stepUp/stepDown, so the
  // next value is one the input accepts: inside min/max and on the
  // step base (min=1 step=5 walks 1, 6, 11, never 8), the rules the
  // form's validity checks. The change is then reported as real input
  // and change events so every pipeline that watches the control sees
  // it. The declared bounds are the server's; contradictory props are
  // refused at render, never repaired here. The input is the button's
  // sibling in the control's own row. The id defaults to the field's
  // name, so two forms with one field share it, and a page-wide
  // getElementById answers the first form's input.
  function step(btn, dir) {
    const id = btn.getAttribute('data-hui-number-input-for');
    const row = btn.parentElement;
    const input = id && row && [...row.children].find((el) => el.id === id);
    if (!input || input.disabled || input.readOnly) return;
    const before = input.value;
    try {
      if (dir > 0) input.stepUp(); else input.stepDown();
    } catch (_) {
      return; // not a stepping input type: nothing the browser would accept
    }
    if (input.value === before) return;
    input.dispatchEvent(new Event('input', { bubbles: true }));
    input.dispatchEvent(new Event('change', { bubbles: true }));
  }

  // ─── slider output mirror ────────────────────────────────────────

  function syncSlider(input) {
    const root = input.closest('[data-hui-slider]');
    const out = root && root.querySelector('[data-hui-slider-output]');
    if (out) out.textContent = valueText(input);
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
      out.textContent = fmt.replaceAll('{{low}}', () => valueText(low)).replaceAll('{{high}}', () => valueText(high));
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

  // ─── JSON text ───────────────────────────────────────────────────

  // A JSON control's text is parsed as it changes: an invalid value
  // takes the control's own sentence as its custom validity (the submit
  // stops on it) and reads aria-invalid; a valid or empty one clears
  // both. Empty is Required's to judge.
  function checkJSON(el) {
    const v = el.value.trim();
    let msg = '';
    if (v) { try { JSON.parse(v); } catch (_) { msg = el.getAttribute('data-hui-json') || 'Invalid JSON'; } }
    el.setCustomValidity(msg);
    if (msg) el.setAttribute('aria-invalid', 'true');
    else el.removeAttribute('aria-invalid');
  }

  document.addEventListener('input', function (e) {
    const t = e.target;
    if (!t || !t.closest) return;
    if (t.matches('[data-hui-json]')) { checkJSON(t); return; }
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
    // The seed first: an animation that starts after it walks toward
    // the value the store already holds.
    for (const el of within(scope, '[data-hui-counter-value]')) seedCounter(el);
    for (const el of within(scope, '[data-hui-counter-animate]')) animateCounter(el);
    for (const el of within(scope, '[data-hui-slider-output]')) {
      const input = el.closest('[data-hui-slider]');
      const range = input && input.querySelector('input[type="range"]');
      if (range) el.textContent = valueText(range);
    }
    for (const el of within(scope, '[data-hui-json]')) if (el.value.trim()) checkJSON(el);
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
