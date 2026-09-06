// GoFastr runtime module, form errors
//
// The failure half of a data-fui-rpc form submission, loaded on demand
// by rpc.js the first time a form's request answers non-2xx. Like ws
// and desktop it has no DOM marker. The server's validation envelope
// ({error, fields: {name: [messages]}}) lands in each named field's
// own error slot, the exact markup framework/ui.FormField renders for
// a server-side error (is-error on the wrapper, aria-invalid and
// aria-describedby on the control, a role=alert paragraph), and when
// no field matched, the error text is toasted (module or fallback).
// Before this module a refused Save did nothing the user could see.
(() => {
  'use strict';
  window.__gofastr = window.__gofastr || {};
  const NS = window.__gofastr;
  const FIELD = '[data-fui-comp="ui-form-field"]';

  // clear removes what a previous failed attempt placed, so a retry
  // starts clean and a success leaves no stale error behind.
  function clear(form) {
    if (!form) return;
    form.querySelectorAll('.ui-form-field__error.is-live').forEach((e) => e.remove());
    form.querySelectorAll('[aria-invalid="true"]').forEach((e) => {
      e.removeAttribute('aria-invalid');
      const w = e.closest(FIELD);
      if (w) w.classList.remove('is-error');
    });
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
      const wrap = el && el.closest && el.closest(FIELD);
      if (!wrap) continue;
      el.setAttribute('aria-invalid', 'true');
      wrap.classList.add('is-error');
      const p = document.createElement('p');
      p.className = 'ui-form-field__error is-live';
      p.setAttribute('role', 'alert');
      if (el.id) { p.id = el.id + '-error'; el.setAttribute('aria-describedby', p.id); }
      p.textContent = [].concat(fields[name]).join(', ');
      wrap.appendChild(p);
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
