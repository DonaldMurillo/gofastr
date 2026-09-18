// local-migrate: schema evolution for framework/local, and nothing
// else.
//
// A collection declares a version and the steps that bring a record
// from one version to the next. Running them is a different job from
// keeping records: it happens once per browser per version, it rewrites
// every record of a collection at once, and when it goes wrong the
// damage is a collection on two schemas rather than one write lost. So
// it is its own module, registered LoadIdle, never on the critical
// path, and local-store asks for it by name at the moment a rewrite is
// due. A collection that declares no version step never runs a line of
// this file.
//
// The contract with local-store is one call, NS._localMigrate(ctx),
// resolving true when the collection reached its declared version and
// false when it did not. False GATES the collection: local-store
// refuses every method rather than serve records on a schema this build
// cannot read. Nothing here writes a version it did not earn.
(() => {
  'use strict';
  const NAME = 'local-migrate';
  const NS = window.__gofastr = window.__gofastr || {};
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  // Flag first (see local-store.js).
  (NS.loadedModules = NS.loadedModules || {})[NAME] = true;

  const own = (o, k) => Object.prototype.hasOwnProperty.call(o, k);
  const isObject = (v) => v !== null && typeof v === 'object' && !Array.isArray(v);
  const encode = (value) => {
    let text;
    try { text = JSON.stringify(value); } catch (_) { return null; }
    return typeof text === 'string' ? text : null;
  };

  // applyStep runs one declared step over one record. Steps are data
  // transforms declared in Go (rename, default, remove) plus `func`,
  // a host-registered function on window.__gofastr._localMigrations,
  // a real function loaded from the script rail, never text, so the
  // page stays CSP-clean. A rename onto an existing field, or of a
  // missing one, is a no-op, which is what lets two tabs migrate the
  // same collection without a lock: every declared step is
  // idempotent; a func step is asked to be.
  const applyStep = (step, record, key) => {
    if (!isObject(record) || !step) return record;
    switch (step.op) {
      case 'rename':
        if (own(record, step.from) && !own(record, step.to)) {
          record[step.to] = record[step.from];
          delete record[step.from];
        }
        return record;
      case 'default':
        if (!own(record, step.field)) record[step.field] = step.value;
        return record;
      case 'remove':
        delete record[step.field];
        return record;
      case 'func': {
        const fns = NS._localMigrations;
        const fn = fns && own(fns, step.name) ? fns[step.name] : null;
        if (typeof fn !== 'function') throw new Error('migration function not registered: ' + step.name);
        const out = fn(record, key);
        return out === undefined ? record : out;
      }
    }
    return record;
  };

  // _localMigrate rewrites every record of one collection through the
  // declared steps, then stamps the new version, in that order, and
  // only if every write landed.
  //
  // P.set settles {ok:false} on a quota refusal or an aborted
  // transaction; it does not reject. Stamping the version over a
  // half-rewritten collection would record the migration as done, so it
  // would never run again and every later read would mix two schemas,
  // which is why every settlement is checked rather than awaited.
  NS._localMigrate = (ctx) => ctx.P.entries(ctx.prefix).then((er) => {
    if (!er.ok) return ctx.fail(ctx.app, ctx.coll, '', 'migration').ok;
    const writes = [];
    const done = [];
    for (const e of er.entries) {
      let rec = e.value;
      const key = e.key.slice(ctx.prefix.length);
      for (const m of ctx.steps) for (const st of m.steps || []) rec = applyStep(st, rec, key);
      done.push({ key: key, rec: rec });
      writes.push(ctx.P.set(e.key, rec));
    }
    return Promise.all(writes).then((rs) => {
      for (const r of rs) if (!r || !r.ok) return ctx.fail(ctx.app, ctx.coll, '', 'migration').ok;
      return ctx.P.set(ctx.meta, { v: ctx.to }).then((r) => {
        if (!r.ok) return ctx.fail(ctx.app, ctx.coll, '', 'migration').ok;
        ctx.emit('gofastr:local-migrated', { app: ctx.app, collection: ctx.coll, from: ctx.from, to: ctx.to });
        // The MIGRATED record, not the one the entry carried: mirroring
        // e.value would put the pre-migration shape in the cookie and
        // hand the server the old schema on every request.
        if (ctx.mirror) for (const d of done) ctx.mirror(d.key, encode(d.rec) || 'null');
        return true;
      });
    });
    // A func step whose function is not registered throws; the records
    // and the version are left as they were.
  }).catch(() => ctx.fail(ctx.app, ctx.coll, '', 'migration').ok);
})();
