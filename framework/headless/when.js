// headless-when: the behaviour module for ConditionalField's
// data-hui-when regions, split from the headless module when the
// system-dismissal cookie mirror pushed it over the size budget (the
// plan's rule: split a module rather than raise the constant). A
// region is shown when its watched field carries the value its
// data-hui-when-value names, and hidden — with its controls disabled
// under the runtime-owned data-hui-when-off mark — when it does not.
// The region renders VISIBLE server-side; this module is what hides
// the non-matching one, so a page without script shows every field.
(function () {
  'use strict';
  const NAME = 'headless-when';
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

  // watchedControls finds the controls a region's condition reads.
  // The region's own form comes first: two forms on one page can each
  // carry a "plan" control, and a region inside one form must follow
  // that form's plan, not whichever control the document happens to
  // offer first. A region with no form of its own — or one whose form
  // holds no control of that name — reads the document, preferring
  // controls no form owns: a form-less control is a page-level switch
  // a region outside the forms can belong to, where the first form's
  // control of the same name is that form's business. Among several
  // candidates the first in document order wins, so the rule is
  // deterministic.
  function watchedControls(region, name) {
    const sel = '[name="' + CSS.escape(name) + '"]';
    const all = document.querySelectorAll(sel);
    const form = region.closest('form');
    if (form) {
      // The form's controls are the ones it owns, not the ones inside
      // it: a control outside the element with form="id" belongs to
      // it, and one inside with form= pointing elsewhere does not.
      const own = [];
      for (const a of all) {
        if (a.form === form) own.push(a);
      }
      if (own.length) return own;
    }
    const loose = [];
    for (const a of all) {
      if (!a.form) loose.push(a);
    }
    return loose.length ? loose : all;
  }

  // whenValue reads the watched field's value the way the form would
  // submit it: the checked radio's value, a checkbox's value when
  // checked and the empty string when not, and any other control's
  // value.
  function whenValue(fields) {
    for (let i = 0; i < fields.length; i++) {
      const el = fields[i];
      if (el.type === 'radio') {
        if (el.checked) return el.value;
        continue;
      }
      if (el.type === 'checkbox') return el.checked ? el.value : '';
      return el.value;
    }
    return '';
  }

  // insideHiddenWhen reports whether el sits inside a [data-hui-when]
  // region that is hidden. Regions nest, and each hides on its own
  // condition; a region inside a hidden region is out whatever its
  // own condition says, because showing it would reach controls the
  // outer region's condition meant to keep out of the page and out of
  // the submit.
  function insideHiddenWhen(el) {
    for (let anc = el.parentElement; anc; anc = anc.parentElement) {
      if (anc.matches && anc.matches('[data-hui-when]') && anc.hidden) return true;
    }
    return false;
  }

  // syncWhenRegions is the whole when behaviour in two passes. The
  // first sets every region's effective visibility — its own
  // condition AND no hidden ancestor region — in document order, so
  // an outer region's fresh state is already on it when its
  // descendants look up. The second disables exactly the controls
  // inside any hidden region and re-enables only the controls hiding
  // disabled, told apart by the runtime-owned data-hui-when-off mark:
  // a control the page disabled itself is never touched. One mark
  // serves the whole nest because the second pass asks where the
  // control sits NOW, not which region disabled it — an inner region
  // showing inside a hidden outer one re-enables nothing.
  function syncWhenRegions(regions) {
    for (const region of regions) {
      const own = whenValue(watchedControls(region, region.dataset.huiWhen)) === region.dataset.huiWhenValue;
      region.hidden = !(own && !insideHiddenWhen(region));
    }
    for (const region of regions) {
      const controls = region.querySelectorAll('input, select, textarea, button');
      for (const c of controls) {
        if (insideHiddenWhen(c)) {
          if (!c.disabled) {
            c.disabled = true;
            c.dataset.huiWhenOff = '';
          }
        } else if (c.dataset.huiWhenOff !== undefined) {
          c.disabled = false;
          delete c.dataset.huiWhenOff;
        }
      }
    }
  }

  function scan(root) {
    const scope = root && root.querySelectorAll ? root : document;
    let regions = within(scope, '[data-hui-when]');
    // A subtree inserted inside a region arrives with no region of its
    // own above it: sync every enclosing region as well, so a control
    // inserted alone inside a hidden region is disabled like the
    // siblings it joined. The enclosing regions go FIRST, outermost
    // first, because the first pass reads an ancestor's hidden state
    // as it goes: an inserted swap that restores the gating value of
    // the region around it must un-hide that region before the regions
    // inside the swap look up, or they read the stale hidden and stay
    // buried until the next input.
    if (scope !== document && scope.closest) {
      const enclosing = [];
      for (let r = scope.closest('[data-hui-when]'); r; r = r.parentElement && r.parentElement.closest('[data-hui-when]')) {
        if (regions.indexOf(r) === -1) enclosing.unshift(r);
      }
      regions = enclosing.concat(regions);
    }
    if (regions.length) syncWhenRegions(regions);
  }

  // Delegated from the document, bound once at load: the watched
  // controls change through the document, and a region may watch a
  // control outside its own form (and one outside every form may watch
  // a control inside one), so no smaller scope holds.
  document.addEventListener('input', function () {
    syncWhenRegions(document.querySelectorAll('[data-hui-when]'));
  });
  document.addEventListener('change', function () {
    syncWhenRegions(document.querySelectorAll('[data-hui-when]'));
  });

  scan(document);
  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;
})();
