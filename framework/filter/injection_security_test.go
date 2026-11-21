package filter

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
)

// TestInjection_SQLCommentPayload verifies that SQL payloads in filter
// values reach the filter unchanged: the parser does no SQL validation,
// the security boundary is the query builder. Attack: SQL comments like
// "foo'--" injected into filter parameters.
func TestInjection_SQLCommentPayload(t *testing.T) {
	fields := []schema.Field{
		{Name: "title", Type: schema.String},
		{Name: "status", Type: schema.String},
	}

	tests := []struct {
		name  string
		param string
		value string
		desc  string
	}{
		{
			name:  "sql_comment_in_eq",
			param: "title",
			value: "foo'--",
			desc:  "SQL comment appended to value",
		},
		{
			name:  "sql_union_in_value",
			param: "title",
			value: "x UNION SELECT * FROM users",
			desc:  "UNION-based SQL injection in filter value",
		},
		{
			name:  "semicolon_injection",
			param: "status",
			value: "active'; DROP TABLE notes;--",
			desc:  "semicolon-based injection",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			url := "/?" + tc.param + "=" + url.QueryEscape(tc.value)
			req := httptest.NewRequest(http.MethodGet, url, nil)
			filters, err := ParseFilters(req, fields)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			// The parser does no SQL validation; raw values must reach
			// the filter unchanged. The security boundary is the query builder.
			if len(filters) != 1 || filters[0].Field != tc.param || filters[0].Value != tc.value {
				t.Errorf("SECURITY: [filter_inject] %s=%q parsed as %+v, want the raw value passed through unchanged. Attack: %s.", tc.param, tc.value, filters, tc.desc)
			}
		})
	}
}

// TestInjection_OperatorInjection verifies the two parser outcomes for
// SQL-laden values: a typed column refuses a mistyped value, a string
// column passes the raw value through to the query builder. Attack:
// operator injection to bypass WHERE clause logic.
func TestInjection_OperatorInjection(t *testing.T) {
	fields := []schema.Field{
		{Name: "title", Type: schema.String},
		{Name: "priority", Type: schema.Int},
	}

	tests := []struct {
		name      string
		query     string
		wantErr   bool
		wantValue string
		desc      string
	}{
		{
			name:    "or_injection_in_gte",
			query:   "priority_gte=1 OR 1=1",
			wantErr: true, // an int column refuses the mistyped value
			desc:    "OR injection through operator suffix value",
		},
		{
			name:      "union_injection_in_value",
			query:     "title=hello' UNION SELECT * FROM users--",
			wantValue: "hello' UNION SELECT * FROM users--",
			desc:      "UNION injection through plain filter value",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/?"+url.QueryEscape(tc.query), nil)
			// Parse the raw query ourselves since httptest won't parse the encoded form
			req.URL.RawQuery = tc.query
			filters, err := ParseFilters(req, fields)
			if tc.wantErr {
				if err == nil {
					t.Errorf("SECURITY: [filter_inject] %q accepted; a value its column's type refuses must be refused. Attack: %s.", tc.query, tc.desc)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			// The parser does no SQL validation; raw values must reach
			// the filter unchanged. The security boundary is the query builder.
			if len(filters) != 1 || filters[0].Field != "title" || filters[0].Value != tc.wantValue {
				t.Errorf("SECURITY: [filter_inject] %q parsed as %+v, want the raw value passed through unchanged. Attack: %s.", tc.query, filters, tc.desc)
			}
		})
	}
}

// TestSort_RepeatedFieldBounded verifies that a single request cannot
// generate an unbounded ORDER BY clause by repeating an allow-listed
// ?sort= param thousands of times. Without a cap, N copies of
// ?sort=title produce N "ORDER BY title" fragments, oversized SQL,
// parse-CPU burn, and statement-cache pollution from one small request.
func TestSort_RepeatedFieldBounded(t *testing.T) {
	fields := []schema.Field{
		{Name: "title", Type: schema.String},
		{Name: "status", Type: schema.String},
	}

	t.Run("single_sort_ok", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/?sort=title", nil)
		sorts, err := ParseSort(req, fields)
		if err != nil {
			t.Fatalf("legitimate single sort rejected: %v", err)
		}
		if len(sorts) != 1 {
			t.Fatalf("got %d sorts, want 1", len(sorts))
		}
	})

	t.Run("repeated_allowed_field", func(t *testing.T) {
		// Same allow-listed field repeated 10,000 times.
		q := make([]string, 10000)
		for i := range q {
			q[i] = "sort=title"
		}
		req := httptest.NewRequest(http.MethodGet, "/?"+strings.Join(q, "&"), nil)
		sorts, err := ParseSort(req, fields)
		// Either fail closed (preferred) or cap, never emit thousands.
		if err == nil && len(sorts) > 16 {
			t.Errorf("SECURITY: [filter_sort] repeated ?sort=title produced %d clauses (want cap <=16 or a rejection). Attack: unbounded ORDER BY via repeated param.", len(sorts))
		}
	})

	t.Run("many_distinct_combos", func(t *testing.T) {
		// Mix of asc/desc on allowed fields, still over the cap.
		var q []string
		for range 5000 {
			q = append(q, "sort=title", "sort=-title", "sort=status", "sort=-status")
		}
		req := httptest.NewRequest(http.MethodGet, "/?"+strings.Join(q, "&"), nil)
		sorts, err := ParseSort(req, fields)
		if err == nil && len(sorts) > 16 {
			t.Errorf("SECURITY: [filter_sort] %d sort clauses accepted (want cap <=16 or a rejection). Attack: unbounded ORDER BY.", len(sorts))
		}
	})
}

// TestLike_WildcardEscaped verifies that a _like filter treats the
// caller value as a literal substring: LIKE metacharacters (% _ \)
// supplied by the caller are escaped (with an ESCAPE clause) so they
// cannot broaden the match or force pathological pattern scans. This
// mirrors the DSL `contains` operator. The contract must hold for both
// the data and the count query.
func TestLike_WildcardEscaped(t *testing.T) {
	fields := []schema.Field{{Name: "title", Type: schema.String}}

	cases := []struct {
		name string
		in   string
		want string // expected bound arg
	}{
		{"plain_substring", "hello", `%hello%`},
		{"percent_wildcard", "%", `%\%%`},
		{"underscore_wildcard", "_", `%\_%`},
		{"escape_char", `\`, `%\\%`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/?title_like="+url.QueryEscape(tc.in), nil)
			filters, err := ParseFilters(req, fields)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(filters) != 1 || filters[0].Op != OpLike {
				t.Fatalf("expected one LIKE filter, got %+v", filters)
			}

			qb := query.Select("*").From("notes")
			ApplyToQuery(qb, filters)
			sql, args := qb.Build()

			// The LIKE fragment must carry an ESCAPE clause so caller
			// metacharacters are interpreted literally.
			if strings.Contains(sql, "LIKE") && !strings.Contains(sql, "ESCAPE") {
				t.Errorf("SECURITY: [filter_like] LIKE clause has no ESCAPE: %q. Attack: ?title_like=%%25 matches every row (wildcard injection).", sql)
			}
			if len(args) != 1 {
				t.Fatalf("expected one bound arg, got %d", len(args))
			}
			if got, _ := args[0].(string); got != tc.want {
				t.Errorf("SECURITY: [filter_like] %q not escaped: arg=%q want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestSearchWildcardEscaped verifies that SearchConditions escapes LIKE
// metacharacters (% _ \) in the search term so they match literally,
// mirroring TestLike_WildcardEscaped. Without escaping, a user could
// supply "%" as a search term to match every row, or "_" to match any
// single character.
func TestSearchWildcardEscaped(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string // expected bound arg
	}{
		{"plain_substring", "hello", `%hello%`},
		{"percent_wildcard", "%", `%\%%`},
		{"underscore_wildcard", "_", `%\_%`},
		{"escape_char", `\`, `%\\%`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conds, _ := SearchConditions([]string{"title"}, tc.in)
			if len(conds) != 1 {
				t.Fatalf("expected 1 condition, got %d", len(conds))
			}
			if got := conds[0].Args[0].(string); got != tc.want {
				t.Errorf("SECURITY: [filter_search] %q not escaped: arg=%q want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestSearchSQLPayloadParameterized proves that SQL injection payloads in
// the search term never appear in the emitted SQL, they go into bound
// args, not the SQL string. The only SQL in a Condition is the fixed
// LOWER(field) LIKE $N ESCAPE pattern.
func TestSearchSQLPayloadParameterized(t *testing.T) {
	payloads := []string{
		"'; DROP TABLE notes--",
		"' OR '1'='1",
		"'; INSERT INTO admin VALUES('pwned'); --",
		"UNION SELECT password FROM users",
	}
	for _, p := range payloads {
		conds, _ := SearchConditions([]string{"title", "body"}, p)
		if len(conds) == 0 {
			t.Fatalf("expected conditions for payload %q, got 0", p)
		}
		for _, c := range conds {
			sql := c.SQL
			// The payload must NOT appear in the SQL string.
			if strings.Contains(sql, "DROP") || strings.Contains(sql, "INSERT") ||
				strings.Contains(sql, "UNION") || strings.Contains(sql, "OR '1'='1") {
				t.Errorf("SECURITY: [filter_search] payload %q leaked into SQL: %q", p, sql)
			}
		}
	}
}

// TestUnknownFilterKeyIsBounded pins the cost ceiling on the "did you mean"
// suggestion path. The unrecognized filter key is a query-param NAME straight
// off the request URL, unauthenticated, no body, and nearestField runs
// fuzzy.Levenshtein over it against every field name, so cost is
// len(key) × Σ len(fieldNames) (~225ms/MiB measured over 30 fields). With
// Go's default 1 MiB MaxHeaderBytes that is a per-GET CPU spike an
// unauthenticated caller can trigger at will. A real field name is short, so
// the suggestion is skipped past a small bound and the plain unknown-field
// error returns instantly.
func TestUnknownFilterKeyIsBounded(t *testing.T) {
	fields := make([]schema.Field, 30)
	for i := range fields {
		fields[i] = schema.Field{Name: fmt.Sprintf("field_%02d", i), Type: schema.String}
	}
	// 8 MiB key: uncapped Levenshtein over 30 fields is ~1.7s here; after the
	// len-bound guard it is microseconds. A 500ms budget separates the two by
	// ~300×, so it holds across any realistic CI machine speed.
	bigKey := strings.Repeat("x", 8<<20)
	q := url.Values{}
	q.Set(bigKey, "1")

	done := make(chan error, 1)
	go func() {
		_, err := ParseFiltersValues(q, fields)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an unknown-filter error for the oversized key, got nil")
		}
		if strings.Contains(err.Error(), "did you mean") {
			t.Errorf("oversized key still computed a suggestion: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("unknown-filter parse did not return promptly — uncapped Levenshtein over an 8 MiB key")
	}
}
