package crud

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// safeIdentifierRE constrains nested-filter field names to a SQL-safe
// identifier shape: letter or underscore start, letters/digits/underscore
// continuation. Anything containing whitespace, quotes, semicolons,
// parentheses, comment markers, or operators is rejected outright.
// Field names come from query-string keys (?author.name OR 1=1 -- = foo)
// and must NEVER be embedded into SQL verbatim.
//
// Deliberately NOT core/query.identRe: that regex additionally allows
// dot-separated schema.table paths, while a nested-filter FIELD half
// must be a single segment (multi-level paths are rejected by design
// above), so the tighter single-segment shape is load-bearing here.
// The two regexes differ on purpose; unifying them would loosen this
// allow-list.
var safeIdentifierRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// isSafeIdentifier reports whether s is a plain SQL identifier (letters,
// digits, underscores, leading non-digit).
func isSafeIdentifier(s string) bool {
	return safeIdentifierRE.MatchString(s)
}

// nestedFilter is one parsed `?author.name=alice` style predicate.
type nestedFilter struct {
	Relation entity.Relation
	Field    string
	Op       filter.FilterOp
	Value    string   // single-value ops (eq/gt/like/…)
	Values   []string // OpIn: the full value set, emitted as one IN (...)
	// isBool marks a filter on a Bool-typed target column so
	// buildExistsSubquery binds a Go bool for true/false spellings,
	// a raw string binds TEXT against SQLite's INTEGER storage and
	// matches nothing (same coercion as filter.ParseFiltersValues).
	isBool bool
	// softDelete marks a target entity that hides trashed rows on every other
	// read surface, so the EXISTS clause must hide them too.
	softDelete bool
	// scopes are the target's row-scope predicates — owner, tenant, and the
	// target's Exposure.ReadScope — that the EXISTS subquery must carry so
	// it counts only rows the caller could already read one at a time
	// through the target's own list route. They are attached by
	// scopeNestedFiltersForCaller, which is the only thing that makes a
	// scoped target safe to filter across; see its doc comment. ParsedFilter
	// (not a local eq-only shape) so the read-scope operators render through
	// renderReadScope like every other sink.
	scopes []filter.ParsedFilter
	// table is the RESOLVED target's table name. Relation.Entity is the
	// registry KEY (the entity name), and the two differ whenever a host
	// declares Name != Table or registers a versioned entity. Every check this
	// file performs, declared fields, Hidden/NoQuery, soft delete, the
	// owner/tenant refusal, is made against the resolved target, so the SQL
	// has to run against that same target's table or the validation describes
	// one table while the query reads another. The eager path documents the
	// identical contract (see eager.go, "The SELECT targets the entity's
	// TABLE, not Relation.Entity").
	table string
}

// parseNestedFilters extracts dotted-path query params and resolves their
// relation references against the entity's declared relations. Only
// single-level nesting is supported today (`?author.name=alice`); deeper
// paths like `?author.team.name=x` are rejected for now.
//
// Suffixes (_gt/_gte/_lt/_lte/_like/_in) mirror ParseFilters semantics, but
// the suffix applies to the FIELD half, not the relation half:
//
//	?author.name_like=al%        ok
//	?author_like.name=al         not supported
//
// Unknown relations and unknown fields on the target return an error so
// the caller can map to 400, silent ignoring would mask client typos.
func parseNestedFilters(r *http.Request, ent *entity.Entity, registry entity.Registry) ([]nestedFilter, error) {
	return parseNestedFiltersValues(r.URL.Query(), ent, registry)
}

func parseNestedFiltersValues(q url.Values, ent *entity.Entity, registry entity.Registry) ([]nestedFilter, error) {
	relsByName := map[string]entity.Relation{}
	for _, rel := range ent.Config.Relations {
		relsByName[rel.Name] = rel
	}

	// filter.FilterSuffixes is the canonical operator-suffix table, reuse
	// it instead of rebuilding a local literal per call. Order is the same
	// (longer suffixes first) so ?author.name_gte=v matches _gte not _gt.

	var out []nestedFilter
	for key, values := range q {
		if !strings.Contains(key, ".") || len(values) == 0 {
			continue
		}
		parts := strings.SplitN(key, ".", 2)
		relName, fieldRaw := parts[0], parts[1]
		if strings.Contains(fieldRaw, ".") {
			return nil, fmt.Errorf("nested filter %q: multi-level paths not supported (yet)", key)
		}
		rel, ok := relsByName[relName]
		if !ok {
			return nil, fmt.Errorf("nested filter %q: unknown relation %q", key, relName)
		}

		fieldName := fieldRaw
		op := filter.OpEq
		for _, s := range filter.FilterSuffixes {
			if before, ok0 := strings.CutSuffix(fieldRaw, s.Suffix); ok0 {
				fieldName = before
				op = s.Op
				break
			}
		}

		// Refuse field names that aren't plain SQL identifiers. The
		// downstream buildExistsSubquery interpolates this directly
		// into the SQL; without this check a query like
		// `?author.name OR 1=1 --=foo` becomes a tautology.
		if !isSafeIdentifier(fieldName) {
			return nil, fmt.Errorf("nested filter %q: unsafe field name", key)
		}

		// Validate the field against the target entity's schema.
		//
		// A Hidden column is treated as NOT declared, the identical error to
		// a nonexistent field, so the response can't distinguish hidden from
		// absent. Otherwise a nested predicate (?author.password_hash_like=…)
		// would resurrect exactly the value-disclosure oracle the flat-filter
		// Hidden exclusion blocks, just one relation hop away.
		//
		// FAIL CLOSED on a resolution error, and resolve against the SOURCE's
		// version. This block used to be wrapped in
		// `if registry != nil { if err == nil { … } }`, which skipped every
		// check on two independent paths: resolution fails precisely when a
		// name has several versions, so two versions of "users" disabled the
		// Hidden check; and a relation pointing at a real table that no entity
		// registers, auth_users is the documented case, dropped it too.
		// isSafeIdentifier gates the SHAPE of a name, not its membership, so
		// either way ?author.password_hash_like=$2a$ reached SQL as a
		// value-disclosure oracle. ResolveTarget also errors on a nil registry,
		// so there is nothing left to guard: no schema, no filter.
		target, err := entity.ResolveTarget(registry, ent, rel.Entity)
		if err != nil {
			return nil, fmt.Errorf("nested filter %q: cannot resolve relation target %q: %w", key, rel.Entity, err)
		}
		// Match the column name OR the field's wire key. A client is told the
		// field is called "content"; ?author.content=x must work for the same
		// reason ?content=x does on the flat path. Hidden and NoQuery both win
		// under BOTH names, resolving an alias past a refusal would make the
		// wire key a way around the guard.
		known, blocked, isBool := false, false, false
		for _, f := range target.GetFields() {
			if f.Name == fieldName || (f.WireName != "" && f.WireName == fieldName) {
				known = !f.Hidden
				blocked = known && f.NoQuery
				if known {
					fieldName = f.Name // rewrite to the column: this reaches SQL
					isBool = f.Type == schema.Bool
				}
				break
			}
		}
		// A NoQuery column is in the response, so it can be named
		// rather than folded into the not-declared rejection.
		if blocked {
			return nil, fmt.Errorf("nested filter %q: field %q cannot be filtered", key, fieldName)
		}
		if !known {
			return nil, fmt.Errorf("nested filter %q: field %q not declared on %q", key, fieldName, rel.Entity)
		}

		if op == filter.OpIn {
			// Coalesce into ONE filter emitting `col IN (...)`. Splitting into
			// separate AND-ed EXISTS made a to-one relation (BelongsTo/HasOne)
			// unmatchable, a single related row can't equal every value, so
			// `?author.name_in=a,b` silently returned nothing. One IN matches
			// the top-level _in semantics, including the union across
			// repeated keys (filter.SplitINValuesBounded) and the entry cap
			// the flat path enforces, same cap, same error shape, so the
			// nested surface can't drive uncapped placeholders per request.
			vals, total := filter.SplitINValuesBounded(values, filter.MaxINListEntries)
			if total > filter.MaxINListEntries {
				return nil, fmt.Errorf("nested filter %q: in-list on %q has %d entries (max %d)",
					key, fieldName, total, filter.MaxINListEntries)
			}
			out = append(out, nestedFilter{Relation: rel, Field: fieldName, Op: op, Values: vals, isBool: isBool, softDelete: target.Config.Scope.SoftDelete, table: resolvedTable(target, rel)})
		} else {
			out = append(out, nestedFilter{Relation: rel, Field: fieldName, Op: op, Value: values[0], isBool: isBool, softDelete: target.Config.Scope.SoftDelete, table: resolvedTable(target, rel)})
		}
	}
	return out, nil
}

// NestedFilter is the in-process (ListOptions) equivalent of a single
// `?author.name=alice` HTTP query param. Typed repositories construct these
// directly instead of synthesising a URL. Relation names the declared
// relation on the parent entity; Field is the column on the target entity;
// Op/Value mirror ParsedFilter semantics. For Op==OpIn, set Values (Value is
// ignored).
type NestedFilter struct {
	Relation string
	Field    string
	Op       filter.FilterOp
	Value    string
	Values   []string
}

// resolveNestedFilters maps in-process NestedFilter specs onto the internal
// nestedFilter slice consumed by applyNestedFilters, running the same
// relation/field validation and identifier-safety checks the HTTP path
// applies in parseNestedFilters. Unknown relations, unknown fields, and
// unsafe identifiers return an error so typed callers see the same 400-class
// failures.
//
// It deliberately does NOT run scopeNestedFiltersForCaller, so a spec resolved
// here carries no owner or tenant narrowing. The asymmetry with the HTTP path
// is intentional and worth stating, because it looks like an omission:
// Hidden/NoQuery are enforced here because those are properties of the DATA — a
// masked column stays masked no matter who asks — while the read posture is a
// question about the CALLER, and an in-process caller is server code acting on
// its own authority, the same carve-out ApplyIncludes makes via realRequestKey. The consequence is real and belongs to the host: a typed
// repo that forwards a user-influenced NestedFilter rebuilds the count oracle
// the HTTP gate exists to close, exactly as forwarding a user-influenced
// Filters entry would. Validate the spec before passing it, or route the
// request through the HTTP surface.
func resolveNestedFilters(ent *entity.Entity, registry entity.Registry, specs []NestedFilter) ([]nestedFilter, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	relsByName := map[string]entity.Relation{}
	for _, rel := range ent.Config.Relations {
		relsByName[rel.Name] = rel
	}
	out := make([]nestedFilter, 0, len(specs))
	for _, spec := range specs {
		rel, ok := relsByName[spec.Relation]
		if !ok {
			return nil, fmt.Errorf("nested filter: unknown relation %q", spec.Relation)
		}
		if !isSafeIdentifier(spec.Field) {
			return nil, fmt.Errorf("nested filter %q.%q: unsafe field name", spec.Relation, spec.Field)
		}
		// Unresolvable target refuses, exactly as on the HTTP path, and
		// resolves against the source's version for the same reason.
		// Skipping the check here let a typed caller predicate on any column
		// of an unregistered table, or of whichever version Get happened to
		// return.
		target, err := entity.ResolveTarget(registry, ent, rel.Entity)
		if err != nil {
			return nil, fmt.Errorf("nested filter %q.%q: cannot resolve relation target %q: %w",
				spec.Relation, spec.Field, rel.Entity, err)
		}
		field := spec.Field
		known, blocked, isBool := false, false, false
		for _, f := range target.GetFields() {
			if f.Name == field || (f.WireName != "" && f.WireName == field) {
				// A Hidden target column is treated as not-declared,
				// the same value-disclosure-oracle rejection the HTTP
				// path applies in parseNestedFilters. Without this, a
				// typed caller passing a partially user-influenced
				// field name rebuilds the oracle one relation hop away.
				// NoQuery is blocked too, but named: it is visible in
				// responses, so hiding its existence buys nothing.
				known = !f.Hidden
				blocked = known && f.NoQuery
				if known {
					field = f.Name // rewrite to the column: this reaches SQL
					isBool = f.Type == schema.Bool
				}
				break
			}
		}
		if blocked {
			return nil, fmt.Errorf("nested filter %q.%q: field cannot be filtered", spec.Relation, spec.Field)
		}
		if !known {
			return nil, fmt.Errorf("nested filter %q.%q: field not declared on %q", spec.Relation, spec.Field, rel.Entity)
		}
		nf := nestedFilter{Relation: rel, Field: field, Op: spec.Op, isBool: isBool, softDelete: target.Config.Scope.SoftDelete, table: resolvedTable(target, rel)}
		if spec.Op == filter.OpIn {
			nf.Values = spec.Values
		} else {
			nf.Value = spec.Value
		}
		out = append(out, nf)
	}
	return out, nil
}

// applyNestedFilters invokes addWhere once per nestedFilter with an EXISTS
// subquery. EXISTS avoids the row duplication that a plain JOIN would
// introduce for HasMany / ManyToMany relations and works uniformly across
// BelongsTo / HasOne too, same SQL pattern, no per-relation special-casing.
//
// addWhere mirrors the Where signature shared by QueryBuilder and
// CountBuilder so a single call site can wire the same filter chain into
// both the data and count queries.
func applyNestedFilters(addWhere func(sql string, args ...any), parentTable, parentPK string, filters []nestedFilter) {
	for _, nf := range filters {
		sql, args := buildExistsSubquery(parentTable, parentPK, nf)
		addWhere(sql, args...)
	}
}

// buildExistsSubquery returns the WHERE fragment for one nested filter.
// Renumbering happens inside QueryBuilder.Build, the args are passed
// through carry semantics that make $N adjustment correct downstream.
//
// The field name on the target relation comes from a URL query key
// (?author.name=...) and is interpolated into the SQL directly, there
// is no parameter placeholder for an identifier. We refuse anything
// that doesn't look like a plain `[A-Za-z_][A-Za-z0-9_]*` identifier so
// payloads like `name OR 1=1 --` can't smuggle SQL fragments through
// parseNestedFilters when the registry can't validate the field.
//
// parentTable / parentPK / rel.Entity / rel.ForeignKey / rel.Through /
// rel.LocalKey / rel.ForeignKeyTarget all originate from server-defined
// metadata, not request input, so they don't need the same gate.
func buildExistsSubquery(parentTable, parentPK string, nf nestedFilter) (string, []any) {
	rel := nf.Relation
	// relTable, never rel.Entity. See nestedFilter.table.
	relTable := nf.table
	if relTable == "" {
		relTable = rel.Entity
	}
	col := nf.Field
	if !isSafeIdentifier(col) {
		// "1 = 0" is an unconditionally-false predicate that lets the
		// outer query still build but matches nothing. Better than
		// returning an error here, buildExistsSubquery has no error
		// channel and the parse layer normally catches unsafe names;
		// this is the last-line defence.
		return "1 = 0", nil
	}
	// Build the predicate on the target column, preceded by the caller's row
	// scopes. Placeholders are local $N; QueryBuilder.Build renumbers them by
	// the running offset when it composes the fragment.
	//
	// Renumbering is POSITIONAL BY ENCOUNTER — the first placeholder token in
	// the string becomes the first arg, whatever digit it carries — so args
	// must be appended in the order the placeholders APPEAR. The scope clauses
	// are emitted first, so their values go into args first. Getting that
	// backwards binds the caller's owner id to the field predicate and the
	// searched value to the owner column: the query returns nothing, which
	// reads as "no matching rows" rather than as a bug.
	var args []any

	// Narrow the subquery to the caller's own rows. Without this the EXISTS
	// clause counts EVERY row in the target table: it does not return them, but
	// the parent's row count moves with the guessed value, which is a count
	// oracle over any column of any other owner's or tenant's data. The
	// predicates come from scopeNestedFiltersForCaller, which reuses the same
	// builder the include and eager paths use, so a narrowed subquery counts
	// exactly the rows the target's own list route would have served.
	//
	// Owner/tenant eq predicates and the target's ReadScope (eq/neq/in/not_in)
	// all render through renderReadScope: one renderer, one meaning, the
	// same fragment shape every other sink uses.
	for _, sc := range nf.scopes {
		if !isSafeIdentifier(sc.Field) {
			// A scope column that is not a plain identifier cannot be emitted,
			// and dropping it would silently widen the subquery back to every
			// row. Match nothing instead.
			return "1 = 0", nil
		}
	}
	var scopeClause string
	if len(nf.scopes) > 0 {
		var scopeArgs []any
		scopeClause, scopeArgs = renderReadScope(nf.scopes, relTable, 1)
		if scopeClause == "" {
			// Non-empty predicates that render to nothing means the renderer
			// refused them; matching nothing is the only safe answer.
			return "1 = 0", nil
		}
		args = append(args, scopeArgs...)
	}

	var predicate string
	if nf.Op == filter.OpIn {
		if len(nf.Values) == 0 {
			return "1 = 0", nil
		}
		ph := make([]string, len(nf.Values))
		for i, v := range nf.Values {
			ph[i] = fmt.Sprintf("$%d", len(args)+1)
			args = append(args, filter.BoolBind(nf.isBool, v))
		}
		predicate = fmt.Sprintf("%s.%s IN (%s)", relTable, col, strings.Join(ph, ","))
	} else if nf.Op == filter.OpLike {
		// One operator, one meaning: `_like` is a literal substring at
		// every depth. Nested filters used to pass the caller's value
		// through as a raw LIKE pattern while the top level escaped and
		// wrapped it, so `?author.name_like=100%` prefix-matched instead of
		// finding "100% cotton" — and a bare `%` matched every row.
		predicate = fmt.Sprintf("%s.%s LIKE $%d"+filter.LikeEscapeSuffix, relTable, col, len(args)+1)
		args = append(args, filter.EscapeLikePattern(nf.Value))
	} else {
		predicate = fmt.Sprintf("%s.%s %s $%d", relTable, col, opToSQL(nf.Op), len(args)+1)
		args = append(args, filter.BoolBind(nf.isBool, nf.Value))
	}
	if scopeClause != "" {
		predicate = scopeClause + " AND " + predicate
	}

	// Every other read surface hides soft-deleted rows, the routes via
	// ApplySoftDeleteFilter, the eager loaders via their softDeleteFilter
	// argument. This subquery did not, so `?rel.field=` counted trashed rows
	// and became a value oracle over data that GET /api/<entity>/{id} answers
	// 404 for. No placeholder needed, so it composes with the renumbering.
	if nf.softDelete {
		predicate = fmt.Sprintf("%s.deleted_at IS NULL AND %s", relTable, predicate)
	}

	switch rel.Type {
	case entity.RelManyToOne:
		// posts.author_id → users.id
		return fmt.Sprintf(
			"EXISTS (SELECT 1 FROM %s WHERE %s.id = %s.%s AND %s)",
			relTable, relTable, parentTable, rel.ForeignKey, predicate,
		), args
	case entity.RelHasOne, entity.RelHasMany:
		// target.fk = parent.pk
		return fmt.Sprintf(
			"EXISTS (SELECT 1 FROM %s WHERE %s.%s = %s.%s AND %s)",
			relTable, relTable, rel.ForeignKey, parentTable, parentPK, predicate,
		), args
	case entity.RelManyToMany:
		// parent → pivot → target
		return fmt.Sprintf(
			"EXISTS (SELECT 1 FROM %s JOIN %s ON %s.id = %s.%s WHERE %s.%s = %s.%s AND %s)",
			relTable, rel.Through,
			relTable, rel.Through, rel.ForeignKeyTarget,
			rel.Through, rel.LocalKey, parentTable, parentPK,
			predicate,
		), args
	}
	return "1 = 0", nil
}

// opToSQL maps a FilterOp to its SQL operator.
func opToSQL(op filter.FilterOp) string {
	switch op {
	case filter.OpEq:
		return "="
	case filter.OpGt:
		return ">"
	case filter.OpGte:
		return ">="
	case filter.OpLt:
		return "<"
	case filter.OpLte:
		return "<="
	case filter.OpLike:
		return "LIKE"
	case filter.OpIn:
		// OpIn is handled directly in buildExistsSubquery as a coalesced
		// IN (...); this branch is unreachable for nested filters. Kept for
		// total mapping completeness.
		return "="
	}
	return "="
}

// scopeNestedFiltersForCaller decides whether the caller may filter across each
// relation, and — for the ones they may — narrows the EXISTS subquery to the
// rows they are allowed to see. The two halves are one function on purpose: the
// refusal it no longer issues is replaced by the predicate it attaches, so a
// path that skips this call gets neither, and the count oracle it exists to
// close is wide open.
//
// `?author.email=jane@example.com` does not return the related row, so it is
// not a disclosure the way `?include=author` was — but it is an oracle: the
// parent's row count changes with the guessed value, so a caller can confirm
// any value in a column the entity's own route refuses to serve. Filtering
// across a relation is a use of that relation's data, so the same posture
// governs it.
//
// Two things are decided here.
//
// May the caller read the target at all? CanReadScoped, not the narrower
// canReadEntityGate: the include path can afford the narrow gate because it
// scopes rows per node, and this one now does the same — but CanReadScoped is
// still the correct predicate for "may this caller read this entity", and it is
// what answers the question when the target is not row-scoped at all.
//
// Which of the target's rows may they count? Every one the target's own list
// route would serve them, and no others. The predicates come from
// eagerScopeFilters — the same builder the include and eager loaders use — so
// the three surfaces cannot drift into three different answers. It fails closed
// by construction: a caller with no owner in context is narrowed to the empty
// value, which matches no real row, so a guessed value confirms nothing. A
// caller holding an explicit cross-owner or cross-tenant grant gets no
// predicate for that axis, because they already read every row of the target
// through its own routes and narrowing would remove a capability without
// protecting anything.
//
// This replaces a blanket refusal of every owner-scoped or multi-tenant target,
// which closed the oracle but also refused an owner filtering their OWN rows —
// the ordinary case, and the one `?rel.field=` is most useful for. The axes
// stay independent: a cross-owner grant narrows nothing on the tenant axis and
// vice versa, because eagerScopeFilters emits them separately.
func (ch *CrudHandler) scopeNestedFiltersForCaller(ctx context.Context, filters []nestedFilter) error {
	if len(filters) == 0 || ch.Registry == nil {
		return nil
	}
	for i := range filters {
		target, err := entity.ResolveTarget(ch.Registry, ch.Entity, filters[i].Relation.Entity)
		if err != nil {
			// Unresolvable target: refuse rather than filter against a table
			// nobody vouched for, matching the include path's stance.
			return &includeForbiddenError{Entity: filters[i].Relation.Entity}
		}
		probe := &CrudHandler{Entity: target, DB: ch.DB, Registry: ch.Registry}
		if !probe.CanReadScoped(ctx) {
			return &includeForbiddenError{Entity: target.GetName()}
		}
		var scopes []filter.ParsedFilter
		scopes = append(scopes, eagerScopeFilters(ctx, target)...)
		// The target's ReadScope narrows the same subquery: without it a
		// `?rel.field=` count is an oracle over rows the target's own route
		// refuses (the drafts), one question at a time. Same builder as
		// every other sink.
		scopes = append(scopes, readScopeFilters(ctx, target)...)
		filters[i].scopes = scopes
	}
	return nil
}
func resolvedTable(target *entity.Entity, rel entity.Relation) string {
	if target != nil && target.GetTable() != "" {
		return target.GetTable()
	}
	return rel.Entity
}
