// headless-wizard: the behaviour module for the step wizard's island
// path. Loaded by the kernel when a [data-hui-step-wizard] marker is
// on the page.
//
// The plain POST path needs nothing here: the server re-renders the
// page and the browser focuses nothing special, which is what a full
// navigation has always done. When the wizard carries an Island the
// submit becomes a region update, and this module records which
// control submitted, then after the swap focuses the error summary of
// a failed step (or the new step's heading) and says the step-of
// sentence the server rendered into the status node — clear-then-frame
// so a repeated sentence announces again.
(function () {
  'use strict';
  const NAME = 'headless-wizard';
  const NS = window.__gofastr = window.__gofastr || {};
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;

  function within(root, sel) {
    const out = [];
    if (root.matches && root.matches(sel)) out.push(root);
    if (root.querySelectorAll) out.push.apply(out, root.querySelectorAll(sel));
    return out;
  }

  // The submit record: the step the reader left. A failed validation
  // answers 200 with the region's HTML (the errors ARE the answer), so
  // the record survives into the swap for both outcomes and the step
  // the answer carries says which one it was.
  let fromStep = null;
  document.addEventListener('click', function (ev) {
    const t = ev.target;
    if (!t || !t.closest) return;
    const btn = t.closest('[data-hui-step-wizard-action]');
    if (!btn) return;
    const form = btn.closest('[data-hui-step-wizard]');
    fromStep = form ? form.getAttribute('data-hui-step-wizard-current') : null;
  }, true);

  function scan(root) {
    if (fromStep === null) return;
    const scope = root && root.querySelectorAll ? root : document;
    const form = within(scope, '[data-hui-step-wizard]')[0];
    if (!form) { fromStep = null; return; }
    // The summary of a failed submit first: it is what the reader must
    // hear. A step that did not move is a failed submit, and the
    // step-of sentence is not re-said for it.
    const summary = form.querySelector('[data-hui-form-errors] [tabindex="-1"]');
    const moved = form.getAttribute('data-hui-step-wizard-current') !== fromStep;
    if (summary) {
      summary.focus({ preventScroll: false });
    } else if (moved) {
      const heading = form.querySelector('h2[tabindex="-1"]');
      if (heading) heading.focus({ preventScroll: false });
    }
    if (moved) {
      const status = form.querySelector('[data-hui-step-wizard-status]');
      if (status && status.textContent) {
        const text = status.textContent;
        status.textContent = '';
        requestAnimationFrame(function () { status.textContent = text; });
      }
    }
    fromStep = null;
  }

  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;
})();
