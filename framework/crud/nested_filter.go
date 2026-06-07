package crud

import (
	"fmt"
	"net/http"
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
	relsByName := map[string]entity.Relation{}
	for _, rel := range ent.Config.Relations {
		relsByName[rel.Name] = rel
	}

	suffixes := []struct {
		suffix string
		op     filter.FilterOp
	}{
		{"_gte", filter.OpGte},
		{"_lte", filter.OpLte},
		{"_gt", filter.OpGt},
		{"_lt", filter.OpLt},
		{"_like", filter.OpLike},
		{"_in", filter.OpIn},
	}

	var out []nestedFilter
	for key, values := range r.URL.Query() {
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
		for _, s := range suffixes {
			if strings.HasSuffix(fieldRaw, s.suffix) {
				fieldName = strings.TrimSuffix(fieldRaw, s.suffix)
				op = s.op
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
		if registry != nil {
			if target, err := registry.Get(rel.Entity); err == nil {
				known := false
				for _, f := range target.GetFields() {
					if f.Name == fieldName {
						known = true
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
		// "contains" semantics, matching the top-level _like: escape the
		// caller's LIKE metacharacters (% _ \) and add ESCAPE so a value
		// like "50%" is matched literally, not as a wildcard probe.
		predicate = fmt.Sprintf("%s.%s LIKE $1 ESCAPE '\\'", rel.Entity, col)
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
