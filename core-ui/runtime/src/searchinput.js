// SearchInput runtime module — shows/hides the clear button based on
// input value, and clears the input on clear-button click with refocus.
//
// Loaded on-demand when [data-fui-comp="ui-search-input"] markers appear.
(function () {
  'use strict';

  function wire(root) {
    var scope = root && root.querySelectorAll ? root : document;
    scope.querySelectorAll('[data-fui-comp="ui-search-input"]').forEach(function (wrapper) {
      var input = wrapper.querySelector('.ui-search-input__input');
      var clearBtn = wrapper.querySelector('.ui-search-input__clear');
      if (!input || !clearBtn) return;

      // Avoid double-binding.
      if (input.__fuiSearchWired) return;
      input.__fuiSearchWired = true;

      function updateClearVisibility() {
        if (input.value.length > 0) {
          clearBtn.removeAttribute('hidden');
        } else {
          clearBtn.setAttribute('hidden', '');
        }
      }

      input.addEventListener('input', updateClearVisibility);
      clearBtn.addEventListener('click', function () {
        input.value = '';
        input.focus();
        updateClearVisibility();
        // Dispatch input event so any form-RPC pipeline sees the change.
        input.dispatchEvent(new Event('input', { bubbles: true }));
      });
      // Escape clears the input — the SearchInput is the canonical
      // example of the "clear-on-esc" widget primitive, so it gets
      // the behaviour by default without needing data-fui-clear-on-esc
      // on every callsite.
      input.addEventListener('keydown', function (e) {
        if (e.key !== 'Escape' || !input.value) return;
        e.preventDefault();
        e.stopPropagation();
        input.value = '';
        updateClearVisibility();
        input.dispatchEvent(new Event('input', { bubbles: true }));
      });

      // Initial state.
      updateClearVisibility();
    });
  }

  wire(document);
  document.addEventListener('gofastr:navigate', function () { wire(document); });

  // Register for SPA rescan.
  (window.__gofastr = window.__gofastr || {});
  (window.__gofastr._moduleScanners = window.__gofastr._moduleScanners || {});
  window.__gofastr._moduleScanners['searchinput'] = wire;

  // Mark module loaded.
  (window.__gofastr.loadedModules = window.__gofastr.loadedModules || {});
  window.__gofastr.loadedModules.searchinput = true;
})();
