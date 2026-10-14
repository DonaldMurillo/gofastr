// SectionMenu's browser half: the filter box, the mobile pill's label
// (the page the reader is on, kept current across client-side
// navigations), and bringing the current page into view in a rail or
// sheet whose list overflows. core-ui/interactive registers this file as
// the `sectionmenu` behaviour; the kernel loads it when a menu is on the
// page.
//
// It binds the menu's own classes, which is why it lives beside the
// component and not in the kernel.
(function () {
  'use strict';
  const NAME = 'sectionmenu';
  const NS = window.__gofastr = window.__gofastr || {};
  // The kernel fetches a module once per page; anything that evaluates
  // this file a second time binds nothing twice.
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;

  const ROOT = '[data-cui-comp="cui-section-menu"]';
  const LINKS = '.cui-section-menu__lead, .cui-section-menu__link';
  // A link's label as rendered, read once: the filter rewrites the
  // link's children to mark a match.
  const labels = new WeakMap();
  // A sheet group's open state before a filter opened it, restored when
  // the filter clears.
  const opened = new WeakMap();

  const labelOf = (a) => {
    if (!labels.has(a)) labels.set(a, a.textContent);
    return labels.get(a);
  };
  const here = () => location.pathname + location.search;

  // Writes the label back with the matched run in a <mark>, or plain.
  // Text nodes only: a label is never parsed as markup.
  function paint(a, label, q) {
    const at = q ? label.toLowerCase().indexOf(q) : -1;
    if (at < 0) {
      if (a.firstElementChild || a.textContent !== label) a.textContent = label;
      return;
    }
    const mark = document.createElement('mark');
    mark.className = 'cui-section-menu__mark';
    mark.textContent = label.slice(at, at + q.length);
    a.replaceChildren(label.slice(0, at), mark, label.slice(at + q.length));
  }

  function filter(menu, raw) {
    const q = raw.trim().toLowerCase();
    let any = false;
    for (const a of menu.querySelectorAll(LINKS)) {
      const label = labelOf(a);
      const hit = !q || label.toLowerCase().includes(q);
      (a.closest('.cui-section-menu__item') || a).hidden = !hit;
      paint(a, label, hit ? q : '');
      if (hit) any = true;
    }
    for (const g of menu.querySelectorAll('.cui-section-menu__group')) {
      // Totals come from the items, not the count's text: a menu the
      // client cache restores carries the last filter's "n/total".
      const total = g.querySelectorAll('.cui-section-menu__item').length;
      const shown = g.querySelectorAll('.cui-section-menu__item:not([hidden])').length;
      const hit = shown > 0;
      g.hidden = !hit;
      const count = g.querySelector('.cui-section-menu__count');
      if (count) count.textContent = q ? shown + '/' + total : String(total);
      if (q) {
        if (!opened.has(g)) opened.set(g, g.open);
        if (hit) g.open = true;
      } else if (opened.has(g)) {
        g.open = opened.get(g);
        opened.delete(g);
      }
    }
    const empty = menu.querySelector('.cui-section-menu__empty');
    if (empty) empty.hidden = any;
    const body = menu.querySelector('.cui-section-menu__rail > .cui-section-menu__body');
    if (body) body.scrollTop = 0;
  }

  // The link for the page at path: an exact href match (the active-link
  // pass's rule), then the path without its query, then, when marked is
  // set, whatever the server marked current (Active on a URL the href
  // does not spell exactly). A navigation passes marked false: the old
  // page's mark may not have moved yet.
  function currentLink(scope, path, marked) {
    if (!scope) return null;
    const links = scope.querySelectorAll(LINKS);
    const bare = path.split('?')[0];
    for (const want of [path, bare]) {
      for (const a of links) if (a.getAttribute('href') === want) return a;
    }
    return marked ? scope.querySelector('[aria-current="page"]') : null;
  }

  // The pill names the current page and its group, or falls back to the
  // trigger label it carries at the start of its accessible name.
  function sync(menu, path, marked) {
    const btn = menu.querySelector(':scope > .cui-section-menu__trigger');
    const label = btn && btn.querySelector('.cui-section-menu__trigger-label');
    const group = btn && btn.querySelector('.cui-section-menu__trigger-group');
    if (!label || !group) return;
    const a = currentLink(menu.querySelector(':scope > .cui-section-menu__rail'), path, marked);
    if (a) {
      label.textContent = labelOf(a);
      const g = a.closest('.cui-section-menu__group');
      const gl = g && g.querySelector('.cui-section-menu__group-label');
      group.textContent = gl ? gl.textContent : '';
      return;
    }
    const sr = btn.querySelector('.cui-section-menu__sr');
    if (sr) label.textContent = sr.textContent.replace(/:\s*$/, '');
    group.textContent = '';
  }

  function scroller(el) {
    for (let n = el.parentElement; n && n !== document.body; n = n.parentElement) {
      const o = getComputedStyle(n).overflowY;
      if ((o === 'auto' || o === 'scroll') && n.scrollHeight > n.clientHeight) return n;
    }
    return null;
  }

  // Brings the current page into view when its list overflows: a third
  // of the way down what the pinned chrome (the sheet's top bar, the
  // group's header) leaves visible. A page already clear of it stays.
  // A sheet's stylesheet may land after it opens, so a list with no
  // scroller yet is retried for a few frames.
  function reveal(scope, tries) {
    const a = currentLink(scope, here(), true);
    if (!a || !a.getClientRects().length) return;
    const box = scroller(a);
    if (!box) {
      if (tries) requestAnimationFrame(() => reveal(scope, tries - 1));
      return;
    }
    const b = box.getBoundingClientRect();
    const bar = box.querySelector('.cui-section-menu__sheet-top');
    const head = a.closest('.cui-section-menu__group');
    const pinned = head && head.querySelector('.cui-section-menu__group-summary');
    const top = (bar ? bar.getBoundingClientRect().bottom : b.top) + (pinned ? pinned.offsetHeight : 0);
    const r = a.getBoundingClientRect();
    if (r.top >= top && r.bottom <= b.bottom - 8) return;
    box.scrollTop += r.top - top - (b.bottom - top) / 3;
  }

  function wire(menu) {
    if (menu.__sectionMenuWired) return;
    menu.__sectionMenuWired = true;
    const input = menu.querySelector('.cui-section-menu__filter-input');
    if (input) {
      input.addEventListener('input', () => filter(menu, input.value));
      // Escape clears a non-empty filter and stops there, so the sheet
      // stays open; the next Escape closes it.
      input.addEventListener('keydown', (e) => {
        if (e.key !== 'Escape' || !input.value) return;
        e.preventDefault();
        e.stopPropagation();
        input.value = '';
        filter(menu, '');
      });
    }
    // A menu the client cache restores keeps the hidden items and
    // marks of its last filter but not the box's text: settle the list
    // to what the box says now.
    if (input) filter(menu, input.value);
    sync(menu, here(), true);
    reveal(menu.querySelector(':scope > .cui-section-menu__rail'), 0);
  }

  // The arrival pass: at load, after an island swap and after a client
  // navigation the kernel calls the scanner with the inserted root.
  function scan(root) {
    const scope = root && root.querySelectorAll ? root : document;
    if (scope.matches && scope.matches(ROOT)) wire(scope);
    for (const menu of scope.querySelectorAll(ROOT)) wire(menu);
  }
  scan(document);
  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;

  // A menu the navigation swapped in carries the server's mark for the
  // new page; one that stayed may still carry the old page's.
  window.addEventListener('gofastr:navigate', (e) => {
    const path = (e.detail && e.detail.path) || here();
    const root = e.detail && e.detail.root;
    for (const menu of document.querySelectorAll(ROOT)) {
      sync(menu, path, !!(root && root.contains && root.contains(menu)));
    }
  });
  // The sheet mounts when it opens: wire its filter and bring the
  // current page into view once it has laid out.
  document.addEventListener('fui:widget-open', (e) => {
    const root = e.detail && e.detail.root;
    if (!root || !root.querySelectorAll) return;
    scan(root);
    const menu = root.matches && root.matches(ROOT) ? root : root.querySelector(ROOT);
    if (!menu) return;
    // The sheet is a <nav>, so the active-link pass opens the current
    // page's group; this only scrolls it into view.
    const body = menu.querySelector('.cui-section-menu__body');
    requestAnimationFrame(() => reveal(body, 30));
  });
})();
