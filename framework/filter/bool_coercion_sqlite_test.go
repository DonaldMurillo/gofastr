package filter_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// TestBoolFilterMatchesSQLiteRows is the execution-level repro of the
// reported bug: GET /things?published=true returned an empty result set
// on SQLite while ?published=1 matched, because the raw string "true"
// was bound against an INTEGER column (numeric affinity converts "1"
// but never "true"). The coercion lives in the parse layer
// (filter.ParseFilters), so a query built from parsed filters must now
// match rows for every bool spelling on both the list and count
// builders.
func TestBoolFilterMatchesSQLiteRows(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE things (id TEXT, published INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO things (id, published) VALUES ('a', 1), ('b', 0)`); err != nil {
		t.Fatal(err)
	}

	fields := []schema.Field{{Name: "published", Type: schema.Bool}}
	for _, tc := range []struct {
		raw  string
		want string
	}{
		{"true", "a"},
		{"false", "b"},
		{"1", "a"},
		{"0", "b"},
	} {
		req := httptest.NewRequest(http.MethodGet, "/?published="+tc.raw, nil)
		filters, err := filter.ParseFilters(req, fields)
		if err != nil {
			t.Fatalf("published=%q: parse error: %v", tc.raw, err)
		}

		qb := query.Select("id").From("things")
		filter.ApplyToQuery(qb, filters)
		dataSQL, args := qb.Build()
		var id string
		if err := db.QueryRowContext(context.Background(), dataSQL, args...).Scan(&id); err != nil {
			t.Errorf("published=%q: list query failed: %v (sql=%q args=%v)", tc.raw, err, dataSQL, args)
			continue
		}
		if id != tc.want {
			t.Errorf("published=%q matched id=%q, want %q", tc.raw, id, tc.want)
		}

		cb := query.Count("things")
		filter.ApplyToCountQuery(cb, filters)
		countSQL, cargs := cb.Build()
		var n int
		if err := db.QueryRowContext(context.Background(), countSQL, cargs...).Scan(&n); err != nil {
			t.Errorf("published=%q: count query failed: %v (sql=%q args=%v)", tc.raw, err, countSQL, cargs)
			continue
		}
		if n != 1 {
			t.Errorf("published=%q count = %d, want 1", tc.raw, n)
		}
	}
}
