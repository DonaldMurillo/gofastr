package crud

import (
	"context"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// A BeforeList or BeforeGet hook is the documented way to scope reads
// beyond a declared ReadScope ("Hide drafts from non-editors",
// hooks-and-transactions.md). List, Get, the cursor and stream pages,
// ?include= and nested filters (childBeforeScopes) all narrow their query
// with the clauses such a hook appends. Every other surface that hands a
// caller a row, or tells it whether one exists, has no query of its own to
// narrow: a write response, the _events feed, a cascade item naming a
// child, a belongs_to reference, a state refusal naming a stored value.
// Each of those asks the one question below, for the one row it is about:
// would the caller's own Before hooks have let a read reach it? Checking
// only the declared ReadScope there served the rows the hook hid.
//
// The check costs a keyed SELECT only when a hook appended a clause, and
// it fails closed: a hook error, a row with no key, or a failed query
// reads as hidden.

// beforeScopeHidesKey reports whether Before-hook clauses hide the row of
// table whose key columns hold vals. It runs the read the hooks were
// written against, a bare SELECT on the table:
//
//	SELECT k1 FROM table WHERE k1 = $1 [AND k2 = $2 ...] AND (c1) AND (c2)
//
// and the row is hidden when that finds nothing. With no clauses nothing
// is hidden and nothing runs.
func beforeScopeHidesKey(ctx context.Context, conn DBExecutor, table string, key []string, vals []any, clauses []hook.WhereClause) bool {
	if len(clauses) == 0 {
		return false
	}
	if len(vals) != len(key) || !isSafeTable(table) || !safeIdentifiers(key) {
		return true
	}
	qb := query.Select(key[0]).From(table)
	for i, k := range key {
		qb.Where(k+" = $1", vals[i])
	}
	for _, c := range clauses {
		qb.Where(c.SQL, c.Args...)
	}
	sqlStr, args := qb.Build()
	var hit any
	return conn.QueryRowContext(ctx, sqlStr, args...).Scan(&hit) != nil
}

// ownBeforeScopes runs this handler's BeforeGet chain for a read of record
// id by r, and its BeforeList chain too when list is set, and returns the
// clauses they appended.
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

// hookScopeHidesKey reports whether this handler's own Before hooks hide
// the record keyed by vals (record id id) from the caller of r: the
// BeforeGet chain GET /<entity>/{id} runs, plus BeforeList when list is
// set. Statements run on ctx's transaction when it carries one.
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

// cascadeReadBack is the child row a cascade write hands back under rel:
// the row, or its id alone when the child's declared ReadScope
// (scopedReadBack) or its own Before hooks (childScopeHidesKey) hide it,
// so a cascade item never echoes a child its own route and ?include= hide.
// child is the tx-bound handler that wrote the row.
func (ch *CrudHandler) cascadeReadBack(ctx context.Context, r *http.Request, rel entity.Relation, child *CrudHandler, row map[string]any) (map[string]any, error) {
	hidden, err := child.readScopeHidesRow(ctx, row)
	if err != nil {
		return nil, err
	}
	if !hidden {
		// A row with no key names nothing the hooks' clauses can be
		// checked against: nil vals, which hides it when they set any.
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
// for a write handed r, and whether the hooks bind it at all: always on an
// HTTP request, and in process only under WithReadHooks, the gate the
// in-process reads use. A route that runs its write through the in-process
// API (the transition route calls RunTransition) tags ctx with its own
// request (withRealRequest), which wins over the synthetic r. A nil r,
// with no such tag, is in process.
func scopeRequest(ctx context.Context, r *http.Request) (*http.Request, bool) {
	if real, ok := ctx.Value(realRequestKey{}).(*http.Request); ok && real != nil && !inProcess(real) {
		return real, true
	}
	if r != nil && !inProcess(r) {
		return r, true
	}
	return r, readHooksEnabled(ctx)
}

// childScopeHidesKey reports whether target's own Before hooks, chosen for
// rel the way ?include= chooses them (childBeforeScopes), hide target's row
// keyed by vals (record id id) from the caller of r. A cascade write and a
// belongs_to reference reach that row from another entity's route; a row
// the include would not attach is one they neither touch nor confirm.
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
