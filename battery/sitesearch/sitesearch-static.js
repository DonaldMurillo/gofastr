// sitesearch-static: the static-export ranker for battery/sitesearch.
// An export has no server to answer queries, so this module fetches the
// manifest, the term shards a query needs and the fragments of the
// pages it shows, and ranks them with the integers the server fixed at
// build time: the same query ranks the same way live and exported (a
// chromium test compares it with Go's Search). It loads only on an
// export, at idle or for the first query, and adds staticSearch (the
// palette's) and staticFind (the search page's) to the API
// sitesearch.js shares.
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
  const MIN_PREFIX = 3;
  const MAX_SECTIONS = 3;
  const EXCERPT_LEAD = 40;
  const EXCERPT_WIDTH = 160;
  const TITLE_BONUS = 1000000;
  const scan = api.scan;

  // prefixes: a term of MIN_PREFIX runes also matches the words it
  // begins (the Go prefixes).
  function prefixes(q) { return Array.from(q).length >= MIN_PREFIX; }

  function hex(k) {
    return Array.from(new TextEncoder().encode(k), function (b) { return b.toString(16).padStart(2, '0'); }).join('');
  }

  // shardsFor names the shards a term's matches live in: every key its
  // match prefix begins (a prefix term), and the key that begins the
  // prefix, whose shard holds every term under it unless it split;
  // its gerund's shard too.
  function shardsFor(m, q) {
    const from = stem(q) || q, ger = gerund(q);
    const out = [];
    for (const k of m.keys) {
      if (from.startsWith(k) || (prefixes(q) && k.startsWith(from)) ||
          (ger && ger.startsWith(k))) out.push('t-' + hex(k) + '-' + m.h + '.json');
    }
    return out;
  }

  // The Go stem: a plural term's singular, matched as a prefix.
  function stem(q) {
    const rs = Array.from(q);
    if (rs.length < 4 || rs[rs.length - 1] !== 's' || rs[rs.length - 2] === 's') return '';
    return rs.slice(0, -1).join('');
  }

  // The Go gerund: the -ing form a term also matches as a whole word.
  function gerund(q) {
    const rs = Array.from(q);
    const n = rs.length;
    if (n < 4 || stem(q) || q.endsWith('ing')) return '';
    if (rs[n - 1] === 'e' && rs[n - 2] !== 'e') return rs.slice(0, -1).join('') + 'ing';
    return q + 'ing';
  }

  // The excerpt window: the Go excerpt, on code points.
  function excerpt(text, terms) {
    const rs = Array.from(text);
    const hits = [];
    let anchor = -1;
    for (const t of scan(text)) {
      let hit = false, whole = false;
      for (const q of terms) {
        const st = stem(q), ger = gerund(q);
        if (t.word === q || (prefixes(q) && t.word.startsWith(q)) || (st && t.word.startsWith(st)) || (ger && t.word === ger)) {
          hit = true;
          whole = whole || t.word === q || t.word === st || t.word === ger;
        }
      }
      if (!hit) continue;
      if (anchor < 0 && whole) anchor = hits.length;
      hits.push(t);
    }
    if (anchor < 0 && hits.length) anchor = 0;
    let start = 0;
    if (anchor >= 0 && hits[anchor].start > EXCERPT_LEAD) {
      const a = hits[anchor].start;
      start = a - EXCERPT_LEAD;
      const i = rs.slice(start, a).indexOf(' ');
      start = i >= 0 ? start + i + 1 : a;
    }
    let end = Math.min(rs.length, start + EXCERPT_WIDTH);
    if (end < rs.length) {
      const from = anchor >= 0 ? Math.min(hits[anchor].end, end) : start;
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
  // a quarter of the rest; a page must match every term. A page whose
  // title holds every term then gets the title bonus: a term naming a
  // title word whole fills its slots with two, one only beginning it
  // puts one in its first slot, each slot keeps its best, and the
  // bonus is the share of two points a slot.
  function rank(shards, terms) {
    let pageScore = null;
    const secScore = new Map();
    const inTitle = new Map(), slots = new Map(), titleN = new Map();
    terms.forEach(function (qt, qi) {
      const perPage = new Map();
      const perSec = new Map();
      const titled = new Set();
      const shard = shards[qi] || {};
      const st = stem(qt), ger = gerund(qt);
      const from = st || qt;
      for (const t of Object.keys(shard)) {
        if (t !== qt && !(prefixes(qt) && t.startsWith(from)) && !(ger && t === ger)) continue;
        const row = shard[t];
        const mult = t === qt || t === ger ? 2 : 1;
        const whole = t === qt || t === st || t === ger;
        const idf = row[0], n = row[1];
        const sums = new Map(), best = new Map();
        // Title quads after the columns: posting, from, count, slots.
        const title = new Map();
        for (let j = 2 + 3 * n; j + 3 < row.length; j += 4) title.set(row[j], j);
        let pg = 0;
        for (let i = 0; i < n; i++) {
          pg += row[2 + i];
          const sec = row[2 + n + i];
          const v = idf * mult * row[2 + 2 * n + i];
          sums.set(pg, (sums.get(pg) || 0) + v);
          best.set(pg, Math.max(best.get(pg) || 0, v));
          const k = pg + ':' + sec;
          perSec.set(k, Math.max(perSec.get(k) || 0, v));
          const j = title.get(i);
          if (j !== undefined) {
            const tf = row[j + 1], tl = row[j + 2];
            titleN.set(pg, row[j + 3]);
            titled.add(pg);
            if (!slots.has(pg)) slots.set(pg, new Map());
            const sl = slots.get(pg);
            if (whole) for (let s = tf; s < tf + tl; s++) sl.set(s, 2);
            else sl.set(tf, Math.max(sl.get(tf) || 0, 1));
          }
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
      for (const pg of titled) inTitle.set(pg, (inTitle.get(pg) || 0) + 1);
    });
    pageScore = pageScore || new Map();
    for (const pg of pageScore.keys()) {
      const n = titleN.get(pg) || 0;
      if (inTitle.get(pg) === terms.length && n > 0) {
        let pts = 0;
        for (const v of slots.get(pg).values()) pts += v;
        pageScore.set(pg, pageScore.get(pg) + Math.floor(TITLE_BONUS * pts / (2 * n)));
      }
    }
    return { pageScore: pageScore, secScore: secScore };
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

  // staticFind ranks q and returns its first limit results with the
  // count of every page that matched, for the search page's status.
  async function staticFind(q, limit) {
    const terms = api.queryTerms(q);
    if (!terms.length) return { results: [], total: 0 };
    const m = await fetchJSON(ROOT + 'manifest.json');
    if (m.v !== 3) throw new Error('sitesearch: index format ' + m.v);
    // Each term's shards merge into one map of its terms.
    const shards = await Promise.all(terms.map(function (t) {
      return Promise.all(shardsFor(m, t).map(function (n) { return fetchJSON(ROOT + n); }))
        .then(function (parts) { return Object.assign(Object.create(null), ...parts); });
    }));
    const ranked = rank(shards, terms);
    const order = Array.from(ranked.pageScore.keys()).sort(function (a, b) {
      return (ranked.pageScore.get(b) - ranked.pageScore.get(a)) || (a - b);
    }).slice(0, limit);
    const frags = await Promise.all(order.map(function (pg) { return fetchJSON(ROOT + 'p' + pg + '-' + m.h + '.json'); }));
    return { results: results(order, frags, ranked, terms), total: ranked.pageScore.size };
  }

  api.staticFind = staticFind;
  api.staticSearch = function (q, limit) { return staticFind(q, limit).then(function (f) { return f.results; }); };
  api.excerpt = excerpt;
})();
