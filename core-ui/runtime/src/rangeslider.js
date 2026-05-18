// RangeSlider runtime module — keeps the min thumb ≤ max thumb on
// every input event and mirrors "lo – hi" into the optional <output>
// next to the label.
//
// Pairs are identified by the shared data-fui-range-slider="<id>"
// attribute. The mirror element carries data-fui-range-slider-value
// with the same id.
(function () {
  'use strict';

  function pairFor(id) {
    return {
      min: document.querySelector('input[data-fui-range-slider="' + id + '"].ui-range-slider__input--min'),
      max: document.querySelector('input[data-fui-range-slider="' + id + '"].ui-range-slider__input--max'),
      out: document.querySelector('output[data-fui-range-slider-value="' + id + '"]'),
    };
  }

  function syncMirror(p) {
    if (!p.out || !p.min || !p.max) return;
    p.out.textContent = p.min.value + ' – ' + p.max.value;
  }

  document.addEventListener('input', function (ev) {
    const t = ev.target;
    if (!t || !t.hasAttribute('data-fui-range-slider')) return;
    const id = t.getAttribute('data-fui-range-slider');
    const p = pairFor(id);
    if (!p.min || !p.max) return;
    // Cross-clamp: if min > max, snap whichever the user just moved.
    const lo = parseFloat(p.min.value);
    const hi = parseFloat(p.max.value);
    if (lo > hi) {
      if (t === p.min) p.min.value = hi;
      else p.max.value = lo;
    }
    syncMirror(p);
  });

  function refresh() {
    const seen = new Set();
    document.querySelectorAll('input[data-fui-range-slider]').forEach(function (el) {
      const id = el.getAttribute('data-fui-range-slider');
      if (seen.has(id)) return;
      seen.add(id);
      syncMirror(pairFor(id));
    });
  }
  refresh();
  document.addEventListener('gofastr:navigate', refresh);
})();
