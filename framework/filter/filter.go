package filter

import (
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/fuzzy"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
)

// MaxINListEntries bounds the number of values a single ?field_in=…
// parameter can expand to, counting repeated occurrences of the
// parameter as one list. Generous for legitimate use (most DBs cap
// IN-lists at a few thousand parameters) but small enough that an
// adversarial 10K-element list can't drive memory or statement-cache
// growth. Exposed so sibling surfaces (nested ?rel.field_in=) enforce
// the same cap instead of growing a private one.
const MaxINListEntries = 1000

// SplitINValues expands every occurrence of a repeated _in parameter
// into one flat, comma-split value list: ?tag_in=a&tag_in=b,c yields
// [a b c]. Reading only values[0] silently narrowed the filter to the
// first key's values, a client asking for a union got a subset with no
// error. Repeated keys and comma-separated values are treated as the
// same list, so the cap (MaxINListEntries) can't be bypassed by
// splitting one huge list across several occurrences either.
func SplitINValues(values []string) []string {
	parts, _ := SplitINValuesBounded(values, math.MaxInt)
	return parts
}

// SplitINValuesBounded is SplitINValues with an allocation bound for the
// HTTP filter paths that enforce MaxINListEntries: it never materializes
// more than max+1 entries. SplitINValues built the entire list before the
// caller compared it against the cap, so one request carrying hundreds of
// thousands of commas allocated that many strings just to earn a 400,
// repeated per request. Here the total is counted first (strings.Count,
// no allocation) and the list is only built in full when it fits; an
// over-cap input yields a max+1-entry prefix plus the exact total, which
// is everything the rejection error needs. parts holds the complete list
// iff total <= max.
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
			// Check before appending so the slice never exceeds max+1
			// entries, the no-comma tail append must respect the bound
			// just like the comma-split pieces.
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

// maxSortFields bounds the number of ORDER BY clauses a single request
// can generate. Mirrors MaxINListEntries: without it, a repeated
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

// EscapeLikePattern escapes v's LIKE metacharacters and wraps it in the
// leading/trailing wildcards that implement "contains". Pair it with an
// `ESCAPE '\'` clause on the LIKE fragment so the wildcards a caller
// supplies are matched literally, not interpreted as patterns.
//
// Exported so relation-scoped filters (framework/crud's nested `?rel.f_like=`
// and `?include=rel(f_like=…)`) build the identical clause. They used to
// pass the caller's value through raw, which made the same query
// parameter mean "literal substring" at the top level and "wildcard
// pattern" one level down.
func EscapeLikePattern(v string) string {
	return "%" + likeEscapeReplacer.Replace(v) + "%"
}

// escapeLikePattern is the package-internal spelling.
func escapeLikePattern(v string) string { return EscapeLikePattern(v) }

// LikeEscapeSuffix is the SQL that must follow a LIKE placeholder for
// EscapeLikePattern's escaping to take effect.
const LikeEscapeSuffix = ` ESCAPE '\'`

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

	// typed holds the schema-coerced form of Value (booleans today) so
	// the query binder binds a real bool instead of its raw string.
	// SQLite stores Bool columns as INTEGER and its affinity rules
	// never match the TEXT 'true'/'false' spellings (only "1"/"0"
	// happened to work), while every dialect binds a Go bool correctly
	// (PG native boolean, SQLite 1/0). nil means "bind Value".
	// Unexported on purpose: callers that construct ParsedFilter
	// directly keep the legacy string-binding behavior, only
	// ParseFiltersValues knows the field's schema type.
	typed any
}

// BindValue returns what a query binder should bind for this filter:
// the schema-coerced value when the filter carries one (set by
// ParseFiltersValues or Coerced), the raw string otherwise. Every
// binder that applies a ParsedFilter to SQL must bind this, not Value,
// binding the raw string re-opens the SQLite Bool/TEXT affinity bug.
func (f ParsedFilter) BindValue() any {
	if f.typed != nil {
		return f.typed
	}
	return f.Value
}

// boolTyped coerces raw for a Bool-typed column, returning nil when the
// column is not Bool or raw is not a strconv.ParseBool spelling (the
// binder then keeps the raw string, the pre-coercion behavior).
func boolTyped(isBool bool, raw string) any {
	if !isBool {
		return nil
	}
	if b, err := strconv.ParseBool(raw); err == nil {
		return b
	}
	return nil
}

// BoolBind returns the dialect-correct bind value for raw against a
// column that may be Bool: a Go bool when isBool holds and raw parses
// as one, the raw string otherwise. For binders that carry their own
// filter shape (nested relation filters, ?where= predicates) instead of
// a ParsedFilter.
func BoolBind(isBool bool, raw string) any {
	if t := boolTyped(isBool, raw); t != nil {
		return t
	}
	return raw
}

// Coerced returns a copy of f whose bind value is schema-coerced for
// the field's type (booleans today). Callers that construct
// ParsedFilter directly, facet UIs, scoped include filters, use this
// so BindValue matches what ParseFiltersValues would have produced.
func (f ParsedFilter) Coerced(t schema.FieldType) ParsedFilter {
	f.typed = boolTyped(t == schema.Bool, f.Value)
	return f
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
// query params being ignored. Prefer the strict default, a dropped filter
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
// hoisted to a package var. ParseFilters/ParseSort no longer rebuild it
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
// changes into a value-disclosure oracle, an attacker could probe a
// Hidden column (e.g. a password hash) via ?password_hash_like=… and
// exfiltrate it prefix by prefix. A Hidden field name is treated as
// an unknown filter param and never produces a ParsedFilter.
//
// NoQuery fields are excluded too, and for the same oracle reason, a
// value masked on the way out (last-4, redacted by an AfterGet hook) is
// still recoverable a character at a time if the stored column remains
// filterable. They are rejected by name rather than as unknown: the field
// is in the response, so its existence is not a secret worth protecting.
//
// STRICT by default: an unknown top-level filter key (a typo like
// ?stauts=active, or a suffixed op on a non-field) returns a structured
// error rather than being silently dropped. Dropping it would return an
// UNFILTERED 200, a broken client reads the whole table and an attacker's
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
	// wireAlias maps a WireName override (and only the override, the
	// case-derived form is still matched via fieldSet) to the DB column
	// name, so ?writer=123 resolves to column "author_id" when the field
	// declares WireName:"writer". The ParsedFilter.Field is always the
	// column name, never the wire key.
	wireAlias := make(map[string]string, len(fields))
	names := make([]string, 0, len(fields))
	// NoQuery fields are tracked separately from the unknown-key path. They
	// are visible in responses, so naming one back to the caller discloses
	// nothing they can't already read, unlike a Hidden field, whose very
	// existence has to stay indistinguishable from "no such column".
	var noQuery map[string]bool
	// boolField records Bool-typed columns so value coercion (true/false
	// → bool) happens once, here, instead of per-dialect in SQL.
	boolField := make(map[string]bool, len(fields))
	for _, f := range fields {
		if f.Hidden {
			continue
		}
		if f.NoQuery {
			if noQuery == nil {
				noQuery = make(map[string]bool)
			}
			noQuery[f.Name] = true
			// Under the wire key as well. It is deliberately NOT added to
			// wireAlias, resolving an alias to its column happens before the
			// refusal is consulted, so registering it there would turn the
			// name clients are actually told to send into a way around the
			// guard. Tracking it here refuses it by the name they used.
			if f.WireName != "" && f.WireName != f.Name {
				noQuery[f.WireName] = true
			}
			continue
		}
		fieldSet[f.Name] = true
		names = append(names, f.Name)
		if f.Type == schema.Bool {
			boolField[f.Name] = true
		}
		if f.WireName != "" {
			wireAlias[f.WireName] = f.Name
		}
	}

	// FilterSuffixes is package-level. See its declaration above.

	var filters []ParsedFilter

	// Track which query keys we've consumed so plain field=value
	// doesn't also match a field that was handled by a suffix.
	consumed := make(map[string]bool)

	// unknown records the first rejected key when strict, surfaced as a
	// single structured error after the loop (query-map iteration order is
	// non-deterministic, so report deterministically: the lexically
	// smallest bad key, with a suggestion).
	unknown := ""

	// blocked records the first NoQuery field a caller tried to filter on,
	// chosen lexically for the same determinism reason as unknown.
	blocked := ""

	// overCapField/overCapCount record the lexically-smallest ?field_in= list
	// that exceeds MaxINListEntries, surfaced as a single deterministic error
	// after the loop for the same reason unknown/blocked are. Silent
	// truncation (parts[:cap]) narrows the predicate, rows past entry N drop
	// out of the result set without the caller knowing, so it fails closed,
	// mirroring parseScopedFilters' cap on the include path.
	overCapField := ""
	overCapCount := 0

	for key, values := range q {
		if len(values) == 0 {
			continue
		}
		// Nested relation filters (author.name=…) are parsed and validated
		// separately by parseNestedFilters (which enforces the same
		// schema/Hidden allow-list), skip dotted keys entirely here.
		if strings.Contains(key, ".") {
			continue
		}

		// A KNOWN field is matched FIRST, before the reserved-control skip,
		// so a column whose name collides with a control word (e.g. a field
		// named "stream" or "q") is still filtered rather than silently
		// swallowed, which would return an unfiltered result set.
		matched := false
		for _, s := range FilterSuffixes {
			if !strings.HasSuffix(key, s.Suffix) {
				continue
			}
			fieldName := strings.TrimSuffix(key, s.Suffix)
			// Refuse a NoQuery column BEFORE resolving an alias to it. The
			// map holds both spellings, so ?writer_like= is refused for the
			// same reason ?author_id_like= is.
			if noQuery[fieldName] {
				if !o.lenient && (blocked == "" || fieldName < blocked) {
					blocked = fieldName
				}
				matched = true
				break
			}
			// Accept both the raw column name and a WireName override;
			// resolve to the DB column for the WHERE clause.
			col := fieldName
			if !fieldSet[col] {
				if w, ok := wireAlias[col]; ok {
					col = w
				} else {
					continue
				}
			}
			consumed[col] = true
			if s.Op == OpIn {
				parts, total := SplitINValuesBounded(values, MaxINListEntries)
				if total > MaxINListEntries {
					// Fail closed: a truncated list silently narrows the
					// predicate (rows past entry N drop out of results)
					// without telling the caller. Record the lexically-
					// smallest offender for a deterministic message under
					// randomized map iteration and error after the loop.
					if overCapField == "" || col < overCapField {
						overCapField = col
						overCapCount = total
					}
				} else {
					for _, p := range parts {
						filters = append(filters, ParsedFilter{Field: col, Op: OpIn, Value: p, typed: boolTyped(boolField[col], p)})
					}
				}
			} else {
				filters = append(filters, ParsedFilter{Field: col, Op: s.Op, Value: values[0], typed: boolTyped(boolField[col], values[0])})
			}
			matched = true
			break
		}
		if matched {
			continue
		}

		// A plain known field name. When it was already consumed by a
		// suffixed op on the same request, drop the redundant equals, but it
		// is still a KNOWN field, so it must never be reported as unknown.
		if noQuery[key] {
			if !o.lenient && (blocked == "" || key < blocked) {
				blocked = key
			}
			continue
		}
		if fieldSet[key] {
			if !consumed[key] {
				filters = append(filters, ParsedFilter{Field: key, Op: OpEq, Value: values[0], typed: boolTyped(boolField[key], values[0])})
			}
			continue
		}
		// WireName override on a plain key (no suffix): resolve to column.
		if col, ok := wireAlias[key]; ok {
			if !consumed[col] {
				filters = append(filters, ParsedFilter{Field: col, Op: OpEq, Value: values[0], typed: boolTyped(boolField[col], values[0])})
			}
			continue
		}

		// Not a field. A reserved list control or a host-declared extra
		// param is consumed elsewhere on the request, skip it silently.
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

	// Reported ahead of unknown: a caller who named a real column wants to be
	// told it isn't filterable, not that it doesn't exist.
	if blocked != "" {
		return nil, notQueryableError(blocked, "filtered")
	}
	if unknown != "" {
		return nil, unknownFilterError(unknown, names)
	}
	// An over-cap ?field_in= list narrows results silently; fail closed
	// with a message shaped like the include path's scoped-IN cap.
	if overCapField != "" {
		return nil, fmt.Errorf("in-list on %q has %d entries (max %d)", overCapField, overCapCount, MaxINListEntries)
	}

	return filters, nil
}

// notQueryableError builds the 400-shaped error for a NoQuery field a caller
// tried to filter or sort on. Naming the field is deliberate: NoQuery fields
// appear in responses, so the caller already knows the column exists and the
// only useful information to add is that the query surface refuses it.
func notQueryableError(field, verb string) error {
	return fmt.Errorf("field %q cannot be %s", field, verb)
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
// "score". Kept deliberately conservative, a wrong suggestion is worse than
// none.
func nearestField(key string, fieldNames []string) string {
	base := key
	for _, s := range FilterSuffixes {
		if strings.HasSuffix(base, s.Suffix) {
			base = strings.TrimSuffix(base, s.Suffix)
			break
		}
	}
	// The key arrives straight off the request URL (a query-param NAME,
	// unauthenticated, no body). Levenshtein's cost is len(key) ×
	// Σ len(fieldNames), so an attacker can push ~1 MiB of key at a 30-field
	// entity for a per-GET CPU spike. A real field name is short, skip the
	// suggestion entirely past a small bound and return the plain
	// unknown-field error. (Build-time/argv callers in contracts and the CLI
	// pass trusted, tiny identifiers and never hit this.)
	const maxSuggestionKeyLen = 64
	if len(base) > maxSuggestionKeyLen {
		return ""
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
// is an information-disclosure path. NoQuery fields are excluded for the
// same reason, but rejected by name, they appear in responses, so the
// caller already knows the column exists. Unknown fields fail closed with a
// 400-shaped error rather than being silently ignored, silent drop
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
	// A field's wire key is what clients are told to use, so it must sort as
	// well as it filters. ParseFiltersValues and ?fields= projection already
	// resolve WireName; without the same resolution here the wire contract
	// splits by entry point, ?writer=x filters fine while ?sort=writer 400s.
	//
	// Both the column name and the alias are accepted: the alias is what a
	// versioned client sends, the column name is what existing unversioned
	// callers already send. Hidden fields are skipped before either is
	// registered, so a hidden column stays unreachable under both names.
	allowed := make(map[string]bool, len(fields))
	var noQuery map[string]bool
	sortAlias := make(map[string]string, len(fields))
	for _, f := range fields {
		if f.Hidden {
			continue
		}
		if f.NoQuery {
			if noQuery == nil {
				noQuery = make(map[string]bool)
			}
			noQuery[f.Name] = true
			// Same reasoning as the filter path: refuse the wire key by the
			// name the client used rather than letting sortAlias resolve it.
			if f.WireName != "" && f.WireName != f.Name {
				noQuery[f.WireName] = true
			}
			continue
		}
		allowed[f.Name] = true
		if f.WireName != "" && f.WireName != f.Name {
			allowed[f.WireName] = true
			sortAlias[f.WireName] = f.Name
		}
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
		// Reject control bytes outright, they have no business in a
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
		if noQuery[field] {
			return nil, notQueryableError(field, "sorted on")
		}
		if !allowed[field] {
			return nil, fmt.Errorf("invalid sort field %q", field)
		}
		// Resolve an alias to its column: ParsedSort.Field reaches ORDER BY,
		// so emitting the wire name would name a column that does not exist.
		if col, ok := sortAlias[field]; ok {
			field = col
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
func inClause(field string, values []any) (string, []any) {
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
			cb.Where(f.Field+" = $1", f.BindValue())
		case OpGt:
			cb.Where(f.Field+" > $1", f.BindValue())
		case OpLt:
			cb.Where(f.Field+" < $1", f.BindValue())
		case OpGte:
			cb.Where(f.Field+" >= $1", f.BindValue())
		case OpLte:
			cb.Where(f.Field+" <= $1", f.BindValue())
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
			qb.Where(f.Field+" = $1", f.BindValue())
		case OpGt:
			qb.Where(f.Field+" > $1", f.BindValue())
		case OpLt:
			qb.Where(f.Field+" < $1", f.BindValue())
		case OpGte:
			qb.Where(f.Field+" >= $1", f.BindValue())
		case OpLte:
			qb.Where(f.Field+" <= $1", f.BindValue())
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
func collectInRun(filters []ParsedFilter, start int) (values []any, n int) {
	field := filters[start].Field
	for j := start; j < len(filters); j++ {
		if filters[j].Op != OpIn || filters[j].Field != field {
			break
		}
		values = append(values, filters[j].BindValue())
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
