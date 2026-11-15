package crud

import (
	"context"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

func beforeScopeHidesKey(ctx context.Context, conn DBExecutor, table string, key []string, vals []any, clauses []hook.WhereClause) bool {
	if len(clauses) == 0 {
		return false
	}
	if len(vals) != len(key) || !isSafeTable(table) || !safeIdentifiers(key) {
		return true
	}
	qb := query.Select(key[0]).From(table)
	for i, k := range key {
		qb.Where(query.QuoteKeyword(k)+" = $1", vals[i])
	}
	return clausesFindNothing(ctx, conn, qb, clauses)
}

func clausesFindNothing(ctx context.Context, conn DBExecutor, qb *query.QueryBuilder, clauses []hook.WhereClause) bool {
	for _, c := range clauses {
		qb.Where(c.SQL, c.Args...)
	}
	sqlStr, args := qb.Build()
	var hit any
	return conn.QueryRowContext(ctx, sqlStr, args...).Scan(&hit) != nil
}

// The rows a record write may target on a soft-delete entity: live only, or
// live and trashed (restore and purge).
const (
	targetLive    = false
	targetTrashed = true
)

// refuseHiddenTarget answers errNotFound when the caller's BeforeGet hooks
// hide record id or, with a guarded hook registered, the write's scope does.
func (ch *CrudHandler) refuseHiddenTarget(ctx context.Context, r *http.Request, id string, trashed bool, guarded ...hook.HookType) error {
	if ch.Hooks == nil {
		return nil
	}
	probe := false
	for _, ht := range guarded {
		if len(ch.Hooks.HooksFor(ht)) > 0 {
			probe = true
		}
	}
	var clauses []hook.WhereClause
	if len(ch.Hooks.HooksFor(hook.BeforeGet)) > 0 && !serverWrites(ctx) {
		if hr, apply := scopeRequest(ctx, r); apply {
			var err error
			if clauses, err = ch.ownBeforeScopes(ctx, hr, id, false); err != nil {
				return errNotFound
			}
		}
	}
	if len(clauses) == 0 && !probe {
		return nil
	}
	table, key := ch.Entity.GetTable(), ch.keyCols()
	if !isSafeTable(table) || !safeIdentifiers(key) {
		return errNotFound
	}
	kc, ka := ch.keyWhere(id)
	qb := query.Select(key[0]).From(table).Where(kc, ka...)
	ch.ApplyTenantScope(qb, r)
	applyOwnerScope(ch, qb, r, false)
	if ch.Entity.Config.Scope.SoftDelete && !trashed {
		qb.Where("deleted_at IS NULL")
	}
	if clausesFindNothing(ctx, ch.conn(ctx), qb, clauses) {
		return errNotFound
	}
	return nil
}

func (ch *CrudHandler) upsertTargetHidden(ctx context.Context, r *http.Request, body map[string]any) bool {
	if ch.Hooks == nil || len(ch.Hooks.HooksFor(hook.BeforeGet)) == 0 || serverWrites(ctx) {
		return false
	}
	hr, apply := scopeRequest(ctx, r)
	if !apply {
		return false
	}
	id, _ := entity.RowKey(body, ch.keyCols(), nil)
	clauses, err := ch.ownBeforeScopes(ctx, hr, id, false)
	if err != nil {
		return true
	}
	return beforeScopeHidesKey(ctx, ch.conn(ctx), ch.Entity.GetTable(), ch.keyCols(), ch.colKeyVals(body), clauses)
}

func (ch *CrudHandler) ownBeforeScopes(ctx context.Context, r *http.Request, id string, list bool) ([]hook.WhereClause, error) {
	if ch.Hooks == nil {
		return nil, nil
	}
	req := hookRequest(r)
	var where []hook.WhereClause
	if list && len(ch.Hooks.HooksFor(hook.BeforeList)) > 0 {
		payload := &hook.ListPayload{Request: req}
		if err := runHooks(ch.Hooks, hookCtx(ctx), hook.BeforeList, payload); err != nil {
			return nil, err
		}
		where = append(where, payload.Where...)
	}
	if len(ch.Hooks.HooksFor(hook.BeforeGet)) > 0 {
		payload := &hook.GetPayload{Request: req, ID: id}
		if err := runHooks(ch.Hooks, hookCtx(ctx), hook.BeforeGet, payload); err != nil {
			return nil, err
		}
		where = append(where, payload.Where...)
	}
	return where, nil
}

func (ch *CrudHandler) hookScopeHidesKey(ctx context.Context, r *http.Request, vals []any, id string, list bool) bool {
	if ch.Hooks == nil {
		return false
	}
	clauses, err := ch.ownBeforeScopes(ctx, r, id, list)
	if err != nil {
		return true
	}
	return beforeScopeHidesKey(ctx, ch.conn(ctx), ch.Entity.GetTable(), ch.keyCols(), vals, clauses)
}

// cascadeReadBack is the child row a cascade write hands back under rel, or
// its key alone when the child's ReadScope or Before hooks hide it.
func (ch *CrudHandler) cascadeReadBack(ctx context.Context, r *http.Request, rel entity.Relation, child *CrudHandler, row map[string]any) (map[string]any, error) {
	hidden, err := child.readScopeHidesRow(ctx, row)
	if err != nil {
		return nil, err
	}
	if !hidden {
		vals, _ := child.rowKeyVals(row)
		id, _ := child.rowID(row)
		hidden = ch.childScopeHidesKey(ctx, r, child.Entity, rel, vals, id)
	}
	if hidden {
		return child.identityOnly(row), nil
	}
	return row, nil
}

// scopeRequest is the request a Before hook sees for a check made on ctx
// for a write handed r, and whether the hooks bind it at all.
func scopeRequest(ctx context.Context, r *http.Request) (*http.Request, bool) {
	if real, ok := ctx.Value(realRequestKey{}).(*http.Request); ok && real != nil && !inProcess(real) {
		return real, true
	}
	if r != nil && !inProcess(r) {
		return r, true
	}
	return r, readHooksEnabled(ctx)
}

// childScopeHidesKey reports whether target's Before hooks, chosen for rel as
// ?include= chooses them, hide target's row keyed by vals from the caller.
func (ch *CrudHandler) childScopeHidesKey(ctx context.Context, r *http.Request, target *entity.Entity, rel entity.Relation, vals []any, id string) bool {
	if ch.ChildHooks == nil || target == nil || serverWrites(ctx) {
		return false
	}
	hr, apply := scopeRequest(ctx, r)
	if !apply {
		return false
	}
	clauses, err := ch.childBeforeScopesFor(withRealRequest(ctx, hr), target, rel, id)
	if err != nil {
		return true
	}
	return beforeScopeHidesKey(ctx, ch.conn(ctx), target.GetTable(), target.KeyColumns(), vals, clauses)
}
