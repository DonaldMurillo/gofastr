// PhoneSheet's browser half. The sheet itself needs no script: it is a
// native popover the pill opens. This keeps the pill on the entry the
// sheet's body marks aria-current (a table of contents' observer moves
// that mark as the reader scrolls), closes the sheet when a link in it
// is followed, and closes an open sheet when the viewport grows past
// the swap, where the body goes back into the page. core-ui/interactive
// registers this file as the `phonesheet` behaviour.
(function () {
  'use strict';
  const NAME = 'phonesheet';
  const NS = window.__gofastr = window.__gofastr || {};
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;

  const ROOT = '[data-cui-comp="cui-phone-sheet"]';
  const CURRENT = 'a[aria-current]:not([aria-current="false"])';

  // The pill names the current entry, or what it opens while nothing
  // is current: its accessible name starts with that, ending in ": ".
  function follow(pill, sheet) {
    const label = pill.querySelector('.cui-sheet-pill__label');
    const sr = pill.querySelector('.cui-sheet-pill__sr');
    if (!label || !sr) return;
    const a = sheet.querySelector(CURRENT);
    const text = a ? a.textContent.trim() : '';
    label.textContent = text || sr.textContent.replace(/:\s*$/, '');
  }

  function open(sheet) {
    try { return sheet.matches(':popover-open'); } catch (_) { return false; }
  }

  function wire(root) {
    if (root.__phoneSheetWired) return;
    const pill = root.querySelector(':scope > .cui-sheet-pill');
    const sheet = root.querySelector(':scope > .cui-phone-sheet__sheet');
    if (!pill || !sheet) return;
    root.__phoneSheetWired = true;
    follow(pill, sheet);
    new MutationObserver(() => follow(pill, sheet))
      .observe(sheet, { subtree: true, attributes: true, attributeFilter: ['aria-current'] });
    sheet.addEventListener('click', (e) => {
      const a = e.target.closest && e.target.closest('a[href]');
      if (a && sheet.contains(a) && open(sheet)) sheet.hidePopover();
    });
    // Opening brings the current entry into view in a long list.
    sheet.addEventListener('toggle', (e) => {
      if (e.newState !== 'open') return;
      const a = sheet.querySelector(CURRENT);
      if (a) a.scrollIntoView({ block: 'nearest' });
    });
  }

  // Past the swap the pill is hidden and the body is back in the page;
  // a sheet left open there would float over it.
  window.addEventListener('resize', () => {
    for (const root of document.querySelectorAll(ROOT)) {
      const pill = root.querySelector(':scope > .cui-sheet-pill');
      const sheet = root.querySelector(':scope > .cui-phone-sheet__sheet');
      if (pill && sheet && open(sheet) && getComputedStyle(pill).display === 'none') sheet.hidePopover();
    }
  });

  function scan(root) {
    const scope = root && root.querySelectorAll ? root : document;
    if (scope.matches && scope.matches(ROOT)) wire(scope);
    for (const el of scope.querySelectorAll(ROOT)) wire(el);
  }
  scan(document);
  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;
})();
