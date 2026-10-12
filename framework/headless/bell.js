// headless-bell: the behaviour module for the notification bell's
// spoken count. Loaded by the kernel on the bell's marker, at boot, on
// insertion, or after a client navigation.
(() => {
  'use strict';
  const NAME = 'headless-bell';
  const NS = window.__gofastr = window.__gofastr || {};
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;

  // The spoken count follows the badge: the kernel writes a bound
  // signal's value into the badge span (UnreadBind puts the binding on
  // the badge, not the anchor), and an observer on that span's text
  // re-formats the anchor's accessible name through the sentence shape
  // the component rendered. Watching the badge, not the signal store,
  // means no subscription outlives a bell a navigation removed.
  const bellsWatched = new WeakSet();
  function sayBellCount(bell, badge) {
    const text = (badge.textContent || '').trim();
    // An empty badge is the signal's "nothing unread" (the sheet hides
    // it); anything else that is not a whole count leaves the name
    // alone: parseInt would read "12x" as 12 and announce a number the
    // badge does not show.
    if (!/^\d{0,9}$/.test(text)) return;
    const n = +text;
    const fmt = bell.getAttribute('data-hui-notification-count-fmt') || '';
    if (fmt) bell.setAttribute('aria-label', fmt.split('%d').join(n));
    // The badge's count attribute follows too, so the next reader of
    // it (a stylesheet's 99+ shaping, a test) sees the same number the
    // anchor says. An attribute write, so the observer does not hear it.
    badge.setAttribute('data-hui-notification-count', n);
  }
  function watchBell(bell) {
    if (bellsWatched.has(bell) || typeof MutationObserver !== 'function') return;
    const badge = bell.querySelector('[data-hui-notification-count]');
    if (!badge) return;
    bellsWatched.add(bell);
    new MutationObserver(() => { sayBellCount(bell, badge); })
      .observe(badge, { childList: true, characterData: true, subtree: true });
  }

  function scan(root) {
    const scope = root && root.querySelectorAll ? root : document;
    if (scope.matches && scope.matches('[data-hui-notification-bell]')) watchBell(scope);
    for (const bell of scope.querySelectorAll('[data-hui-notification-bell]')) watchBell(bell);
  }

  scan(document);
  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;
})();
