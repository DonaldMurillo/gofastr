// MultiSelect runtime module — chip rendering for checked options.
//
// On boot + every change inside a [data-fui-multiselect] disclosure,
// re-render the chips strip above the disclosure with one chip per
// :checked option. Clicking a chip's × button unchecks the option
// (which triggers a re-render).
//
// Native form-submit semantics: each checked checkbox submits its
// value under the shared Name. No hidden field, no JSON glue.
(function () {
  'use strict';

  function findRoot(node) {
    return node && node.closest && node.closest('[data-fui-comp="ui-multiselect"]');
  }

  function renderChips(root) {
    if (!root) return;
    const chips = root.querySelector('[data-fui-multiselect-chips]');
    if (!chips) return;
    const checked = root.querySelectorAll('.ui-multiselect__check:checked');
    chips.innerHTML = '';
    checked.forEach(function (cb) {
      const label = root.querySelector('label[for="' + cb.id + '"] .ui-multiselect__row-label');
      const text = label ? label.textContent : cb.value;
      const chip = document.createElement('span');
      chip.className = 'ui-multiselect__chip';

      const txt = document.createElement('span');
      txt.textContent = text;
      chip.appendChild(txt);

      const btn = document.createElement('button');
      btn.type = 'button';
      btn.className = 'ui-multiselect__chip-remove';
      btn.setAttribute('aria-label', 'Remove ' + text);
      btn.dataset.fuiMultiselectRemove = cb.id;
      btn.textContent = '×';
      chip.appendChild(btn);

      chips.appendChild(chip);
    });
  }

  // Change detection: any checkbox toggle inside a multiselect
  // re-renders that multiselect's chips.
  document.addEventListener('change', function (ev) {
    const t = ev.target;
    if (!t || !t.classList || !t.classList.contains('ui-multiselect__check')) return;
    renderChips(findRoot(t));
  });

  // Chip removal: click on .ui-multiselect__chip-remove unchecks
  // the linked input and re-renders.
  document.addEventListener('click', function (ev) {
    const btn = ev.target && ev.target.closest && ev.target.closest('[data-fui-multiselect-remove]');
    if (!btn) return;
    const cbId = btn.getAttribute('data-fui-multiselect-remove');
    if (!cbId) return;
    const cb = document.getElementById(cbId);
    if (!cb) return;
    cb.checked = false;
    cb.dispatchEvent(new Event('change', { bubbles: true }));
  });

  // Click-outside-to-close. Any click whose target is NOT inside an
  // open multiselect closes every open multiselect on the page. Use
  // mousedown rather than click so the close fires before a click on
  // a sibling button (e.g. a form submit) — feels snappier and
  // avoids the case where the click handler that we want to fire is
  // INSIDE the disclosure (it stays open until the action runs).
  document.addEventListener('mousedown', function (ev) {
    const opens = document.querySelectorAll('[data-fui-comp="ui-multiselect"] details.ui-multiselect__disclosure[open]');
    if (opens.length === 0) return;
    opens.forEach(function (d) {
      if (!d.contains(ev.target)) d.removeAttribute('open');
    });
  });

  // Esc closes any open multiselect, mirroring popover / menu UX.
  document.addEventListener('keydown', function (ev) {
    if (ev.key !== 'Escape') return;
    const opens = document.querySelectorAll('[data-fui-comp="ui-multiselect"] details.ui-multiselect__disclosure[open]');
    if (opens.length === 0) return;
    opens.forEach(function (d) {
      d.removeAttribute('open');
      const summary = d.querySelector('.ui-multiselect__summary');
      if (summary) summary.focus();
    });
  });

  function scan(root) {
    const scope = root && root.querySelectorAll ? root : document;
    scope.querySelectorAll('[data-fui-comp="ui-multiselect"]').forEach(renderChips);
  }
  scan(document);
  document.addEventListener('gofastr:navigate', function () { scan(document); });

  window.__gofastr = window.__gofastr || {};
  window.__gofastr.multiselect = { rescan: scan };
})();
