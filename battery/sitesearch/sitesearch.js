// sitesearch: the browser client for battery/sitesearch. It answers a
// command palette whose listbox carries data-search: each query runs
// against the site's index and the matching pages and their sections
// render as option rows after the palette's own commands, which stay
// the list a reader sees before typing.
//
// On a live server the battery answers Root+"query" with ranked
// results. On a static export (<html data-cui-static>) there is no
// server: sitesearch-static.js ranks from the index files in the
// browser, and this module loads it for the first query. Rows are built
// with DOM calls; index text never reaches innerHTML.
(function () {
  'use strict';
  const NAME = 'sitesearch';
  const NS = window.__gofastr = window.__gofastr || {};
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;

  // The export bakes its base path into every /__gofastr/ literal in a
  // runtime module, so ROOT carries it; BASE is what comes before it,
  // spelled so the rewrite cannot reach the search string.
  const ROOT = '/__gofastr/search/';
  const BASE = ROOT.slice(0, ROOT.indexOf('/__' + 'gofastr/'));

  const MIN_TERM = 2;
  const LIMIT = 8;
  const DEBOUNCE_MS = 120;
  // A query still pending after this long shows the loading row; a
  // faster answer never flashes it.
  const PENDING_MS = 150;
  const WORD = /[\p{L}\p{Nd}]/u;
  // The static ranker adds staticSearch here when it loads.
  const api = NS._sitesearch = { scan: scan, queryTerms: queryTerms };

  // scan splits text into lowercased word runs with code-point offsets,
  // the Go tokenizer's runes. It lowercases one code point at a time
  // and keeps the first, as Go does: a whole-word toLowerCase is
  // contextual (ΟΣ → ος, a final sigma) and expands İ to two. The
  // static ranker shares it and queryTerms through NS._sitesearch.
  function scan(text) {
    const out = [];
    let pos = 0, start = -1, word = '';
    for (const ch of text) {
      if (WORD.test(ch)) {
        if (start < 0) start = pos;
        word += String.fromCodePoint(ch.toLowerCase().codePointAt(0));
      } else if (start >= 0) {
        out.push({ word: word, start: start, end: pos });
        start = -1; word = '';
      }
      pos++;
    }
    if (start >= 0) out.push({ word: word, start: start, end: pos });
    return out;
  }

  function queryTerms(q) {
    const seen = new Set();
    const out = [];
    for (const t of scan(q)) {
      if (Array.from(t.word).length < MIN_TERM || seen.has(t.word)) continue;
      seen.add(t.word);
      out.push(t.word);
    }
    return out;
  }

  async function liveSearch(q, limit) {
    const r = await fetch(ROOT + 'query?q=' + encodeURIComponent(q) + '&limit=' + limit, { credentials: 'same-origin' });
    if (!r.ok) throw new Error('sitesearch: ' + r.status);
    return (await r.json()).results || [];
  }

  function search(q, limit) {
    if (!document.documentElement.hasAttribute('data-cui-static')) return liveSearch(q, limit);
    return Promise.resolve(NS.loadModule?.('sitesearch-static')).then(() => api.staticSearch(q, limit));
  }

  // ── Rows ──────────────────────────────────────────────────────────

  function span(cls, text) {
    const s = document.createElement('span');
    s.className = cls;
    s.setAttribute('data-cui-internal', '');
    if (text != null) s.textContent = text;
    return s;
  }

  function excerptEl(segs) {
    const el = span('fui-cmd-palette__option-excerpt');
    for (const s of segs) {
      if (s.h) {
        const m = document.createElement('mark');
        m.textContent = s.t;
        el.appendChild(m);
      } else el.appendChild(document.createTextNode(s.t));
    }
    return el;
  }

  function row(lb, n, label, meta, segs, dest, section) {
    const li = document.createElement('li');
    li.setAttribute('role', 'option');
    li.id = lb.id + '-search-' + n;
    li.className = 'fui-cmd-palette__option fui-cmd-palette__option--result' + (section ? ' fui-cmd-palette__option--section' : '');
    li.setAttribute('data-value', label);
    li.setAttribute('data-search-result', '');
    li.setAttribute('data-cui-push-state', dest);
    li.appendChild(span('fui-cmd-palette__option-label', label));
    if (meta) li.appendChild(span('fui-cmd-palette__option-meta', meta));
    if (segs && segs.length) li.appendChild(excerptEl(segs));
    return li;
  }

  function clearRows(lb) {
    for (const el of lb.querySelectorAll('[data-search-result]')) el.remove();
  }

  function openList(lb, input) {
    input.setAttribute('aria-expanded', 'true');
    lb.removeAttribute('hidden');
  }

  // statusRow is the disabled row that says the results are loading or
  // that nothing matched, in the words the listbox carries.
  function statusRow(lb, input, attr) {
    const word = lb.getAttribute(attr);
    if (!word) return;
    const li = document.createElement('li');
    li.setAttribute('role', 'option');
    li.setAttribute('aria-disabled', 'true');
    li.className = 'fui-cmd-palette__option';
    li.setAttribute('data-search-result', '');
    li.textContent = word;
    lb.appendChild(li);
    openList(lb, input);
  }

  // render swaps in the new result rows. A row whose destination
  // matches the one already in its place keeps its node and only takes
  // the new text, so the rows that stay put while the reader types do
  // not replay their entrance.
  function render(lb, input, list) {
    const fresh = [];
    for (const r of list) {
      const url = BASE + r.url;
      fresh.push(row(lb, fresh.length, r.title, r.group, r.excerpt, url, false));
      for (const s of r.sections || []) {
        fresh.push(row(lb, fresh.length, s.heading, '', s.excerpt, s.id ? url + '#' + s.id : url, true));
      }
    }
    const old = lb.querySelectorAll('[data-search-result]');
    let i = 0;
    for (; i < old.length && i < fresh.length; i++) {
      const was = old[i], now = fresh[i];
      if (was.getAttribute('data-cui-push-state') !== now.getAttribute('data-cui-push-state')) break;
      was.id = now.id;
      was.className = now.className;
      was.setAttribute('data-value', now.getAttribute('data-value'));
      was.replaceChildren(...now.childNodes);
    }
    for (let j = i; j < old.length; j++) old[j].remove();
    lb.append(...fresh.slice(i));
    const visible = lb.querySelectorAll('[role="option"]:not([hidden]):not([aria-disabled="true"])');
    if (!visible.length) statusRow(lb, input, 'data-search-empty');
    if (visible.length) {
      openList(lb, input);
      for (const o of lb.querySelectorAll('[role="option"].is-active')) o.classList.remove('is-active');
      visible[0].classList.add('is-active');
      input.setAttribute('aria-activedescendant', visible[0].id || '');
    }
    const root = input.closest('[data-hui-combobox]');
    const status = root && root.querySelector('[data-hui-combobox-status]');
    if (status) {
      const fmt = lb.getAttribute('data-hui-combobox-count');
      status.textContent = visible.length
        ? (fmt ? fmt.replace('{n}', function () { return String(visible.length); }) : '')
        : (status.getAttribute('data-hui-combobox-no-results') || '');
    }
  }

  const seq = new WeakMap();
  const timers = new WeakMap();

  function listboxOf(input) {
    const id = input.getAttribute('aria-controls');
    const lb = id ? document.getElementById(id) : null;
    return lb && lb.hasAttribute('data-search') ? lb : null;
  }

  function query(input) {
    const lb = listboxOf(input);
    if (!lb) return;
    clearTimeout(timers.get(lb));
    const q = (input.value || '').trim();
    const n = (seq.get(lb) || 0) + 1;
    seq.set(lb, n);
    if (queryTerms(q).length === 0) { clearRows(lb); return; }
    timers.set(lb, setTimeout(function () {
      let done = false;
      setTimeout(function () {
        if (!done && seq.get(lb) === n) { clearRows(lb); statusRow(lb, input, 'data-search-loading'); }
      }, PENDING_MS);
      search(q, LIMIT).then(function (list) {
        done = true;
        if (seq.get(lb) === n) render(lb, input, list);
      }).catch(function (err) {
        done = true;
        if (seq.get(lb) === n) clearRows(lb);
        if (window.console) console.warn('sitesearch:', err);
      });
    }, DEBOUNCE_MS));
  }

  document.addEventListener('input', function (e) {
    const input = e.target && e.target.closest && e.target.closest('[data-hui-combobox-input]');
    if (input) query(input);
  });

  // The module loads when the palette first opens; a reader who typed
  // before it arrived gets that query answered now.
  const typed = document.activeElement;
  if (typed && typed.matches && typed.matches('[data-hui-combobox-input]') && typed.value) query(typed);

})();
