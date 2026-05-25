// PasswordInput runtime module — wires the show/hide toggle button to
// flip the input type between "password" and "text". Updates aria-label
// and aria-pressed on the toggle button to reflect the current state.
//
// Loaded on-demand when [data-fui-comp="ui-password-input"] markers appear.
(() => {
  'use strict';

  const wire = (root) => {
    const scope = root && root.querySelectorAll ? root : document;
    for (const wrapper of scope.querySelectorAll('[data-fui-comp="ui-password-input"]')) {
      const toggle = wrapper.querySelector('.ui-password-input__toggle');
      const input = wrapper.querySelector('.ui-password-input__input');
      if (!toggle || !input) continue;

      // Avoid double-binding.
      if (toggle.__fuiPasswordWired) continue;
      toggle.__fuiPasswordWired = true;

      toggle.addEventListener('click', () => {
        const showing = input.type === 'text';
        input.type = showing ? 'password' : 'text';
        toggle.setAttribute('aria-label', showing ? 'Show password' : 'Hide password');
        toggle.setAttribute('aria-pressed', showing ? 'false' : 'true');
        toggle.textContent = showing ? '⊙' : '⊘';
      });
    }
  };

  wire(document);
  document.addEventListener('gofastr:navigate', () => wire(document));

  // Register for SPA rescan.
  (window.__gofastr = window.__gofastr || {});
  (window.__gofastr._moduleScanners = window.__gofastr._moduleScanners || {});
  window.__gofastr._moduleScanners['passwordinput'] = wire;

  // Mark module loaded.
  (window.__gofastr.loadedModules = window.__gofastr.loadedModules || {});
  window.__gofastr.loadedModules.passwordinput = true;
})();
