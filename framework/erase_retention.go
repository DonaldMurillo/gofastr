package framework

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"slices"
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
const eraseIdentitiesDDL = `CREATE TABLE IF NOT EXISTS ` + eraseIdentitiesTable + ` (tenant TEXT NOT NULL DEFAULT '', user_id TEXT NOT NULL, store TEXT NOT NULL, kind BIGINT NOT NULL, value TEXT NOT NULL, expires_at BIGINT NOT NULL, PRIMARY KEY (tenant, user_id, store, kind))`

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

// storeOutcome is one resolved store's run: its error and the user it
// was given.
type storeOutcome struct {
	err  error
	user datexport.ErasedUser
	// incomplete: a kept value was withheld because whether another
	// account holds it could not be told, so its rows must stay.
	incomplete bool
}

// retained is what one failed store keeps for one user, and when it
// expires (Unix seconds; the earliest of its rows).
type retained struct {
	ids     map[datexport.IdentityKind]string
	expires int64
}

// readRetention answers the unexpired identities kept for tenant and
// userID, by store; nil when the table does not exist.
func (a *App) readRetention(ctx context.Context, dialect migrate.Dialect, tenant, userID string) (map[string]retained, error) {
	phys, exists, err := physicalTable(ctx, a.DB, eraseIdentitiesTable, dialect, false)
	if err != nil || !exists {
		return nil, err
	}
	rows, err := a.DB.QueryContext(ctx, fmt.Sprintf(`SELECT store, kind, value, expires_at FROM %s WHERE tenant = $1 AND user_id = $2 AND expires_at >= $3`, quotePhysical(phys)),
		tenant, userID, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]retained{}
	for rows.Next() {
		var store, val string
		var kind, expires int64
		if err := rows.Scan(&store, &kind, &val, &expires); err != nil {
			return nil, err
		}
		r, ok := out[store]
		if !ok {
			r = retained{ids: map[datexport.IdentityKind]string{}, expires: expires}
		}
		r.ids[datexport.IdentityKind(kind)] = val
		r.expires = min(r.expires, expires)
		out[store] = r
	}
	return out, rows.Err()
}

// retainedUserFor is user with the identities kept for one store added
// where nothing resolves now, unless they expired by now. A kept value
// another account now holds is left out: the store would match that
// account's data by it. incomplete reports a value left out because
// that could not be told.
func (a *App) retainedUserFor(ctx context.Context, dialect migrate.Dialect, user datexport.ErasedUser, kept retained) (out datexport.ErasedUser, incomplete bool) {
	if len(kept.ids) == 0 || kept.expires < time.Now().Unix() {
		return user, false
	}
	out = datexport.ErasedUser{ID: user.ID, Identities: maps.Clone(user.Identities)}
	if out.Identities == nil {
		out.Identities = map[datexport.IdentityKind]string{}
	}
	for kind, val := range kept.ids {
		if _, ok := out.Identity(kind); ok {
			continue
		}
		held, err := a.identityHeldByOther(ctx, dialect, kind, val, user.ID)
		if err != nil {
			incomplete = true
			continue
		}
		if held {
			continue
		}
		out.Identities[kind] = val
	}
	return out, incomplete
}

// identityHeldByOther reports whether a live row of kind's resolver table
// other than userID's holds val. An unknown answer reads as held.
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
	// Identifiers are validated and quoted; the value is a parameter.
	stmt := fmt.Sprintf(`SELECT %s, %s FROM %s WHERE LOWER(%s) = LOWER($1)`, qID, qVal, quotePhysical(phys), qVal)
	args := []any{val}
	if r.Canonicalize != nil {
		// The resolver's fold can join spellings LOWER keeps apart (NFD
		// forms, invisible runes, non-ASCII case on SQLite), so every row
		// is folded here. It runs only on a retry of a failed erasure.
		stmt, args = fmt.Sprintf(`SELECT %s, %s FROM %s`, qID, qVal, quotePhysical(phys)), nil
	}
	rows, err := a.DB.QueryContext(ctx, stmt, args...)
	if err != nil {
		return true, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, v sql.NullString
		if err := rows.Scan(&id, &v); err != nil {
			return true, err
		}
		if id.String == userID {
			continue
		}
		// Without the resolver's own fold, a spelling that differs only
		// in case counts as held: withholding is the safe side.
		if c := v.String; strings.EqualFold(c, val) || foldsTo(r.Canonicalize, c, val) {
			return true, nil
		}
	}
	return false, rows.Err()
}

// updateRetention records the stores' outcome: a store that succeeded
// loses what was kept for it; a resolved store that failed for the first
// time keeps the identities it was given, until keptIdentitiesFor from
// now; one that failed again keeps its rows and their first expiry, and
// is never written back when a concurrent retry already removed them.
// Every expired row goes too.
func (a *App) updateRetention(ctx context.Context, dialect migrate.Dialect, tenant, userID string, before map[string]retained, outcome map[string]storeOutcome) error {
	var fresh []string
	for name, o := range outcome {
		if o.err != nil && before[name].ids == nil && len(o.user.Identities) > 0 {
			fresh = append(fresh, name)
		}
	}
	_, exists, err := physicalTable(ctx, a.DB, eraseIdentitiesTable, dialect, false)
	if err != nil {
		return err
	}
	if !exists {
		if len(fresh) == 0 {
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
	now := time.Now()
	if _, err := tx.ExecContext(ctx, `DELETE FROM `+eraseIdentitiesTable+` WHERE expires_at < $1`, now.Unix()); err != nil {
		return err
	}
	for name, o := range outcome {
		if o.err == nil && !o.incomplete {
			if _, err := tx.ExecContext(ctx, `DELETE FROM `+eraseIdentitiesTable+` WHERE tenant = $1 AND user_id = $2 AND store = $3`, tenant, userID, name); err != nil {
				return err
			}
		}
	}
	slices.Sort(fresh)
	for _, name := range fresh {
		ids := outcome[name].user.Identities
		for _, kind := range slices.Sorted(maps.Keys(ids)) {
			if v := ids[kind]; v != "" {
				if _, err := tx.ExecContext(ctx, `INSERT INTO `+eraseIdentitiesTable+` (tenant, user_id, store, kind, value, expires_at) VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT DO NOTHING`,
					tenant, userID, name, int64(kind), v, now.Add(keptIdentitiesFor).Unix()); err != nil {
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
	_, err = a.DB.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE expires_at < $1`, quotePhysical(phys)), time.Now().Unix())
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
	go func() {
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
	}()
}

// foldsTo reports whether canonicalize maps v to want; a panic in the
// host's callback reads as a match, the side that withholds the value.
func foldsTo(canonicalize func(string) string, v, want string) (match bool) {
	if canonicalize == nil {
		return false
	}
	defer func() {
		if recover() != nil {
			match = true
		}
	}()
	return canonicalize(v) == want
}
