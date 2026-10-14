// sitesearch-static: the static-export ranker for battery/sitesearch.
// An export has no server to answer queries, so this module fetches the
// manifest, the term shards a query needs and the fragments of the
// pages it shows, and ranks them with the integers the server fixed at
// build time: the same query ranks the same way live and exported (a
// chromium test compares it with Go's Search). It loads only on an
// export, at idle or for the first query, and adds staticSearch to the
// API sitesearch.js shares.
(function () {
  'use strict';
  const NAME = 'sitesearch-static';
  const NS = window.__gofastr = window.__gofastr || {};
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  const api = NS._sitesearch;
  if (!api) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;

  // ROOT carries the export's base path, baked in like sitesearch.js's.
  const ROOT = '/__gofastr/search/';
  const MIN_TERM = 2;
  const MAX_SECTIONS = 3;
  const EXCERPT_LEAD = 40;
  const EXCERPT_WIDTH = 160;
  const scan = api.scan;

  function shardKey(t) { return Array.from(t).slice(0, MIN_TERM).join(''); }

  // The Go stem: a plural term's singular, matched as a prefix.
  function stem(q) {
    const rs = Array.from(q);
    if (rs.length < 4 || rs[rs.length - 1] !== 's' || rs[rs.length - 2] === 's') return '';
    return rs.slice(0, -1).join('');
  }

  // The excerpt window: the Go excerpt, on code points.
  function excerpt(text, terms) {
    const rs = Array.from(text);
    const hits = scan(text).filter(function (t) {
      return terms.some(function (q) { return t.word.startsWith(q); });
    });
    let start = 0;
    if (hits.length && hits[0].start > EXCERPT_LEAD) {
      start = hits[0].start - EXCERPT_LEAD;
      const i = rs.slice(start, hits[0].start).indexOf(' ');
      if (i >= 0) start += i + 1;
    }
    let end = Math.min(rs.length, start + EXCERPT_WIDTH);
    if (end < rs.length) {
      const from = hits.length ? Math.min(hits[0].end, end) : start;
      const i = rs.slice(from, end).lastIndexOf(' ');
      if (i >= 0) end = from + i;
    }
    const segs = [];
    function plain(s) {
      if (!s) return;
      const last = segs[segs.length - 1];
      if (last && !last.h) last.t += s;
      else segs.push({ t: s });
    }
    if (start > 0) plain('…');
    let at = start;
    for (const h of hits) {
      if (h.start < start || h.end > end) continue;
      plain(rs.slice(at, h.start).join(''));
      segs.push({ t: rs.slice(h.start, h.end).join(''), h: true });
      at = h.end;
    }
    plain(rs.slice(at, end).join(''));
    if (end < rs.length) plain('…');
    return segs;
  }

  const cache = new Map();
  function fetchJSON(url) {
    if (!cache.has(url)) {
      cache.set(url, fetch(url, { credentials: 'same-origin' }).then(function (r) {
        if (!r.ok) throw new Error(url + ': ' + r.status);
        return r.json();
      }).catch(function (e) { cache.delete(url); throw e; }));
    }
    return cache.get(url);
  }

  // rank runs the Go Search over the shards: per query term, a page
  // scores its best matching term, each section idf × (2 exact, 1
  // prefix) × the term's section score, the best section in full and
  // a quarter of the rest; a page must match every term.
  function rank(shards, terms) {
    let pageScore = null;
    const secScore = new Map();
    terms.forEach(function (qt, qi) {
      const perPage = new Map();
      const perSec = new Map();
      const shard = shards[qi] || {};
      const from = stem(qt) || qt;
      for (const t of Object.keys(shard)) {
        if (!t.startsWith(from)) continue;
        const row = shard[t];
        const mult = t === qt ? 2 : 1;
        const idf = row[0];
        const sums = new Map(), best = new Map();
        for (let i = 1; i + 2 < row.length; i += 3) {
          const pg = row[i], sec = row[i + 1];
          const v = idf * mult * row[i + 2];
          sums.set(pg, (sums.get(pg) || 0) + v);
          best.set(pg, Math.max(best.get(pg) || 0, v));
          const k = pg + ':' + sec;
          perSec.set(k, Math.max(perSec.get(k) || 0, v));
        }
        // The best section in full, a quarter of the rest.
        for (const [pg, sum] of sums) {
          const b = best.get(pg);
          perPage.set(pg, Math.max(perPage.get(pg) || 0, b + Math.floor((sum - b) / 4)));
        }
      }
      if (qi === 0) pageScore = perPage;
      else {
        for (const pg of Array.from(pageScore.keys())) {
          if (perPage.has(pg)) pageScore.set(pg, pageScore.get(pg) + perPage.get(pg));
          else pageScore.delete(pg);
        }
      }
      for (const [k, v] of perSec) secScore.set(k, (secScore.get(k) || 0) + v);
    });
    return { pageScore: pageScore || new Map(), secScore: secScore };
  }

  function results(order, frags, ranked, terms) {
    return order.map(function (pg, i) {
      const f = frags[i];
      const sec = function (si) { return ranked.secScore.get(pg + ':' + si) || 0; };
      let hits = [];
      for (let si = 1; si < f.s.length; si++) if (f.s[si][0] && sec(si) > 0) hits.push(si);
      hits.sort(function (a, b) { return sec(b) - sec(a); });
      hits = hits.slice(0, MAX_SECTIONS);
      return {
        url: f.u, title: f.t, group: f.g || '', score: ranked.pageScore.get(pg),
        excerpt: sec(0) > 0 || !hits.length ? excerpt(f.s[0][2], terms) : [],
        sections: hits.map(function (si) {
          return { id: f.s[si][0], heading: f.s[si][1], excerpt: excerpt(f.s[si][2], terms) };
        }),
      };
    });
  }

  async function staticSearch(q, limit) {
    const terms = api.queryTerms(q);
    if (!terms.length) return [];
    const m = await fetchJSON(ROOT + 'manifest.json');
    if (m.v !== 1) throw new Error('sitesearch: index format ' + m.v);
    const names = terms.map(function (t) { return m.shards[shardKey(t)]; });
    if (names.some(function (n) { return !n; })) return [];
    const shards = await Promise.all(names.map(function (n) { return fetchJSON(ROOT + n); }));
    const ranked = rank(shards, terms);
    const order = Array.from(ranked.pageScore.keys()).sort(function (a, b) {
      return (ranked.pageScore.get(b) - ranked.pageScore.get(a)) || (a - b);
    }).slice(0, limit);
    const frags = await Promise.all(order.map(function (pg) { return fetchJSON(ROOT + m.pages[pg]); }));
    return results(order, frags, ranked, terms);
  }

  api.staticSearch = staticSearch;
  api.excerpt = excerpt;
})();
