// headless-datepicker: the behaviour module for headless.DatePicker —
// a calendar grid beside the native date input(s) it fills. The
// inputs are the value; the module writes a picked day into them and
// keeps the grid's marks in step with whatever they hold (a typed
// date, a restored form). It owns the APG date-grid keyboard: arrows
// move a day or a week (mirrored on a right-to-left page), Home/End
// the week's ends, PageUp/PageDown a month and with Shift a year,
// Enter/Space pick (the day is a button). A move past the month
// clicks the matching nav anchor, whose island the kernel fetches and
// swaps; when the new month lands, focus goes to the day the key
// named, or back to the anchor that was clicked, and the caption is
// said. The month math, refused days and notes are the server's.
(function () {
  'use strict';
  const NAME = 'headless-datepicker';
  const NS = window.__gofastr = window.__gofastr || {};
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;

  const DAY = 864e5;
  // A month request older than this is taken as lost: the kernel says
  // nothing to a module when an anchor's fetch fails (the anchor's
  // error toast tells the reader), so a move waiting on it must not
  // wait forever.
  const STALE = 5000;
  // A calendar's month request in flight: { date } is the day the
  // reader's last key asked for, { nav } the anchor clicked, at its
  // start time. Per calendar, never module-wide.
  const pending = new WeakMap();
  // The start a reader picked, { date, name }, for the range sentence.
  const startName = new WeakMap();
  const watched = new WeakSet();

  function within(root, sel) {
    const out = [];
    if (root.matches && root.matches(sel)) out.push(root);
    if (root.querySelectorAll) out.push(...root.querySelectorAll(sel));
    return out;
  }

  function closest(el, sel) {
    return el && el.closest ? el.closest(sel) : null;
  }

  // utc builds a UTC time without Date.UTC's quirk of reading years
  // 0-99 as 1900-1999.
  function utc(y, m, d) {
    const t = new Date(0);
    t.setUTCFullYear(y, m, d);
    return t.getTime();
  }

  function parse(s) {
    const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(s || '');
    if (!m) return null;
    const t = utc(+m[1], +m[2] - 1, +m[3]);
    return iso(t) === s ? t : null;
  }

  function iso(t) {
    return new Date(t).toISOString().slice(0, 10);
  }

  function valid(s) {
    return parse(s) === null ? '' : s;
  }

  function addDays(s, n) {
    return iso(parse(s) + n * DAY);
  }

  // The same day n months on, held to the target month's last day:
  // January 31 plus one month is February 28, not March 3.
  function addMonths(s, n) {
    const d = new Date(parse(s));
    const y = d.getUTCFullYear();
    const m = d.getUTCMonth() + n;
    const last = new Date(utc(y, m + 1, 0)).getUTCDate();
    return iso(utc(y, m, Math.min(d.getUTCDate(), last)));
  }

  function monthIndex(ym) {
    return (+ym.slice(0, 4)) * 12 + (+ym.slice(5, 7));
  }

  function input(root, which) {
    return root.querySelector('[data-hui-datepicker-input="' + CSS.escape(which) + '"]');
  }

  function calOf(root) {
    return root.querySelector('[data-hui-datepicker-calendar]');
  }

  function values(root) {
    const s = input(root, 'start');
    if (s) {
      const e = input(root, 'end');
      return { range: true, start: valid(s.value), end: valid(e && e.value) };
    }
    const i = input(root, 'single');
    return { range: false, value: valid(i && i.value) };
  }

  // Where day d sits in the value: "single", "start", "end", "inside"
  // or "" — the server's DatePickerProps.selected, in the browser.
  function place(v, d) {
    if (!v.range) return v.value && d === v.value ? 'single' : '';
    if (!v.start) return '';
    if (!v.end || v.end < v.start) return d === v.start ? 'start' : '';
    if (d === v.start) return 'start';
    if (d === v.end) return 'end';
    return d > v.start && d < v.end ? 'inside' : '';
  }

  function rove(cal, btn) {
    if (!btn) return;
    for (const b of cal.querySelectorAll('[data-hui-date][tabindex="0"]')) {
      if (b !== btn) b.setAttribute('tabindex', '-1');
    }
    btn.setAttribute('tabindex', '0');
  }

  // sync marks the grid from the inputs and keeps one day in the tab
  // order: the focused one, else the selected one, else today, else
  // the first that can be picked.
  function sync(root) {
    const cal = calOf(root);
    if (!cal) return;
    const v = values(root);
    const days = cal.querySelectorAll('[data-hui-date]');
    let focused = null, sel = null, today = null, open = null;
    for (const btn of days) {
      const p = place(v, btn.getAttribute('data-hui-date'));
      btn.setAttribute('aria-pressed', p ? 'true' : 'false');
      const cell = btn.closest('td');
      if (cell) {
        cell.setAttribute('aria-selected', p ? 'true' : 'false');
        if (p && p !== 'single') cell.setAttribute('data-hui-datepicker-range', p);
        else cell.removeAttribute('data-hui-datepicker-range');
      }
      if (btn === document.activeElement) focused = btn;
      if (!sel && (p === 'single' || p === 'start')) sel = btn;
      if (!today && btn.getAttribute('aria-current') === 'date') today = btn;
      if (!open && btn.getAttribute('aria-disabled') !== 'true') open = btn;
    }
    rove(cal, focused || sel || today || open || days[0]);
  }

  // say writes a sentence into the status region, cleared first so a
  // sentence said twice is heard twice.
  function say(root, text) {
    const status = root.querySelector('[data-hui-datepicker-status]');
    if (!status) return;
    status.textContent = '';
    requestAnimationFrame(function () { status.textContent = text || ''; });
  }

  function fill(fmt, pairs) {
    let out = fmt || '';
    for (const k of Object.keys(pairs)) out = out.replaceAll('{{' + k + '}}', function () { return pairs[k]; });
    return out;
  }

  // nameOf is a day's full name: its button's, when the day is drawn,
  // else the page language's own long date.
  function nameOf(cal, d) {
    const b = cal && cal.querySelector('[data-hui-date="' + CSS.escape(d) + '"]');
    if (b && b.getAttribute('aria-label')) return b.getAttribute('aria-label');
    try {
      return new Date(parse(d)).toLocaleDateString(document.documentElement.lang || undefined,
        { dateStyle: 'full', timeZone: 'UTC' });
    } catch (_) {
      return d;
    }
  }

  function setValue(el, v) {
    if (!el || el.value === v) return;
    el.value = v;
    // The host form hears the edit (its leave guard, its watchers).
    el.dispatchEvent(new Event('input', { bubbles: true }));
    el.dispatchEvent(new Event('change', { bubbles: true }));
  }

  function pick(root, btn) {
    const cal = calOf(root);
    const d = btn.getAttribute('data-hui-date');
    const name = btn.getAttribute('aria-label') || d;
    if (!root.hasAttribute('data-hui-datepicker-range-mode')) {
      setValue(input(root, 'single'), d);
      say(root, fill(root.getAttribute('data-hui-datepicker-chosen'), { date: name }));
    } else {
      const v = values(root);
      if (!v.start || v.end || d < v.start) {
        setValue(input(root, 'start'), d);
        setValue(input(root, 'end'), '');
        startName.set(root, { date: d, name: name });
        say(root, fill(root.getAttribute('data-hui-datepicker-choose-end'), { date: name }));
      } else {
        setValue(input(root, 'end'), d);
        // The start as the input holds it now: a start typed after the
        // pick is the one the sentence names.
        const s = startName.get(root);
        say(root, fill(root.getAttribute('data-hui-datepicker-ranged'), {
          start: s && s.date === v.start ? s.name : nameOf(cal, v.start), end: name,
        }));
      }
    }
    sync(root);
    if (cal) rove(cal, btn);
  }

  function inFlight(cal) {
    const p = pending.get(cal);
    return p && Date.now() - p.at < STALE ? p : null;
  }

  // moveTo focuses day target: in place when it is drawn, else through
  // the nav anchor one month or one year away, the swap focusing it.
  // While a month is in flight the last key wins: its day is recorded,
  // and the arrival walks on to it, so quick keys are never lost to the
  // kernel's newest-request-wins abort.
  function moveTo(root, target) {
    const cal = calOf(root);
    if (!cal || !target) return;
    const p = inFlight(cal);
    const btn = cal.querySelector('[data-hui-date="' + CSS.escape(target) + '"]');
    if (btn) {
      rove(cal, btn);
      btn.focus();
      if (p) p.date = target;
      return;
    }
    if (p) {
      p.date = target;
      return;
    }
    const grid = cal.querySelector('[data-hui-datepicker-grid]');
    if (!grid) return;
    // Step toward the day: a year while it is a year or more away,
    // else a month. A day further off arrives by several steps, each
    // arrival walking on.
    const diff = monthIndex(target.slice(0, 7)) - monthIndex(grid.getAttribute('data-hui-datepicker-grid') || '');
    if (!diff) return;
    const which = (diff < 0 ? 'prev-' : 'next-') + (Math.abs(diff) >= 12 ? 'year' : 'month');
    const a = cal.querySelector('[data-hui-datepicker-nav="' + CSS.escape(which) + '"]');
    if (!a || a.getAttribute('aria-disabled') === 'true') return;
    a.click();
    // After the click: the anchor's own click (below) recorded itself,
    // and the key's day is what the swap should focus instead.
    pending.set(cal, { date: target, at: Date.now() });
  }

  document.addEventListener('keydown', function (e) {
    const btn = closest(e.target, '[data-hui-date]');
    const root = closest(btn, '[data-hui-datepicker]');
    if (!root || e.altKey || e.ctrlKey || e.metaKey || e.isComposing) return;
    // Keys pressed while a month is loading count from the day the
    // last one asked for, not from the old grid still on screen.
    const p = inFlight(calOf(root));
    const d = (p && p.date) || btn.getAttribute('data-hui-date');
    if (!valid(d)) return;
    const rtl = getComputedStyle(btn).direction === 'rtl';
    const row = btn.closest('tr');
    const inRow = row ? row.querySelectorAll('[data-hui-date]') : [];
    let target = null;
    switch (e.key) {
      case 'ArrowLeft': target = addDays(d, rtl ? 1 : -1); break;
      case 'ArrowRight': target = addDays(d, rtl ? -1 : 1); break;
      case 'ArrowUp': target = addDays(d, -7); break;
      case 'ArrowDown': target = addDays(d, 7); break;
      case 'Home': target = inRow.length ? inRow[0].getAttribute('data-hui-date') : null; break;
      case 'End': target = inRow.length ? inRow[inRow.length - 1].getAttribute('data-hui-date') : null; break;
      case 'PageUp': target = addMonths(d, e.shiftKey ? -12 : -1); break;
      case 'PageDown': target = addMonths(d, e.shiftKey ? 12 : 1); break;
      default: return;
    }
    e.preventDefault();
    moveTo(root, target);
  });

  // A nav anchor records itself before the kernel's delegate fetches
  // the month, so the swap can put focus back on it.
  document.addEventListener('click', function (e) {
    const a = closest(e.target, '[data-hui-datepicker-nav]');
    const cal = closest(a, '[data-hui-datepicker-calendar]');
    if (cal && a.getAttribute('aria-disabled') !== 'true') {
      pending.set(cal, { nav: a.getAttribute('data-hui-datepicker-nav'), at: Date.now() });
    }
  }, true);

  document.addEventListener('click', function (e) {
    const btn = closest(e.target, '[data-hui-date]');
    const root = closest(btn, '[data-hui-datepicker]');
    if (!root || btn.getAttribute('aria-disabled') === 'true') return;
    e.preventDefault();
    pick(root, btn);
  });

  // A typed date (or a cleared one) moves the marks with it.
  function edited(e) {
    const root = closest(closest(e.target, '[data-hui-datepicker-input]'), '[data-hui-datepicker]');
    if (root) sync(root);
  }
  document.addEventListener('input', edited);
  document.addEventListener('change', edited);

  // A month arrives by the kernel's html signal swap, which replaces
  // the calendar's children: the arrival pass sees the new header and
  // grid, never the calendar. An observer on the calendar's own
  // children hears each swap, re-marks the grid, says the caption and
  // puts focus where the key or the anchor left it.
  function arrived(cal) {
    const root = closest(cal, '[data-hui-datepicker]');
    if (!root) return;
    const p = pending.get(cal);
    pending.delete(cal);
    sync(root);
    const cap = cal.querySelector('[data-hui-datepicker-caption]');
    if (cap) say(root, cap.textContent);
    if (!p) return;
    // A reader who moved on while the month loaded keeps their place:
    // focus comes back only when it was lost with the old month.
    const active = document.activeElement;
    if (active && active !== document.body && !cal.contains(active)) return;
    let to = p.date ? cal.querySelector('[data-hui-date="' + CSS.escape(p.date) + '"]') : null;
    if (to) rove(cal, to);
    if (!to && p.date) {
      // The day is not in this month (a key pressed during the load,
      // or a year move held at Min or Max): walk on toward it, and
      // when no step is left, take the same day number in this month.
      moveTo(root, p.date);
      if (pending.has(cal)) return;
      const days = cal.querySelectorAll('[data-hui-date]');
      to = days[Math.min(+p.date.slice(8), days.length) - 1] || null;
      if (to) rove(cal, to);
    }
    if (!to && p.nav) {
      to = cal.querySelector('[data-hui-datepicker-nav="' + CSS.escape(p.nav) + '"]');
      if (to && to.getAttribute('aria-disabled') === 'true') to = null;
    }
    if (!to) to = cal.querySelector('[data-hui-date][tabindex="0"]');
    if (to) to.focus();
  }

  function watch(cal) {
    if (watched.has(cal) || typeof MutationObserver !== 'function') return;
    watched.add(cal);
    new MutationObserver(function () { arrived(cal); }).observe(cal, { childList: true });
    // A failed month reaches the calendar's signal as an error object,
    // which the html swap ignores; hearing it here ends the wait at
    // once, so the next key moves instead of queueing.
    const name = cal.getAttribute('data-cui-signal');
    const store = NS._signals;
    if (!store || !name || name === '__proto__' || name === 'constructor' || name === 'prototype') return;
    const slot = Object.prototype.hasOwnProperty.call(store, name) ? store[name] : (store[name] = { value: undefined, listeners: [] });
    slot.listeners.push(function (v) { if (typeof v !== 'string') pending.delete(cal); });
  }

  function scan(scope) {
    for (const root of within(scope && scope.querySelectorAll ? scope : document, '[data-hui-datepicker]')) {
      const cal = calOf(root);
      if (cal) watch(cal);
      sync(root);
    }
  }

  scan(document);
  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;
})();
