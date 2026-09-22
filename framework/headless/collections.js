// headless-collections: the behaviour module for the tag input and
// the repeater. Loaded by the kernel on one of its markers, at boot,
// on insertion, or after a client navigation.
//
// The tag input's committed values are chips and hidden inputs the
// server already rendered; this module commits the draft (Enter,
// comma, the add control, blur), removes chips (the × or Backspace on
// an empty draft), returns focus to the field, and announces through
// the sentences the component carries from its Strings — {name}
// substituted where the value goes, never a sentence of its own. The
// repeater's module half records which named submit control fired and
// which row it hit, and after the island swap replaces the region it
// puts focus back: the row's first control for a removal, the add
// control for an addition, and the server's own status sentence in
// the live region.
(function () {
  'use strict';
  const NAME = 'headless-collections';
  const NS = window.__gofastr = window.__gofastr || {};
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;

  function within(root, sel) {
    const out = [];
    if (root.matches && root.matches(sel)) out.push(root);
    if (root.querySelectorAll) out.push.apply(out, root.querySelectorAll(sel));
    return out;
  }

  // say writes a sentence into a live region clear-then-frame, so a
  // repeated identical sentence is announced again the way the table's
  // is.
  function say(node, text) {
    node.textContent = '';
    requestAnimationFrame(function () { node.textContent = text; });
  }

  // ─── tag input ───────────────────────────────────────────────────

  function tagRoot(input) {
    return input.closest('[data-hui-tag-input]');
  }
  function fieldOf(root) {
    return root && root.querySelector('[data-hui-tag-input-field]');
  }
  function statusOf(root) {
    return root && root.querySelector('[data-hui-tag-input-status]');
  }
  function hiddensOf(root) {
    return root ? Array.prototype.slice.call(root.querySelectorAll('input[type="hidden"]')) : [];
  }

  // makeChip builds the same shape the server rendered: a list item
  // carrying the remove hook, its text, and its hidden input. The
  // remove control's accessible name comes from the label template the
  // root carries, %s where the value goes.
  function makeChip(root, v) {
    const doc = document;
    const li = doc.createElement('li');
    li.setAttribute('data-hui-tag-input-remove', '');
    const labelFmt = root.getAttribute('data-hui-tag-input-remove-label') || '';
    li.setAttribute('aria-label', labelFmt.replace('%s', v));
    li.appendChild(doc.createTextNode(v));
    const rm = doc.createElement('button');
    rm.type = 'button';
    rm.setAttribute('aria-label', labelFmt.replace('%s', v));
    rm.textContent = '×';
    li.appendChild(rm);
    const hidden = doc.createElement('input');
    hidden.type = 'hidden';
    hidden.name = root.getAttribute('data-hui-tag-input') || '';
    hidden.value = v;
    li.appendChild(hidden);
    return li;
  }

  function commit(input) {
    const v = (input.value || '').trim();
    const root = tagRoot(input);
    if (!root || !v) return false;
    const list = root.querySelector('[data-hui-tag-input-list]');
    if (!list) return false;
    // A chip already holding the value is a duplicate: the draft
    // clears and nothing else happens.
    for (const h of hiddensOf(root)) {
      if (h.value === v) { input.value = ''; return false; }
    }
    const cap = parseInt(root.getAttribute('data-hui-tag-input-maxlength') || '0', 10);
    let val = v;
    if (cap > 0 && val.length > cap) val = val.slice(0, cap);
    list.appendChild(makeChip(root, val));
    input.value = '';
    const status = statusOf(root);
    if (status) say(status, (status.getAttribute('data-hui-tag-input-added') || '').replace('{name}', val));
    return true;
  }

  function removeChip(chip) {
    const root = chip.closest('[data-hui-tag-input]');
    const v = chip.textContent.replace('×', '').trim();
    // The hidden input this chip rode with is inside it; removing the
    // list item removes the value from the form.
    const status = statusOf(root);
    if (status) say(status, (status.getAttribute('data-hui-tag-input-removed') || '').replace('{name}', v));
    chip.remove();
    const field = fieldOf(root);
    if (field) field.focus();
  }

  // Enter's default in a single-input form is implicit submission,
  // and Chromium fires that submit even when the keydown's default is
  // prevented — TestE2E_TagInputEnterCommitsWithoutSubmittingTheForm
  // is the proof. The suppression is TASK-SCOPED, not timed: the flag
  // is set on the Enter keydown, consumed by the first submit of the
  // same task, and dropped at the task's end, so a submit from any
  // later event — a Save click, always a later macrotask — can never
  // land inside it. (The 50ms window this replaces was a heuristic
  // that could swallow a fast real submit; the task boundary cannot.)
  let suppressSubmit = false;
  document.addEventListener('submit', function (ev) {
    if (suppressSubmit) {
      suppressSubmit = false;
      ev.preventDefault();
      ev.stopImmediatePropagation();
    }
  }, true);

  document.addEventListener('keydown', function (ev) {
    const t = ev.target;
    if (!t || !t.matches || !t.matches('[data-hui-tag-input-field]')) return;
    // IME composition (CJK input): the Enter that confirms a
    // conversion candidate carries isComposing=true and the field
    // still holds the pre-conversion text; committing there ships the
    // raw romaji as a tag.
    if (ev.isComposing) return;
    if (ev.key === 'Enter' || ev.key === ',') {
      ev.preventDefault();
      suppressSubmit = true;
      setTimeout(function () { suppressSubmit = false; }, 0);
      commit(t);
      return;
    }
    if (ev.key === 'Backspace' && t.value === '') {
      const root = tagRoot(t);
      const chips = root ? root.querySelectorAll('[data-hui-tag-input-remove]') : [];
      if (chips.length === 0) return;
      ev.preventDefault();
      removeChip(chips[chips.length - 1]);
    }
  }, true);

  // Commit on blur so a half-typed tag is not lost on tab.
  document.addEventListener('blur', function (ev) {
    const t = ev.target;
    if (t && t.matches && t.matches('[data-hui-tag-input-field]')) commit(t);
  }, true);

  document.addEventListener('click', function (ev) {
    const t = ev.target;
    if (!t || !t.closest) return;
    const chip = t.closest('[data-hui-tag-input-remove]');
    if (chip) { removeChip(chip); return; }
    const add = t.closest('[data-hui-tag-input-add]');
    if (add) {
      const root = add.closest('[data-hui-tag-input]');
      const field = fieldOf(root);
      if (field) commit(field);
    }
  });

  // ─── repeater ────────────────────────────────────────────────────

  // The click record: which operation fired and which row it hit, so
  // the swap-restore pass knows where focus belongs. Any other click
  // clears it, so an old click cannot own a later swap.
  let repOp = null;
  document.addEventListener('click', function (ev) {
    const t = ev.target;
    if (!t || !t.closest) return;
    const btn = t.closest('[data-hui-repeater-action]');
    if (!btn) { repOp = null; return; }
    const root = btn.closest('[data-hui-repeater]');
    const row = btn.closest('[data-hui-repeater-item]');
    repOp = {
      root: root,
      action: btn.getAttribute('data-hui-repeater-action'),
      index: row ? parseInt(row.getAttribute('data-hui-repeater-index') || '-1', 10) : -1,
    };
  }, true);

  // ─── the arrival pass ────────────────────────────────────────────

  // After an island swap the kernel hands the inserted region here:
  // the pending operation puts focus back where it belongs and the
  // region's own status sentence (the server's words, re-rendered
  // with the region) is said clear-then-frame.
  function scan(root) {
    if (!repOp) return;
    const scope = root && root.querySelectorAll ? root : document;
    const rep = within(scope, '[data-hui-repeater]')[0] ||
      (scope.closest ? scope.closest('[data-hui-repeater]') : null);
    if (!rep || repOp.root == null) { repOp = null; return; }
    if (repOp.action === 'remove' && repOp.index >= 0) {
      const rows = rep.querySelectorAll('[data-hui-repeater-index]');
      let target = rows[repOp.index] || rows[rows.length - 1];
      const ctl = target && target.querySelector('input,select,textarea');
      if (ctl) ctl.focus({ preventScroll: true });
    } else {
      const add = rep.querySelector('[data-hui-repeater-action="add"]');
      if (add) add.focus({ preventScroll: true });
    }
    const status = rep.querySelector('[data-hui-repeater-status]');
    if (status && status.textContent) say(status, status.textContent);
    repOp = null;
  }

  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;
})();
