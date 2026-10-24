// doc-dir: keeps <html dir> on the direction of the page's language
// across client navigations. The server writes <html dir> on every full
// page and data-cui-dir beside data-cui-lang on the doc markers; the
// core runtime copies only the language onto <html> after a swap, so
// this module copies the direction. It loads on the first right-to-left
// marker a page carries ([data-cui-dir="rtl"]): a site that never
// serves a right-to-left language never fetches it, and the core
// runtime carries no bytes for it.
//
// The direction is a function of the language, so any carrier whose
// data-cui-lang matches <html lang> (which the core has already set by
// the time gofastr:navigate fires) names the right direction, a stale
// outer layer included. A carrier the core built itself (the cached
// first page's) names only the language; then the direction comes from
// the language's likely script, the CLDR rule the server's i18n.Dir
// applies.
(function () {
  'use strict';
  const NAME = 'doc-dir';
  const NS = window.__gofastr = window.__gofastr || {};
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;

  // The scripts written right to left, as in core/i18n's rtlScripts.
  const RTL = new Set(['Adlm', 'Arab', 'Hebr', 'Mand', 'Nkoo', 'Rohg', 'Samr', 'Syrc', 'Thaa']);

  function scriptDir(lang) {
    try {
      return RTL.has(new Intl.Locale(lang).maximize().script) ? 'rtl' : 'ltr';
    } catch (_) {
      return '';
    }
  }

  function sync() {
    const html = document.documentElement;
    const lang = html.getAttribute('lang');
    if (!lang) return;
    let dir = '';
    document.querySelectorAll('[data-cui-dir][data-cui-lang]').forEach(function (c) {
      if (c.getAttribute('data-cui-lang') === lang) dir = c.getAttribute('data-cui-dir');
    });
    if (dir !== 'rtl' && dir !== 'ltr') dir = scriptDir(lang);
    if (dir && html.getAttribute('dir') !== dir) html.setAttribute('dir', dir);
  }

  sync();
  window.addEventListener('gofastr:navigate', sync);
})();
