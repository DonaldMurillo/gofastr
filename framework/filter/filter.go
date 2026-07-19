package filter

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/fuzzy"
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

// likeEscapeReplacer escapes the LIKE metacharacters (\ % _) so a _like
// filter value is matched as a literal substring rather than a pattern,
// mirroring the DSL `contains` operator. Backslash is escaped first (it
// is the ESCAPE char appended to the LIKE fragment), then the wildcards.
var likeEscapeReplacer = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// escapeLikePattern escapes v's LIKE metacharacters and wraps it in the
// leading/trailing wildcards that implement "contains". Pair it with an
// `ESCAPE '\'` clause on the LIKE fragment so the wildcards a caller
// supplies are matched literally, not interpreted as patterns.
func escapeLikePattern(v string) string {
	return "%" + likeEscapeReplacer.Replace(v) + "%"
}

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

// reservedListParams are the list-endpoint control keys that are never
// entity fields. Strict parsing skips them so a legitimate ?sort=/?page=/…
// is not rejected as an unknown filter. Keep in sync with the params the
// CRUD list handler actually reads (crud.go, pagination, projection,
// include, search, where-tree, soft-delete, streaming).
var reservedListParams = map[string]bool{
	"sort": true, "page": true, "limit": true, "per_page": true,
	"offset": true, "cursor": true, "direction": true, "where": true,
	"fields": true, "include": true, "trashed": true, "stream": true,
	"q": true,
}

// filterOpts holds the resolved options for a ParseFilters call.
type filterOpts struct {
	lenient bool
	allowed map[string]bool
}

// FilterOption tunes ParseFilters behavior.
type FilterOption func(*filterOpts)

// Lenient restores the pre-strict behavior: an unknown top-level filter key
// is silently dropped instead of returning an error. It exists as a
// migration escape hatch for apps that historically relied on unrecognized
// query params being ignored. Prefer the strict default — a dropped filter
// returns an UNFILTERED result set, which is a data-exposure and
// broken-client hazard.
func Lenient() FilterOption { return func(o *filterOpts) { o.lenient = true } }

// Allow declares extra query-param keys that are NOT entity fields but are
// legitimately consumed elsewhere on the request (a BeforeList hook, custom
// middleware). Strict parsing skips them instead of rejecting them, so a
// host keeps typo-protection for real fields without falling back to
// Lenient (which disables it entirely). Keys are matched exactly.
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

// FilterSuffixOp pairs a query-string operator suffix (e.g. "_gt") with
// its FilterOp. Exported so the CRUD layer's nested-filter parser can
// share the same canonical table (no per-call rebuild, no duplicate
// literal to drift between packages).
type FilterSuffixOp struct {
	Suffix string
	Op     FilterOp
}

// FilterSuffixes is the canonical operator-suffix table for the equality,
// comparison, LIKE, and IN operators. Order matters: longer suffixes MUST
// be tested before their shorter prefixes (e.g. `_gte` before `_gt`,
// otherwise `?score_gte=5` matches `_gt` and leaves an `e=` field-name
// fragment). The table is a pure function of the operator set, so it is
// hoisted to a package var — ParseFilters/ParseSort no longer rebuild it
// per call.
var FilterSuffixes = [...]FilterSuffixOp{
	{"_gte", OpGte},
	{"_lte", OpLte},
	{"_gt", OpGt},
	{"_lt", OpLt},
	{"_like", OpLike},
	{"_in", OpIn},
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
// Only fields present in the schema are accepted. Hidden fields are
// excluded from the allow-list (mirroring ParseSort): building a WHERE
// predicate on a column the caller can't read turns row-count/result
// changes into a value-disclosure oracle — an attacker could probe a
// Hidden column (e.g. a password hash) via ?password_hash_like=… and
// exfiltrate it prefix by prefix. A Hidden field name is treated as
// an unknown filter param and never produces a ParsedFilter.
//
// STRICT by default: an unknown top-level filter key (a typo like
// ?stauts=active, or a suffixed op on a non-field) returns a structured
// error rather than being silently dropped. Dropping it would return an
// UNFILTERED 200 — a broken client reads the whole table and an attacker's
// probe looks identical to the real query. Reserved list controls (sort,
// page, cursor, …) and nested relation filters (dotted keys like
// author.name, validated separately by parseNestedFilters) are skipped, not
// rejected. Pass [Lenient] to restore the old drop-silently behavior.
//
// This is a thin wrapper around ParseFiltersValues that parses the request
// URL once. Callers that already have a url.Values (e.g. the CRUD List
// handler, which parses once and threads the result through every helper)
// should call ParseFiltersValues directly to avoid the re-parse.
func ParseFilters(r *http.Request, fields []schema.Field, opts ...FilterOption) ([]ParsedFilter, error) {
	return ParseFiltersValues(r.URL.Query(), fields, opts...)
}

// ParseFiltersValues is the allocation-conscious variant of ParseFilters:
// it accepts an already-parsed url.Values so a caller that parsed
// ?field=value once can reuse it across filter/sort/paginate/include
// helpers without re-paying url.URL.Query (which re-parses RawQuery and
// allocates a fresh url.Values on every call). Behaviour is identical to
// ParseFilters for the same underlying query string.
func ParseFiltersValues(q url.Values, fields []schema.Field, opts ...FilterOption) ([]ParsedFilter, error) {
	var o filterOpts
	for _, opt := range opts {
		opt(&o)
	}

	fieldSet := make(map[string]bool, len(fields))
	names := make([]string, 0, len(fields))
	for _, f := range fields {
		if f.Hidden {
			continue
		}
		fieldSet[f.Name] = true
		names = append(names, f.Name)
	}

	// FilterSuffixes is package-level — see its declaration above.

	var filters []ParsedFilter

	// Track which query keys we've consumed so plain field=value
	// doesn't also match a field that was handled by a suffix.
	consumed := make(map[string]bool)

	// unknown records the first rejected key when strict — surfaced as a
	// single structured error after the loop (query-map iteration order is
	// non-deterministic, so report deterministically: the lexically
	// smallest bad key, with a suggestion).
	unknown := ""

	for key, values := range q {
		if len(values) == 0 {
			continue
		}
		// Nested relation filters (author.name=…) are parsed and validated
		// separately by parseNestedFilters (which enforces the same
		// schema/Hidden allow-list) — skip dotted keys entirely here.
		if strings.Contains(key, ".") {
			continue
		}

		// A KNOWN field is matched FIRST — before the reserved-control skip —
		// so a column whose name collides with a control word (e.g. a field
		// named "stream" or "q") is still filtered rather than silently
		// swallowed, which would return an unfiltered result set.
		matched := false
		for _, s := range FilterSuffixes {
			if strings.HasSuffix(key, s.Suffix) {
				fieldName := strings.TrimSuffix(key, s.Suffix)
				if !fieldSet[fieldName] {
					continue
				}
				consumed[fieldName] = true
				if s.Op == OpIn {
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
					filters = append(filters, ParsedFilter{Field: fieldName, Op: s.Op, Value: values[0]})
				}
				matched = true
				break
			}
		}
		if matched {
			continue
		}

		// A plain known field name. When it was already consumed by a
		// suffixed op on the same request, drop the redundant equals — but it
		// is still a KNOWN field, so it must never be reported as unknown.
		if fieldSet[key] {
			if !consumed[key] {
				filters = append(filters, ParsedFilter{Field: key, Op: OpEq, Value: values[0]})
			}
			continue
		}

		// Not a field. A reserved list control or a host-declared extra
		// param is consumed elsewhere on the request — skip it silently.
		if reservedListParams[key] || o.allowed[key] {
			continue
		}

		// Truly unrecognized. Fail closed unless the caller opted into
		// lenient mode. Record the lexically smallest so the error is
		// deterministic under randomized map iteration.
		if !o.lenient && (unknown == "" || key < unknown) {
			unknown = key
		}
	}

	if unknown != "" {
		return nil, unknownFilterError(unknown, names)
	}

	return filters, nil
}

// unknownFilterError builds the structured 400-shaped error for an
// unrecognized filter key, appending a "did you mean" suggestion when a
// field name is an unambiguous near-match. The bad key is always named so a
// generated client can surface it verbatim.
func unknownFilterError(key string, fieldNames []string) error {
	if suggestion := nearestField(key, fieldNames); suggestion != "" {
		return fmt.Errorf("unknown filter %q (did you mean %q?)", key, suggestion)
	}
	return fmt.Errorf("unknown filter %q", key)
}

// nearestField returns the single closest field name to key within a small
// edit distance, or "" when there is no close or unambiguous match. It also
// strips a known operator suffix from key first, so ?scor_gt suggests
// "score". Kept deliberately conservative — a wrong suggestion is worse than
// none.
func nearestField(key string, fieldNames []string) string {
	base := key
	for _, s := range FilterSuffixes {
		if strings.HasSuffix(base, s.Suffix) {
			base = strings.TrimSuffix(base, s.Suffix)
			break
		}
	}
	best, bestDist, ties := "", 1<<30, 0
	// Allow more slack for longer names; a 1-char typo in "status" and a
	// 2-char transposition should both resolve.
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

// ParseSort extracts sort information from query parameters.
// Supported: ?sort=field (ascending), ?sort=-field (descending).
//
// Hidden fields are excluded from the allow-list: sorting by a hidden
// column reveals row ordering by a value the caller can't read, which
// is an information-disclosure path. Unknown fields fail closed with a
// 400-shaped error rather than being silently ignored — silent drop
// turns probe attempts into "the API works the same with or without
// this param" oracles that mask broken client code.
//
// Thin wrapper around ParseSortValues; callers that already hold a
// url.Values should call ParseSortValues directly.
func ParseSort(r *http.Request, fields []schema.Field) ([]ParsedSort, error) {
	return ParseSortValues(r.URL.Query(), fields)
}

// ParseSortValues is the allocation-conscious variant of ParseSort: it
// accepts an already-parsed url.Values so the CRUD List handler can
// thread the same parsed query through every helper.
func ParseSortValues(q url.Values, fields []schema.Field) ([]ParsedSort, error) {
	allowed := make(map[string]bool, len(fields))
	for _, f := range fields {
		if f.Hidden {
			continue
		}
		allowed[f.Name] = true
	}

	sortParams := q["sort"]
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

// inClause builds an `field IN ($1,$2,…)` fragment and its argument
// slice for a run of OpIn filters on the same field. A single value still
// yields `field IN ($1)`, which is equivalent to equality. Returning a
// real set-membership predicate is what makes ?status_in=active,pending
// match the union of values instead of ANDing one equality per value
// (status = $1 AND status = $2), which no single row can satisfy.
func inClause(field string, values []string) (string, []any) {
	var sb strings.Builder
	sb.WriteString(field)
	sb.WriteString(" IN (")
	args := make([]any, len(values))
	for i, v := range values {
		if i > 0 {
			sb.WriteByte(',')
		}
		// Placeholders are renumbered by the query builder; the index
		// here only needs to be a valid $N so renumberPlaceholders
		// advances correctly.
		fmt.Fprintf(&sb, "$%d", i+1)
		args[i] = v
	}
	sb.WriteByte(')')
	return sb.String(), args
}

// applyFiltersToCountQuery applies parsed filters to a count builder.
func ApplyToCountQuery(cb *query.CountBuilder, filters []ParsedFilter) {
	for i := 0; i < len(filters); i++ {
		f := filters[i]
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
			cb.Where(f.Field+` LIKE $1 ESCAPE '\'`, escapeLikePattern(f.Value))
		case OpIn:
			vals, n := collectInRun(filters, i)
			cond, args := inClause(f.Field, vals)
			cb.Where(cond, args...)
			i += n - 1
		}
	}
}

// applyFiltersToQuery applies parsed filters to a query builder.
func ApplyToQuery(qb *query.QueryBuilder, filters []ParsedFilter) {
	for i := 0; i < len(filters); i++ {
		f := filters[i]
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
			qb.Where(f.Field+` LIKE $1 ESCAPE '\'`, escapeLikePattern(f.Value))
		case OpIn:
			vals, n := collectInRun(filters, i)
			cond, args := inClause(f.Field, vals)
			qb.Where(cond, args...)
			i += n - 1
		}
	}
}

// collectInRun gathers the contiguous run of OpIn filters on the same
// field starting at index start (ParseFilters emits one ParsedFilter per
// comma-separated value, all adjacent). It returns the collected values
// and the run length so the caller can advance past them and emit a
// single IN clause.
func collectInRun(filters []ParsedFilter, start int) (values []string, n int) {
	field := filters[start].Field
	for j := start; j < len(filters); j++ {
		if filters[j].Op != OpIn || filters[j].Field != field {
			break
		}
		values = append(values, filters[j].Value)
		n++
	}
	return values, n
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
