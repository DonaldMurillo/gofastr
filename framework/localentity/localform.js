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
      specs.set(wrap, spec && Array.isArray(spec.fields) && spec.msgs ? spec : null);
    }
    return specs.get(wrap);
  };
  const say = (msg, n) => String(msg || '').split('{n}').join(n === undefined ? '' : String(n));
  const text = (v) => (v === null || v === undefined || typeof v === 'object' ? '' : String(v));

  // coerce reads one field from the form and checks it against its
  // declaration. Returns {value} (undefined value = leave unset) or
  // {error}.
  function coerce(f, data, msgs) {
    if (f.t === 'bool') {
      const all = data.getAll(f.name);
      return { value: all.length ? all.includes('true') || all.includes('on') : f.def };
    }
    const raw = data.get(f.name);
    if (raw === null || raw === '') return f.req ? { error: msgs.required } : { value: f.def };
    if (typeof raw !== 'string') return { error: msgs.required };
    if (f.t === 'int' || f.t === 'float') {
      const v = raw.trim() === '' ? NaN : Number(raw.trim());
      if (!Number.isFinite(v)) return { error: msgs.number };
      if (f.t === 'int' && !Number.isSafeInteger(v)) return { error: msgs.integer };
      if (f.min !== undefined && v < f.min) return { error: say(msgs.min, f.min) };
      if (f.max !== undefined && v > f.max) return { error: say(msgs.max, f.max) };
      return { value: v };
    }
    if (f.t === 'enum' && !(f.values || []).includes(raw)) return { error: msgs.choice };
    if ((f.t === 'date' && !/^\d{4}-\d{2}-\d{2}$/.test(raw)) ||
        ((f.t === 'date' || f.t === 'timestamp') && !Number.isFinite(Date.parse(raw)))) {
      return { error: msgs.date };
    }
    if (f.min !== undefined && raw.length < f.min) return { error: say(msgs.minlen, f.min) };
    if (f.max !== undefined && raw.length > f.max) return { error: say(msgs.maxlen, f.max) };
    if (f.pattern) {
      let re = null;
      try { re = new RegExp(f.pattern, 'u'); } catch (_) { re = null; }
      if (re && !re.test(raw)) return { error: msgs.pattern };
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
    const entries = [];
    const errors = {};
    let failed = false;
    for (const f of spec.fields) {
      const r = coerce(f, data, msgs);
      if (r.error) {
        errors[f.name] = [r.error];
        failed = true;
      } else if (r.value !== undefined) {
        entries.push([f.name, r.value]);
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
    const values = Object.fromEntries(entries);
    const editing = wrap.getAttribute(P + 'editing');
    const now = new Date().toISOString();
    // The cap is decided inside the transaction, but localdb re-codes
    // every failure that leaves one, so the verdict rides a flag.
    let full = false;
    try {
      const db = await NS.localdb.open(dbName);
      await db.tx(store, 'readwrite', async (tx) => {
        if (editing) {
          const current = await tx.get(store, editing);
          if (!current) throw new Error('gone');
          await tx.put(store, Object.assign({}, current, values, { id: current.id, updated_at: now }));
          return;
        }
        if (spec.max && (await tx.count(store)) >= spec.max) {
          full = true;
          throw new Error('full');
        }
        await tx.put(store, Object.assign({}, values, { created_at: now, updated_at: now }));
      });
    } catch (_) {
      if (typeof NS._toastOrFallback === 'function') {
        NS._toastOrFallback({ variant: 'error', title: full ? say(msgs.full, spec.max) : msgs.failed, ttl: 6000 });
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
        if (c.type === 'checkbox') c.checked = value === true;
        else if (c.type === 'radio') c.checked = c.value === text(value);
        else if (c.type !== 'hidden' || list.length === 1) c.value = text(value);
      }
    }
    wrap.setAttribute(P + 'editing', text(rec.id));
    const first = form.querySelector('input:not([type=hidden]), select, textarea');
    if (first) first.focus();
  }
  NS._localForm = { fill: fill };

  // Submit is caught in the capture phase, before the kernel's own
  // form handling, and never reaches the network.
  document.addEventListener('submit', (e) => {
    const form = e.target;
    const wrap = form && form.closest && form.closest(FORM);
    if (!wrap) return;
    e.preventDefault();
    e.stopImmediatePropagation();
    save(wrap, form);
  }, true);

  document.addEventListener('reset', (e) => {
    const wrap = e.target && e.target.closest && e.target.closest(FORM);
    if (!wrap) return;
    wrap.removeAttribute(P + 'editing');
    if (NS._formErrors) NS._formErrors.clear(e.target);
  }, true);
})();
