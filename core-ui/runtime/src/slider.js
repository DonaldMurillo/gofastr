// Slider runtime module — mirrors the live range value into the
// associated <output> element so the displayed number tracks the
// thumb as the user drags. Pure delegation; survives partial-island
// swaps. Loaded on-demand when [data-fui-slider-mirror] appears.
(function () {
  'use strict';

  function syncOutput(input) {
    if (!input || input.type !== 'range') return;
    const id = input.id;
    if (!id) return;
    const out = document.querySelector('output[for="' + id + '"]');
    if (out) out.textContent = input.value;
  }

  document.addEventListener('input', function (ev) {
    const t = ev.target;
    if (!t || t.type !== 'range') return;
    if (!t.hasAttribute('data-fui-slider-mirror')) return;
    syncOutput(t);
  });

  // Initial pass + SPA-nav refresh — ensures the SSR-rendered output
  // matches the input.value at boot (in case the browser restored a
  // user-set value on bfcache restore).
  function refresh() {
    document.querySelectorAll('input[type="range"][data-fui-slider-mirror]')
      .forEach(syncOutput);
  }
  refresh();
  document.addEventListener('gofastr:navigate', refresh);
})();
