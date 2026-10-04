// SearchInput's clear control: shows and hides the clear button from
// the input's value, clears the input on click or Escape and refocuses
// it. framework/ui registers this file as the `searchinput` behaviour
// and the kernel loads it when a SearchInput marker is on the page.
//
// This module is framework/ui's: SearchInput has no headless
// counterpart by binding decision (the icon, the clear button and the
// role="search" wrap are its own), so the module that drives it lives
// beside the component and binds the component's own classes. The
// kernel reaches no kit class; that is the whole reason this file left
// core-ui/runtime/src.
(function () {
  'use strict';
  const NAME = 'searchinput';
  const NS = window.__gofastr = window.__gofastr || {};
  // The kernel fetches a module once per page; anything that
  // evaluates this file a second time binds nothing twice.
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;

  function wireOne(wrapper) {
    const input = wrapper.querySelector('.fui-search__input');
    const clearBtn = wrapper.querySelector('.fui-search__clear');
    if (!input || !clearBtn) return;
    if (input.__searchWired) return;
    input.__searchWired = true;

    const updateClearVisibility = function () {
      if (input.value.length > 0) {
        clearBtn.removeAttribute('hidden');
      } else {
        clearBtn.setAttribute('hidden', '');
      }
    };
    const clear = function () {
      input.value = '';
      updateClearVisibility();
      // A real input event, so a form-RPC pipeline sees the change.
      input.dispatchEvent(new Event('input', { bubbles: true }));
    };

    input.addEventListener('input', updateClearVisibility);
    clearBtn.addEventListener('click', function () {
      clear();
      input.focus();
    });
    // Escape clears a non-empty box: the SearchInput is the canonical
    // clear-on-esc control and gets the behaviour without a marker on
    // every call site.
    input.addEventListener('keydown', function (e) {
      if (e.key !== 'Escape' || !input.value) return;
      e.preventDefault();
      e.stopPropagation();
      clear();
    });

    updateClearVisibility();
  }

  // The arrival pass: at load, after an island swap and after a client
  // navigation the kernel calls the scanner with the inserted root.
  function scan(root) {
    const scope = root && root.querySelectorAll ? root : document;
    const wrappers = scope.querySelectorAll('[data-cui-comp="ui-search-input"]');
    for (let i = 0; i < wrappers.length; i++) wireOne(wrappers[i]);
  }
  scan(document);
  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;
})();
