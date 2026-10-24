// sitesearch-page: hydrates the search page ui.SearchResults renders
// (data-search-page). The server rendered the form, the status line,
// the results and the pager, and the page works without script; this
// module makes typing update them in place, the input never replaced,
// and moves ?q= along with history.replaceState through the router.
//
// On a live server it fetches the page's own render for the new query
// through the router's partial protocol and swaps the status, list and
// pager in, so the markup and the pagination stay the server's. A
// static export has no server: there it ranks with the static ranker,
// draws the rows with DOM calls, and a "more" button fetches the next
// results. Index text never reaches innerHTML.
(function () {
  'use strict';
  const NAME = 'sitesearch-page';
  const NS = window.__gofastr = window.__gofastr || {};
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;

  const ROOT = '/__gofastr/search/';
  const BASE = ROOT.slice(0, ROOT.indexOf('/__' + 'gofastr/'));
  const DEBOUNCE_MS = 150;
  const STATIC = document.documentElement.hasAttribute('data-cui-static');
  const bound = new WeakSet();
  const seq = new WeakMap();

  // The parts ui.SearchResults marks, as literal selectors.
  const PART = {
    input: '[data-search-page-input]', status: '[data-search-page-status]',
    list: '[data-search-page-list]', pager: '[data-search-page-pager]',
  };

  function part(sec, name) { return sec.querySelector(PART[name]); }

  function words(sec, total, q) {
    if (!q) return '';
    const f = sec.getAttribute('data-search-page-' + (total === 0 ? 'none' : total === 1 ? 'one' : 'count')) || '';
    return f.replace(/\{\{([nq])\}\}/g, function (_, k) { return k === 'n' ? String(total) : q; });
  }

  // moveURL keeps the address on the query the list shows, replacing
  // the entry: a typed query is no new page.
  function moveURL(q) {
    const url = location.pathname + (q ? '?q=' + encodeURIComponent(q) : '') + location.hash;
    if (NS._pushURL) NS._pushURL(url, { replace: true });
  }

  // live swaps in the server's render of the page for q. The answer is
  // a navigation answer, so it is read the way the router reads one: a
  // session rollover lands on the stream meta before anything else (the
  // cookie already moved), and a policy redirect goes to the router.
  async function live(sec, q, n) {
    const url = location.pathname + (q ? '?q=' + encodeURIComponent(q) : '');
    const r = await fetch(url, {
      credentials: 'same-origin',
      headers: { 'X-Gofastr-Navigate': '1', 'X-Gofastr-Markup': NS._markup || '2', 'X-Gofastr-From': location.pathname },
    });
    const rs = r.headers.get('X-Gofastr-Session'), meta = rs && document.querySelector('meta[name="gofastr-sse"]');
    if (meta) meta.setAttribute('content', meta.getAttribute('content').replace(/([?&]session=)[^&]*/, function (_, p) { return p + rs; }));
    if (!current(sec, n)) return;
    const to = r.headers.get('X-Gofastr-Location');
    if (to) return NS.navigate(to, { replace: true });
    if (!r.ok) throw new Error('sitesearch-page: ' + r.status);
    const doc = new DOMParser().parseFromString(await r.text(), 'text/html');
    const fresh = doc.querySelector('[data-search-page]');
    if (!fresh || !current(sec, n)) return;
    part(sec, 'status').textContent = part(fresh, 'status').textContent;
    part(sec, 'list').replaceChildren(...Array.from(part(fresh, 'list').childNodes, function (c) { return document.adoptNode(c); }));
    const pager = part(sec, 'pager'), next = part(fresh, 'pager');
    if (pager) pager.remove();
    if (next) sec.append(document.adoptNode(next));
    sec.setAttribute('data-search-page-query', q);
  }

  // ── Static rows: ui.SearchResults' markup, built with DOM calls ──

  function el(tag, cls, text) {
    const e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text != null) e.textContent = text;
    return e;
  }

  function link(cls, href, text) {
    const a = el('a', cls, text);
    a.href = BASE + href;
    return a;
  }

  function excerpt(segs) {
    const p = el('p', 'fui-search-results__excerpt');
    for (const s of segs) p.append(s.h ? el('mark', '', s.t) : document.createTextNode(s.t));
    return p;
  }

  function item(r) {
    const li = el('li', 'fui-search-results__item');
    const head = el('div', 'fui-search-results__head');
    head.append(link('fui-search-results__title', r.url, r.title));
    if (r.group) head.append(el('span', 'fui-search-results__group', r.group));
    li.append(head);
    if (r.excerpt && r.excerpt.length) li.append(excerpt(r.excerpt));
    if (r.sections && r.sections.length) {
      const ul = el('ul', 'fui-search-results__sections');
      for (const s of r.sections) {
        const sli = el('li');
        sli.append(link('fui-search-results__section', s.id ? r.url + '#' + s.id : r.url, s.heading));
        if (s.excerpt && s.excerpt.length) sli.append(excerpt(s.excerpt));
        ul.append(sli);
      }
      li.append(ul);
    }
    return li;
  }

  // statics ranks q in the browser and shows the first limit results,
  // with a button for the next ones while there are more.
  async function statics(sec, q, n, shown) {
    const limit = (+sec.getAttribute('data-search-page-limit') || 20) + shown;
    await NS.loadModule('sitesearch-static');
    const found = await NS._sitesearch.staticFind(q, limit);
    if (!current(sec, n)) return;
    part(sec, 'status').textContent = words(sec, found.total, q);
    const list = part(sec, 'list');
    list.replaceChildren(...found.results.map(item));
    const pager = part(sec, 'pager');
    if (pager) pager.remove();
    if (found.total > found.results.length) {
      const wrap = el('div', 'fui-search-results__pager');
      wrap.setAttribute('data-search-page-pager', '');
      const more = el('button', 'fui-button fui-button--secondary', sec.getAttribute('data-search-page-more'));
      more.type = 'button';
      more.addEventListener('click', function () {
        const m = (seq.get(sec) || 0) + 1;
        seq.set(sec, m);
        statics(sec, q, m, found.results.length).catch(warn);
      });
      wrap.append(more);
      sec.append(wrap);
    }
    sec.setAttribute('data-search-page-query', q);
    // "More" replaced the button the reader pressed: focus goes to the
    // first result it revealed, not to the body.
    const first = shown && list.children[shown];
    if (first) first.querySelector('a').focus();
  }

  function warn(err) { if (window.console) console.warn('sitesearch-page:', err); }

  // current: n is still the latest query and the page is still on
  // screen. A navigation away detaches the section, and its pending
  // work must not write the new page's address or list.
  function current(sec, n) { return seq.get(sec) === n && sec.isConnected && own(sec); }

  function run(sec, raw) {
    if (!sec.isConnected || !own(sec)) return;
    const q = raw.trim();
    const n = (seq.get(sec) || 0) + 1;
    seq.set(sec, n);
    moveURL(q);
    (STATIC ? (q ? statics(sec, q, n, 0) : Promise.resolve(clear(sec))) : live(sec, q, n)).catch(warn);
  }

  function clear(sec) {
    part(sec, 'status').textContent = '';
    part(sec, 'list').replaceChildren();
    const pager = part(sec, 'pager');
    if (pager) pager.remove();
    sec.setAttribute('data-search-page-query', '');
  }

  // own: the page is the search page the markup names. Elsewhere (a
  // showcase, a sidebar) the form stays a plain GET to that page.
  function own(sec) {
    const trim = function (p) { return p.length > 1 ? p.replace(/\/$/, '') : p; };
    return trim(BASE + (sec.getAttribute('data-search-page') || '')) === trim(location.pathname);
  }

  function bind(sec) {
    if (bound.has(sec)) return;
    bound.add(sec);
    const input = part(sec, 'input');
    if (!input || !own(sec)) return;
    let timer = 0;
    input.addEventListener('input', function () {
      clearTimeout(timer);
      timer = setTimeout(function () { run(sec, input.value); }, DEBOUNCE_MS);
    });
    if (input.form) {
      input.form.addEventListener('submit', function (e) {
        e.preventDefault();
        clearTimeout(timer);
        run(sec, input.value);
      });
    }
    // An export renders the page with no query: the one in the address
    // goes in the box as a reader's input would, so the box's own
    // controls (its clear button) see it too.
    if (STATIC) {
      const q = new URLSearchParams(location.search).get('q');
      if (q && !input.value) {
        input.value = q;
        input.dispatchEvent(new Event('input', { bubbles: true }));
        return;
      }
    }
    // A reader may have typed before this module arrived: answer what
    // the box says.
    if (input.value.trim() !== (sec.getAttribute('data-search-page-query') || '').trim()) run(sec, input.value);
  }

  function scan(root) {
    if (root.matches && root.matches('[data-search-page]')) bind(root);
    for (const sec of (root.querySelectorAll ? root.querySelectorAll('[data-search-page]') : [])) bind(sec);
  }

  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;
  scan(document);
})();
