// NumberInput runtime module — wires the +/- step buttons to the
// associated <input type="number">. Click the button, parse its
// data-fui-number-step (signed delta), clamp to min/max, write back,
// dispatch an `input` event so any form-RPC pipeline that watches
// the input picks up the change.
//
// Loaded on-demand when [data-fui-number-step] markers appear.
(function () {
  'use strict';

  document.addEventListener('click', function (ev) {
    const btn = ev.target && ev.target.closest && ev.target.closest('[data-fui-number-step]');
    if (!btn) return;
    const inputId = btn.getAttribute('data-fui-number-for');
    if (!inputId) return;
    const input = document.getElementById(inputId);
    if (!input) return;
    const delta = parseFloat(btn.getAttribute('data-fui-number-step') || '0');
    if (!Number.isFinite(delta) || delta === 0) return;

    const cur = parseFloat(input.value || '0');
    let next = Number.isFinite(cur) ? cur + delta : delta;

    // Honor min / max attrs when set.
    const minAttr = input.getAttribute('min');
    const maxAttr = input.getAttribute('max');
    if (minAttr !== null && minAttr !== '') {
      const min = parseFloat(minAttr);
      if (Number.isFinite(min) && next < min) next = min;
    }
    if (maxAttr !== null && maxAttr !== '') {
      const max = parseFloat(maxAttr);
      if (Number.isFinite(max) && next > max) next = max;
    }

    // Round to avoid floating-point creep on small steps.
    const stepStr = input.getAttribute('step') || '1';
    const stepNum = parseFloat(stepStr);
    if (Number.isFinite(stepNum) && stepNum > 0 && stepStr.indexOf('.') === -1) {
      next = Math.round(next);
    }

    input.value = String(next);
    input.dispatchEvent(new Event('input', { bubbles: true }));
    input.dispatchEvent(new Event('change', { bubbles: true }));
  });
})();
