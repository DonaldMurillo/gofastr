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
)

// maxDSLLimit caps the value the parser will accept from `limit(N)`.
// Without a hard cap a malicious or sloppy client can request an
// arbitrarily large page, turning a single query into a memory /
// CPU exhaustion vector. Callers can still apply a smaller policy
// cap on top, this is the parser's last line of defence.
const maxDSLLimit = 10_000

// dslIdentRe is the allow-list for DSL identifiers: entity names,
// include (relation) names, and where()/order() field names. SQL
// metacharacters (`;`, quotes, parens, dashes, spaces) are rejected
// at the parser so they can never reach the query builder or schema
// lookup, where a missing-field error would still leak the unsafe
// string into logs.
//
// Deliberately NOT core/query.identRe (already imported here): that
// regex additionally allows dot-separated schema.table paths, while
// DSL identifiers are single names, the tighter shape is
// load-bearing. The two differ on purpose; unifying them would loosen
// this allow-list.
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

// parseCache caches ParseDSL results. Agents often issue the same query
// template repeatedly; cached lookups hit ~50ns / 0 allocs vs ~6µs / 7
// allocs for a fresh parse.
const maxParseCacheSize = 256

// maxDSLInputSize caps the byte length of input passed to ParseDSL.
// Without a cap, a single megabyte-class payload could consume parser
// time AND get cached, amplifying its cost across the whole process.
const maxDSLInputSize = 8 * 1024

var (
	parseCache   = make(map[string]DSLQuery, 64)
	parseCacheMu sync.RWMutex
)

// ParseDSL parses strings like:
//
//	Post.where(status="published").include(author).order(created_at DESC).limit(10)
//
// Results are cached by input string. The cache is bounded to 256 entries;
// when full, a single randomly-chosen entry is evicted (Go map iteration
// is randomised, so the first key returned by `range` is effectively
// random, this is not LRU).
//
// The returned DSLQuery is caller-isolated: the cache stores the canonical
// copy and every call (hit or miss) returns a shallow copy with fresh
// Filters/Includes/Orders slices, the HooksFor posture. A value-typed
// result invites callers to adjust it in place; sharing the cached backing
// arrays would turn one caller's edit into every later caller's answer.
func ParseDSL(input string) (DSLQuery, error) {
	if len(input) > maxDSLInputSize {
		return DSLQuery{}, i18n.NewError(reasonInputTooLong, map[string]any{"max": maxDSLInputSize})
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return DSLQuery{}, i18n.NewError(reasonEmptyQuery)
	}

	// Fast path: check cache
	parseCacheMu.RLock()
	if cached, ok := parseCache[input]; ok {
		parseCacheMu.RUnlock()
		return cloneDSLQuery(cached), nil
	}
	parseCacheMu.RUnlock()

	// Slow path: parse
	result, err := parseDSLUncached(input)
	if err != nil {
		return DSLQuery{}, err
	}

	// Store in cache (evict a random entry when full). Random eviction
	// keeps the working set roughly intact for hot queries without the
	// bookkeeping cost of LRU.
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

// cloneDSLQuery copies the slice-bearing fields of a DSLQuery so the
// caller's copy never aliases the cache's backing arrays.
func cloneDSLQuery(q DSLQuery) DSLQuery {
	q.Filters = append([]DSLFilter(nil), q.Filters...)
	q.Includes = append([]string(nil), q.Includes...)
	q.Orders = append([]DSLOrder(nil), q.Orders...)
	return q
}

// parseDSLUncached does the actual parsing without caching.
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
			includes := splitDSLArgs(args)
			for _, inc := range includes {
				// include() arguments may have been quoted by the
				// caller; trimDSLValue strips the wrapping quotes so
				// the allow-list compares against the bare identifier.
				name := trimDSLValue(inc)
				if !dslIdentRe.MatchString(name) {
					return DSLQuery{}, i18n.NewError(reasonInvalidInclude, map[string]any{"name": strconv.Quote(name)})
				}
			}
			out.Includes = append(out.Includes, includes...)
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
			// Cursor opacity is enforced downstream by the cursor
			// decoder; here we only need to strip control bytes so a
			// CR/LF in an after() literal can't smuggle a forged log
			// line or break out of a re-encoded DSL string.
			out.After = textsafe.SanitizeControlBytes(trimDSLValue(args))
		default:
			return DSLQuery{}, i18n.NewError(reasonUnknownCall, map[string]any{"name": strconv.Quote(name)})
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
	// Project VisibleFields, not Names(): Names() includes Hidden columns, so
	// selecting it would hand back the password hashes the HTTP read paths
	// strip. NoQuery columns stay in the projection, they are meant to be
	// returned, just not queried on.
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
			// Hidden folds into not-found so the DSL can't distinguish a
			// hidden column from an absent one, matching ParseFilters.
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
	for _, order := range parsed.Orders {
		orderField, ok := entitySchema.FieldByName(order.Field)
		if !ok || orderField.Hidden {
			return nil, i18n.NewError(reasonFieldNotFound, map[string]any{"field": strconv.Quote(order.Field), "entity": parsed.Entity})
		}
		if orderField.NoQuery {
			return nil, i18n.NewError(reasonFieldNotSortable, map[string]any{"field": strconv.Quote(order.Field), "entity": parsed.Entity})
		}
		qb.Order(order.Field, order.Direction)
	}
	if parsed.Limit > 0 {
		qb.Limit(parsed.Limit)
	}
	// Wire after(cursor) into a forward keyset predicate. The DSL is the
	// agent-facing surface: after() carries the bare keyset value (e.g.
	// after(1024) → "rows after id 1024"), NOT the opaque base64 token the
	// HTTP cursor layer round-trips. Resolve the keyset column from the
	// entity config (CursorField, else the primary key); composite cursors
	// (CursorFields) have no single-value form in the DSL, so refuse them
	// with a clear error rather than silently paging from page 1.
	if parsed.After != "" {
		if len(ent.Config.Pagination.CursorFields) > 0 {
			return nil, i18n.NewError(reasonAfterComposite, map[string]any{"entity": parsed.Entity})
		}
		// The default cursor is the primary key; a key of more than one
		// column is a composite cursor, refused the same way.
		keys := ent.KeyColumns()
		cursorField := ent.Config.Pagination.CursorField
		if cursorField == "" {
			if len(keys) > 1 {
				return nil, i18n.NewError(reasonAfterComposite, map[string]any{"entity": parsed.Entity})
			}
			cursorField = keys[0]
		}
		cf, ok := entitySchema.FieldByName(cursorField)
		if !ok || cf.Hidden {
			return nil, i18n.NewError(reasonAfterFieldNotFound, map[string]any{"field": strconv.Quote(cursorField), "entity": parsed.Entity})
		}
		if cf.NoQuery {
			return nil, i18n.NewError(reasonAfterFieldNotQueryable, map[string]any{"field": strconv.Quote(cursorField), "entity": parsed.Entity})
		}
		// after() keysets on this one column, so rows tied on its value
		// with the one it resumes after would never be served. Only the
		// sole key or a Unique field cannot tie; anything else is refused
		// rather than paged with silent gaps.
		if !cf.Unique && (len(keys) != 1 || keys[0] != cursorField) {
			return nil, i18n.NewError(reasonAfterNotUnique, map[string]any{"field": strconv.Quote(cursorField), "entity": parsed.Entity})
		}
		// after() is a bare keyset value: of the cursor column's type, or
		// refused, the same as a filter value.
		after, err := filter.CoerceValue(cursorField, cf.Type, parsed.After)
		if err != nil {
			return nil, err
		}
		qb.Cursor(cursorField, after, "forward")
	}
	return qb, nil
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

// dslFilterOps are the where() operators. At one position the first
// that matches wins, so >= and <= are tried before > and <.
var dslFilterOps = []string{" contains ", " in ", ">=", "<=", "!=", "=", ">", "<"}

// parseDSLFilter splits one where() term at its leftmost operator. The
// field comes first, so an operator inside the value is never the split:
// title="Drive in Cinema" compares with =, not in.
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
		dir = strings.ToUpper(parts[1])
	}
	if dir != "ASC" && dir != "DESC" {
		return DSLOrder{}, i18n.NewError(reasonInvalidDirection, map[string]any{"dir": strconv.Quote(dir)})
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
	switch op {
	case "=", "!=", ">", "<", ">=", "<=":
		value, err := filter.CoerceValue(field.Name, field.Type, raw)
		if err != nil {
			return "", nil, err
		}
		return field.Name + " " + op + " $1", []any{value}, nil
	case "contains":
		// Case-insensitive on both dialects, the ?q= and ?field_like=
		// shape (filter.LikeClause). On a non-text column the substring
		// match means nothing and Postgres refuses LOWER(integer).
		if err := filter.CheckOpType(field.Name, filter.OpLike, field.Type); err != nil {
			return "", nil, err
		}
		clause, arg := filter.LikeClause(field.Name, 1, raw)
		return clause, []any{arg}, nil
	case "in":
		values := splitDSLArgs(strings.Trim(raw, "[]"))
		if len(values) == 0 {
			return "", nil, i18n.NewError(reasonInNeedsValue)
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
		return field.Name + " IN (" + strings.Join(placeholders, ", ") + ")", args, nil
	default:
		return "", nil, i18n.NewError(reasonUnsupportedOperator, map[string]any{"op": strconv.Quote(op)})
	}
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
