// SearchInput runtime module — shows/hides the clear button based on
// input value, and clears the input on clear-button click with refocus.
//
// Loaded on-demand when [data-fui-comp="ui-search-input"] markers appear.
(() => {
  'use strict';

  const wire = (root) => {
    const scope = root && root.querySelectorAll ? root : document;
    for (const wrapper of scope.querySelectorAll('[data-fui-comp="ui-search-input"]')) {
      const input = wrapper.querySelector('.ui-search-input__input');
      const clearBtn = wrapper.querySelector('.ui-search-input__clear');
      if (!input || !clearBtn) continue;

      // Avoid double-binding.
      if (input.__fuiSearchWired) continue;
      input.__fuiSearchWired = true;

      const updateClearVisibility = () => {
        if (input.value.length > 0) {
          clearBtn.removeAttribute('hidden');
        } else {
          clearBtn.setAttribute('hidden', '');
        }
      };

      input.addEventListener('input', updateClearVisibility);
      clearBtn.addEventListener('click', () => {
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
      input.addEventListener('keydown', (e) => {
        if (e.key !== 'Escape' || !input.value) return;
        e.preventDefault();
        e.stopPropagation();
        input.value = '';
        updateClearVisibility();
        input.dispatchEvent(new Event('input', { bubbles: true }));
      });

      // Initial state.
      updateClearVisibility();
    }
  };

  wire(document);
  document.addEventListener('gofastr:navigate', () => wire(document));

  // Register for SPA rescan.
  (window.__gofastr = window.__gofastr || {});
  (window.__gofastr._moduleScanners = window.__gofastr._moduleScanners || {});
  window.__gofastr._moduleScanners['searchinput'] = wire;

  // Mark module loaded.
  (window.__gofastr.loadedModules = window.__gofastr.loadedModules || {});
  window.__gofastr.loadedModules.searchinput = true;
})();
