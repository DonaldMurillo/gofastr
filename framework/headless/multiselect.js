// headless-multiselect: the behaviour module for this package's
// multiselects — the chips strip above the disclosure, rebuilt from
// the checkboxes' own state after every change, and the chip remove
// buttons that uncheck the option they name. The disclosure half
// (Escape to close with focus returned, the aria-expanded mirror) is
// headless-disclosure's, which this module requires; what it adds is
// the click-outside close and the chips.
//
// The submit contract is the plain form and stays that way: every
// checkbox shares the field name, and a page with this module blocked
// submits exactly the checked options. No sentence is said here — the
// placeholder and the chip remove labels travel as attributes the
// component rendered from its Strings — and no inline style is
// written.
(function () {
  'use strict';
  const NAME = 'headless-multiselect';
  const NS = window.__gofastr = window.__gofastr || {};
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;

  function rootOf(node) {
    return node && node.closest && node.closest('[data-hui-multiselect]');
  }

  function fill(tpl, label) {
    return tpl.replace(/\{label\}/g, label);
  }

  // renderChips rebuilds the strip from the checked options. The chip
  // text is the LABEL the reader saw on the row, not the submit
  // value: the value is for the server and the label is for the
  // person.
  function renderChips(root) {
    if (!root) return;
    const chips = root.querySelector('[data-hui-multiselect-chips]');
    if (!chips) return;
    const removeLabel = chips.getAttribute('data-hui-multiselect-remove-label') || '';
    chips.textContent = '';
    root.querySelectorAll('input[type="checkbox"]').forEach(function (cb) {
      if (!cb.checked) return;
      const label = root.querySelector('label[for="' + CSS.escape(cb.id) + '"]');
      const text = label ? label.textContent.trim() : cb.value;
      const chip = document.createElement('span');
      chip.setAttribute('data-hui-multiselect-chip', '');

      const txt = document.createElement('span');
      txt.setAttribute('data-hui-multiselect-chip-text', '');
      txt.textContent = text;
      chip.appendChild(txt);

      const btn = document.createElement('button');
      btn.type = 'button';
      btn.setAttribute('data-hui-multiselect-remove', cb.id);
      if (removeLabel) btn.setAttribute('aria-label', fill(removeLabel, text));
      btn.textContent = '×';
      chip.appendChild(btn);

      chips.appendChild(chip);
    });
  }

  // Change detection: any checkbox toggle inside a multiselect
  // re-renders that multiselect's chips from the new state.
  document.addEventListener('change', function (ev) {
    const t = ev.target;
    if (!t || t.type !== 'checkbox') return;
    renderChips(rootOf(t));
  });

  // Chip removal: clicking a chip's × unchecks the linked checkbox,
  // which fires change and re-renders the chips.
  document.addEventListener('click', function (ev) {
    const btn = ev.target && ev.target.closest && ev.target.closest('[data-hui-multiselect-remove]');
    if (!btn) return;
    const cbID = btn.getAttribute('data-hui-multiselect-remove');
    if (!cbID) return;
    const cb = document.getElementById(cbID);
    if (!cb) return;
    cb.checked = false;
    cb.dispatchEvent(new Event('change', { bubbles: true }));
  });

  // Click-outside close: any click whose target is outside an open
  // multiselect closes it. Mousedown rather than click so the close
  // fires before a click on a sibling control (a form submit) — the
  // disclosure stays open long enough for an action inside it to run.
  document.addEventListener('mousedown', function (ev) {
    document.querySelectorAll('[data-hui-multiselect] details[open]').forEach(function (d) {
      if (!d.contains(ev.target)) d.removeAttribute('open');
    });
  });

  function scan(root) {
    const scope = root && root.querySelectorAll ? root : document;
    scope.querySelectorAll('[data-hui-multiselect]').forEach(renderChips);
  }

  scan(document);
  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;
})();
