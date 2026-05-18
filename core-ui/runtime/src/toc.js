// TableOfContents runtime module — walk the target region, emit a
// <li><a> for every <h2>/<h3> with an id, and track which heading is
// currently in view via IntersectionObserver.
//
// Loaded on-demand when a [data-fui-toc] element appears.
(function () {
  'use strict';

  function levelsFor(navEl) {
    const raw = navEl.getAttribute('data-fui-toc-levels') || '2,3';
    return raw.split(',').map(function (s) { return parseInt(s.trim(), 10); }).filter(Boolean);
  }

  function buildList(navEl) {
    const target = navEl.getAttribute('data-fui-toc');
    if (!target) return;
    const root = document.querySelector(target);
    if (!root) return;
    const wanted = levelsFor(navEl);
    const selector = wanted.map(function (l) { return 'h' + l + '[id]'; }).join(',');
    const heads = root.querySelectorAll(selector);
    const list = navEl.querySelector('.ui-toc__list');
    if (!list) return;
    list.innerHTML = '';
    if (heads.length === 0) {
      navEl.style.display = 'none';
      return;
    }
    navEl.style.display = '';
    heads.forEach(function (h) {
      const level = parseInt(h.tagName.substr(1), 10);
      const li = document.createElement('li');
      li.className = 'ui-toc__item ui-toc__item--h' + level;
      const a = document.createElement('a');
      a.className = 'ui-toc__link';
      a.href = '#' + h.id;
      a.textContent = h.textContent;
      a.dataset.fuiTocFor = h.id;
      li.appendChild(a);
      list.appendChild(li);
    });

    // IntersectionObserver — when a heading is in the upper half of
    // the viewport, mark its TOC link active.
    const links = {};
    navEl.querySelectorAll('.ui-toc__link[data-fui-toc-for]').forEach(function (a) {
      links[a.dataset.fuiTocFor] = a;
    });
    const observer = new IntersectionObserver(function (entries) {
      entries.forEach(function (e) {
        if (!e.isIntersecting) return;
        const id = e.target.id;
        // Clear previous active, set new.
        navEl.querySelectorAll('.ui-toc__link.is-active').forEach(function (a) {
          a.classList.remove('is-active');
        });
        if (links[id]) links[id].classList.add('is-active');
      });
    }, { rootMargin: '0px 0px -70% 0px', threshold: 0 });
    heads.forEach(function (h) { observer.observe(h); });
  }

  function scan(root) {
    const scope = root && root.querySelectorAll ? root : document;
    scope.querySelectorAll('[data-fui-comp="ui-toc"]').forEach(buildList);
  }

  // Initial pass — wait one rAF so the page DOM has settled.
  requestAnimationFrame(function () { scan(document); });
  document.addEventListener('gofastr:navigate', function () {
    requestAnimationFrame(function () { scan(document); });
  });

  window.__gofastr = window.__gofastr || {};
  window.__gofastr.toc = { rescan: scan };
})();
