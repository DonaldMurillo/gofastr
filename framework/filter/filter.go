package filter

import (
	"net/http"
	"strings"

	"github.com/gofastr/gofastr/core/query"
	"github.com/gofastr/gofastr/core/schema"
)

// FilterOp represents a comparison operator for query filtering.
type FilterOp string

const (
	OpEq   FilterOp = "eq"
	OpGt   FilterOp = "gt"
	OpLt   FilterOp = "lt"
	OpGte  FilterOp = "gte"
	OpLte  FilterOp = "lte"
	OpLike FilterOp = "like"
	OpIn   FilterOp = "in"
)

// ParsedFilter represents a single parsed filter from query parameters.
type ParsedFilter struct {
	Field string
	Op    FilterOp
	Value string
}

// ParsedSort represents sort direction for a field.
type ParsedSort struct {
	Field string
	Desc  bool
}

// ParseFilters extracts filters from query parameters based on entity fields.
// Supported patterns:
//
//	?field=value        → equals
//	?field_gt=value     → greater than
//	?field_lt=value     → less than
//	?field_gte=value    → greater than or equal
//	?field_lte=value    → less than or equal
//	?field_like=value   → LIKE (contains)
//	?field_in=v1,v2,v3  → IN
//
// Only fields present in the schema are accepted.
func ParseFilters(r *http.Request, fields []schema.Field) ([]ParsedFilter, error) {
	fieldSet := make(map[string]bool, len(fields))
	for _, f := range fields {
		fieldSet[f.Name] = true
	}

	q := r.URL.Query()
	var filters []ParsedFilter

	suffixes := []struct {
		suffix string
		op     FilterOp
	}{
		{"_gte", OpGte},
		{"_lte", OpLte},
		{"_gt", OpGt},
		{"_lt", OpLt},
		{"_like", OpLike},
		{"_in", OpIn},
	}

	// Track which query keys we've consumed so plain field=value
	// doesn't also match a field that was handled by a suffix.
	consumed := make(map[string]bool)

	for key, values := range q {
		if len(values) == 0 || key == "sort" || key == "page" || key == "limit" || key == "offset" || key == "cursor" {
			continue
		}

		matched := false
		for _, s := range suffixes {
			if strings.HasSuffix(key, s.suffix) {
				fieldName := strings.TrimSuffix(key, s.suffix)
				if !fieldSet[fieldName] {
					continue
				}
				consumed[fieldName] = true
				if s.op == OpIn {
					parts := strings.Split(values[0], ",")
					for _, p := range parts {
						filters = append(filters, ParsedFilter{Field: fieldName, Op: OpIn, Value: p})
					}
				} else {
					filters = append(filters, ParsedFilter{Field: fieldName, Op: s.op, Value: values[0]})
				}
				matched = true
				break
			}
		}
		if matched {
			continue
		}

		// Plain field=value → equals
		if fieldSet[key] && !consumed[key] {
			filters = append(filters, ParsedFilter{Field: key, Op: OpEq, Value: values[0]})
		}
	}

	return filters, nil
}

// ParseSort extracts sort information from query parameters.
// Supported: ?sort=field (ascending), ?sort=-field (descending)
func ParseSort(r *http.Request, fields []schema.Field) []ParsedSort {
	fieldSet := make(map[string]bool, len(fields))
	for _, f := range fields {
		fieldSet[f.Name] = true
	}

	sortParams := r.URL.Query()["sort"]
	if len(sortParams) == 0 {
		return nil
	}

	var sorts []ParsedSort
	for _, s := range sortParams {
		if s == "" {
			continue
		}
		desc := false
		field := s
		if strings.HasPrefix(s, "-") {
			desc = true
			field = s[1:]
		}
		if fieldSet[field] {
			sorts = append(sorts, ParsedSort{Field: field, Desc: desc})
		}
	}
	return sorts
}

// applyFiltersToCountQuery applies parsed filters to a count builder.
func ApplyToCountQuery(cb *query.CountBuilder, filters []ParsedFilter) {
	for _, f := range filters {
		switch f.Op {
		case OpEq:
			cb.Where(f.Field+" = $1", f.Value)
		case OpGt:
			cb.Where(f.Field+" > $1", f.Value)
		case OpLt:
			cb.Where(f.Field+" < $1", f.Value)
		case OpGte:
			cb.Where(f.Field+" >= $1", f.Value)
		case OpLte:
			cb.Where(f.Field+" <= $1", f.Value)
		case OpLike:
			cb.Where(f.Field+" LIKE $1", "%"+f.Value+"%")
		case OpIn:
			cb.Where(f.Field+" = $1", f.Value)
		}
	}
}

// applyFiltersToQuery applies parsed filters to a query builder.
func ApplyToQuery(qb *query.QueryBuilder, filters []ParsedFilter) {
	for _, f := range filters {
		switch f.Op {
		case OpEq:
			qb.Where(f.Field+" = $1", f.Value)
		case OpGt:
			qb.Where(f.Field+" > $1", f.Value)
		case OpLt:
			qb.Where(f.Field+" < $1", f.Value)
		case OpGte:
			qb.Where(f.Field+" >= $1", f.Value)
		case OpLte:
			qb.Where(f.Field+" <= $1", f.Value)
		case OpLike:
			qb.Where(f.Field+" LIKE $1", "%"+f.Value+"%")
		case OpIn:
			qb.Where(f.Field+" = $1", f.Value)
		}
	}
}

// applySortToQuery applies parsed sorts to a query builder.
func ApplySortToQuery(qb *query.QueryBuilder, sorts []ParsedSort) {
	for _, s := range sorts {
		dir := "ASC"
		if s.Desc {
			dir = "DESC"
		}
		qb.Order(s.Field, dir)
	}
}
