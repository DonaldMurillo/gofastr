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

		// Validate the field exists on the target entity (when the registry
		// has it). Without the registry we trust the field name as-is.
		//
		// A Hidden column is treated as NOT declared — the identical error to
		// a nonexistent field, so the response can't distinguish hidden from
		// absent. Otherwise a nested predicate (?author.password_hash_like=…)
		// would resurrect exactly the value-disclosure oracle the flat-filter
		// Hidden exclusion blocks, just one relation hop away.
		if registry != nil {
			if target, err := registry.Get(rel.Entity); err == nil {
				known := false
				for _, f := range target.GetFields() {
					if f.Name == fieldName {
						known = !f.Hidden
						break
					}
				}
				if !known {
					return nil, fmt.Errorf("nested filter %q: field %q not declared on %q", key, fieldName, rel.Entity)
				}
			}
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
		if registry != nil {
			if target, err := registry.Get(rel.Entity); err == nil {
				known := false
				for _, f := range target.GetFields() {
					if f.Name == spec.Field {
						// A Hidden target column is treated as not-declared —
						// the same value-disclosure-oracle rejection the HTTP
						// path applies in parseNestedFilters. Without this, a
						// typed caller passing a partially user-influenced
						// field name rebuilds the oracle one relation hop away.
						known = !f.Hidden
						break
					}
				}
				if !known {
					return nil, fmt.Errorf("nested filter %q.%q: field not declared on %q", spec.Relation, spec.Field, rel.Entity)
				}
			}
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
	} else {
		// Note: nested _like is intentionally a RAW LIKE pattern — the caller
		// supplies the wildcards (?author.name_like=A%). The value is still
		// parameterized via $1, so this is not an injection vector; the
		// wildcards are the documented API. (Top-level _like differs: it
		// treats the value as a literal substring and wraps/escapes it.)
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
