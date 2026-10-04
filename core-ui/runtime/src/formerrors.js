// GoFastr runtime module, form errors
//
// The failure half of a data-cui-rpc form submission, loaded on demand
// by rpc.js the first time a form's request answers non-2xx. Like ws
// and desktop it has no DOM marker. The server's validation envelope
// ({error, fields: {name: [messages]}}) lands beside each named
// field's control through the headless layer's hooks, never a kit
// class: aria-invalid and aria-describedby on the control, the words
// in the field's error node ([data-hui-field-error], which
// headless.Field renders filled, reserved and empty, or not at all).
// A field with no error node gets one: a role=alert paragraph carrying
// the hook with the value "live", so clear() can tell it from the
// server's, placed where the rendered one would sit (last in a
// [data-hui-field], right after a bare [data-hui-choice] label). The
// kit's sheet styles the hook, so this module names no class and the
// kit may rename every one of its own. When no field matched, the
// error text is toasted (module or fallback). Before this module a
// refused Save did nothing the user could see.
(() => {
  'use strict';
  window.__gofastr = window.__gofastr || {};
  const NS = window.__gofastr;
  const FIELD = '[data-hui-field]';
  const CHOICE = '[data-hui-choice]';
  const ERR = '[data-hui-field-error]';

  // describe adds id to a control's aria-describedby tokens (on) or
  // drops it, keeping every other token: a hint stays described.
  function describe(el, id, on) {
    const t = (el.getAttribute('aria-describedby') || '').split(/\s+/).filter((x) => x && x !== id);
    if (on) t.push(id);
    if (t.length) el.setAttribute('aria-describedby', t.join(' '));
    else el.removeAttribute('aria-describedby');
  }

  // clear removes what a previous failed attempt placed, so a retry
  // starts clean and a success leaves no stale error behind: live
  // paragraphs go with their describedby token, filled rendered nodes
  // empty back to reserved.
  function clear(form) {
    if (!form) return;
    form.querySelectorAll('[data-hui-field-error="live"]').forEach((e) => {
      if (e.id) form.querySelectorAll('[aria-describedby]').forEach((c) => describe(c, e.id, false));
      e.remove();
    });
    form.querySelectorAll('[data-hui-field-error="filled"]').forEach((e) => {
      e.textContent = '';
      e.setAttribute('data-hui-field-error', '');
    });
    form.querySelectorAll('[aria-invalid="true"]').forEach((e) => e.removeAttribute('aria-invalid'));
  }

  // renderedNode finds the error node the server shipped for this
  // control: a field's is its own child, a bare choice's follows the
  // label (the errored render's shape).
  function renderedNode(field, choice) {
    if (field) return field.querySelector(':scope > ' + ERR);
    const next = choice.nextElementSibling;
    return next && next.matches(ERR) ? next : null;
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
      let p = renderedNode(field, choice);
      if (p) {
        p.setAttribute('data-hui-field-error', 'filled');
      } else {
        p = document.createElement('p');
        p.setAttribute('data-hui-field-error', 'live');
        p.setAttribute('role', 'alert');
        if (el.id) p.id = el.id + '-error';
        if (field) field.appendChild(p);
        else choice.after(p);
      }
      if (p.id) describe(el, p.id, true);
      p.textContent = [].concat(fields[name]).join(', ');
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
