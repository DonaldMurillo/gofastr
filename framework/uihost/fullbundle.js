// Completes a first-paint subset. A page whose component bundle did not
// fit the first round trip inlines only the rules its markup can match,
// on <style data-cui-bundle data-cui-bundle-href="<the full bundle>">
// (firstpaint_prune.go). This module links the full sheet right after
// that block, so the states a click or a client navigation adds are
// styled, in the same cascade position: before app.css, which must keep
// winning ties. framework/uihost registers this file as the
// `fullbundle` behaviour and the kernel loads it when the marker is on
// the page.
(function () {
  'use strict';
  const NAME = 'fullbundle';
  const NS = window.__gofastr = window.__gofastr || {};
  // The kernel fetches a module once per page; anything that
  // evaluates this file a second time links nothing twice.
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;

  // The framework's two component-sheet routes: the bundle, or one
  // component's own sheet. A name never holds '/', so neither form can
  // climb out of the route.
  const SHEET = /^\/__gofastr\/comp(?:-bundle\.css\?|\/[\w:.-]+\.css\?)/;

  // Head only: the server writes the marker there, on the subset's
  // <style>, and markup in the body is page content. Only a sheet route
  // is linked, and the marker is spent either way.
  document.head.querySelectorAll('[data-cui-bundle-href]').forEach(function (el) {
    const href = el.getAttribute('data-cui-bundle-href');
    el.removeAttribute('data-cui-bundle-href');
    if (!SHEET.test(href)) return;
    const link = document.createElement('link');
    link.rel = 'stylesheet';
    link.href = href;
    link.setAttribute('data-cui-bundle', el.getAttribute('data-cui-bundle') || '');
    // The kernel counts the subset's components as loaded, so a client
    // navigation that renders more of them waits on this link (kernel
    // _sheetsFor finds it by data-cui-bundle), bounded like any
    // component sheet's wait.
    el.after(link);
  });
})();
