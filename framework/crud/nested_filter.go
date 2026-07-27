package crud

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// safeIdentifierRE constrains nested-filter field names to a SQL-safe
// identifier shape: letter or underscore start, letters/digits/underscore
// continuation. Anything containing whitespace, quotes, semicolons,
// parentheses, comment markers, or operators is rejected outright.
// Field names come from query-string keys (?author.name OR 1=1 -- = foo)
// and must NEVER be embedded into SQL verbatim.
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
// the caller can map to 400 — silent ignoring would mask client typos.
func parseNestedFilters(r *http.Request, ent *entity.Entity, registry entity.Registry) ([]nestedFilter, error) {
	return parseNestedFiltersValues(r.URL.Query(), ent, registry)
}

func parseNestedFiltersValues(q url.Values, ent *entity.Entity, registry entity.Registry) ([]nestedFilter, error) {
	relsByName := map[string]entity.Relation{}
	for _, rel := range ent.Config.Relations {
		relsByName[rel.Name] = rel
	}

	// filter.FilterSuffixes is the canonical operator-suffix table — reuse
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
			if strings.HasSuffix(fieldRaw, s.Suffix) {
				fieldName = strings.TrimSuffix(fieldRaw, s.Suffix)
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
		// A Hidden column is treated as NOT declared — the identical error to
		// a nonexistent field, so the response can't distinguish hidden from
		// absent. Otherwise a nested predicate (?author.password_hash_like=…)
		// would resurrect exactly the value-disclosure oracle the flat-filter
		// Hidden exclusion blocks, just one relation hop away.
		//
		// An unresolvable target is a REFUSAL, not a skip. This used to read
		// "if registry != nil { if err == nil { … } }", so a relation pointing
		// at a real table that isn't a registered entity — auth_users is the
		// documented case — dropped the whole check and let the caller name
		// any column of it in an EXISTS predicate. isSafeIdentifier gates the
		// shape of the name, not its membership, so `?author.password_hash_
		// like=$2a$` came back 200 with a row set that varies by the stored
		// value. parseIncludeTree refuses the same shape for the same reason
		// (include.go); this is its sibling and now matches it.
		target, err := nestedFilterTarget(registry, rel)
		if err != nil {
			return nil, fmt.Errorf("nested filter %q: %w", key, err)
		}
		known, blocked := false, false
		for _, f := range target.GetFields() {
			if f.Name == fieldName {
				known = !f.Hidden
				blocked = known && f.NoQuery
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
			// unmatchable — a single related row can't equal every value — so
			// `?author.name_in=a,b` silently returned nothing. One IN matches
			// the top-level _in semantics.
			out = append(out, nestedFilter{Relation: rel, Field: fieldName, Op: op, Values: strings.Split(values[0], ",")})
		} else {
			out = append(out, nestedFilter{Relation: rel, Field: fieldName, Op: op, Value: values[0]})
		}
	}
	return out, nil
}

// nestedFilterTarget resolves the entity a nested filter predicates on, or
// returns the error that refuses the filter.
//
// Both callers need the target's schema to run the Hidden/NoQuery/declared
// checks, and there is no safe way to proceed without it: the field name is
// interpolated into an EXISTS subquery, so "we couldn't check" means "we
// filtered on an arbitrary column of a table nobody vouched for". The
// messages name the missing registration, because that is what the operator
// has to fix.
func nestedFilterTarget(registry entity.Registry, rel entity.Relation) (*entity.Entity, error) {
	if registry == nil {
		return nil, fmt.Errorf(
			"nested filters require an entity registry: set CrudHandler.Registry (framework apps do this automatically)")
	}
	target, err := registry.Get(rel.Entity)
	if err != nil {
		return nil, fmt.Errorf(
			"relation %q targets entity %q, which is not registered — register it, or filter on the parent instead",
			rel.Name, rel.Entity)
	}
	return target, nil
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
		// Unresolvable target refuses, exactly as on the HTTP path — see
		// nestedFilterTarget. Skipping the check here let a typed caller
		// predicate on any column of an unregistered table.
		target, err := nestedFilterTarget(registry, rel)
		if err != nil {
			return nil, fmt.Errorf("nested filter %q.%q: %w", spec.Relation, spec.Field, err)
		}
		known, blocked := false, false
		for _, f := range target.GetFields() {
			if f.Name == spec.Field {
				// A Hidden target column is treated as not-declared —
				// the same value-disclosure-oracle rejection the HTTP
				// path applies in parseNestedFilters. Without this, a
				// typed caller passing a partially user-influenced
				// field name rebuilds the oracle one relation hop away.
				// NoQuery is blocked too, but named: it is visible in
				// responses, so hiding its existence buys nothing.
				known = !f.Hidden
				blocked = known && f.NoQuery
				break
			}
		}
		if blocked {
			return nil, fmt.Errorf("nested filter %q.%q: field cannot be filtered", spec.Relation, spec.Field)
		}
		if !known {
			return nil, fmt.Errorf("nested filter %q.%q: field not declared on %q", spec.Relation, spec.Field, rel.Entity)
		}
		nf := nestedFilter{Relation: rel, Field: spec.Field, Op: spec.Op}
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
// BelongsTo / HasOne too — same SQL pattern, no per-relation special-casing.
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
// Renumbering happens inside QueryBuilder.Build — the args are passed
// through carry semantics that make $N adjustment correct downstream.
//
// The field name on the target relation comes from a URL query key
// (?author.name=...) and is interpolated into the SQL directly — there
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
	col := nf.Field
	if !isSafeIdentifier(col) {
		// "1 = 0" is an unconditionally-false predicate that lets the
		// outer query still build but matches nothing. Better than
		// returning an error here — buildExistsSubquery has no error
		// channel and the parse layer normally catches unsafe names;
		// this is the last-line defence.
		return "1 = 0", nil
	}
	// Build the predicate on the target column: a single `col OP $1`, or a
	// coalesced `col IN ($1,$2,…)` for OpIn. Placeholders are local $N; the
	// QueryBuilder renumbers them by the running offset when it composes the
	// fragment, so multiple placeholders in one fragment are fine.
	var predicate string
	var args []any
	if nf.Op == filter.OpIn {
		if len(nf.Values) == 0 {
			return "1 = 0", nil
		}
		ph := make([]string, len(nf.Values))
		for i, v := range nf.Values {
			ph[i] = fmt.Sprintf("$%d", i+1)
			args = append(args, v)
		}
		predicate = fmt.Sprintf("%s.%s IN (%s)", rel.Entity, col, strings.Join(ph, ","))
	} else if nf.Op == filter.OpLike {
		// One operator, one meaning: `_like` is a literal substring at
		// every depth. Nested filters used to pass the caller's value
		// through as a raw LIKE pattern while the top level escaped and
		// wrapped it, so `?author.name_like=100%` prefix-matched instead
		// of finding "100% cotton" — and a bare `%` matched every row.
		predicate = fmt.Sprintf("%s.%s LIKE $1"+filter.LikeEscapeSuffix, rel.Entity, col)
		args = []any{filter.EscapeLikePattern(nf.Value)}
	} else {
		predicate = fmt.Sprintf("%s.%s %s $1", rel.Entity, col, opToSQL(nf.Op))
		args = []any{nf.Value}
	}

	switch rel.Type {
	case entity.RelManyToOne:
		// posts.author_id → users.id
		return fmt.Sprintf(
			"EXISTS (SELECT 1 FROM %s WHERE %s.id = %s.%s AND %s)",
			rel.Entity, rel.Entity, parentTable, rel.ForeignKey, predicate,
		), args
	case entity.RelHasOne, entity.RelHasMany:
		// target.fk = parent.pk
		return fmt.Sprintf(
			"EXISTS (SELECT 1 FROM %s WHERE %s.%s = %s.%s AND %s)",
			rel.Entity, rel.Entity, rel.ForeignKey, parentTable, parentPK, predicate,
		), args
	case entity.RelManyToMany:
		// parent → pivot → target
		return fmt.Sprintf(
			"EXISTS (SELECT 1 FROM %s JOIN %s ON %s.id = %s.%s WHERE %s.%s = %s.%s AND %s)",
			rel.Entity, rel.Through,
			rel.Entity, rel.Through, rel.ForeignKeyTarget,
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
