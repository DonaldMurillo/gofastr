package dsl

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/gofastr/gofastr/core/query"
	"github.com/gofastr/gofastr/core/schema"
	"github.com/gofastr/gofastr/framework/entity"
)

// DSLQuery is the parsed representation of a GoFastr query DSL string.
type DSLQuery struct {
	Entity   string
	Filters  []DSLFilter
	Includes []string
	Orders   []DSLOrder
	Limit    int
	After    string
}

// DSLFilter is one where() predicate.
type DSLFilter struct {
	Field    string
	Operator string
	Value    string
}

// DSLOrder is one order() clause.
type DSLOrder struct {
	Field     string
	Direction string
}

// ParseDSL parses strings like:
//
//	Post.where(status="published").include(author).order(created_at DESC).limit(10)
func ParseDSL(input string) (DSLQuery, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return DSLQuery{}, fmt.Errorf("dsl: query is empty")
	}
	ent, rest, _ := strings.Cut(input, ".")
	if ent == "" {
		return DSLQuery{}, fmt.Errorf("dsl: entity is required")
	}
	out := DSLQuery{Entity: ent}
	if rest == "" {
		return out, nil
	}

	for rest != "" {
		name, args, tail, err := nextDSLCall(rest)
		if err != nil {
			return DSLQuery{}, err
		}
		switch name {
		case "where":
			filters, err := parseDSLFilters(args)
			if err != nil {
				return DSLQuery{}, err
			}
			out.Filters = append(out.Filters, filters...)
		case "include":
			out.Includes = append(out.Includes, splitDSLArgs(args)...)
		case "order":
			order, err := parseDSLOrder(args)
			if err != nil {
				return DSLQuery{}, err
			}
			out.Orders = append(out.Orders, order)
		case "limit":
			n, err := strconv.Atoi(strings.TrimSpace(args))
			if err != nil || n < 1 {
				return DSLQuery{}, fmt.Errorf("dsl: limit must be a positive integer")
			}
			out.Limit = n
		case "after":
			out.After = trimDSLValue(args)
		default:
			return DSLQuery{}, fmt.Errorf("dsl: unknown call %q", name)
		}
		rest = strings.TrimPrefix(tail, ".")
	}
	return out, nil
}

// BuildDSLQuery validates a DSL query against the app registry and returns a
// core query builder. Includes are validated; callers can resolve eager loading
// separately until relationship joins are fully generated.
func BuildDSLQuery(registry entity.Registry, input string) (*query.QueryBuilder, error) {
	parsed, err := ParseDSL(input)
	if err != nil {
		return nil, err
	}
	ent, err := registry.Get(parsed.Entity)
	if err != nil {
		return nil, err
	}
	entitySchema := ent.Schema()
	qb := query.Select(entitySchema.Names()...).From(ent.GetTable())

	for _, include := range parsed.Includes {
		if !hasRelation(ent, include) {
			return nil, fmt.Errorf("dsl: relation %q not found on %s", include, parsed.Entity)
		}
	}
	for _, filter := range parsed.Filters {
		field, ok := entitySchema.FieldByName(filter.Field)
		if !ok {
			return nil, fmt.Errorf("dsl: field %q not found on %s", filter.Field, parsed.Entity)
		}
		condition, args, err := dslCondition(field, filter.Operator, filter.Value)
		if err != nil {
			return nil, err
		}
		qb.Where(condition, args...)
	}
	for _, order := range parsed.Orders {
		if _, ok := entitySchema.FieldByName(order.Field); !ok {
			return nil, fmt.Errorf("dsl: field %q not found on %s", order.Field, parsed.Entity)
		}
		qb.Order(order.Field, order.Direction)
	}
	if parsed.Limit > 0 {
		qb.Limit(parsed.Limit)
	}
	return qb, nil
}

func nextDSLCall(input string) (name, args, tail string, err error) {
	open := strings.IndexByte(input, '(')
	if open <= 0 {
		return "", "", "", fmt.Errorf("dsl: expected call in %q", input)
	}
	name = strings.TrimSpace(input[:open])
	depth := 0
	inQuote := rune(0)
	for i, r := range input[open:] {
		pos := open + i
		if inQuote != 0 {
			if r == inQuote {
				inQuote = 0
			}
			continue
		}
		if r == '"' || r == '\'' {
			inQuote = r
			continue
		}
		switch r {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return name, input[open+1 : pos], input[pos+1:], nil
			}
		}
	}
	return "", "", "", fmt.Errorf("dsl: unclosed call %q", name)
}

func parseDSLFilters(args string) ([]DSLFilter, error) {
	parts := splitDSLArgs(args)
	filters := make([]DSLFilter, 0, len(parts))
	for _, part := range parts {
		filter, err := parseDSLFilter(part)
		if err != nil {
			return nil, err
		}
		filters = append(filters, filter)
	}
	return filters, nil
}

func parseDSLFilter(input string) (DSLFilter, error) {
	ops := []string{" contains ", " in ", ">=", "<=", "!=", "=", ">", "<"}
	for _, op := range ops {
		if idx := strings.Index(input, op); idx >= 0 {
			field := strings.TrimSpace(input[:idx])
			value := strings.TrimSpace(input[idx+len(op):])
			if field == "" || value == "" {
				return DSLFilter{}, fmt.Errorf("dsl: invalid filter %q", input)
			}
			return DSLFilter{Field: field, Operator: strings.TrimSpace(op), Value: trimDSLValue(value)}, nil
		}
	}
	return DSLFilter{}, fmt.Errorf("dsl: invalid filter %q", input)
}

func parseDSLOrder(input string) (DSLOrder, error) {
	parts := strings.Fields(input)
	if len(parts) == 0 || len(parts) > 2 {
		return DSLOrder{}, fmt.Errorf("dsl: invalid order %q", input)
	}
	dir := "ASC"
	if len(parts) == 2 {
		dir = strings.ToUpper(parts[1])
	}
	if dir != "ASC" && dir != "DESC" {
		return DSLOrder{}, fmt.Errorf("dsl: invalid order direction %q", dir)
	}
	return DSLOrder{Field: parts[0], Direction: dir}, nil
}

func splitDSLArgs(args string) []string {
	var out []string
	start := 0
	inQuote := rune(0)
	depth := 0
	for i, r := range args {
		if inQuote != 0 {
			if r == inQuote {
				inQuote = 0
			}
			continue
		}
		if r == '"' || r == '\'' {
			inQuote = r
			continue
		}
		if r == '(' || r == '[' {
			depth++
		}
		if r == ')' || r == ']' {
			depth--
		}
		if r == ',' && depth == 0 {
			if part := strings.TrimSpace(args[start:i]); part != "" {
				out = append(out, part)
			}
			start = i + 1
		}
	}
	if part := strings.TrimSpace(args[start:]); part != "" {
		out = append(out, part)
	}
	return out
}

func trimDSLValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			return value[1 : len(value)-1]
		}
	}
	return value
}

func dslCondition(field schema.Field, op, raw string) (string, []any, error) {
	value := dslTypedValue(field, raw)
	switch op {
	case "=":
		return field.Name + " = $1", []any{value}, nil
	case "!=":
		return field.Name + " != $1", []any{value}, nil
	case ">", "<", ">=", "<=":
		return field.Name + " " + op + " $1", []any{value}, nil
	case "contains":
		return field.Name + " LIKE $1 ESCAPE '\\'", []any{"%" + escapeLikePattern(raw) + "%"}, nil
	case "in":
		values := splitDSLArgs(strings.Trim(raw, "[]"))
		if len(values) == 0 {
			return "", nil, fmt.Errorf("dsl: in operator requires at least one value")
		}
		placeholders := make([]string, len(values))
		args := make([]any, len(values))
		for i, item := range values {
			placeholders[i] = fmt.Sprintf("$%d", i+1)
			args[i] = dslTypedValue(field, trimDSLValue(item))
		}
		return field.Name + " IN (" + strings.Join(placeholders, ", ") + ")", args, nil
	default:
		return "", nil, fmt.Errorf("dsl: unsupported operator %q", op)
	}
}

// escapeLikePattern escapes the SQL LIKE wildcards (% _) and the escape char
// itself so user-supplied input matches literally rather than as a pattern.
// Used with `LIKE ... ESCAPE '\\'`.
func escapeLikePattern(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value)
}

func dslTypedValue(field schema.Field, value string) any {
	switch field.Type {
	case schema.Int:
		if n, err := strconv.Atoi(value); err == nil {
			return n
		}
	case schema.Float, schema.Decimal:
		if n, err := strconv.ParseFloat(value, 64); err == nil {
			return n
		}
	case schema.Bool:
		if b, err := strconv.ParseBool(value); err == nil {
			return b
		}
	}
	return value
}

func hasRelation(ent *entity.Entity, name string) bool {
	for _, relation := range ent.Config.Relations {
		if relation.Name == name {
			return true
		}
	}
	for _, field := range ent.GetFields() {
		if field.Type == schema.Relation && relationNameFromField(field.Name) == name {
			return true
		}
	}
	return false
}

func relationNameFromField(name string) string {
	name = strings.TrimSuffix(name, "_id")
	var out []rune
	upperNext := false
	for _, r := range name {
		if r == '_' || r == '-' {
			upperNext = true
			continue
		}
		if upperNext {
			out = append(out, unicode.ToUpper(r))
			upperNext = false
			continue
		}
		out = append(out, r)
	}
	return string(out)
}
