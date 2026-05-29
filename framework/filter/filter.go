package filter

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
)

// maxINListEntries bounds the number of values a single ?field_in=…
// parameter can expand to. Generous for legitimate use (most DBs cap
// IN-list at a few thousand parameters) but small enough that an
// adversarial 10K-element list can't drive memory or statement-cache
// growth.
const maxINListEntries = 1000

// maxSortFields bounds the number of ORDER BY clauses a single request
// can generate. Mirrors maxINListEntries: without it, a repeated
// allow-listed ?sort=title (N copies) produces N "ORDER BY title"
// fragments, inflating SQL text, burning statement-parse CPU, and
// polluting the statement cache from one small request. 16 is far more
// sort keys than any legitimate UI needs.
const maxSortFields = 16

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
					// Cap the IN list. An attacker can otherwise post a
					// 10K-element ?id_in=a,a,a,… string and force the
					// query builder to expand a parameter list that
					// blows DB statement-cache or buffer limits. 1 000
					// is generous for legitimate use and short of any
					// driver's parameter limit.
					if len(parts) > maxINListEntries {
						parts = parts[:maxINListEntries]
					}
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
// Supported: ?sort=field (ascending), ?sort=-field (descending).
//
// Hidden fields are excluded from the allow-list: sorting by a hidden
// column reveals row ordering by a value the caller can't read, which
// is an information-disclosure path. Unknown fields fail closed with a
// 400-shaped error rather than being silently ignored — silent drop
// turns probe attempts into "the API works the same with or without
// this param" oracles that mask broken client code.
func ParseSort(r *http.Request, fields []schema.Field) ([]ParsedSort, error) {
	allowed := make(map[string]bool, len(fields))
	for _, f := range fields {
		if f.Hidden {
			continue
		}
		allowed[f.Name] = true
	}

	sortParams := r.URL.Query()["sort"]
	if len(sortParams) == 0 {
		return nil, nil
	}

	// Bound the number of sort clauses. A repeated allow-listed
	// ?sort=title would otherwise produce one ORDER BY fragment per
	// occurrence, letting a single small request inflate the generated
	// SQL and pollute the statement cache. Fail closed rather than
	// silently truncate, mirroring the unknown-field policy above.
	if len(sortParams) > maxSortFields {
		return nil, fmt.Errorf("too many sort fields: %d (max %d)", len(sortParams), maxSortFields)
	}

	var sorts []ParsedSort
	for _, s := range sortParams {
		if s == "" {
			continue
		}
		// Reject control bytes outright — they have no business in a
		// SQL identifier, and silently dropping them masks broken or
		// adversarial clients.
		for i := 0; i < len(s); i++ {
			if s[i] < 0x20 || s[i] == 0x7f {
				return nil, fmt.Errorf("invalid sort %q: control bytes not allowed", s)
			}
		}
		desc := false
		field := s
		if strings.HasPrefix(s, "-") {
			desc = true
			field = s[1:]
		}
		if !allowed[field] {
			return nil, fmt.Errorf("invalid sort field %q", field)
		}
		sorts = append(sorts, ParsedSort{Field: field, Desc: desc})
	}
	return sorts, nil
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
