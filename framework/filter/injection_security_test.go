package filter

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
)

// TestInjection_SQLCommentPayload verifies that SQL comment injection
// in filter values is handled safely. Attack: attacker injects SQL
// comments like "foo'--" into filter parameters to manipulate queries.
func TestInjection_SQLCommentPayload(t *testing.T) {
	fields := []schema.Field{
		{Name: "title", Type: schema.String},
		{Name: "status", Type: schema.String},
	}

	tests := []struct {
		name    string
		param   string
		value   string
		wantErr bool
		desc    string
	}{
		{
			name:    "sql_comment_in_eq",
			param:   "title",
			value:   "foo'--",
			wantErr: false, // filter parsing doesn't validate SQL; returns raw value
			desc:    "SQL comment appended to value",
		},
		{
			name:    "sql_union_in_value",
			param:   "title",
			value:   "x UNION SELECT * FROM users",
			wantErr: false,
			desc:    "UNION-based SQL injection in filter value",
		},
		{
			name:    "semicolon_injection",
			param:   "status",
			value:   "active'; DROP TABLE notes;--",
			wantErr: false,
			desc:    "semicolon-based injection",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			url := "/?" + tc.param + "=" + url.QueryEscape(tc.value)
			req := httptest.NewRequest(http.MethodGet, url, nil)
			filters, err := ParseFilters(req, fields)

			// ParseFilters returns filters without SQL validation —
			// the security boundary is at the query builder layer.
			// This test documents that raw values pass through.
			if err != nil && tc.wantErr {
				return // expected error
			}
			if err != nil && !tc.wantErr {
				t.Fatalf("unexpected error: %v", err)
			}

			for _, f := range filters {
				if strings.Contains(f.Value, "'") || strings.Contains(f.Value, "--") {
					t.Logf("SECURITY: [filter_inject] raw filter value %q passed through without sanitization. Attack: %s. Note: security boundary is at the query builder, not the parser.", f.Value, tc.desc)
				}
			}
		})
	}
}

// TestInjection_OperatorInjection verifies that crafted operator suffixes
// like "title[gte]=1 OR 1=1" don't cause SQL injection through operator
// parsing. Attack: operator injection to bypass WHERE clause logic.
func TestInjection_OperatorInjection(t *testing.T) {
	fields := []schema.Field{
		{Name: "title", Type: schema.String},
		{Name: "priority", Type: schema.Int},
	}

	tests := []struct {
		name  string
		query string
		desc  string
	}{
		{
			name:  "or_injection_in_gte",
			query: "priority_gte=1 OR 1=1",
			desc:  "OR injection through operator suffix value",
		},
		{
			name:  "union_injection_in_value",
			query: "title=hello' UNION SELECT * FROM users--",
			desc:  "UNION injection through plain filter value",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/?"+url.QueryEscape(tc.query), nil)
			// Parse the raw query ourselves since httptest won't parse the encoded form
			req.URL.RawQuery = tc.query
			filters, err := ParseFilters(req, fields)
			if err != nil {
				t.Logf("ParseFilters rejected malicious input: %v", err)
				return
			}

			for _, f := range filters {
				// Values containing SQL keywords are passed through raw
				if strings.Contains(strings.ToUpper(f.Value), "OR") ||
					strings.Contains(strings.ToUpper(f.Value), "UNION") {
					t.Logf("SECURITY: [filter_inject] value %q with SQL keyword passed to filter (op=%s). Attack: %s.", f.Value, f.Op, tc.desc)
				}
			}
		})
	}
}

// TestInjection_OversizedINList verifies that an extremely large IN
// list does not cause memory exhaustion or unbounded query growth.
// Attack: 10,000+ comma-separated values in a single _in filter.
func TestInjection_OversizedINList(t *testing.T) {
	fields := []schema.Field{
		{Name: "id", Type: schema.String},
	}

	// Build a query with 10,000 comma-separated values
	vals := make([]string, 10000)
	for i := range vals {
		vals[i] = "val"
	}
	oversizedValue := strings.Join(vals, ",")

	req := httptest.NewRequest(http.MethodGet, "/?id_in="+oversizedValue, nil)
	filters, err := ParseFilters(req, fields)
	if err != nil {
		t.Logf("ParseFilters rejected oversized IN list: %v", err)
		return
	}

	inCount := 0
	for _, f := range filters {
		if f.Op == OpIn {
			inCount++
		}
	}

	if inCount > 1000 {
		t.Errorf("SECURITY: [filter_inject] oversized IN list produced %d filter entries (want cap at ~1000). Attack: memory exhaustion via unbounded IN clause.", inCount)
	}
}

// TestSort_RepeatedFieldBounded verifies that a single request cannot
// generate an unbounded ORDER BY clause by repeating an allow-listed
// ?sort= param thousands of times. Without a cap, N copies of
// ?sort=title produce N "ORDER BY title" fragments — oversized SQL,
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
		// Either fail closed (preferred) or cap — never emit thousands.
		if err == nil && len(sorts) > 16 {
			t.Errorf("SECURITY: [filter_sort] repeated ?sort=title produced %d clauses (want cap <=16 or a rejection). Attack: unbounded ORDER BY via repeated param.", len(sorts))
		}
	})

	t.Run("many_distinct_combos", func(t *testing.T) {
		// Mix of asc/desc on allowed fields, still over the cap.
		var q []string
		for i := 0; i < 5000; i++ {
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
