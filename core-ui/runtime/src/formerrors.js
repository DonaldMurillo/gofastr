// GoFastr runtime module, form errors
//
// The failure half of a data-fui-rpc form submission, loaded on demand
// by rpc.js the first time a form's request answers non-2xx. Like ws
// and desktop it has no DOM marker. The server's validation envelope
// ({error, fields: {name: [messages]}}) lands beside each named
// field's control in the exact markup framework/ui renders for a
// server-side error: aria-invalid and aria-describedby on the control,
// a role=alert paragraph carrying the field's error class
// (fui-field__error in a FormField, fui-choice-field__error beside a
// standalone Checkbox/Switch). When no field matched, the error text
// is toasted (module or fallback). Before this module a refused Save
// did nothing the user could see.
(() => {
  'use strict';
  window.__gofastr = window.__gofastr || {};
  const NS = window.__gofastr;
  const FIELD = '[data-fui-comp="ui-form-field"]';
  const CHOICE = '[data-fui-comp="ui-toggle"]';
  const LIVE = '.fui-field__error.is-live, .fui-choice-field__error.is-live';

  // clear removes what a previous failed attempt placed, so a retry
  // starts clean and a success leaves no stale error behind.
  function clear(form) {
    if (!form) return;
    form.querySelectorAll(LIVE).forEach((e) => e.remove());
    form.querySelectorAll('[aria-invalid="true"]').forEach((e) => e.removeAttribute('aria-invalid'));
  }

  // report renders the envelope into the form. status and txt are the
  // response's; a body that is not the envelope falls back to a toast
  // naming the status.
  function report(form, status, txt) {
    if (!form) return;
    let d = null;
    try { d = JSON.parse(txt); } catch (_) { d = null; }
    const fields = d && d.fields && typeof d.fields === 'object' ? d.fields : {};
    let placed = 0;
    for (const name of Object.keys(fields)) {
      let el = form.elements.namedItem(name);
      // A repeated name (the hidden+checkbox pair) answers a list; the
      // last control is the visible one.
      if (el && !el.tagName && el.length) el = el[el.length - 1];
      const field = el && el.closest && el.closest(FIELD);
      const choice = !field && el && el.closest && el.closest(CHOICE);
      if (!field && !choice) continue;
      el.setAttribute('aria-invalid', 'true');
      const p = document.createElement('p');
      p.className = (field ? 'fui-field__error' : 'fui-choice-field__error') + ' is-live';
      p.setAttribute('role', 'alert');
      if (el.id) { p.id = el.id + '-error'; el.setAttribute('aria-describedby', p.id); }
      p.textContent = [].concat(fields[name]).join(', ');
      // A FormField takes the message as its last child. A bare
      // Checkbox's root is its <label>, which may not hold a <p>: the
      // message follows it, the way the errored choice renders.
      if (field) field.appendChild(p);
      else if (choice.tagName === 'LABEL') choice.after(p);
      else choice.appendChild(p);
      placed++;
    }
    if (!placed && typeof NS._toastOrFallback === 'function') {
      const title = (d && typeof d.error === 'string' && d.error) || ('Request failed (' + status + ')');
      NS._toastOrFallback({ variant: 'error', title, ttl: 6000 });
    }
  }

  NS._formErrors = { clear, report };
  (NS.loadedModules ||= {}).formerrors = true;
})();
