package crud

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// Row-scope operators for Exposure.ReadScope that the filter package does
// not define. FilterOp is a string type, so declaring the values here needs
// no change to framework/filter, whose ApplyToQuery/ApplyToCountQuery
// helpers silently SKIP unknown ops, which is exactly why read-scope
// predicates are rendered by renderReadScope below instead. A scope
// predicate that a renderer drops, or degrades to equality, silently serves
// rows the declaration says are hidden.
const (
	readOpNeq   filter.FilterOp = "neq"
	readOpNotIn filter.FilterOp = "not_in"
)

// readScopeFilters is the ONE builder of the entity's ReadScope predicates
// for a caller. Every read sink (the routes: list/count/get/cursor/
// stream), the in-process API, typed queries, the eager loaders, the
// include path, and the ?rel.field= subquery) renders this function's
// output through renderReadScope, so there is one answer to "which rows may
// this caller read". Three copies of a security predicate have already
// become three different answers twice in this package; do not add a fourth.
//
// Nil (no predicates) in three cases: the entity declares no ReadScope, the
// Filter is empty (both true no-ops for every existing entity), or the
// caller is unrestricted; see readScopeUnrestricted.
//
// Values are coerced to the column's schema type (booleans today) so a
// predicate on a Bool column binds a Go bool, exactly as a caller's
// ?field=true filter would (filter.ParseFiltersValues); a raw "true"
// string binds TEXT against SQLite's INTEGER storage and matches nothing.
func readScopeFilters(ctx context.Context, ent *entity.Entity) []filter.ParsedFilter {
	if ent == nil {
		return nil
	}
	rs := ent.Config.Exposure.ReadScope
	if rs == nil || len(rs.Filter) == 0 || readScopeUnrestricted(ctx, ent) {
		return nil
	}
	types := map[string]schema.FieldType{}
	for _, f := range ent.GetFields() {
		types[f.Name] = f.Type
	}
	var out []filter.ParsedFilter
	for _, p := range rs.Filter {
		ft := types[p.Field]
		switch p.Op {
		case "", "eq":
			out = append(out, filter.ParsedFilter{Field: p.Field, Op: filter.OpEq, Value: p.Value}.Coerced(ft))
		case "neq":
			out = append(out, filter.ParsedFilter{Field: p.Field, Op: readOpNeq, Value: p.Value}.Coerced(ft))
		case "in":
			// One OpIn filter per value; renderReadScope coalesces the
			// adjacent run back into a single IN (...).
			for _, v := range p.Values {
				out = append(out, filter.ParsedFilter{Field: p.Field, Op: filter.OpIn, Value: v}.Coerced(ft))
			}
		case "not_in":
			// Emitted per value and ANDed: `col NOT IN (a) AND col NOT IN (b)`
			// is equivalent to `col NOT IN (a, b)` for every SQL value
			// including NULL (both filter NULL rows out).
			for _, v := range p.Values {
				out = append(out, filter.ParsedFilter{Field: p.Field, Op: readOpNotIn, Value: v}.Coerced(ft))
			}
		}
	}
	return out
}

// readScopeUnrestricted reports whether the caller reads every row of ent
// despite its ReadScope.
//
// Unrestricted non-empty names a permission: a caller holding it is
// unrestricted (checked through access.Can, fail-closed with no policy in
// context, exactly like crossOwnerReadGranted).
//
// Unrestricted EMPTY is the weaker, deliberate lift the issue names: ANY
// caller with a session reads every row, and an anonymous caller gets the
// filter. The signal is handler.GetUser, the same one requireAuthenticated
// uses, not an editor role.
func readScopeUnrestricted(ctx context.Context, ent *entity.Entity) bool {
	rs := ent.Config.Exposure.ReadScope
	if rs.Unrestricted != "" {
		return access.Can(ctx, access.Permission(rs.Unrestricted))
	}
	_, signedIn := handler.GetUser(ctx)
	return signedIn
}

// renderReadScope renders read-scope predicates into ONE parenthesised,
// AND-composed SQL fragment: "(col = $3 AND col2 NOT IN ($4, $5))". All
// four operators render here: eq, neq, in (coalesced from the builder's
// per-value filters into a single IN list), not_in, with optional
// table qualification for joins (the ManyToMany loader, the ?rel.field=
// EXISTS subquery). startIdx is the first $N; callers know how many
// placeholders precede the fragment.
//
// The fragment is deliberately NOT fed through renderFilterClause: that
// path renders unknown ops as equality (opToSQL's default), which would
// turn a `neq` scope into an `eq` scope and serve the complement of the
// declared rows. An op this renderer does not know matches nothing
// ("1 = 0"): fail closed, never fail open.
func renderReadScope(preds []filter.ParsedFilter, qualifier string, startIdx int) (string, []any) {
	if len(preds) == 0 {
		return "", nil
	}
	col := func(field string) string {
		if qualifier != "" {
			return query.QuoteIdent(qualifier) + "." + query.QuoteIdent(field)
		}
		return query.QuoteIdent(field)
	}
	var parts []string
	var args []any
	idx := startIdx
	for i := 0; i < len(preds); i++ {
		f := preds[i]
		switch f.Op {
		case filter.OpIn:
			// Coalesce the adjacent same-field run the builder emitted for
			// one declared `in` predicate into a single IN (...). Runs from
			// different sources are never merged here because only the
			// builder's output reaches this renderer.
			phs := []string{fmt.Sprintf("$%d", idx)}
			args = append(args, f.BindValue())
			idx++
			for i+1 < len(preds) && preds[i+1].Op == filter.OpIn && preds[i+1].Field == f.Field {
				i++
				phs = append(phs, fmt.Sprintf("$%d", idx))
				args = append(args, preds[i].BindValue())
				idx++
			}
			parts = append(parts, fmt.Sprintf("%s IN (%s)", col(f.Field), strings.Join(phs, ", ")))
		case filter.OpEq:
			parts = append(parts, fmt.Sprintf("%s = $%d", col(f.Field), idx))
			args = append(args, f.BindValue())
			idx++
		case readOpNeq:
			parts = append(parts, fmt.Sprintf("%s <> $%d", col(f.Field), idx))
			args = append(args, f.BindValue())
			idx++
		case readOpNotIn:
			parts = append(parts, fmt.Sprintf("%s NOT IN ($%d)", col(f.Field), idx))
			args = append(args, f.BindValue())
			idx++
		default:
			// Fail closed: a scope predicate that cannot render must match
			// nothing, never everything.
			parts = append(parts, "1 = 0")
		}
	}
	return "(" + strings.Join(parts, " AND ") + ")", args
}

// applyReadScopeWhere ANDs the read-scope fragment into any builder with a
// Where(string, ...any) method; QueryBuilder and CountBuilder share the
// shape, which is why one helper serves both. Empty preds are a no-op, so
// an entity without a ReadScope (every existing entity) builds identical
// SQL to before.
func applyReadScopeWhere(addWhere func(sql string, args ...any), preds []filter.ParsedFilter) {
	if len(preds) == 0 {
		return
	}
	clause, args := renderReadScope(preds, "", 1)
	addWhere(clause, args...)
}

// ApplyReadScope ANDs the entity's ReadScope predicates into a SELECT the
// caller is building against this entity's own table: the read-scope
// mirror of ApplyOwnerScope. No-op when the entity declares no ReadScope,
// when the caller is unrestricted, or when the handler's entity is not the
// table being queried (each entity's handler applies its own scope).
//
// READS only. Update/delete/upsert statements deliberately do not carry
// this predicate in this version: a write is authorized by the write gates
// (owner, tenant, RBAC), and filtering writes by a read posture would
// strand rows their own editor cannot save. The asymmetry is documented,
// not accidental; see the ReadScopeConfig doc comment.
func (ch *CrudHandler) ApplyReadScope(qb *query.QueryBuilder, r *http.Request) {
	applyReadScopeWhere(func(sql string, args ...any) { qb.Where(sql, args...) }, readScopeFilters(r.Context(), ch.Entity))
}

// ApplyReadScopeCount mirrors ApplyReadScope for count queries, so a list
// envelope's total is the count of the rows the caller may see, not the
// table's.
func (ch *CrudHandler) ApplyReadScopeCount(cb *query.CountBuilder, r *http.Request) {
	applyReadScopeWhere(func(sql string, args ...any) { cb.Where(sql, args...) }, readScopeFilters(r.Context(), ch.Entity))
}
