// Local entity lists: render the records saved in the visitor's
// browser. framework/localentity registers this file as the
// `localentity` behaviour; the kernel loads it (after localdb) when a
// local list or count is on the page. Forms are the sibling
// `localentity-form` behaviour (localform.js).
//
// Everything visible is server-rendered: a list carries an inert row
// <template> the server built from the design system, and this module
// only clones it and sets textContent. It never writes markup from a
// record. Records are untrusted input on the way back out (anyone can
// edit IndexedDB in devtools): values become text, keys become
// attribute strings.
//
// Hooks (all data-fui-local-*, all emitted by framework/localentity):
//   -list="<db>/<store>"  list carrier; -order, -dir, -limit shape it,
//                         -fail is the words a failed delete toasts,
//                         -state is set by this module when the
//                         database could not be read (an error code)
//   -row / -empty         its two <template>s
//   -item / -key          set on each rendered row root by this module
//   -text="<field>"       a span filled with the record's value
//   -delete / -edit="id"  click wrappers inside a row; -edit names the
//                         form carrier the record loads into
//   -count="<db>/<store>" a span filled with the record count
(function () {
  'use strict';
  const NAME = 'localentity';
  const NS = window.__gofastr = window.__gofastr || {};
  const has = (o, k) => o != null && Object.prototype.hasOwnProperty.call(o, k);
  if (NS.loadedModules && has(NS.loadedModules, NAME)) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;

  const P = 'data-fui-local-';
  const LIST = '[' + P + 'list]';
  const COUNT = '[' + P + 'count]';
  const ITEM = '[' + P + 'item]';
  const HOOKS = '[' + P + 'delete],[' + P + 'edit]';

  // target splits "<db>/<store>"; both halves are Go-validated names.
  const target = (el, attr) => {
    const v = el.getAttribute(P + attr) || '';
    const i = v.indexOf('/');
    return i > 0 ? { db: v.slice(0, i), store: v.slice(i + 1), key: v } : null;
  };
  const open = (name) => NS.localdb.open(name);

  // Renderers are keyed by "<db>/<store>". One watch per key schedules
  // every bound list and count of that store; a burst of writes
  // re-renders once.
  const bound = new Map();
  const pending = new Set();
  const refresh = (key) => {
    for (const el of Array.from(bound.get(key) || [])) {
      if (el.isConnected) render(el);
      else bound.get(key).delete(el);
    }
  };
  const schedule = (key) => {
    if (!pending.size) {
      queueMicrotask(() => {
        const keys = Array.from(pending);
        pending.clear();
        keys.forEach(refresh);
      });
    }
    pending.add(key);
  };
  const bind = (el, t) => {
    if (!bound.has(t.key)) {
      bound.set(t.key, new Set());
      open(t.db).then((db) => db.watch(t.store, () => schedule(t.key)), () => bound.delete(t.key));
    }
    const set = bound.get(t.key);
    if (!set || set.has(el)) return false;
    set.add(el);
    return true;
  };

  // text turns a stored value into display text. Objects and arrays
  // render empty: a row slot shows a scalar or nothing.
  const text = (v) => (v === null || v === undefined || typeof v === 'object' ? '' : String(v));
  const isList = (el) => el.matches(LIST);
  const render = (el) => (isList(el) ? renderList(el) : renderCount(el));

  async function renderList(el) {
    const t = target(el, 'list');
    if (!t) return;
    let records = [];
    try {
      const q = { index: el.getAttribute(P + 'order') || undefined };
      if (el.getAttribute(P + 'dir') === 'prev') q.direction = 'prev';
      const limit = parseInt(el.getAttribute(P + 'limit') || '', 10);
      if (limit > 0) q.limit = limit;
      records = await (await open(t.db)).list(t.store, q);
      el.removeAttribute(P + 'state');
    } catch (e) {
      el.setAttribute(P + 'state', (e && e.code) || 'failed');
    }
    // Focus inside a row that is about to be replaced moves to the row
    // now at the same position, onto its same control.
    const items = () => el.querySelectorAll(':scope > ' + ITEM);
    const active = document.activeElement;
    const oldItem = active && active.closest && active.closest(ITEM);
    const hook = oldItem && oldItem.parentElement === el && active.closest(HOOKS);
    const focusAt = hook ? Array.prototype.indexOf.call(items(), oldItem) : -1;
    const focusHook = hook && (hook.hasAttribute(P + 'delete') ? P + 'delete' : P + 'edit');
    items().forEach((n) => n.remove());

    const clone = (kind) => {
      const tpl = el.querySelector(':scope > template[' + P + kind + ']');
      const root = tpl && tpl.content.firstElementChild;
      return root ? root.cloneNode(true) : null;
    };
    const frag = document.createDocumentFragment();
    const empty = !records.length && clone('empty');
    if (empty) {
      empty.setAttribute(P + 'item', 'empty');
      frag.appendChild(empty);
    }
    for (const rec of records) {
      const node = clone('row');
      if (!node) break;
      node.setAttribute(P + 'item', '');
      node.setAttribute(P + 'key', text(rec.id));
      node.querySelectorAll('[' + P + 'text]').forEach((slot) => {
        const field = slot.getAttribute(P + 'text');
        slot.textContent = has(rec, field) ? text(rec[field]) : '';
      });
      frag.appendChild(node);
    }
    el.appendChild(frag);
    if (focusAt >= 0) {
      const now = items();
      const item = now[Math.min(focusAt, now.length - 1)];
      const wrap = item && item.querySelector('[' + focusHook + ']');
      const control = wrap && wrap.querySelector('button, a[href], input, select, textarea, [tabindex]');
      if (control) control.focus();
    }
  }

  async function renderCount(el) {
    const t = target(el, 'count');
    if (!t) return;
    try {
      el.textContent = String(await (await open(t.db)).count(t.store));
    } catch (_) {
      el.textContent = '';
    }
  }

  // Delete removes the row's record; Edit loads it into the named form
  // carrier through the form module (loaded by that carrier's marker).
  document.addEventListener('click', (e) => {
    const hook = e.target && e.target.closest && e.target.closest(HOOKS);
    const item = hook && hook.closest(ITEM);
    const list = item && item.parentElement;
    const t = list && isList(list) && target(list, 'list');
    const key = item && item.getAttribute(P + 'key');
    if (!t || !key) return;
    e.preventDefault();
    const db = open(t.db);
    if (hook.hasAttribute(P + 'delete')) {
      db.then((h) => h.delete(t.store, key)).catch(() => {
        if (typeof NS._toastOrFallback === 'function') {
          NS._toastOrFallback({ variant: 'error', title: list.getAttribute(P + 'fail'), ttl: 6000 });
        }
      });
      return;
    }
    const wrap = document.getElementById(hook.getAttribute(P + 'edit') || '');
    if (!wrap || wrap.getAttribute(P + 'form') !== t.key || !NS._localForm) return;
    db.then((h) => h.get(t.store, key)).then((rec) => { if (rec) NS._localForm.fill(wrap, rec); }, () => {});
  });

  // The arrival pass: at load, after an island swap and after a client
  // navigation the kernel calls the scanner with the inserted root.
  function scan(root) {
    const scope = root && root.querySelectorAll ? root : document;
    const els = Array.from(scope.querySelectorAll(LIST + ',' + COUNT));
    if (scope.matches && scope.matches(LIST + ',' + COUNT)) els.push(scope);
    for (const el of els) {
      const t = target(el, isList(el) ? 'list' : 'count');
      if (t && bind(el, t)) render(el);
    }
  }
  scan(document);
  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;
})();
