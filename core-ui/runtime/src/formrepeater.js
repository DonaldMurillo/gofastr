// Form repeater: intercepts data-fui-rpc clicks on add/remove buttons
// inside a repeater island, collects all field values from the parent
// repeater, and appends them to the RPC URL so the server can pre-fill
// on re-render. Without this, swapping the island loses user input.
(() => {
  document.addEventListener('click', async (e) => {
    const btn = e.target.closest('[data-fui-comp="ui-form-repeater"] [data-fui-rpc]');
    if (!btn) return;

    const repeater = btn.closest('[data-fui-comp="ui-form-repeater"]');
    if (!repeater) return;

    const baseUrl = btn.getAttribute('data-fui-rpc');
    const sep = baseUrl.includes('?') ? '&' : '?';

    // Collect all input/select/textarea values in the repeater
    const params = new URLSearchParams();
    repeater.querySelectorAll('input:not([type="hidden"]),select,textarea').forEach(el => {
      if (el.name) params.append(el.name, el.value);
    });

    const qs = params.toString();
    const fullUrl = qs ? baseUrl + sep + qs : baseUrl;

    // Temporarily override the URL for this dispatch
    const orig = btn.getAttribute('data-fui-rpc');
    btn.setAttribute('data-fui-rpc', fullUrl);

    // Restore after a tick so subsequent clicks re-collect
    requestAnimationFrame(() => {
      btn.setAttribute('data-fui-rpc', orig);
    });
  }, true); // capture phase → runs before the global RPC handler
  (window.__gofastr.loadedModules = window.__gofastr.loadedModules || {});
  window.__gofastr.loadedModules.formrepeater = true;
})();
