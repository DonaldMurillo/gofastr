package filter

import (
	"maps"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/fuzzy"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// MaxINListEntries bounds the values one ?field_in= key expands to.
const MaxINListEntries = 1000

// SplitINValues comma-splits every occurrence of an _in key into one list.
func SplitINValues(values []string) []string {
	parts, _ := SplitINValuesBounded(values, math.MaxInt)
	return parts
}

// SplitINValuesBounded is SplitINValues building at most max+1 entries;
// total is the full count.
func SplitINValuesBounded(values []string, max int) (parts []string, total int) {
	n := 0
	for _, v := range values {
		n += strings.Count(v, ",") + 1
	}
	if n <= max {
		out := make([]string, 0, n)
		for _, v := range values {
			out = append(out, strings.Split(v, ",")...)
		}
		return out, n
	}
	out := make([]string, 0, max+1)
outer:
	for _, v := range values {
		for {
			if len(out) > max {
				break outer
			}
			if i := strings.IndexByte(v, ','); i >= 0 {
				out = append(out, v[:i])
				v = v[i+1:]
			} else {
				out = append(out, v)
				break
			}
		}
	}
	return out, n
}

const maxSortFields = 16

var likeEscapeReplacer = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// EscapeLikePattern escapes v's LIKE metacharacters and wraps it in %…%.
func EscapeLikePattern(v string) string {
	return "%" + likeEscapeReplacer.Replace(v) + "%"
}

// LikeEscapeSuffix must follow a placeholder bound to EscapeLikePattern.
const LikeEscapeSuffix = ` ESCAPE '\'`

// FilterOp represents a comparison operator for query filtering.
type FilterOp string

const (
	OpEq   FilterOp = "eq"
	OpNe   FilterOp = "ne"
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
	typed any
	inKey string
}

// Key returns the query key the filter was parsed from, or "".
func (f ParsedFilter) Key() string { return f.inKey }

// InList returns f tagged with the query key whose IN list it belongs to.
func (f ParsedFilter) InList(key string) ParsedFilter {
	f.inKey = key
	return f
}

// SameINList reports whether g continues f's IN list: same field and key.
func (f ParsedFilter) SameINList(g ParsedFilter) bool {
	return f.Op == OpIn && g.Op == OpIn && f.Field == g.Field && f.inKey == g.inKey
}

// BindValue returns the coerced value when f carries one, else Value.
func (f ParsedFilter) BindValue() any {
	if f.typed != nil {
		return f.typed
	}
	return f.Value
}

// Typed returns f with its value coerced for type t, or CoerceValue's error.
func (f ParsedFilter) Typed(t schema.FieldType) (ParsedFilter, error) {
	return f.typedAs(f.Field, t)
}

func (f ParsedFilter) typedAs(sent string, t schema.FieldType) (ParsedFilter, error) {
	f.typed = nil
	if f.Op == OpLike {
		if !textsafe.Storable(f.Value) {
			return f, valueTypeError(sent, t)
		}
		return f, nil
	}
	v, err := CoerceValue(sent, t, f.Value)
	if err != nil {
		return f, err
	}
	if s, isString := v.(string); !isString || s != f.Value {
		f.typed = v
	}
	return f, nil
}

// Coerced is Typed that keeps the raw value instead of erroring.
func (f ParsedFilter) Coerced(t schema.FieldType) ParsedFilter {
	if typed, err := f.Typed(t); err == nil {
		return typed
	}
	f.typed = nil
	return f
}

// ParsedSort represents sort direction for a field.
type ParsedSort struct {
	Field string
	Desc  bool
	// PinNulls spells the null position (query.OrderPinNulls).
	PinNulls bool
}

var reservedListParams = map[string]bool{
	"sort": true, "page": true, "limit": true, "per_page": true,
	"offset": true, "cursor": true, "direction": true, "where": true,
	"fields": true, "include": true, "trashed": true, "stream": true,
	"q": true,
}

// ListControlParams returns a sorted copy of the reserved list control keys.
func ListControlParams() []string {
	return slices.Sorted(maps.Keys(reservedListParams))
}

type fieldIndex struct {
	allow   map[string]bool
	noQuery map[string]bool
	alias   map[string]string
	colType map[string]schema.FieldType
}

func newFieldIndex(fields []schema.Field) *fieldIndex {
	idx := &fieldIndex{
		allow:   make(map[string]bool, len(fields)),
		alias:   make(map[string]string, len(fields)),
		colType: make(map[string]schema.FieldType, len(fields)),
	}
	for _, f := range fields {
		if f.Hidden {
			continue
		}
		if f.NoQuery {
			if idx.noQuery == nil {
				idx.noQuery = make(map[string]bool, len(fields))
			}
			idx.noQuery[f.Name] = true
			if f.WireName != "" && f.WireName != f.Name {
				idx.noQuery[f.WireName] = true
			}
			continue
		}
		idx.allow[f.Name] = true
		idx.colType[f.Name] = f.Type
		if f.WireName != "" && f.WireName != f.Name {
			idx.allow[f.WireName] = true
			idx.alias[f.WireName] = f.Name
		}
	}
	return idx
}

type filterOpts struct {
	lenient bool
	allowed map[string]bool
}

// FilterOption tunes ParseFilters behavior.
type FilterOption func(*filterOpts)

// Lenient drops an unknown top-level filter key instead of refusing it.
func Lenient() FilterOption { return func(o *filterOpts) { o.lenient = true } }

// Allow names extra non-field query keys that strict parsing skips.
func Allow(keys ...string) FilterOption {
	return func(o *filterOpts) {
		if o.allowed == nil {
			o.allowed = make(map[string]bool, len(keys))
		}
		for _, k := range keys {
			o.allowed[k] = true
		}
	}
}

// FilterSuffixOp pairs an operator suffix such as "_gt" with its FilterOp.
type FilterSuffixOp struct {
	Suffix string
	Op     FilterOp
}

// FilterSuffixes is the suffix table for every operator but equality.
var FilterSuffixes = [...]FilterSuffixOp{
	{"_gte", OpGte},
	{"_lte", OpLte},
	{"_gt", OpGt},
	{"_lt", OpLt},
	{"_like", OpLike},
	{"_ne", OpNe},
	{"_in", OpIn},
}

// ParseFilters reads ?field=, ?field_<op>= and ?field[op]= keys against
// fields, skipping list controls and dotted relation keys; see ReadKey.
func ParseFilters(r *http.Request, fields []schema.Field, opts ...FilterOption) ([]ParsedFilter, error) {
	q, err := ParseQueryStrict(r.URL.RawQuery)
	if err != nil {
		return nil, err
	}
	return ParseFiltersValues(q, fields, opts...)
}

// ParseQueryStrict is url.ParseQuery that errors (filter.query_undecodable)
// instead of dropping an undecodable pair.
func ParseQueryStrict(rawQuery string) (url.Values, error) {
	q, err := url.ParseQuery(rawQuery)
	if err != nil {
		return nil, &i18n.Error{Reason: reasonQueryUndecodable, Err: err}
	}
	return q, nil
}

// ParseFiltersValues is ParseFilters over an already-parsed query.
func ParseFiltersValues(q url.Values, fields []schema.Field, opts ...FilterOption) ([]ParsedFilter, error) {
	var o filterOpts
	for _, opt := range opts {
		opt(&o)
	}
	idx := newFieldIndex(fields)
	named := func(n string) bool { return idx.allow[n] || idx.noQuery[n] }

	var filters []ParsedFilter
	unknown := ""
	blocked := ""
	malformed, malformedErr := "", error(nil)
	overCapField := ""
	overCapCount := 0
	mistyped, mistypedErr := "", error(nil)
	recordMistyped := func(key string, err error) {
		if mistyped == "" || key < mistyped {
			mistyped, mistypedErr = key, err
		}
	}
	add := func(key, sent, col string, op FilterOp, raw string) {
		if err := CheckOpType(sent, op, idx.colType[col]); err != nil {
			recordMistyped(key, err)
			return
		}
		pf, err := ParsedFilter{Field: col, Op: op, Value: raw}.typedAs(sent, idx.colType[col])
		if err != nil {
			recordMistyped(key, err)
			return
		}
		pf.inKey = key
		filters = append(filters, pf)
	}
	repeated, repeatedCount := "", 0
	single := func(key string, values []string) (string, bool) {
		if len(values) > 1 {
			if repeated == "" || key < repeated {
				repeated, repeatedCount = key, len(values)
			}
			return "", false
		}
		return values[0], true
	}

	for _, key := range slices.Sorted(maps.Keys(q)) {
		values := q[key]
		if len(values) == 0 {
			continue
		}
		if strings.Contains(key, ".") {
			continue
		}
		name, op, ok, keyErr := ReadKey(key, named)
		if ok {
			if keyErr != nil {
				if malformed == "" || key < malformed {
					malformed, malformedErr = key, keyErr
				}
				continue
			}
			if idx.noQuery[name] {
				if !o.lenient && (blocked == "" || name < blocked) {
					blocked = name
				}
				continue
			}
			col := name
			if _, isColumn := idx.colType[col]; !isColumn {
				col = idx.alias[col]
			}
			if op != OpEq {
				if err := CheckOpType(name, op, idx.colType[col]); err != nil {
					if !o.lenient {
						recordMistyped(key, err)
					}
					continue
				}
			}
			if op == OpIn {
				parts, total := SplitINValuesBounded(values, MaxINListEntries)
				if total > MaxINListEntries {
					if overCapField == "" || col < overCapField {
						overCapField = col
						overCapCount = total
					}
				} else {
					for _, p := range parts {
						add(key, name, col, OpIn, p)
					}
				}
			} else if v, ok := single(key, values); ok {
				add(key, name, col, op, v)
			}
			continue
		}
		if reservedListParams[key] || o.allowed[key] {
			continue
		}
		if !o.lenient && (unknown == "" || key < unknown) {
			unknown = key
		}
	}

	if blocked != "" {
		return nil, notQueryableError(blocked, reasonNotFilterable)
	}
	if unknown != "" {
		return nil, unknownFilterError(unknown, slices.Collect(maps.Keys(idx.colType)))
	}
	if malformedErr != nil {
		return nil, malformedErr
	}
	if repeated != "" {
		return nil, RepeatedFilterError(repeated, repeatedCount)
	}
	if mistypedErr != nil {
		return nil, mistypedErr
	}
	if overCapField != "" {
		return nil, i18n.NewError(reasonInListOverCap, map[string]any{"field": strconv.Quote(overCapField), "count": overCapCount, "max": MaxINListEntries})
	}
	return filters, nil
}

// RepeatedFilterError refuses a scalar filter key sent count times.
func RepeatedFilterError(key string, count int) error {
	return i18n.NewError(reasonRepeatedFilter, map[string]any{"key": strconv.Quote(key), "count": count})
}

func notQueryableError(field string, reason i18n.Phrase) error {
	return i18n.NewError(reason, map[string]any{"field": strconv.Quote(field)})
}

func unknownFilterError(key string, fieldNames []string) error {
	if suggestion := nearestField(key, fieldNames); suggestion != "" {
		return i18n.NewError(reasonUnknownFilterHint, map[string]any{"key": strconv.Quote(key), "suggestion": strconv.Quote(suggestion)})
	}
	return i18n.NewError(reasonUnknownFilter, map[string]any{"key": strconv.Quote(key)})
}

func nearestField(key string, fieldNames []string) string {
	base := key
	if name, _, ok := splitBracket(key); ok {
		base = name
	} else if cut, _, ok := cutSuffix(key); ok {
		base = cut
	}
	const maxSuggestionKeyLen = 64
	if len(base) > maxSuggestionKeyLen {
		return ""
	}
	best, bestDist, ties := "", 1<<30, 0
	maxDist := 2
	if len(base) <= 4 {
		maxDist = 1
	}
	for _, name := range fieldNames {
		d := fuzzy.Levenshtein(base, name)
		if d < bestDist {
			best, bestDist, ties = name, d, 1
		} else if d == bestDist {
			ties++
		}
	}
	if best == "" || bestDist > maxDist || ties > 1 {
		return ""
	}
	return best
}

// ParseSort reads ?sort=field and ?sort=-field, comma-separated or
// repeated; it errors on a field it may not sort on or past 16 fields.
func ParseSort(r *http.Request, fields []schema.Field) ([]ParsedSort, error) {
	q, err := ParseQueryStrict(r.URL.RawQuery)
	if err != nil {
		return nil, err
	}
	return ParseSortValues(q, fields)
}

// ParseSortValues is ParseSort over an already-parsed query.
func ParseSortValues(q url.Values, fields []schema.Field) ([]ParsedSort, error) {
	sortValues := q["sort"]
	if len(sortValues) == 0 {
		return nil, nil
	}
	idx := newFieldIndex(fields)
	n := 0
	for _, v := range sortValues {
		n += strings.Count(v, ",") + 1
	}
	if n > maxSortFields {
		return nil, i18n.NewError(reasonTooManySorts, map[string]any{"count": n, "max": maxSortFields})
	}
	sortParams := make([]string, 0, n)
	for _, v := range sortValues {
		sortParams = append(sortParams, strings.Split(v, ",")...)
	}

	var sorts []ParsedSort
	for _, s := range sortParams {
		if s == "" {
			continue
		}
		for i := 0; i < len(s); i++ {
			if s[i] < 0x20 || s[i] == 0x7f {
				return nil, i18n.NewError(reasonSortControlBytes, map[string]any{"sort": strconv.Quote(s)})
			}
		}
		desc := false
		field := s
		if strings.HasPrefix(s, "-") {
			desc = true
			field = s[1:]
		}
		if idx.noQuery[field] {
			return nil, notQueryableError(field, reasonNotSortable)
		}
		if !idx.allow[field] {
			return nil, i18n.NewError(reasonInvalidSortField, map[string]any{"field": strconv.Quote(field)})
		}
		if col, ok := idx.alias[field]; ok {
			field = col
		}
		sorts = append(sorts, ParsedSort{Field: field, Desc: desc})
	}
	return sorts, nil
}

func inClause(field string, n, start int) string {
	ph := make([]string, n)
	for i := range ph {
		ph[i] = "$" + strconv.Itoa(start+i+1)
	}
	return query.QuoteKeyword(field) + " IN (" + strings.Join(ph, ",") + ")"
}

// ApplyToCountQuery applies parsed filters to a count builder.
func ApplyToCountQuery(cb *query.CountBuilder, filters []ParsedFilter) {
	applyFilters(cb, filters)
}

// ApplyToQuery applies parsed filters to a query builder.
func ApplyToQuery(qb *query.QueryBuilder, filters []ParsedFilter) {
	applyFilters(qb, filters)
}

func applyFilters[B interface{ Where(string, ...any) B }](b B, filters []ParsedFilter) {
	for i := 0; i < len(filters); i++ {
		f := filters[i]
		switch f.Op {
		case OpEq, OpNe, OpGt, OpLt, OpGte, OpLte:
			b.Where(query.QuoteKeyword(f.Field)+" "+sqlOp(f.Op)+" $1", f.BindValue())
		case OpLike:
			b.Where(LikeClause(f.Field, 1, f.Value))
		case OpIn:
			vals, n := collectInRun(filters, i)
			b.Where(inClause(f.Field, n, 0), vals...)
			i += n - 1
		}
	}
}

func collectInRun(filters []ParsedFilter, start int) (values []any, n int) {
	for j := start; j < len(filters); j++ {
		if !filters[start].SameINList(filters[j]) {
			break
		}
		values = append(values, filters[j].BindValue())
		n++
	}
	return values, n
}

// ApplySortToQuery applies parsed sorts to a query builder.
func ApplySortToQuery(qb *query.QueryBuilder, sorts []ParsedSort) {
	for _, s := range sorts {
		dir := "ASC"
		if s.Desc {
			dir = "DESC"
		}
		if s.PinNulls {
			qb.OrderPinNulls(s.Field, dir)
			continue
		}
		qb.Order(s.Field, dir)
	}
}
