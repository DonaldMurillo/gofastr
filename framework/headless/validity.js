// headless-validity: the bridge between the browser's verdict on a
// field and the one assistive tech reads.
//
// The browser judges a control the reader has interacted with
// (:user-invalid and :user-valid: after they leave a changed field, or
// try to submit) and the kit's sheet draws that verdict, but assistive
// tech reads aria-invalid. This module copies the verdict onto
// aria-invalid, "true" or "false", so the state a screen reader hears
// is the state the border shows. It never runs ahead of the browser:
// a field nobody has left or submitted keeps no aria-invalid, and a
// script's checkValidity() (whose invalid events no reader caused)
// changes nothing.
//
// A server's verdict stands: while the control's described error node
// (Field's [data-hui-field-error], rendered or filled by the runtime's
// formerrors module) holds words, a native "valid" never flips the
// control back to "false"; the next submit's clear() decides. Loaded
// by the kernel when a Field or a Choice is on the page; an engine
// without :user-invalid loads it and does nothing.
(function () {
  'use strict';
  const NAME = 'headless-validity';
  const NS = window.__gofastr = window.__gofastr || {};
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;
  try { document.documentElement.matches(':user-invalid'); } catch (_) { return; }

  // told reports whether a server's words describe the control.
  const told = (el) => (el.getAttribute('aria-describedby') || '').split(/\s+/).some((id) => {
    const n = id && document.getElementById(id);
    return n && n.matches('[data-hui-field-error]') && n.textContent.trim() !== '';
  });

  // mirror copies the browser's verdict, and only once it has one.
  function mirror(el) {
    if (!el || !el.willValidate || !el.matches('input,select,textarea')) return;
    // Only a control in a Field or a Choice, the markup that loaded this.
    if (!el.closest('[data-hui-field],[data-hui-choice]')) return;
    const cur = el.getAttribute('aria-invalid');
    if (el.matches(':user-invalid')) {
      if (cur !== 'true') el.setAttribute('aria-invalid', 'true');
    } else if (el.matches(':user-valid') && cur !== 'false' && !told(el)) {
      el.setAttribute('aria-invalid', 'false');
    }
  }

  for (const type of ['focusout', 'change', 'input']) document.addEventListener(type, (e) => mirror(e.target), true);
  // A blocked submit marks every refused control at once; the browser
  // sets their user validity in the same task, so the read waits a turn.
  document.addEventListener('invalid', (e) => { const el = e.target; setTimeout(() => mirror(el), 0); }, true);
})();
