// ConditionalField runtime module — show/hide content based on another field's value.
//
// Loaded on-demand when any [data-fui-comp="ui-conditional-field"] marker
// is on the page (or arrives via SPA-nav). Responsibilities:
//
//   1. On boot/SPA-swap: find all conditional fields, locate their parent
//      form, and evaluate the initial visibility state.
//   2. Listen for `change` and `input` events on each parent form.
//   3. For each conditional field, check if the watched field's current
//      value matches the WhenValue. Toggle `hidden` attribute and
//      `aria-hidden` accordingly.
//
// No server round-trip — visibility is client-only based on form state.

(() => {
  'use strict';

  // Evaluate all conditional fields within a root element.
  const evaluateAll = (root) => {
    const fields = (root || document).querySelectorAll('[data-fui-comp="ui-conditional-field"]');
    for (const f of fields) evaluateField(f);
  };

  // Evaluate a single conditional field's visibility.
  const evaluateField = (el) => {
    const whenName = el.getAttribute('data-when-name');
    const whenValue = el.getAttribute('data-when-value');
    if (!whenName || !whenValue) return;

    // Find the parent form (or fallback to document body).
    const form = el.closest('form') || el.closest('[data-fui-comp="ui-form"]') || document.body;

    // Find all inputs/selects/textareas with the matching name.
    const inputs = form.querySelectorAll('[name="' + whenName + '"]');
    let matched = false;

    for (const input of inputs) {
      const type = (input.getAttribute('type') || '').toLowerCase();

      if (type === 'radio') {
        // For radios, the value must match AND be checked.
        if (input.checked && input.value === whenValue) {
          matched = true;
          break;
        }
      } else if (type === 'checkbox') {
        // For checkboxes with a specific value, check value + checked.
        // For checkboxes without a value (or value="on"), match checked state
        // against whenValue === "true" or "checked".
        if (input.value === whenValue) {
          if (input.checked) {
            matched = true;
            break;
          }
        } else if (!input.value || input.value === 'on') {
          if (whenValue === 'true' || whenValue === 'checked') {
            matched = input.checked;
            if (matched) break;
          }
        }
      } else if (input.tagName === 'SELECT') {
        if (input.value === whenValue) {
          matched = true;
          break;
        }
      } else {
        // Text inputs, textareas, etc.
        if (input.value === whenValue) {
          matched = true;
          break;
        }
      }
    }

    if (matched) {
      el.removeAttribute('hidden');
      el.removeAttribute('aria-hidden');
      // Enable all form controls inside so they submit.
      toggleDisabled(el, false);
    } else {
      el.setAttribute('hidden', '');
      el.setAttribute('aria-hidden', 'true');
      // Disable all form controls so they are excluded from submission.
      toggleDisabled(el, true);
    }
  };

  // Enable or disable all form controls inside an element.
  // K-2: Mark controls we disable with data-fui-cond-disabled so we
  // only re-enable the ones we disabled — not ones the developer set.
  const toggleDisabled = (container, disabled) => {
    const controls = container.querySelectorAll('input,select,textarea,button');
    for (const c of controls) {
      if (disabled) {
        // Only disable controls that aren't already disabled.
        if (!c.disabled) {
          c.setAttribute('disabled', '');
          c.setAttribute('data-fui-cond-disabled', '');
        }
      } else {
        // Only re-enable controls that WE disabled.
        if (c.hasAttribute('data-fui-cond-disabled')) {
          c.removeAttribute('disabled');
          c.removeAttribute('data-fui-cond-disabled');
        }
      }
    }
  };

  // Attach change/input listeners to the parent form of all conditional
  // fields within root. Uses a Set of forms to avoid duplicate listeners.
  const attachListeners = (root) => {
    const fields = (root || document).querySelectorAll('[data-fui-comp="ui-conditional-field"]');
    const forms = new Set();

    for (const field of fields) {
      const form = field.closest('form') || field.closest('[data-fui-comp="ui-form"]') || document.body;
      if (!forms.has(form)) {
        forms.add(form);
        // J-1: Prevent duplicate listeners after SPA navigations.
        if (!form.__fuiConditionalWired) {
          form.__fuiConditionalWired = true;
          form.addEventListener('change', onFormChange);
          form.addEventListener('input', onFormChange);
        }
      }
    }
  };

  // Event handler: re-evaluate all conditional fields in the form.
  const onFormChange = (e) => {
    const form = e.target.closest('form') || e.target.closest('[data-fui-comp="ui-form"]') || document.body;
    const fields = form.querySelectorAll('[data-fui-comp="ui-conditional-field"]');
    for (const f of fields) evaluateField(f);
  };

  const init = (root) => {
    evaluateAll(root);
    attachListeners(root);
  };

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', () => init(document));
  } else {
    init(document);
  }

  // Register SPA rescan handler.
  if (window.__gofastr) {
    window.__gofastr._moduleScanners = window.__gofastr._moduleScanners || {};
    window.__gofastr._moduleScanners['conditionalfield'] = (root) => init(root);
  }

  (window.__gofastr.loadedModules = window.__gofastr.loadedModules || {}).conditionalfield = true;
})();
