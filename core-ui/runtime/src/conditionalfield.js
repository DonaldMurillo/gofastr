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

(function () {
  'use strict';

  // Evaluate all conditional fields within a root element.
  function evaluateAll(root) {
    var fields = (root || document).querySelectorAll('[data-fui-comp="ui-conditional-field"]');
    for (var i = 0; i < fields.length; i++) {
      evaluateField(fields[i]);
    }
  }

  // Evaluate a single conditional field's visibility.
  function evaluateField(el) {
    var whenName = el.getAttribute('data-when-name');
    var whenValue = el.getAttribute('data-when-value');
    if (!whenName || !whenValue) return;

    // Find the parent form (or fallback to document body).
    var form = el.closest('form') || el.closest('[data-fui-comp="ui-form"]') || document.body;

    // Find all inputs/selects/textareas with the matching name.
    var inputs = form.querySelectorAll('[name="' + whenName + '"]');
    var matched = false;

    for (var j = 0; j < inputs.length; j++) {
      var input = inputs[j];
      var type = (input.getAttribute('type') || '').toLowerCase();

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
  }

  // Enable or disable all form controls inside an element.
  // K-2: Mark controls we disable with data-fui-cond-disabled so we
  // only re-enable the ones we disabled — not ones the developer set.
  function toggleDisabled(container, disabled) {
    var controls = container.querySelectorAll('input,select,textarea,button');
    for (var i = 0; i < controls.length; i++) {
      if (disabled) {
        // Only disable controls that aren't already disabled.
        if (!controls[i].disabled) {
          controls[i].setAttribute('disabled', '');
          controls[i].setAttribute('data-fui-cond-disabled', '');
        }
      } else {
        // Only re-enable controls that WE disabled.
        if (controls[i].hasAttribute('data-fui-cond-disabled')) {
          controls[i].removeAttribute('disabled');
          controls[i].removeAttribute('data-fui-cond-disabled');
        }
      }
    }
  }

  // Attach change/input listeners to the parent form of all conditional
  // fields within root. Uses a Set of forms to avoid duplicate listeners.
  function attachListeners(root) {
    var fields = (root || document).querySelectorAll('[data-fui-comp="ui-conditional-field"]');
    var forms = new Set();

    for (var i = 0; i < fields.length; i++) {
      var form = fields[i].closest('form') || fields[i].closest('[data-fui-comp="ui-form"]') || document.body;
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
  }

  // Event handler: re-evaluate all conditional fields in the form.
  function onFormChange(e) {
    var form = e.target.closest('form') || e.target.closest('[data-fui-comp="ui-form"]') || document.body;
    var fields = form.querySelectorAll('[data-fui-comp="ui-conditional-field"]');
    for (var i = 0; i < fields.length; i++) {
      evaluateField(fields[i]);
    }
  }

  function init(root) {
    evaluateAll(root);
    attachListeners(root);
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', function () { init(document); });
  } else {
    init(document);
  }

  // Register SPA rescan handler.
  if (window.__gofastr) {
    window.__gofastr._moduleScanners = window.__gofastr._moduleScanners || {};
    window.__gofastr._moduleScanners['conditionalfield'] = function (root) {
      init(root);
    };
  }

  (window.__gofastr.loadedModules = window.__gofastr.loadedModules || {}).conditionalfield = true;
})();
