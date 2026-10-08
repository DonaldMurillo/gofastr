// GoFastr runtime module, confirm
//
// The themed answer to data-cui-confirm, loaded on demand by the
// submit bridge and rpc.js the first time a gated control fires. The
// page carries the kit's dialog as an inert
// <template data-cui-confirm-dialog> (the host renders the template a
// kit registers); this module clones it, fills the parts it names by
// hook, opens it modal and resolves true on the accept button, false
// on cancel, Escape or a close by any other path. It names no class:
// every look is the kit's. A page with no template, or a browser with
// no showModal, falls back to window.confirm, so the gate never opens.
//
// Hooks on the gated element: data-cui-confirm (the message, required),
// data-cui-confirm-title, data-cui-confirm-accept (the accept button's
// label) and data-cui-confirm-tone="danger". Hooks in the template:
// data-cui-confirm-part="title|message|accept|accept-danger|cancel".
// The template carries the default title and labels; the two accept
// buttons are the kit's primary and danger variants, and the tone
// picks one, so the module never restyles a button. askWords asks the
// same dialog from words instead of an element's hooks: the leave
// guard's question comes from the form it would discard.
(() => {
  'use strict';
  window.__gofastr = window.__gofastr || {};
  const NS = window.__gofastr;
  const A = 'data-cui-confirm';

  function part(root, name) {
    for (const el of root.querySelectorAll('[data-cui-confirm-part]')) {
      if (el.getAttribute('data-cui-confirm-part') === name) return el;
    }
    return null;
  }

  function ask(el) {
    return askWords({
      message: el.getAttribute(A) || '',
      title: el.getAttribute(A + '-title'),
      accept: el.getAttribute(A + '-accept'),
      danger: el.getAttribute(A + '-tone') === 'danger',
    });
  }

  // askWords: {message, title, accept, danger}; an empty title or
  // accept keeps the template's own.
  function askWords(w) {
    const msg = w.message || '';
    const tpl = document.querySelector('template[data-cui-confirm-dialog]');
    const dlg = tpl && tpl.content.firstElementChild && tpl.content.firstElementChild.cloneNode(true);
    if (!dlg || typeof dlg.showModal !== 'function') {
      return Promise.resolve(typeof window.confirm === 'function' && window.confirm(msg));
    }
    const title = w.title;
    const label = w.accept;
    const danger = !!w.danger;
    const t = part(dlg, 'title');
    const m = part(dlg, 'message');
    const ok = part(dlg, danger ? 'accept-danger' : 'accept');
    const other = part(dlg, danger ? 'accept' : 'accept-danger');
    const cancel = part(dlg, 'cancel');
    if (t && title) t.textContent = title;
    if (m) m.textContent = msg;
    if (other) other.remove();
    if (ok && label) {
      const text = part(ok, 'label') || ok;
      text.textContent = label;
    }
    const back = document.activeElement;
    return new Promise((resolve) => {
      let answer = false;
      const finish = (v) => {
        answer = v;
        if (dlg.open) dlg.close();
      };
      if (ok) ok.addEventListener('click', (e) => { e.preventDefault(); finish(true); });
      if (cancel) cancel.addEventListener('click', (e) => { e.preventDefault(); finish(false); });
      // A click on the backdrop lands on the dialog element itself.
      dlg.addEventListener('click', (e) => { if (e.target === dlg) finish(false); });
      dlg.addEventListener('close', () => {
        dlg.remove();
        if (back && back.isConnected && typeof back.focus === 'function') back.focus();
        resolve(answer);
      }, { once: true });
      document.body.appendChild(dlg);
      dlg.showModal();
      // Cancel takes focus: Enter on an open confirm must never be the
      // destructive answer.
      if (cancel) cancel.focus();
    });
  }

  // confirmSubmit finishes a submit the boot gate cancelled. The
  // submitter's data-cui-confirm wins over the form's; on an accept the
  // form submits again, marked for the one synchronous re-dispatch
  // requestSubmit makes so the gate lets it through.
  function confirmSubmit(form, sub) {
    const el = sub && sub.getAttribute(A) ? sub : form;
    return ask(el).then((ok) => {
      if (!ok) return;
      form._ok = true;
      try {
        form.requestSubmit(sub && sub.form === form ? sub : null);
      } finally {
        delete form._ok;
      }
    });
  }

  NS.ask = ask;
  NS.askWords = askWords;
  NS.confirm = confirmSubmit;
  (NS.loadedModules ||= {}).confirm = true;
})();
