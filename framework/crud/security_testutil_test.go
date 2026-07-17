package crud

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/owner"
)

// RequestOpts replaces positional string parameters for test requests.
type RequestOpts struct {
	Method string
	Path   string
	Body   string // JSON-encoded, empty for GET
	UserID string // empty = unauthenticated
}

// setupDB creates an in-memory SQLite database and runs the provided DDL
// statements. The database is closed via t.Cleanup.
//
// MaxOpenConns is pinned to 1 because go-sqlite3's `:memory:` mode gives
// every pool connection its own private database — concurrent CRUD
// handlers and direct test queries would otherwise see different
// tables. The race-condition tests in particular need a shared view of
// the data.
func setupDB(t *testing.T, ddl ...string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })

	for _, stmt := range ddl {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec DDL %q: %v", stmt[:min(60, len(stmt))], err)
		}
	}
	return db
}

// setupSecurityTestHandler creates a CrudHandler over an in-memory SQLite
// database with the given entity configuration and DDL. The owner
// extractor is wired automatically — without it, requests carrying a
// testUser in the context would still look unauthenticated to the CRUD
// layer and short-circuit with 401 long before the per-row owner check
// (the original test-harness bug that masked these IDOR tests as
// "401 instead of 404").
func setupSecurityTestHandler(t *testing.T, cfg entity.EntityConfig, ddl string) (*CrudHandler, *sql.DB) {
	t.Helper()
	db := setupDB(t, ddl)
	ent := entity.Define(cfg.Table, cfg)
	ent.SetDB(db)
	installSecurityOwnerExtractor(t)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	return ch, db
}

// seedRows inserts rows into the given table. Each row map provides
// column→value pairs. Uses a single INSERT statement per row.
func seedRows(t *testing.T, db *sql.DB, table string, rows []map[string]any) {
	t.Helper()
	for _, row := range rows {
		var cols []string
		var vals []any
		for col, val := range row {
			cols = append(cols, col)
			vals = append(vals, val)
		}
		ph := make([]string, len(cols))
		for i := range ph {
			ph[i] = "?"
		}
		stmt := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
			table, strings.Join(cols, ", "), strings.Join(ph, ", "))
		if _, err := db.Exec(stmt, vals...); err != nil {
			t.Fatalf("seed %s: %v", table, err)
		}
	}
}

// makeRequest creates an *http.Request from the given options.
// If UserID is set, a testUser is injected into the context.
func makeRequest(t *testing.T, opts RequestOpts) *http.Request {
	t.Helper()
	var body io.Reader
	if opts.Body != "" {
		body = strings.NewReader(opts.Body)
	}
	req := httptest.NewRequest(opts.Method, opts.Path, body)
	if opts.Body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if opts.UserID != "" {
		req = withTestUser(req, opts.UserID)
	}
	return req
}

// assertStatus checks the response status code and reports a security-
// formatted error on mismatch. Returns true on match.
func assertStatus(t *testing.T, rr *httptest.ResponseRecorder, want int, category, desc string) bool {
	t.Helper()
	if rr.Code != want {
		body := rr.Body.String()
		if len(body) > 80 {
			body = body[:80] + "…"
		}
		t.Errorf("SECURITY: [%s] %s returned %d (body: %s) — want %d. Attack: %s",
			category, "request", rr.Code, body, want, desc)
		return false
	}
	return true
}

// assertBodyNotContains checks that the response body does not contain
// the forbidden substring.
func assertBodyNotContains(t *testing.T, rr *httptest.ResponseRecorder, forbidden, category, desc string) bool {
	t.Helper()
	if strings.Contains(rr.Body.String(), forbidden) {
		t.Errorf("SECURITY: [%s] response body contains forbidden string %q. Attack: %s",
			category, forbidden, desc)
		return false
	}
	return true
}

// assertHeader checks that the response has the expected header value.
func assertHeader(t *testing.T, rr *httptest.ResponseRecorder, name, wantValue, category, desc string) bool {
	t.Helper()
	got := rr.Header().Get(name)
	if got != wantValue {
		t.Errorf("SECURITY: [%s] header %q = %q, want %q. Attack: %s",
			category, name, got, wantValue, desc)
		return false
	}
	return true
}

// assertHeaderAbsent checks that the response does not have the named header.
func assertHeaderAbsent(t *testing.T, rr *httptest.ResponseRecorder, name, category, desc string) bool {
	t.Helper()
	if v := rr.Header().Get(name); v != "" {
		t.Errorf("SECURITY: [%s] unexpected header %q = %q. Attack: %s",
			category, name, v, desc)
		return false
	}
	return true
}

// securityOwnerExtractorOnce installs the test owner extractor exactly
// once for the lifetime of the test binary. Per-test install/restore
// raced under t.Parallel — a teardown from one test would reset the
// extractor while another parallel test was still mid-request, surfacing
// as a spurious 401. The extractor itself is pure (no shared state with
// individual tests) so a process-lifetime install is safe.
var securityOwnerExtractorOnce sync.Once

// installSecurityOwnerExtractor wires the owner extractor for security tests.
func installSecurityOwnerExtractor(t *testing.T) {
	t.Helper()
	securityOwnerExtractorOnce.Do(func() {
		owner.SetExtractor(func(ctx context.Context) (any, bool) {
			raw, ok := handler.GetUser(ctx)
			if !ok || raw == nil {
				return nil, false
			}
			if u, ok := raw.(*testUser); ok {
				return u.GetID(), true
			}
			return nil, false
		})
	})
}

// decodeListResponse is a test helper to decode a ListResponse JSON body.
func decodeListResponse(t *testing.T, body string) ListResponse {
	t.Helper()
	var resp ListResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode list response: %v; body=%s", err, body)
	}
	return resp
}

func decodeSingleResponse(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var response singleResponse
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode single response: %v", err)
	}
	return response.Data
}

// makeEntityConfig creates an EntityConfig with the given parameters.
func makeEntityConfig(name, table, ownerField string, fields []schema.Field, mutators ...func(*entity.EntityConfig)) entity.EntityConfig {
	cfg := entity.EntityConfig{
		Name:       name,
		Table:      table,
		Fields:     fields,
		OwnerField: ownerField,
	}.WithTimestamps(false)
	for _, m := range mutators {
		m(&cfg)
	}
	return cfg
}

// Suppress unused import warnings.
var (
	_ context.Context
	_ io.Reader
	_ = schema.String
	_ = (*entity.Entity)(nil)
)
