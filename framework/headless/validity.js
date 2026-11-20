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

  // told reports whether a server's words stand for the control: an
  // error node it is described by, or its field's own, or the one after
  // its bare choice, still holding text.
  const said = (n) => !!n && n.matches('[data-hui-field-error]') && n.textContent.trim() !== '';
  const told = (el) => {
    if ((el.getAttribute('aria-describedby') || '').split(/\s+/).some((id) => said(id && document.getElementById(id)))) return true;
    const f = el.closest('[data-hui-field]');
    if (f) return [...f.querySelectorAll('[data-hui-field-error]')].some(said);
    const c = el.closest('[data-hui-choice]');
    return said(c && c.nextElementSibling);
  };

  // group is a radio's whole group (one verdict for all of it), or the
  // control alone.
  const group = (el) => el.type !== 'radio' || !el.name ? [el]
    : [...(el.form ? el.form.elements : document.querySelectorAll('input[type="radio"]'))].filter((r) => r.type === 'radio' && r.name === el.name);

  // mirror copies the browser's verdict, and only once it has one.
  function mirror(el) {
    if (!el || !el.willValidate || !el.matches('input,select,textarea')) return;
    // Only a control in a Field or a Choice, the markup that loaded this.
    if (!el.closest('[data-hui-field],[data-hui-choice]')) return;
    const all = group(el);
    const bad = all.some((r) => r.matches(':user-invalid'));
    if (!bad && (!all.some((r) => r.matches(':user-valid')) || all.some(told))) return;
    for (const r of all) r.setAttribute('aria-invalid', bad ? 'true' : 'false');
  }

  for (const type of ['focusout', 'change', 'input']) document.addEventListener(type, (e) => mirror(e.target), true);
  // A blocked submit marks every refused control at once; the browser
  // sets their user validity in the same task, so the read waits a turn.
  document.addEventListener('invalid', (e) => { const el = e.target; setTimeout(() => mirror(el), 0); }, true);
})();
