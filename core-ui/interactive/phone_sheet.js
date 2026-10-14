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

  // An entry's name is the link's own text: a rail link wraps its label
  // in chips (a number before it, a count after it) that are not part
  // of the name. A link whose text sits only in elements reads it all.
  function entryName(a) {
    let own = '';
    for (const n of a.childNodes) if (n.nodeType === 3) own += n.textContent;
    return own.trim() || a.textContent.trim();
  }

  // The pill names the current entry, or what it opens while nothing
  // is current: its accessible name starts with that, ending in ": ".
  function follow(pill, sheet) {
    const label = pill.querySelector('.cui-sheet-pill__label');
    const sr = pill.querySelector('.cui-sheet-pill__sr');
    if (!label || !sr) return;
    const a = sheet.querySelector(CURRENT);
    const text = a ? entryName(a) : '';
    label.textContent = text || sr.textContent.replace(/:\s*$/, '');
  }

  function hang(pill, sheet) {
    sheet.style.setProperty('--cui-sheet-hang', pill.getBoundingClientRect().bottom + 'px');
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
    // An island or a poll can swap the list for markup that already
    // carries its mark, so new children and text count as well as the
    // attribute moving.
    new MutationObserver(() => follow(pill, sheet))
      .observe(sheet, { subtree: true, childList: true, characterData: true, attributes: true, attributeFilter: ['aria-current'] });
    sheet.addEventListener('click', (e) => {
      const a = e.target.closest && e.target.closest('a[href]');
      if (a && sheet.contains(a) && open(sheet)) sheet.hidePopover();
    });
    // A sheet that hangs from a bar hangs from where the bar is drawn:
    // pinned under the header once the page has scrolled to it, lower
    // while it still sits in flow. The open sheet holds the page still,
    // so the bar stays there until it closes.
    if (root.classList.contains('cui-phone-sheet--top')) {
      sheet.addEventListener('beforetoggle', (e) => {
        if (e.newState === 'open') hang(pill, sheet);
      });
    }
    // The sheet is a dialog, so opening it moves focus in: to the
    // current entry, brought into view in a long list, or else to the
    // close button, or the first entry where the sheet hangs from a bar
    // and drops its title row. Closing returns focus to the pill
    // (native popover).
    sheet.addEventListener('toggle', (e) => {
      if (e.newState !== 'open') return;
      const a = sheet.querySelector(CURRENT);
      const close = sheet.querySelector('.cui-sheet-head__close');
      const target = a || (close && close.getClientRects().length ? close : sheet.querySelector('a[href]'));
      if (a) a.scrollIntoView({ block: 'nearest' });
      if (target) target.focus({ preventScroll: true });
    });
  }

  // Past the swap the pill is hidden and the body is back in the page;
  // a sheet left open there would float over it. Below it, a turned
  // phone moves the bar, and a hanging sheet follows.
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
