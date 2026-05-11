package framework

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gofastr/gofastr/framework/entity"
	"github.com/gofastr/gofastr/framework/filter"
)

// nestedFilter is one parsed `?author.name=alice` style predicate.
type nestedFilter struct {
	Relation entity.Relation
	Field    string
	Op       filter.FilterOp
	Value    string
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
func parseNestedFilters(r *http.Request, ent *entity.Entity, registry *Registry) ([]nestedFilter, error) {
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
			for _, p := range strings.Split(values[0], ",") {
				out = append(out, nestedFilter{Relation: rel, Field: fieldName, Op: op, Value: p})
			}
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
func buildExistsSubquery(parentTable, parentPK string, nf nestedFilter) (string, []any) {
	rel := nf.Relation
	col := nf.Field
	predicate := opToSQL(nf.Op)
	args := []any{nf.Value}

	switch rel.Type {
	case entity.RelManyToOne:
		// posts.author_id → users.id
		sub := fmt.Sprintf(
			"EXISTS (SELECT 1 FROM %s WHERE %s.id = %s.%s AND %s.%s %s $1)",
			rel.Entity, rel.Entity, parentTable, rel.ForeignKey, rel.Entity, col, predicate,
		)
		return sub, args
	case entity.RelHasOne, entity.RelHasMany:
		// target.fk = parent.pk
		sub := fmt.Sprintf(
			"EXISTS (SELECT 1 FROM %s WHERE %s.%s = %s.%s AND %s.%s %s $1)",
			rel.Entity, rel.Entity, rel.ForeignKey, parentTable, parentPK, rel.Entity, col, predicate,
		)
		return sub, args
	case entity.RelManyToMany:
		// parent → pivot → target
		sub := fmt.Sprintf(
			"EXISTS (SELECT 1 FROM %s JOIN %s ON %s.id = %s.%s WHERE %s.%s = %s.%s AND %s.%s %s $1)",
			rel.Entity, rel.Through,
			rel.Entity, rel.Through, rel.ForeignKeyTarget,
			rel.Through, rel.LocalKey, parentTable, parentPK,
			rel.Entity, col, predicate,
		)
		return sub, args
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
		// IN is handled by parser splitting values; we still emit "=" per row.
		return "="
	}
	return "="
}
