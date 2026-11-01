package crud

import (
	"context"
	"fmt"
	"math/big"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

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
// entityReadScope returns the entity's declared read scope, or nil when it has
// none.
//
// Exposure is a POINTER and is not always populated: a CrudHandler built
// directly in a test, or an entity registered without an exposure block, leaves
// it nil. Reaching through it unguarded panicked the SSE feed the moment the
// read-scope check was called there unconditionally, on an entity that declares
// no scope at all. Every caller goes through here.
func entityReadScope(ent *entity.Entity) *entity.ReadScopeConfig {
	if ent == nil || ent.Config.Exposure == nil {
		return nil
	}
	return ent.Config.Exposure.ReadScope
}

func readScopeFilters(ctx context.Context, ent *entity.Entity) []filter.ParsedFilter {
	rs := entityReadScope(ent)
	if rs == nil || len(rs.Filter) == 0 || readScopeUnrestricted(ctx, ent) {
		return nil
	}
	types := map[string]schema.FieldType{}
	for _, f := range ent.GetFields() {
		types[f.Name] = f.Type
	}
	var out []filter.ParsedFilter
	for pi, p := range rs.Filter {
		ft := types[p.Field]
		switch p.Op {
		case "", "eq":
			out = append(out, filter.ParsedFilter{Field: p.Field, Op: filter.OpEq, Value: p.Value}.Coerced(ft))
		case "neq":
			out = append(out, filter.ParsedFilter{Field: p.Field, Op: readOpNeq, Value: p.Value}.Coerced(ft))
		case "in":
			// One OpIn filter per value; renderReadScope coalesces the
			// adjacent run back into a single IN (...).
			// Each value is tagged with its predicate, so two declared
			// `in` predicates on one column never coalesce into a union
			// even if the entity.Define guard is ever loosened.
			for _, v := range p.Values {
				out = append(out, filter.ParsedFilter{Field: p.Field, Op: filter.OpIn, Value: v}.Coerced(ft).InList("scope."+strconv.Itoa(pi)))
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
// unrestricted. The check routes through access.CanResource with the
// entity-shaped Ref (ID "", like every collection-level gate) so a
// resource-aware Decider installed in ctx can keep a caller scoped even
// when the role policy still grants the capability — lifting a declared
// row filter is a resource-scoped decision, the same seam
// requirePermission and crossOwnerReadGranted consult. Fail-closed with
// no policy in context, exactly like those gates.
//
// Unrestricted EMPTY is the weaker, deliberate lift the issue names: ANY
// caller with a session reads every row, and an anonymous caller gets the
// filter. The signal is handler.GetUser, the same one requireAuthenticated
// uses, not an editor role.
func readScopeUnrestricted(ctx context.Context, ent *entity.Entity) bool {
	rs := entityReadScope(ent)
	if rs == nil {
		// No scope to lift. Answering "unrestricted" keeps every caller of
		// this predicate on the cheap path for an entity that declares none.
		return true
	}
	if rs.Unrestricted != "" {
		return access.CanResource(ctx, access.Permission(rs.Unrestricted),
			access.Ref{Type: ent.GetName(), ID: ""})
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
	// Identifiers go in validated but unquoted, the way the DDL wrote
	// them: a quoted mixed-case name misses the folded column on
	// Postgres. One that fails validation matches no row.
	if qualifier != "" && !isSafeTable(qualifier) {
		return "(1 = 0)", nil
	}
	for _, f := range preds {
		if !isSafeIdentifier(f.Field) {
			return "(1 = 0)", nil
		}
	}
	col := func(field string) string {
		if qualifier != "" {
			return qualifier + "." + field
		}
		return field
	}
	var parts []string
	var args []any
	idx := startIdx
	for i := 0; i < len(preds); i++ {
		f := preds[i]
		switch f.Op {
		case filter.OpIn:
			// Coalesce the adjacent same-field run the builder emitted for
			// one declared `in` predicate into a single IN (...).
			//
			// The builder tags each declared predicate's values (InList),
			// so two `in` predicates on one column stay two ANDed lists;
			// entity.Define also refuses that declaration outright.
			phs := []string{fmt.Sprintf("$%d", idx)}
			args = append(args, f.BindValue())
			idx++
			for i+1 < len(preds) && f.SameINList(preds[i+1]) {
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

// readScopeAllowsRecord reports whether a caller under this entity's read scope
// may see one emitted record.
//
// Every other read surface narrows rows in SQL. The `_events` SSE feed cannot:
// it filters payloads that a writer already produced, so there is no query to
// add a predicate to and the declared filter has to be evaluated against the
// row itself.
//
// It evaluates the DECLARED predicates rather than the ParsedFilters
// readScopeFilters emits. Those flatten an `in` into one filter per value for
// the SQL renderer to coalesce back into a single IN, and AND-ing them here
// would read `in: [a, b]` as "a AND b", which matches nothing.
//
// Fail closed on anything it cannot evaluate: a record that is not a map, a
// column the record does not carry, or an operator it does not know. A delete
// event carries only the primary key, so a restricted caller stops seeing
// deletes for a scoped entity, which is correct. Learning that a row existed
// and was deleted is the same disclosure as learning it exists.
func (ch *CrudHandler) readScopeAllowsRecord(record any) bool {
	rs := entityReadScope(ch.Entity)
	if rs == nil || len(rs.Filter) == 0 {
		return true
	}
	row, ok := record.(map[string]any)
	if !ok {
		return false
	}
	types := map[string]schema.FieldType{}
	for _, f := range ch.Entity.GetFields() {
		types[f.Name] = f.Type
	}
	for _, p := range rs.Filter {
		ft := types[p.Field]
		v, found := row[p.Field]
		if !found {
			// The payload carries wire keys, which differ from column names
			// whenever JSONCase or an explicit WireName applies.
			v, found = row[ch.convertKey(p.Field)]
		}
		if !found {
			return false
		}
		// NULL is outside the scope on EVERY operator, because that is what
		// SQL does: `col <> 'draft'` and `col NOT IN ('draft')` are UNKNOWN
		// for a NULL col, so the row is excluded from the list, the count,
		// and the get. Without this, fmt.Sprint rendered NULL as "<nil>",
		// which is unequal to any declared value, so neq and not_in ADMITTED
		// the row and the SSE feed announced a row that GET answers 404 for.
		if v == nil {
			return false
		}
		// Both sides go through scopeText, so the declared value compares
		// as the SQL side binds it and the record's as it was stored.
		got := scopeText(ft, v)
		in := func() bool {
			return slices.ContainsFunc(p.Values, func(s string) bool { return scopeText(ft, s) == got })
		}
		switch p.Op {
		case "", "eq":
			if got != scopeText(ft, p.Value) {
				return false
			}
		case "neq":
			if got == scopeText(ft, p.Value) {
				return false
			}
		case "in":
			if !in() {
				return false
			}
		case "not_in":
			if in() {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// scopeText spells v the one way the read-scope matcher compares it for a
// column of type ft (2026-10-27 D2). The SQL side binds a scope value
// typed (filter.CoerceValue: "1", "t" and "true" are all true; "+5" and
// "05" are 5; a Timestamp in any accepted layout is one instant), so a
// string on either side goes through the same coercion, and the
// record's value is rendered the way the coerced one is. A value the
// coercion refuses stays as sent. Text, not a typed value, because the
// record's numbers arrive as int64 locally and as float64 after the
// fanout bridge's JSON round trip; fmt.Sprint reads both the same.
func scopeText(ft schema.FieldType, v any) string {
	if ft == schema.Decimal {
		// DECIMAL(19,4) compares numerically on both dialects, while
		// Postgres hands the value back as "1.5000" and SQLite as a
		// float64 1.5: the exact rational is the one spelling. It carries
		// a prefix no raw value can match, or a scope written "3/2", which
		// the coercion refuses and so stays as sent, would equal 1.5.
		if r, ok := decimalRat(v); ok {
			return "\x00" + r.RatString()
		}
	}
	if s, ok := v.(string); ok {
		v = filter.TypedBind(ft, s)
	}
	switch x := v.(type) {
	case time.Time:
		if ft == schema.Timestamp {
			return schema.FormatTimestamp(x)
		}
	case int64:
		if ft == schema.Bool {
			return strconv.FormatBool(x != 0)
		}
	case float64:
		if ft == schema.Bool {
			return strconv.FormatBool(x != 0)
		}
	}
	return fmt.Sprint(v)
}

// decimalRat reads a Decimal value as a rational. A float64 goes through
// its shortest text first, so the REAL 1.1 SQLite returns is 11/10, as
// the scope's "1.1" is, not the binary fraction nearest it.
func decimalRat(v any) (*big.Rat, bool) {
	switch x := v.(type) {
	case string:
		if _, err := filter.CoerceValue("", schema.Decimal, x); err != nil {
			return nil, false
		}
		return new(big.Rat).SetString(x)
	case float64:
		// NaN and ±Inf format as text SetString refuses.
		return new(big.Rat).SetString(strconv.FormatFloat(x, 'g', -1, 64))
	case int64:
		return new(big.Rat).SetInt64(x), true
	}
	return nil, false
}
