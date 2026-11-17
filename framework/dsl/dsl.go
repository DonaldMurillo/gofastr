package dsl

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

const maxDSLLimit = 10_000

var dslIdentRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

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

const maxParseCacheSize = 256

const maxDSLInputSize = 8 * 1024

var (
	parseCache   = make(map[string]DSLQuery, 64)
	parseCacheMu sync.RWMutex
)

// ParseDSL parses a query such as
//
//	Post.where(status="published").include(author).order(created_at DESC).limit(10)
//
// Results are cached by input in a bounded cache; each call returns its
// own copy.
func ParseDSL(input string) (DSLQuery, error) {
	if len(input) > maxDSLInputSize {
		return DSLQuery{}, i18n.NewError(reasonInputTooLong, map[string]any{"max": maxDSLInputSize})
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return DSLQuery{}, i18n.NewError(reasonEmptyQuery)
	}

	parseCacheMu.RLock()
	if cached, ok := parseCache[input]; ok {
		parseCacheMu.RUnlock()
		return cloneDSLQuery(cached), nil
	}
	parseCacheMu.RUnlock()

	result, err := parseDSLUncached(input)
	if err != nil {
		return DSLQuery{}, err
	}

	parseCacheMu.Lock()
	if len(parseCache) >= maxParseCacheSize {
		for k := range parseCache {
			delete(parseCache, k)
			break
		}
	}
	parseCache[input] = result
	parseCacheMu.Unlock()

	return cloneDSLQuery(result), nil
}

func cloneDSLQuery(q DSLQuery) DSLQuery {
	q.Filters = append([]DSLFilter(nil), q.Filters...)
	q.Includes = append([]string(nil), q.Includes...)
	q.Orders = append([]DSLOrder(nil), q.Orders...)
	return q
}

func parseDSLUncached(input string) (DSLQuery, error) {
	ent, rest, _ := strings.Cut(input, ".")
	if ent == "" {
		return DSLQuery{}, i18n.NewError(reasonEntityRequired)
	}
	if !dslIdentRe.MatchString(ent) {
		return DSLQuery{}, i18n.NewError(reasonInvalidEntity, map[string]any{"name": strconv.Quote(ent)})
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
			for _, f := range filters {
				if !dslIdentRe.MatchString(f.Field) {
					return DSLQuery{}, i18n.NewError(reasonInvalidFilterField, map[string]any{"name": strconv.Quote(f.Field)})
				}
			}
			out.Filters = append(out.Filters, filters...)
		case "include":
			includes, err := splitDSLArgs(args)
			if err != nil {
				return DSLQuery{}, err
			}
			for _, inc := range includes {
				name := trimDSLValue(inc)
				if !dslIdentRe.MatchString(name) {
					return DSLQuery{}, i18n.NewError(reasonInvalidInclude, map[string]any{"name": strconv.Quote(name)})
				}
				out.Includes = append(out.Includes, name)
			}
		case "order":
			order, err := parseDSLOrder(args)
			if err != nil {
				return DSLQuery{}, err
			}
			if !dslIdentRe.MatchString(order.Field) {
				return DSLQuery{}, i18n.NewError(reasonInvalidOrderField, map[string]any{"name": strconv.Quote(order.Field)})
			}
			out.Orders = append(out.Orders, order)
		case "limit":
			n, err := strconv.Atoi(strings.TrimSpace(args))
			if err != nil || n < 1 {
				return DSLQuery{}, i18n.NewError(reasonLimitNotPositive)
			}
			if n > maxDSLLimit {
				return DSLQuery{}, i18n.NewError(reasonLimitOverMax, map[string]any{"limit": n, "max": maxDSLLimit})
			}
			out.Limit = n
		case "after":
			out.After = textsafe.SanitizeControlBytes(trimDSLValue(args))
		default:
			return DSLQuery{}, i18n.NewError(reasonUnknownCall, map[string]any{"name": strconv.Quote(name)})
		}
		rest = strings.TrimPrefix(tail, ".")
	}

	if len(out.Orders) > maxSortTerms {
		return DSLQuery{}, i18n.NewError(reasonOrderTooMany, map[string]any{"count": len(out.Orders), "max": maxSortTerms})
	}
	return out, nil
}

// BuildDSLQuery validates a DSL query against registry and returns its
// query builder. It applies no row scope.
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

	visible := entitySchema.VisibleFields()
	cols := make([]string, 0, len(visible))
	for _, f := range visible {
		cols = append(cols, f.Name)
	}
	qb := query.Select(cols...).From(ent.GetTable())

	for _, include := range parsed.Includes {
		if !hasRelation(ent, include) {
			return nil, i18n.NewError(reasonRelationNotFound, map[string]any{"name": strconv.Quote(include), "entity": parsed.Entity})
		}
	}
	for _, filter := range parsed.Filters {
		field, ok := entitySchema.FieldByName(filter.Field)
		if !ok || field.Hidden {
			return nil, i18n.NewError(reasonFieldNotFound, map[string]any{"field": strconv.Quote(filter.Field), "entity": parsed.Entity})
		}
		if field.NoQuery {
			return nil, i18n.NewError(reasonFieldNotFilterable, map[string]any{"field": strconv.Quote(filter.Field), "entity": parsed.Entity})
		}
		condition, args, err := dslCondition(field, filter.Operator, filter.Value)
		if err != nil {
			return nil, err
		}
		qb.Where(condition, args...)
	}
	cursorField, after, err := dslAfter(ent, entitySchema, parsed)
	if err != nil {
		return nil, err
	}
	for _, order := range parsed.Orders {
		orderField, ok := entitySchema.FieldByName(order.Field)
		if !ok || orderField.Hidden {
			return nil, i18n.NewError(reasonFieldNotFound, map[string]any{"field": strconv.Quote(order.Field), "entity": parsed.Entity})
		}
		if orderField.NoQuery {
			return nil, i18n.NewError(reasonFieldNotSortable, map[string]any{"field": strconv.Quote(order.Field), "entity": parsed.Entity})
		}
		if cursorField != "" {
			if order.Field != cursorField || order.Direction != "ASC" {
				return nil, i18n.NewError(reasonAfterOrderMismatch, map[string]any{"field": strconv.Quote(cursorField), "order": strconv.Quote(order.Field + " " + order.Direction)})
			}
			continue
		}

		if entity.CanHoldNull(orderField, ent.KeyColumns()) {
			qb.OrderPinNulls(order.Field, order.Direction)
			continue
		}
		qb.Order(order.Field, order.Direction)
	}
	if parsed.Limit > 0 {
		qb.Limit(parsed.Limit)
	}
	if cursorField != "" {
		qb.Where(query.QuoteKeyword(cursorField)+" > $1", after)
		qb.Order(cursorField, "ASC")
	}
	return qb, nil
}

func dslAfter(ent *entity.Entity, entitySchema schema.Schema, parsed DSLQuery) (string, any, error) {
	if parsed.After != "" {
		if len(ent.Config.Pagination.CursorFields) > 0 {
			return "", nil, i18n.NewError(reasonAfterComposite, map[string]any{"entity": parsed.Entity})
		}

		keys := ent.KeyColumns()
		cursorField := ent.Config.Pagination.CursorField
		if cursorField == "" {
			if len(keys) > 1 {
				return "", nil, i18n.NewError(reasonAfterComposite, map[string]any{"entity": parsed.Entity})
			}
			cursorField = keys[0]
		}
		cf, ok := entitySchema.FieldByName(cursorField)
		if !ok || cf.Hidden {
			return "", nil, i18n.NewError(reasonAfterFieldNotFound, map[string]any{"field": strconv.Quote(cursorField), "entity": parsed.Entity})
		}
		if cf.NoQuery {
			return "", nil, i18n.NewError(reasonAfterFieldNotQueryable, map[string]any{"field": strconv.Quote(cursorField), "entity": parsed.Entity})
		}

		if !cf.Unique && (len(keys) != 1 || keys[0] != cursorField) {
			return "", nil, i18n.NewError(reasonAfterNotUnique, map[string]any{"field": strconv.Quote(cursorField), "entity": parsed.Entity})
		}

		after, err := filter.CoerceValue(cursorField, cf.Type, parsed.After)
		if err != nil {
			return "", nil, err
		}
		return cursorField, after, nil
	}
	return "", nil, nil
}

func nextDSLCall(input string) (name, args, tail string, err error) {
	open := strings.IndexByte(input, '(')
	if open <= 0 {
		return "", "", "", i18n.NewError(reasonExpectedCall, map[string]any{"input": strconv.Quote(input)})
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
	return "", "", "", i18n.NewError(reasonUnclosedCall, map[string]any{"name": strconv.Quote(name)})
}

func parseDSLFilters(args string) ([]DSLFilter, error) {
	parts, err := splitDSLArgs(args)
	if err != nil {
		return nil, err
	}
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

var dslFilterOps = []string{" contains ", " in ", ">=", "<=", "!=", "=", ">", "<"}

func parseDSLFilter(input string) (DSLFilter, error) {
	for i := range len(input) {
		for _, op := range dslFilterOps {
			if !strings.HasPrefix(input[i:], op) {
				continue
			}
			field := strings.TrimSpace(input[:i])
			value := strings.TrimSpace(input[i+len(op):])
			if field == "" || value == "" {
				return DSLFilter{}, i18n.NewError(reasonInvalidFilter, map[string]any{"input": strconv.Quote(input)})
			}
			return DSLFilter{Field: field, Operator: strings.TrimSpace(op), Value: trimDSLValue(value)}, nil
		}
	}
	return DSLFilter{}, i18n.NewError(reasonInvalidFilter, map[string]any{"input": strconv.Quote(input)})
}

func parseDSLOrder(input string) (DSLOrder, error) {
	parts := strings.Fields(input)
	if len(parts) == 0 || len(parts) > 2 {
		return DSLOrder{}, i18n.NewError(reasonInvalidOrder, map[string]any{"input": strconv.Quote(input)})
	}
	dir := "ASC"
	if len(parts) == 2 {
		switch asciicase.Lower(parts[1]) {
		case "asc":
		case "desc":
			dir = "DESC"
		default:
			return DSLOrder{}, i18n.NewError(reasonInvalidDirection, map[string]any{"dir": strconv.Quote(parts[1])})
		}
	}
	return DSLOrder{Field: parts[0], Direction: dir}, nil
}

func splitDSLArgs(args string) ([]string, error) {
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
			if depth < 0 {
				return nil, i18n.NewError(reasonUnbalancedBrackets, map[string]any{"input": strconv.Quote(args)})
			}
		}
		if r == ',' && depth == 0 {
			if part := strings.TrimSpace(args[start:i]); part != "" {
				out = append(out, part)
			}
			start = i + 1
		}
	}
	if depth != 0 {
		return nil, i18n.NewError(reasonUnbalancedBrackets, map[string]any{"input": strconv.Quote(args)})
	}
	if part := strings.TrimSpace(args[start:]); part != "" {
		out = append(out, part)
	}
	return out, nil
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

var dslOps = map[string]filter.FilterOp{
	"=": filter.OpEq, "!=": filter.OpNe,
	">": filter.OpGt, "<": filter.OpLt, ">=": filter.OpGte, "<=": filter.OpLte,
	"contains": filter.OpLike, "in": filter.OpIn,
}

func dslCondition(field schema.Field, op, raw string) (string, []any, error) {
	fop, ok := dslOps[op]
	if !ok {
		return "", nil, i18n.NewError(reasonUnsupportedOperator, map[string]any{"op": strconv.Quote(op)})
	}
	if err := filter.CheckOpType(field.Name, fop, field.Type); err != nil {
		return "", nil, err
	}
	switch op {
	case "=", "!=", ">", "<", ">=", "<=":
		value, err := filter.CoerceValue(field.Name, field.Type, raw)
		if err != nil {
			return "", nil, err
		}
		return query.QuoteKeyword(field.Name) + " " + op + " $1", []any{value}, nil
	case "contains":
		if _, err := filter.CoerceValue(field.Name, field.Type, raw); err != nil {
			return "", nil, err
		}
		clause, arg := filter.LikeClause(field.Name, 1, raw)
		return clause, []any{arg}, nil
	case "in":
		values, err := splitDSLArgs(strings.Trim(raw, "[]"))
		if err != nil {
			return "", nil, err
		}
		if len(values) == 0 {
			return "", nil, i18n.NewError(reasonInNeedsValue)
		}

		if len(values) > filter.MaxINListEntries {
			return "", nil, i18n.NewError(reasonInListOverCap, map[string]any{"field": strconv.Quote(field.Name), "count": len(values), "max": filter.MaxINListEntries})
		}
		placeholders := make([]string, len(values))
		args := make([]any, len(values))
		for i, item := range values {
			placeholders[i] = fmt.Sprintf("$%d", i+1)
			v, err := filter.CoerceValue(field.Name, field.Type, trimDSLValue(item))
			if err != nil {
				return "", nil, err
			}
			args[i] = v
		}
		return query.QuoteKeyword(field.Name) + " IN (" + strings.Join(placeholders, ", ") + ")", args, nil
	default:
		return "", nil, i18n.NewError(reasonUnsupportedOperator, map[string]any{"op": strconv.Quote(op)})
	}
}

func hasRelation(ent *entity.Entity, name string) bool {
	for _, relation := range ent.Config.Relations {
		if relation.Name == name {
			return !ent.HidesRelation(relation)
		}
	}
	for _, field := range ent.GetFields() {
		if field.Type == schema.Relation && !field.Hidden && relationNameFromField(field.Name) == name {
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
