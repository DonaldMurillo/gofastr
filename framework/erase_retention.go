package framework

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// eraseIdentitiesTable keeps, per tenant, erased user and failed store,
// the identities (an email, say) that store needs when the erasure is
// retried: the user row they resolved from is gone by then. Rules:
// primitives/app/privacy, erase-retention-*.
const eraseIdentitiesTable = "gofastr_erase_identities"

// eraseIdentitiesDDL creates eraseIdentitiesTable. An operator whose
// runtime role may not CREATE runs it once ahead of time.
//
//gofastr:allow(GOFASTR1415) erasure's own bookkeeping, outside every export and erase plane on purpose: a restore must not bring an erased user's email back; rows live keptIdentitiesFor at most (primitives/app/privacy)
const eraseIdentitiesDDL = `CREATE TABLE IF NOT EXISTS ` + eraseIdentitiesTable + ` (tenant TEXT NOT NULL DEFAULT '', user_id TEXT NOT NULL, store TEXT NOT NULL, kind BIGINT NOT NULL, value TEXT NOT NULL, expires_at BIGINT NOT NULL, PRIMARY KEY (tenant, user_id, store, kind, value))`

// keptIdentitiesFor is how long a failed store's identities are kept,
// counted from the first failure; a retry never extends it.
const keptIdentitiesFor = 30 * 24 * time.Hour

// retentionWriteTimeout bounds each step of the post-commit bookkeeping
// (the read, each holder check, the write), which runs on a context
// detached from the request's deadline. A var so a test can shorten it.
var retentionWriteTimeout = 30 * time.Second

// heldCheckFault is a test seam: when set, a holder check fails with its
// error before it queries.
var heldCheckFault func() error

// retentionSweepEvery is how often a running App deletes expired rows.
const retentionSweepEvery = time.Hour

func hasResolvedStoreErasers() bool {
	return slices.ContainsFunc(datexport.AllStoreErasers(), func(s datexport.StoreEraser) bool { return s.EraseResolved != nil })
}

// retentionNow is the retention clock; a var so a test can move it.
var retentionNow = time.Now

// storeKey names a store eraser in the retention table: its source and
// registration name, the source length-prefixed. A process restart registers every store again
// under the same name, and a retry must find its rows across one, so
// the name is the identity; a store that registers under a name another
// store used before is taken as the same store.
func storeKey(s datexport.StoreEraser) string {
	// The source's length goes first, so no two source and name pairs
	// share a key ("app" + "cache/x" and "app/cache" + "x" differ).
	return strconv.Itoa(len(s.Source)) + ":" + s.Source + "/" + s.Name
}

// keptRow is one kept identity of one store.
type keptRow struct {
	kind    datexport.IdentityKind
	value   string
	expires int64
}

// storeOutcome is one resolved store's run.
type storeOutcome struct {
	err error
	// read is every row read for the store before it ran: a success
	// removes these and no row written since.
	read []keptRow
	// incomplete: a kept value was withheld because whether another
	// account holds it could not be told, so its rows must stay.
	incomplete bool
}

// readRetention answers the rows kept for tenant and userID that are
// unexpired now, by store key; nil when the table does not exist.
func (a *App) readRetention(ctx context.Context, dialect migrate.Dialect, tenant, userID string) (map[string][]keptRow, error) {
	phys, exists, err := physicalTable(ctx, a.DB, eraseIdentitiesTable, dialect, false)
	if err != nil || !exists {
		return nil, err
	}
	rows, err := a.DB.QueryContext(ctx, fmt.Sprintf(`SELECT store, kind, value, expires_at FROM %s WHERE tenant = $1 AND user_id = $2 AND expires_at >= $3`, quotePhysical(phys)),
		tenant, userID, retentionNow().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]keptRow{}
	for rows.Next() {
		var store string
		var r keptRow
		var kind int64
		if err := rows.Scan(&store, &kind, &r.value, &r.expires); err != nil {
			return nil, err
		}
		r.kind = datexport.IdentityKind(kind)
		out[store] = append(out[store], r)
	}
	return out, rows.Err()
}

// identityHeldByOther reports whether a live row of kind's resolver table
// other than userID's holds val: the same value, one differing only in
// case (Unicode case, not SQL LOWER's), or one the resolver's Canonicalize
// folds to val. Every row is read, which only a retry of a failed erasure
// does. An answer it cannot give is an error.
func (a *App) identityHeldByOther(ctx context.Context, dialect migrate.Dialect, kind datexport.IdentityKind, val, userID string) (bool, error) {
	if heldCheckFault != nil {
		if err := heldCheckFault(); err != nil {
			return true, err
		}
	}
	r, ok := datexport.ResolveIdentity(kind)
	if !ok {
		return true, errors.New("no resolver")
	}
	phys, exists, err := physicalTable(ctx, a.DB, r.Table, dialect, false)
	if err != nil {
		return true, err
	}
	if !exists {
		return false, nil
	}
	qID := query.QuoteFolded(query.MustIdent(r.IDColumn))
	qVal := query.QuoteFolded(query.MustIdent(r.ValueColumn))
	// Identifiers are validated and quoted; no value enters the text.
	rows, err := a.DB.QueryContext(ctx, fmt.Sprintf(`SELECT %s, %s FROM %s`, qID, qVal, quotePhysical(phys)))
	if err != nil {
		return true, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, v sql.NullString
		if err := rows.Scan(&id, &v); err != nil {
			return true, err
		}
		// Resolution skips a NULL or empty value; so does this.
		if id.String == userID || !v.Valid || v.String == "" {
			continue
		}
		if strings.EqualFold(v.String, val) {
			return true, nil
		}
		match, panicked := foldsTo(r.Canonicalize, v.String, val)
		if panicked {
			return true, errors.New("the resolver's Canonicalize panicked")
		}
		if match {
			return true, nil
		}
	}
	return false, rows.Err()
}

// storeRun is one run of a store eraser: the user it gets and, for each
// kind its value came from retention, that kept row (checked for expiry
// again just before the run).
type storeRun struct {
	user datexport.ErasedUser
	kept map[datexport.IdentityKind]keptRow
}

// storePlan is how one store eraser is run on a retry.
type storePlan struct {
	runs []storeRun
	// handled is the kept rows this attempt answers for: handed over,
	// held by another account, or expired. Only these go on success; a
	// row deferred because the kind resolves to another value now stays.
	handled []keptRow
	// incomplete: a kept value was withheld because whether another
	// account holds it could not be told.
	incomplete bool
}

// retainedPlanFor plans one store eraser's runs: user itself, its kept
// values filling kinds where nothing resolves now, and one more run for
// each further kept value of a kind. A value expired, or that another
// account holds, is left out.
func (a *App) retainedPlanFor(ctx context.Context, dialect migrate.Dialect, user datexport.ErasedUser, kept []keptRow) storePlan {
	var plan storePlan
	var cands []keptRow
	for _, r := range kept {
		if _, live := user.Identity(r.kind); live {
			continue // deferred: stays for a run that gets it
		}
		if r.expires < retentionNow().Unix() {
			plan.handled = append(plan.handled, r)
			continue
		}
		held, err := a.identityHeldByOther(ctx, dialect, r.kind, r.value, user.ID)
		if err != nil {
			plan.incomplete = true
			continue
		}
		plan.handled = append(plan.handled, r)
		if !held {
			cands = append(cands, r)
		}
	}
	base := storeRun{user: datexport.ErasedUser{ID: user.ID, Identities: maps.Clone(user.Identities)}, kept: map[datexport.IdentityKind]keptRow{}}
	if base.user.Identities == nil {
		base.user.Identities = map[datexport.IdentityKind]string{}
	}
	var extra []keptRow
	for _, r := range cands {
		if _, ok := base.user.Identity(r.kind); ok {
			extra = append(extra, r)
			continue
		}
		base.user.Identities[r.kind] = r.value
		base.kept[r.kind] = r
	}
	plan.runs = []storeRun{base}
	for _, r := range extra {
		run := storeRun{user: datexport.ErasedUser{ID: user.ID, Identities: maps.Clone(base.user.Identities)}, kept: maps.Clone(base.kept)}
		run.user.Identities[r.kind] = r.value
		run.kept[r.kind] = r
		plan.runs = append(plan.runs, run)
	}
	return plan
}

// current drops from run the kept values expired by now; ok is false
// when the run existed only for a value that expired.
func (run storeRun) current(extra bool) (datexport.ErasedUser, bool) {
	now := retentionNow().Unix()
	u := datexport.ErasedUser{ID: run.user.ID, Identities: maps.Clone(run.user.Identities)}
	for kind, r := range run.kept {
		if r.expires < now {
			if extra {
				return u, false
			}
			delete(u.Identities, kind)
		}
	}
	return u, true
}

// eraseStoresRetained runs the store erasers after the SQL commit, each
// with what failed erasures kept for it, and records the outcome.
func (a *App) eraseStoresRetained(ctx context.Context, dialect migrate.Dialect, tenant string, user datexport.ErasedUser, rep *EraseReport) error {
	ret, rerr, planFor := a.retainedPlans(ctx, dialect, tenant, user)
	outcome := map[string]storeOutcome{}
	storeErr := a.eraseStores(ctx, false, rep, planFor, func(s datexport.StoreEraser, err error, plan storePlan) {
		if s.EraseResolved != nil {
			outcome[storeKey(s)] = storeOutcome{err: err, read: plan.handled, incomplete: plan.incomplete}
		}
	})
	rerr = errors.Join(rerr, withheldError(outcome))
	if !hasResolvedStoreErasers() && ret == nil {
		return errors.Join(rerr, storeErr)
	}
	// The SQL erasure has committed: the bookkeeping must land even when
	// the request's deadline, or the stores' run time, has used it up.
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), retentionWriteTimeout)
	defer cancel()
	if err := a.updateRetention(wctx, dialect, tenant, user, outcome); err != nil {
		err = fmt.Errorf("framework: erase: the identities a retry of the failed stores needs could not be updated: %w", err)
		return errors.Join(rerr, storeErr, err)
	}
	return errors.Join(rerr, storeErr)
}

// eraseStoresDry counts what a retry would reach: each failed store with
// what was kept for it. It writes nothing, and says when a count may be
// short because a kept value was withheld for want of an answer.
func (a *App) eraseStoresDry(ctx context.Context, dialect migrate.Dialect, tenant string, user datexport.ErasedUser, rep *EraseReport) error {
	_, rerr, planFor := a.retainedPlans(ctx, dialect, tenant, user)
	outcome := map[string]storeOutcome{}
	storeErr := a.eraseStores(ctx, true, rep, planFor, func(s datexport.StoreEraser, err error, plan storePlan) {
		if s.EraseResolved != nil {
			outcome[storeKey(s)] = storeOutcome{err: err, incomplete: plan.incomplete}
		}
	})
	return errors.Join(rerr, withheldError(outcome), storeErr)
}

// withheldError names the stores a kept value was withheld from because
// whether another account holds it could not be told.
func withheldError(outcome map[string]storeOutcome) error {
	var held []string
	for name, o := range outcome {
		if o.incomplete {
			held = append(held, name)
		}
	}
	if len(held) == 0 {
		return nil
	}
	slices.Sort(held)
	return fmt.Errorf("framework: erase: whether another account holds a kept identity could not be told, so it was withheld from %s and kept for a retry", strings.Join(held, ", "))
}

// retainedPlans reads what failed erasures kept for user and answers each
// store eraser's plan. Each step runs on its own deadline, detached from
// the request's.
func (a *App) retainedPlans(ctx context.Context, dialect migrate.Dialect, tenant string, user datexport.ErasedUser) (map[string][]keptRow, error, func(datexport.StoreEraser) storePlan) {
	detached := context.WithoutCancel(ctx)
	rctx, cancel := context.WithTimeout(detached, retentionWriteTimeout)
	ret, rerr := a.readRetention(rctx, dialect, tenant, user.ID)
	cancel()
	if rerr != nil {
		rerr = fmt.Errorf("framework: erase: the identities kept by an earlier failed erasure could not be read, so its failed stores got only what resolves now: %w", rerr)
	}
	return ret, rerr, func(s datexport.StoreEraser) storePlan {
		hctx, cancel := context.WithTimeout(detached, retentionWriteTimeout)
		defer cancel()
		return a.retainedPlanFor(hctx, dialect, user, ret[storeKey(s)])
	}
}

// eraseStores runs every store eraser by its plan and reports each
// store's error. A dry run counts what is keyed by the user id once: a
// further run's count is less the id-only count.
func (a *App) eraseStores(ctx context.Context, dryRun bool, rep *EraseReport, planFor func(datexport.StoreEraser) storePlan, done func(datexport.StoreEraser, error, storePlan)) error {
	var failed []string
	for _, s := range datexport.AllStoreErasers() {
		plan := planFor(s)
		total := 0
		var err error
		for i, run := range plan.runs {
			u, ok := run.current(i > 0)
			if !ok {
				continue
			}
			n, uerr := runStoreEraser(ctx, s, u, dryRun)
			if uerr != nil {
				err = uerr
				break
			}
			if s.EraseResolved == nil {
				total += n
				break // an id-keyed store needs one run
			}
			if dryRun && i > 0 {
				idOnly, ierr := runStoreEraser(ctx, s, datexport.ErasedUser{ID: u.ID}, true)
				if ierr != nil {
					err = ierr
					break
				}
				n = max(n-idOnly, 0)
			}
			total += n
		}
		done(s, err, plan)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s (%v)", s.Name, err))
			continue
		}
		rep.Stores = append(rep.Stores, EraseTableResult{Name: s.Name, Source: s.Source, Mode: "delete", RowsAffected: total})
	}
	if len(failed) == 0 {
		return nil
	}
	if dryRun {
		return fmt.Errorf("framework: erase dry run: %d store(s) could not be counted: %s", len(failed), strings.Join(failed, "; "))
	}
	return fmt.Errorf("framework: erase: rows deleted but %d store(s) failed and may still hold the user's data: %s",
		len(failed), strings.Join(failed, "; "))
}

// updateRetention records the stores' outcome. A store that succeeded
// with nothing withheld loses the rows read for it before it ran, and no
// row a concurrent erasure wrote since. A resolved store that failed
// keeps the identities that resolve now (they are gone from the user row
// after this erasure), each until keptIdentitiesFor from now, without
// changing a row already kept; a value it got only from retention is
// never written back, so a row a concurrent retry removed stays removed.
// Expired rows go first.
func (a *App) updateRetention(ctx context.Context, dialect migrate.Dialect, tenant string, user datexport.ErasedUser, outcome map[string]storeOutcome) error {
	var failed []string
	for name, o := range outcome {
		if o.err != nil && len(user.Identities) > 0 {
			failed = append(failed, name)
		}
	}
	_, exists, err := physicalTable(ctx, a.DB, eraseIdentitiesTable, dialect, false)
	if err != nil {
		return err
	}
	if !exists {
		if len(failed) == 0 {
			return nil
		}
		if _, err := a.DB.ExecContext(ctx, eraseIdentitiesDDL); err != nil {
			return err
		}
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now := retentionNow()
	if _, err := tx.ExecContext(ctx, `DELETE FROM `+eraseIdentitiesTable+` WHERE expires_at < $1`, now.Unix()); err != nil {
		return err
	}
	for _, name := range slices.Sorted(maps.Keys(outcome)) {
		o := outcome[name]
		if o.err != nil || o.incomplete {
			continue
		}
		for _, r := range o.read {
			if _, err := tx.ExecContext(ctx, `DELETE FROM `+eraseIdentitiesTable+` WHERE tenant = $1 AND user_id = $2 AND store = $3 AND kind = $4 AND value = $5`,
				tenant, user.ID, name, int64(r.kind), r.value); err != nil {
				return err
			}
		}
	}
	slices.Sort(failed)
	for _, name := range failed {
		for _, kind := range slices.Sorted(maps.Keys(user.Identities)) {
			if v := user.Identities[kind]; v != "" {
				if _, err := tx.ExecContext(ctx, `INSERT INTO `+eraseIdentitiesTable+` (tenant, user_id, store, kind, value, expires_at) VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT DO NOTHING`,
					tenant, user.ID, name, int64(kind), v, now.Add(keptIdentitiesFor).Unix()); err != nil {
					return err
				}
			}
		}
	}
	return tx.Commit()
}

// SweepErasureRetention deletes every kept erasure identity past its
// expiry. A running App calls it at start and every hour; a host that
// runs no App process for long can call it from its own scheduler.
func (a *App) SweepErasureRetention(ctx context.Context) error {
	if a == nil || a.DB == nil {
		return nil
	}
	dialect := migrate.DetectDialect(a.DB)
	phys, exists, err := physicalTable(ctx, a.DB, eraseIdentitiesTable, dialect, false)
	if err != nil || !exists {
		return err
	}
	_, err = a.DB.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE expires_at < $1`, quotePhysical(phys)), retentionNow().Unix())
	return err
}

// startRetentionSweep runs SweepErasureRetention now and every
// retentionSweepEvery until ctx ends.
func (a *App) startRetentionSweep(ctx context.Context) {
	if a.DB == nil {
		return
	}
	sweep := func() {
		if err := a.SweepErasureRetention(ctx); err != nil && ctx.Err() == nil {
			a.Logger().Warn("framework: sweeping expired erasure retention", "error", err)
		}
	}
	a.retentionSweep.Go(func() {
		sweep()
		t := time.NewTicker(retentionSweepEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				sweep()
			}
		}
	})
}

// foldsTo reports whether canonicalize maps v to want, and whether the
// host's callback panicked (which the caller reads as no answer).
func foldsTo(canonicalize func(string) string, v, want string) (match, panicked bool) {
	if canonicalize == nil {
		return false, false
	}
	defer func() {
		if recover() != nil {
			match, panicked = false, true
		}
	}()
	return canonicalize(v) == want, false
}
