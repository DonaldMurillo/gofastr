// TagInput runtime module — commit / remove tags as the user types.
//
// On the text input:
//   - Enter or comma → commit current value as a tag
//   - Backspace on empty → remove the last tag
// On a chip × button: remove that tag
//
// Each tag becomes a <input type=hidden name=<Name> value=<tag>>
// before the text input, so the form-submit shape is the standard
// repeated-key pattern.
(function () {
  'use strict';

  function zone(input) {
    return input.closest('[data-fui-tag-input-zone]');
  }

  function name(input) {
    return input.getAttribute('data-fui-tag-input');
  }

  function makeChip(value, formName) {
    const chip = document.createElement('span');
    chip.className = 'ui-tag-input__chip';

    const txt = document.createElement('span');
    txt.textContent = value;
    chip.appendChild(txt);

    const rm = document.createElement('button');
    rm.type = 'button';
    rm.className = 'ui-tag-input__chip-remove';
    rm.setAttribute('aria-label', 'Remove ' + value);
    rm.textContent = '×';
    chip.appendChild(rm);

    const hidden = document.createElement('input');
    hidden.type = 'hidden';
    hidden.name = formName;
    hidden.value = value;
    hidden.className = 'ui-tag-input__hidden';
    chip.appendChild(hidden);

    return chip;
  }

  function commit(input) {
    const v = (input.value || '').trim();
    if (!v) return false;
    const z = zone(input);
    if (!z) return false;
    const formName = name(input);
    // De-dupe: skip if a chip already holds this value.
    const existing = z.querySelectorAll('.ui-tag-input__chip .ui-tag-input__hidden');
    for (const h of existing) {
      if (h.value === v) {
        input.value = '';
        return false;
      }
    }
    const chip = makeChip(v, formName);
    z.insertBefore(chip, input);
    input.value = '';
    return true;
  }

  document.addEventListener('keydown', function (ev) {
    const t = ev.target;
    if (!t || !t.matches || !t.matches('input[data-fui-tag-input]')) return;
    if (ev.key === 'Enter' || ev.key === ',') {
      ev.preventDefault();
      commit(t);
      return;
    }
    if (ev.key === 'Backspace' && t.value === '') {
      const z = zone(t);
      if (!z) return;
      const chips = z.querySelectorAll('.ui-tag-input__chip');
      if (chips.length === 0) return;
      ev.preventDefault();
      chips[chips.length - 1].remove();
    }
  });

  // Commit on blur so the user doesn't lose a half-typed tag when
  // they tab away.
  document.addEventListener('blur', function (ev) {
    const t = ev.target;
    if (!t || !t.matches || !t.matches('input[data-fui-tag-input]')) return;
    commit(t);
  }, true);

  // Chip remove buttons.
  document.addEventListener('click', function (ev) {
    const btn = ev.target && ev.target.closest && ev.target.closest('.ui-tag-input__chip-remove');
    if (!btn) return;
    const chip = btn.closest('.ui-tag-input__chip');
    if (chip) chip.remove();
  });

  // For initial SSR-rendered tags: convert the hidden inputs into
  // chips so the SSR markup (which renders bare <input type=hidden>
  // before the text input) becomes the same DOM shape as runtime-
  // committed tags. This makes the remove/dedupe code uniform.
  function hydrate(root) {
    const scope = root && root.querySelectorAll ? root : document;
    scope.querySelectorAll('[data-fui-tag-input-zone]').forEach(function (z) {
      const hiddens = z.querySelectorAll(':scope > input.ui-tag-input__hidden');
      const textInput = z.querySelector('input[data-fui-tag-input]');
      hiddens.forEach(function (h) {
        const chip = makeChip(h.value, h.name);
        z.insertBefore(chip, textInput);
        h.remove();
      });
    });
  }
  hydrate(document);
  document.addEventListener('gofastr:navigate', function () { hydrate(document); });

  window.__gofastr = window.__gofastr || {};
  window.__gofastr.taginput = { rescan: hydrate };
})();
