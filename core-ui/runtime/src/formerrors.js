// GoFastr runtime module, form errors
//
// The failure half of a data-cui-rpc form submission, loaded on demand
// by rpc.js the first time a form's request answers non-2xx. Like ws
// and desktop it has no DOM marker. The server's validation envelope
// ({error, fields: {name: [messages]}}, or core/handler's nested
// {error: {message, fields}}) lands beside each named
// field's control through the headless layer's hooks, never a kit
// class: aria-invalid and aria-describedby on the control, the words
// in the field's error node ([data-hui-field-error], which
// headless.Field renders filled, reserved and empty, or not at all).
// A field with no error node gets one: a paragraph carrying the hook
// with the value "live", so clear() can tell it from the server's,
// placed where the rendered one would sit (last in a [data-hui-field],
// right after a bare [data-hui-choice] label). No error node is a live
// region: every refusal is read once, as one sentence per field, from
// the document's one polite region (#cui-form-errors). The kit's sheet
// styles the hook, so this module names no class of the kit's and the
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

  // say writes text into the one polite region, emptied first and
  // filled a beat later so the same words twice are read twice.
  let sayTimer = 0;
  function say(text) {
    clearTimeout(sayTimer);
    let r = document.getElementById('cui-form-errors');
    if (!r) {
      if (!text) return;
      r = document.createElement('div');
      r.id = 'cui-form-errors';
      r.setAttribute('role', 'status');
      r.setAttribute('aria-live', 'polite');
      r.className = 'cui-visually-hidden';
      document.body.appendChild(r);
    }
    r.textContent = '';
    if (text) sayTimer = setTimeout(() => { r.textContent = text; }, 50);
  }

  // describe adds id to a control's aria-describedby tokens (on) or
  // drops it, keeping every other token: a hint stays described.
  function describe(el, id, on) {
    const t = (el.getAttribute('aria-describedby') || '').split(/\s+/).filter((x) => x && x !== id);
    if (on) t.push(id);
    if (t.length) el.setAttribute('aria-describedby', t.join(' '));
    else el.removeAttribute('aria-describedby');
  }

  // clear removes every error the form shows, so a retry starts clean
  // and a success leaves no stale error behind: live paragraphs go with
  // their describedby token, and every other error node (one this
  // module filled, or one the server rendered with the page) empties
  // back to reserved.
  function clear(form) {
    if (!form) return;
    say('');
    // The form's own error nodes and described controls, plus those of
    // every control associated from outside it (form="id"), which
    // report() marks like any other: its field's, or the node right after
    // a bare choice.
    const errs = [...form.querySelectorAll(ERR)];
    const ctrls = [...form.querySelectorAll('[aria-describedby]')];
    for (const e of form.elements) {
      if (form.contains(e)) continue;
      ctrls.push(e);
      const f = e.closest(FIELD);
      const c = !f && e.closest(CHOICE);
      if (f) {
        errs.push(...f.querySelectorAll(ERR));
        ctrls.push(...f.querySelectorAll('[aria-describedby]'));
      } else if (c && c.nextElementSibling && c.nextElementSibling.matches(ERR)) {
        errs.push(c.nextElementSibling);
      }
    }
    for (const e of new Set(errs)) {
      if (e.matches('[data-hui-field-error="live"]')) {
        if (e.id) ctrls.forEach((c) => describe(c, e.id, false));
        e.remove();
      } else {
        e.textContent = '';
        e.setAttribute('data-hui-field-error', '');
      }
    }
    for (const e of [...form.querySelectorAll('[aria-invalid="true"]'), ...form.elements]) {
      if (e.getAttribute('aria-invalid') === 'true') e.removeAttribute('aria-invalid');
    }
  }


  // renderedNode finds the error node the server shipped for this
  // control: a field's is its own child, a bare choice's follows the
  // label (the errored render's shape).
  function renderedNode(field, choice) {
    if (field) return field.querySelector(':scope > ' + ERR);
    const next = choice.nextElementSibling;
    return next && next.matches(ERR) ? next : null;
  }

  // reveal opens every closed <details> around a refused control, so the
  // error is on screen. Setting open fires the element's toggle event,
  // which a disclosure controller mirrors into aria-expanded.
  function reveal(el) {
    for (let d = el.closest('details'); d; d = d.parentElement && d.parentElement.closest('details')) {
      if (!d.open) d.open = true;
    }
  }

  // report renders the envelope into the form. status and txt are the
  // response's; a body that is not the envelope falls back to a toast
  // naming the status. opts are the dispatch's. Only a submit carries
  // a submitter key (the submit bridges set it, null or a button), and
  // only a submit moves focus: never a click on a control or an
  // as-you-type dispatch, and only while focus is still in the form or
  // nowhere, so a late answer never pulls the reader out of what they
  // moved on to.
  function report(form, status, txt, opts) {
    if (!form) return;
    // The first refusal in a document loads this module after the
    // request, so rpc.js could not clear the server's own messages.
    clear(form);
    let d = null;
    try { d = JSON.parse(txt); } catch (_) { d = null; }
    // Every route refuses in one envelope ({error: "...", code, reason,
    // fields}): the CRUD routes and core/handler.WriteError, which every
    // typed handler answers through, alike.
    const fields = d && d.fields && typeof d.fields === 'object' ? d.fields : {};
    let placed = 0;
    const said = [];
    for (const name of Object.keys(fields)) {
      let el = form.elements.namedItem(name);
      // A repeated name (the hidden+checkbox pair) answers a list; the
      // last control is the visible one.
      if (el && !el.tagName && el.length) el = el[el.length - 1];
      const field = el && el.closest && el.closest(FIELD);
      const choice = !field && el && el.closest && el.closest(CHOICE);
      if (!field && !choice) continue;
      el.setAttribute('aria-invalid', 'true');
      reveal(el);
      let p = renderedNode(field, choice);
      if (p) {
        p.setAttribute('data-hui-field-error', 'filled');
      } else {
        p = document.createElement('p');
        p.setAttribute('data-hui-field-error', 'live');
        if (el.id) p.id = el.id + '-error';
        if (field) field.appendChild(p);
        else choice.after(p);
      }
      if (p.id) describe(el, p.id, true);
      p.textContent = [].concat(fields[name]).join(', ');
      const l = (field && field.querySelector(':scope > legend')) || (el.labels && el.labels[0]);
      said.push(((l && l.textContent.trim()) || name) + ': ' + p.textContent);
      placed++;
    }
    if (placed) say(said.join('. '));
    if (placed && opts && 'submitter' in opts) {
      const a = document.activeElement;
      // form.elements, not a descendant query: it is in tree order and
      // holds a control associated from outside the form (form="id").
      // A hidden input (a combobox's value) or a disabled control (its own
      // attribute or a disabled fieldset's) cannot take focus.
      // A refused radio group is marked on its last radio only, which may
      // be disabled or sit after another field, so every radio of a
      // marked group counts as refused; the reader lands on the group's
      // checked radio, or its first, among those that can take focus.
      const groups = new Set([...form.elements].filter((r) => r.type === 'radio' && r.getAttribute('aria-invalid') === 'true').map((r) => r.name));
      const enabled = [...form.elements].filter((e) => e.type !== 'hidden' && !e.matches(':disabled'));
      let first = enabled.find((e) => (e.type === 'radio' ? groups.has(e.name) : e.getAttribute('aria-invalid') === 'true'));
      if (first && first.type === 'radio') first = enabled.find((r) => r.type === 'radio' && r.name === first.name && r.checked) || first;
      if (first && (!a || a === document.body || form.contains(a) || a.form === form)) first.focus();
    }
    if (!placed && typeof NS._toastOrFallback === 'function') {
      const title = (d && typeof d.error === 'string' && d.error) || NS.say('runtime.request_failed_status', 'Request failed ({{status}})', { status });
      NS._toastOrFallback({ variant: 'error', title, ttl: 6000 });
    }
  }

  NS._formErrors = { clear, report };
  (NS.loadedModules ||= {}).formerrors = true;
})();
