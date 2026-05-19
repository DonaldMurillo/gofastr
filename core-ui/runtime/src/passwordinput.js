// PasswordInput runtime module — wires the show/hide toggle button to
// flip the input type between "password" and "text". Updates aria-label
// and aria-pressed on the toggle button to reflect the current state.
//
// Loaded on-demand when [data-fui-comp="ui-password-input"] markers appear.
(function () {
  'use strict';

  function wire(root) {
    var scope = root && root.querySelectorAll ? root : document;
    scope.querySelectorAll('[data-fui-comp="ui-password-input"]').forEach(function (wrapper) {
      var toggle = wrapper.querySelector('.ui-password-input__toggle');
      var input = wrapper.querySelector('.ui-password-input__input');
      if (!toggle || !input) return;

      // Avoid double-binding.
      if (toggle.__fuiPasswordWired) return;
      toggle.__fuiPasswordWired = true;

      toggle.addEventListener('click', function () {
        var showing = input.type === 'text';
        input.type = showing ? 'password' : 'text';
        toggle.setAttribute('aria-label', showing ? 'Show password' : 'Hide password');
        toggle.setAttribute('aria-pressed', showing ? 'false' : 'true');
        toggle.textContent = showing ? '⊙' : '⊘';
      });
    });
  }

  wire(document);
  document.addEventListener('gofastr:navigate', function () { wire(document); });

  // Register for SPA rescan.
  (window.__gofastr = window.__gofastr || {});
  (window.__gofastr._moduleScanners = window.__gofastr._moduleScanners || {});
  window.__gofastr._moduleScanners['passwordinput'] = wire;

  // Mark module loaded.
  (window.__gofastr.loadedModules = window.__gofastr.loadedModules || {});
  window.__gofastr.loadedModules.passwordinput = true;
})();
