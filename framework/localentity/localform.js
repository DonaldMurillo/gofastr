// Local entity forms: save a design-system form into the visitor's
// browser instead of posting it. framework/localentity registers this
// file as the `localentity-form` behaviour; the kernel loads it (after
// localdb and formerrors) when a local form carrier is on the page.
// Lists are the sibling `localentity` behaviour, which hands an Edit
// click here through NS._localForm.fill.
//
// Only fields the Go declaration names are read from the form and
// written to a record: an injected control (an "id", a "__proto__",
// the CSRF input) never reaches IndexedDB. Each value is converted to
// its declared type and checked against the declaration; refusals are
// placed beside their fields by the formerrors module, the same way a
// server form's are.
//
// Hooks (data-fui-local-*, emitted by framework/localentity):
//   -form="<db>/<store>"  the form carrier; -schema is the field spec
//   -editing="<key>"      set by this module while the form edits a
//                         record; reset ends the edit
(function () {
  'use strict';
  const NAME = 'localentity-form';
  const NS = window.__gofastr = window.__gofastr || {};
  const has = (o, k) => o != null && Object.prototype.hasOwnProperty.call(o, k);
  if (NS.loadedModules && has(NS.loadedModules, NAME)) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;

  const P = 'data-fui-local-';
  const FORM = '[' + P + 'form]';

  const specs = new WeakMap();
  const specOf = (wrap) => {
    if (!specs.has(wrap)) {
      let spec = null;
      try { spec = JSON.parse(wrap.getAttribute(P + 'schema') || ''); } catch (_) { spec = null; }
      specs.set(wrap, spec && Array.isArray(spec.fields) ? spec : null);
    }
    return specs.get(wrap);
  };
  // The default messages, in the reader's language (the localentity.*
  // phrases registered in localentity.go); spec.msgs holds the app's
  // own words for any it replaced.
  const say = NS.say || ((k, en) => en);
  const DEFAULTS = {
    required: () => say('localentity.required', 'This field is required.'),
    number: () => say('localentity.number', 'Enter a number.'),
    integer: () => say('localentity.integer', 'Enter a whole number.'),
    min: () => say('localentity.min', 'Must be at least {{n}}.'),
    max: () => say('localentity.max', 'Must be at most {{n}}.'),
    minlen: () => say('localentity.minlen', 'Must be at least {{n}} characters.'),
    maxlen: () => say('localentity.maxlen', 'Must be at most {{n}} characters.'),
    pattern: () => say('localentity.pattern', 'Use the requested format.'),
    choice: () => say('localentity.choice', 'Choose one of the listed options.'),
    date: () => say('localentity.date', 'Enter a valid date.'),
    full: () => say('localentity.full', 'This list is full ({{n}} at most).'),
    failed: () => say('localentity.failed', 'Could not save on this device.'),
  };
  // msg is one message with {{n}} filled: the app's override when the
  // spec carries a string for k, else the default.
  const msg = (msgs, k, n) => {
    const own = has(msgs, k) && typeof msgs[k] === 'string' && msgs[k];
    const s = own || DEFAULTS[k]();
    return s.split('{{n}}').join(n === undefined ? '' : NS.formatNumber(n));
  };
  const text = (v) => (v == null || typeof v === 'object' ? '' : String(v));
  // seps is the decimal and group marks of the page's language
  // (<html lang>), the one NS.formatNumber writes numbers in.
  const seps = () => {
    let f;
    try { f = new Intl.NumberFormat(document.documentElement.lang || undefined); } catch (_) { f = new Intl.NumberFormat(); }
    const o = { decimal: '.', group: ',', g: [] };
    for (const x of f.formatToParts(1234567890.5)) if (x.type === 'integer') o.g.push(x.value.length); else if (has(o, x.type)) o[x.type] = x.value;
    return o;
  };
  // num reads a number typed in a text control the reader's way:
  // "1.234,5" on a Spanish page, "1,234.5" on an English one and
  // "12,34,567.8" on an Indian English one. Group marks count only where
  // they match the locale's grouping (the last group and the ones before
  // it), so "2.5" still reads 2.5 where "." groups; a space-like mark
  // (French) also takes a plain space.
  const num = (raw) => {
    const o = seps(), s = raw.trim(), at = s.lastIndexOf(o.decimal), p = o.g.at(-1), q = o.g.at(-2) || p;
    let i = at < 0 ? s : s.slice(0, at);
    const g = i.split(/\s/.test(o.group) ? /\s/ : o.group);
    if (g.length > 1 && g.every((x, k) => new RegExp(k ? '^\\d{' + (k < g.length - 1 ? q : p) + '}$' : '^-?\\d{1,' + q + '}$').test(x))) i = g.join('');
    const n = at < 0 ? i : i + '.' + s.slice(at + 1);
    return n ? Number(n) : NaN;
  };
  // typed: a control the reader types digits into, which reads and
  // shows a number with the page's marks. A range, select, radio or
  // hidden input holds the browser's dot-decimal value both ways.
  const typed = (c) => /^(text|search|textarea)$/.test(c.type);
  const pad = (n) => String(n).padStart(2, '0');

  // day accepts a real calendar date, YYYY-MM-DD, as core/schema's
  // Date does (2024-02-31 is refused, not rolled into March).
  const day = (raw) => {
    const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(raw);
    if (!m) return null;
    const d = new Date(Date.UTC(+m[1], m[2] - 1, +m[3]));
    return d.getUTCFullYear() === +m[1] && d.getUTCMonth() === m[2] - 1 && d.getUTCDate() === +m[3] ? raw : null;
  };
  // stamp accepts RFC 3339 (what core/schema's Timestamp parses) or a
  // datetime-local value, read in the visitor's time zone, and stores
  // either as RFC 3339 in UTC, so records sort and sync as one format.
  const stamp = (raw) => {
    const m = /^(\d{4}-\d{2}-\d{2})T\d{2}:\d{2}(?::\d{2}(?:\.\d+)?)?(Z|[+-]\d{2}:\d{2})?$/i.exec(raw);
    const t = m && day(m[1]) ? Date.parse(raw) : NaN;
    return Number.isFinite(t) ? new Date(t).toISOString() : null;
  };
  // shown holds what fill wrote into a datetime-local control, the record
  // it came from, and the stored value behind it. The control shows
  // minutes, so an edit that leaves it untouched saves the stored value,
  // seconds and milliseconds included (2026-10-27 H7).
  const shown = new WeakMap();
  const kept = (form, name, raw) => {
    const ctl = live(form, name)[0];
    const s = ctl && shown.get(ctl);
    const wrap = form.closest(FORM);
    return s && raw === s.value && wrap && wrap.getAttribute(P + 'editing') === s.id ? stamp(s.stored) : null;
  };

  // live is a field's controls the browser will submit: a disabled
  // control (or one in a disabled fieldset) is not in FormData, so it
  // must not read as an emptied field.
  const live = (form, name) => {
    const ctl = form.elements.namedItem(name);
    return ctl ? (ctl.tagName ? [ctl] : Array.from(ctl)).filter((c) => !c.matches(':disabled')) : [];
  };

  // coerce reads one field from the form and checks it against its
  // declaration. It returns {value}, {empty} (a control with nothing in
  // it: clears the field on an edit, takes the Default on a create),
  // {absent} (no enabled control for the field: an edit leaves it
  // alone), or {error}. A bound the spec omits is undefined, and no
  // comparison with undefined is true, so it checks nothing.
  function coerce(f, data, msgs, form) {
    const ctl = live(form, f.name)[0];
    if (!ctl) return { absent: true };
    if (f.t === 'bool') {
      // A checked box submits its value (whatever it is: "true", "on",
      // "yes"); the hidden "false" twin is the unchecked half.
      return { value: data.getAll(f.name).some((x) => x !== 'false' && x !== '') };
    }
    const raw = data.get(f.name);
    if (raw === null || raw === '') return f.req ? { error: msg(msgs, 'required') } : { empty: true };
    if (typeof raw !== 'string') return { error: msg(msgs, 'required') };
    if (f.t === 'int' || f.t === 'float') {
      // A text control holds what the reader typed; every other
      // control (number, range, select, radio, hidden) hands over the
      // browser's canonical spelling (a number input empties a value it
      // cannot read).
      const v = typed(ctl) ? num(raw) : Number(raw);
      if (!Number.isFinite(v)) return { error: msg(msgs, 'number') };
      if (f.t === 'int' && !Number.isSafeInteger(v)) return { error: msg(msgs, 'integer') };
      if (v < f.min) return { error: msg(msgs, 'min', f.min) };
      if (v > f.max) return { error: msg(msgs, 'max', f.max) };
      return { value: v };
    }
    if (f.t === 'enum' && !(f.values || []).includes(raw)) return { error: msg(msgs, 'choice') };
    if (f.t === 'date' || f.t === 'timestamp') {
      const v = f.t === 'date' ? day(raw) : kept(form, f.name, raw) || stamp(raw);
      return v === null ? { error: msg(msgs, 'date') } : { value: v };
    }
    // Lengths count code points, as core/schema counts runes: an emoji
    // is one character on both sides.
    const len = [...raw].length;
    if (len < f.min) return { error: msg(msgs, 'minlen', f.min) };
    if (len > f.max) return { error: msg(msgs, 'maxlen', f.max) };
    if (f.pattern) {
      // Fails closed: a pattern this browser cannot compile refuses the
      // value rather than letting anything through. Define refuses the
      // RE2-only syntax it can see, so this is the last line.
      let ok = false;
      try { ok = new RegExp(f.pattern, 'u').test(raw); } catch (_) { ok = false; }
      if (!ok) return { error: msg(msgs, 'pattern') };
    }
    return { value: raw };
  }

  async function save(wrap, form) {
    const v = wrap.getAttribute(P + 'form') || '';
    const i = v.indexOf('/');
    const spec = specOf(wrap);
    if (i < 1 || !spec) return;
    const dbName = v.slice(0, i);
    const store = v.slice(i + 1);
    const msgs = spec.msgs;
    const data = new FormData(form);
    // set: the values to write; cleared: fields an edit empties;
    // defaults: what a create writes for a field left empty or absent.
    // missing: Required fields with no control and no Default, which a
    // create cannot fill (an edit keeps the stored value).
    const set = [];
    const cleared = [];
    const defaults = [];
    const missing = [];
    const errors = {};
    let failed = false;
    for (const f of spec.fields) {
      const r = coerce(f, data, msgs, form);
      if (r.error) {
        errors[f.name] = [r.error];
        failed = true;
      } else if (r.empty || r.absent) {
        if (r.empty) cleared.push(f.name);
        if (f.def !== undefined) defaults.push([f.name, f.def]);
        else if (r.absent && f.req) missing.push(f.name);
      } else {
        set.push([f.name, r.value]);
      }
    }
    const fe = NS._formErrors;
    if (fe) fe.clear(form);
    if (failed) {
      if (fe) fe.report(form, 422, JSON.stringify({ fields: errors }));
      return;
    }
    // fromEntries defines properties: a field name can never reach a
    // prototype setter (Go refuses any name that could spell one too).
    const values = Object.fromEntries(set);
    const editing = wrap.getAttribute(P + 'editing');
    const now = new Date().toISOString();
    // Refusals decided inside the transaction ride this variable:
    // localdb re-codes every failure that leaves one.
    let refusal = '';
    try {
      const db = await NS.localdb.open(dbName);
      await db.tx(store, 'readwrite', async (tx) => {
        // An edit whose record is gone (released here or in another
        // tab) saves as a new record: the visitor's words are kept, and
        // the reset below ends the stale edit.
        const current = editing ? await tx.get(store, editing) : undefined;
        if (current) {
          const next = Object.assign({}, current, values, { id: current.id, updated_at: now });
          for (const name of cleared) delete next[name];
          await tx.put(store, next);
          return;
        }
        if (missing.length) {
          refusal = msg(msgs, 'required') + ' (' + missing.join(', ') + ')';
          throw new Error('missing');
        }
        if (spec.max && (await tx.count(store)) >= spec.max) {
          refusal = msg(msgs, 'full', spec.max);
          throw new Error('full');
        }
        await tx.put(store, Object.assign(Object.fromEntries(defaults), values, { created_at: now, updated_at: now }));
      });
    } catch (_) {
      if (typeof NS._toastOrFallback === 'function') {
        NS._toastOrFallback({ variant: 'error', title: refusal || msg(msgs, 'failed'), ttl: 6000 });
      }
      return;
    }
    form.reset();
    wrap.dispatchEvent(new CustomEvent('gofastr:local-saved', { bubbles: true, detail: { target: v } }));
  }

  // fill loads a record into the form's controls by field name; the
  // next save updates that record.
  function fill(wrap, rec) {
    const form = wrap.querySelector('form');
    const spec = specOf(wrap);
    if (!form || !spec) return;
    for (const f of spec.fields) {
      const value = has(rec, f.name) ? rec[f.name] : undefined;
      const ctl = form.elements.namedItem(f.name);
      if (!ctl) continue;
      const list = ctl.tagName ? [ctl] : Array.from(ctl);
      for (const c of list) {
        shown.delete(c);
        if (c.type === 'checkbox') c.checked = value === true;
        else if (c.type === 'datetime-local' && Number.isFinite(Date.parse(text(value)))) {
          const d = new Date(text(value));
          c.value = d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate()) +
            'T' + pad(d.getHours()) + ':' + pad(d.getMinutes());
          shown.set(c, { value: c.value, id: text(rec.id), stored: text(value) });
        }
        else if (c.type === 'radio') c.checked = c.value === text(value);
        // A number shows with the page's decimal mark in a text
        // control, so saving the edit untouched reads it back the same.
        else if (typeof value === 'number' && typed(c)) c.value = String(value).split('.').join(seps().decimal);
        else if (c.type !== 'hidden' || list.length === 1) c.value = text(value);
      }
    }
    wrap.setAttribute(P + 'editing', text(rec.id));
    const first = form.querySelector('input:not([type=hidden]), select, textarea');
    if (first) first.focus();
  }
  NS._localForm = { fill: fill };

  // Submit is caught in the capture phase, before the kernel's own
  // form handling, and never reaches the network. One save at a time
  // per form: a second submit while the first is in flight (a double
  // click) is dropped, not saved twice.
  const busy = new WeakSet();
  document.addEventListener('submit', (e) => {
    const form = e.target;
    const wrap = form && form.closest && form.closest(FORM);
    if (!wrap) return;
    e.preventDefault();
    e.stopImmediatePropagation();
    if (busy.has(wrap)) return;
    busy.add(wrap);
    form.setAttribute('aria-busy', 'true');
    save(wrap, form).finally(() => {
      busy.delete(wrap);
      form.removeAttribute('aria-busy');
    });
  }, true);

  // A local form posts nowhere. enctype="application/json" is the
  // runtime's mark for a form script handles: the embed frame's
  // navigation guard passes it instead of reporting a blocked submit.
  // The kernel hands the scanner each inserted node, and a node that IS
  // the <form> is marked like one inside the wrapper.
  function scan(root) {
    const scope = root && root.querySelectorAll ? root : document;
    const sel = FORM + ' form';
    [scope, ...scope.querySelectorAll(sel)].forEach((f) => f.matches?.(sel) && f.setAttribute('enctype', 'application/json'));
  }
  scan(document);
  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;

  document.addEventListener('reset', (e) => {
    const wrap = e.target && e.target.closest && e.target.closest(FORM);
    if (!wrap) return;
    wrap.removeAttribute(P + 'editing');
    if (NS._formErrors) NS._formErrors.clear(e.target);
  }, true);
})();
