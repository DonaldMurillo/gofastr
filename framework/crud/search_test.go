package crud

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// setupSearchHandler builds a CrudHandler over a "sarticles" table with
// SearchFields on title+body, seeded with rows from two owners.
func setupSearchHandler(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	db := setupDB(t, `CREATE TABLE sarticles (
		id TEXT PRIMARY KEY,
		user_id TEXT,
		title TEXT,
		body TEXT,
		deleted_at TEXT
	)`)
	ent := entity.Define("sarticles", entity.EntityConfig{
		Table:        "sarticles",
		Fields:       []schema.Field{{Name: "user_id", Type: schema.String}, {Name: "title", Type: schema.String}, {Name: "body", Type: schema.Text}},
		OwnerField:   "user_id",
		SearchFields: []string{"title", "body"},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	seedRows(t, db, "sarticles", []map[string]any{
		{"id": "a1", "user_id": "alice", "title": "Go concurrency", "body": "Goroutines and channels"},
		{"id": "a2", "user_id": "alice", "title": "Rust safety", "body": "Ownership and borrowing"},
		{"id": "b1", "user_id": "bob", "title": "Go testing", "body": "Table driven tests"},
		{"id": "b2", "user_id": "bob", "title": "Python async", "body": "Asyncio coroutines"},
	})
	return ch, db
}

// TestSearch_QRespectsOwnerScope: alice's search for "go" returns only
// her own Go rows, never bob's.
func TestSearch_QRespectsOwnerScope(t *testing.T) {
	installOwnerExtractor(t)
	ch, _ := setupSearchHandler(t)

	req := withTestUser(httptest.NewRequest(http.MethodGet, "/api/sarticles?q=go", nil), "alice")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "a1") {
		t.Fatalf("alice's Go article missing: %s", body)
	}
	if strings.Contains(body, "b1") {
		t.Fatalf("bob's Go article leaked into alice's search: %s", body)
	}
}

// TestSearch_QComposesWithFieldFilters: ?q= ANDs with field filters.
func TestSearch_QComposesWithFieldFilters(t *testing.T) {
	installOwnerExtractor(t)
	ch, _ := setupSearchHandler(t)

	// Search "go" AND title_like="concurrency" → only a1 matches.
	req := withTestUser(httptest.NewRequest(http.MethodGet, "/api/sarticles?q=go&title_like=concurrency", nil), "alice")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	lr := decodeListResponse(t, rec.Body.String())
	if len(lr.Data) != 1 {
		t.Fatalf("composed search returned %d rows, want 1 (a1 only)", len(lr.Data))
	}
}

// TestSearch_CountEnvelopeMatches: the total in the ListResponse reflects
// the search filter.
func TestSearch_CountEnvelopeMatches(t *testing.T) {
	installOwnerExtractor(t)
	ch, _ := setupSearchHandler(t)

	req := withTestUser(httptest.NewRequest(http.MethodGet, "/api/sarticles?q=go", nil), "alice")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	lr := decodeListResponse(t, rec.Body.String())
	// alice has 2 rows: a1 (Go concurrency) and a2 (Rust safety). Only a1
	// matches "go" across title+body.
	if lr.Total != 1 {
		t.Fatalf("count envelope total=%d, want 1", lr.Total)
	}
	if len(lr.Data) != 1 {
		t.Fatalf("data rows=%d, want 1", len(lr.Data))
	}
}

// TestSearch_CursorHonorsQ: cursor path respects ?q=.
func TestSearch_CursorHonorsQ(t *testing.T) {
	installOwnerExtractor(t)
	ch, _ := setupSearchHandler(t)

	req := withTestUser(httptest.NewRequest(http.MethodGet, "/api/sarticles?q=go&cursor=&limit=10", nil), "alice")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cursor status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "cursor") {
		t.Fatalf("cursor envelope missing: %s", body)
	}
	// Only alice's Go article should appear.
	if strings.Contains(body, "rust") {
		t.Fatalf("rust article appeared in go search: %s", body)
	}
}

// TestSearch_StreamHonorsQ: stream path respects ?q=.
func TestSearch_StreamHonorsQ(t *testing.T) {
	installOwnerExtractor(t)
	ch, _ := setupSearchHandler(t)

	req := withTestUser(httptest.NewRequest(http.MethodGet, "/api/sarticles?q=go&stream=true&limit=10", nil), "alice")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("stream status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "a1") {
		t.Fatalf("alice's go article missing from stream: %s", body)
	}
	if strings.Contains(body, "rust") || strings.Contains(body, "Rust") {
		t.Fatalf("rust article in go search stream: %s", body)
	}
}

// TestSearch_NoSearchFieldsBackCompat: entity without SearchFields ignores ?q=.
func TestSearch_NoSearchFieldsBackCompat(t *testing.T) {
	installOwnerExtractor(t)
	db := setupDB(t, `CREATE TABLE splain (
		id TEXT PRIMARY KEY,
		user_id TEXT,
		note TEXT
	)`)
	ent := entity.Define("splain", entity.EntityConfig{
		Table:      "splain",
		Fields:     []schema.Field{{Name: "user_id", Type: schema.String}, {Name: "note", Type: schema.String}},
		OwnerField: "user_id",
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	seedRows(t, db, "splain", []map[string]any{
		{"id": "p1", "user_id": "alice", "note": "hello world"},
		{"id": "p2", "user_id": "alice", "note": "goodbye"},
	})

	req := withTestUser(httptest.NewRequest(http.MethodGet, "/api/splain?q=hello", nil), "alice")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	lr := decodeListResponse(t, rec.Body.String())
	// Without SearchFields, ?q= is ignored — both rows return.
	if len(lr.Data) != 2 {
		t.Fatalf("no-SearchFields entity returned %d rows, want 2 (?q= ignored)", len(lr.Data))
	}
}

// TestSearch_QColumnEdge: an entity WITH SearchFields that also has a
// physical column named "q" — plain ?q= means search, not column filter.
func TestSearch_QColumnEdge(t *testing.T) {
	installOwnerExtractor(t)
	db := setupDB(t, `CREATE TABLE sedge (
		id TEXT PRIMARY KEY,
		user_id TEXT,
		title TEXT,
		q TEXT
	)`)
	ent := entity.Define("sedge", entity.EntityConfig{
		Table:        "sedge",
		Fields:       []schema.Field{{Name: "user_id", Type: schema.String}, {Name: "title", Type: schema.String}, {Name: "q", Type: schema.String}},
		OwnerField:   "user_id",
		SearchFields: []string{"title"},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	seedRows(t, db, "sedge", []map[string]any{
		{"id": "e1", "user_id": "alice", "title": "quality score", "q": "metric"},
		{"id": "e2", "user_id": "alice", "title": "other", "q": "quantity"},
	})

	// ?q=quality → searches title for "quality", NOT filtering q column.
	req := withTestUser(httptest.NewRequest(http.MethodGet, "/api/sedge?q=quality", nil), "alice")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// "quality" matches e1's title; e2's title "other" doesn't match.
	// If ?q= were filtering the q column, e2 would match (q="quantity").
	if strings.Contains(body, "e2") {
		t.Fatalf("q-column edge: e2 appeared — ?q= filtered the column instead of searching: %s", body)
	}
	if !strings.Contains(body, "e1") {
		t.Fatalf("q-column edge: e1 missing from title search: %s", body)
	}
}

// TestSearch_MultiTokenAND: multiple tokens AND together.
func TestSearch_MultiTokenAND(t *testing.T) {
	installOwnerExtractor(t)
	ch, _ := setupSearchHandler(t)

	// "go concurrency" → both tokens must match (in title OR body).
	// a1 title="Go concurrency" matches both. a2 doesn't match either.
	req := withTestUser(httptest.NewRequest(http.MethodGet, "/api/sarticles?q=go%20concurrency", nil), "alice")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	lr := decodeListResponse(t, rec.Body.String())
	if len(lr.Data) != 1 {
		t.Fatalf("multi-token AND returned %d rows, want 1", len(lr.Data))
	}
}

// TestSearch_ListAllParity: in-process ListAll with opts.Search matches.
func TestSearch_ListAllParity(t *testing.T) {
	installOwnerExtractor(t)
	ch, _ := setupSearchHandler(t)

	ctx := signedIn("alice")
	rows, err := ch.ListAll(ctx, ListOptions{Search: "go"})
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("ListAll search returned %d rows, want 1", len(rows))
	}

	n, err := ch.CountAll(ctx, ListOptions{Search: "go"})
	if err != nil {
		t.Fatalf("CountAll: %v", err)
	}
	if n != 1 {
		t.Fatalf("CountAll search = %d, want 1", n)
	}
}

// TestSearch_ListAllNoSearchFieldsError: Search on entity without
// SearchFields fails loud.
func TestSearch_ListAllNoSearchFieldsError(t *testing.T) {
	installOwnerExtractor(t)
	db := setupDB(t, `CREATE TABLE sbare (
		id TEXT PRIMARY KEY,
		user_id TEXT,
		note TEXT
	)`)
	ent := entity.Define("sbare", entity.EntityConfig{
		Table:      "sbare",
		Fields:     []schema.Field{{Name: "user_id", Type: schema.String}, {Name: "note", Type: schema.String}},
		OwnerField: "user_id",
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)

	ctx := signedIn("alice")
	_, err := ch.ListAll(ctx, ListOptions{Search: "hello"})
	if err == nil {
		t.Fatal("ListAll with Search on no-SearchFields entity should error")
	}
}

// TestSearch_CaseInsensitive: search matches regardless of case.
func TestSearch_CaseInsensitive(t *testing.T) {
	installOwnerExtractor(t)
	ch, _ := setupSearchHandler(t)

	for _, q := range []string{"GO", "Go", "go", "gO"} {
		req := withTestUser(httptest.NewRequest(http.MethodGet, "/api/sarticles?q="+q, nil), "alice")
		rec := httptest.NewRecorder()
		ch.List()(rec, req)
		lr := decodeListResponse(t, rec.Body.String())
		if len(lr.Data) != 1 {
			t.Fatalf("case-insensitive search q=%q returned %d rows, want 1", q, len(lr.Data))
		}
	}
}

// TestSearch_DefinePanics: Define panics on invalid SearchFields entries.
func TestSearch_DefinePanics(t *testing.T) {
	cases := []struct {
		name   string
		fields []schema.Field
		search []string
		errMsg string
	}{
		{"unknown_field", []schema.Field{{Name: "title", Type: schema.String}}, []string{"nonexistent"}, "not a declared field"},
		{"hidden_field", []schema.Field{{Name: "secret", Type: schema.String, Hidden: true}}, []string{"secret"}, "Hidden"},
		{"non_text", []schema.Field{{Name: "count", Type: schema.Int}}, []string{"count"}, "String or Text"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatal("Define did not panic")
				}
				msg, ok := r.(string)
				if !ok || !strings.Contains(msg, tc.errMsg) {
					t.Fatalf("panic message %q does not contain %q", msg, tc.errMsg)
				}
			}()
			entity.Define("bad", entity.EntityConfig{
				Fields:       tc.fields,
				SearchFields: tc.search,
			}.WithTimestamps(false))
		})
	}
}

// TestSearch_SoftDeleteRespected: search respects soft-delete scoping.
func TestSearch_SoftDeleteRespected(t *testing.T) {
	installOwnerExtractor(t)
	db := setupDB(t, `CREATE TABLE ssoft (
		id TEXT PRIMARY KEY,
		user_id TEXT,
		title TEXT,
		deleted_at TEXT
	)`)
	ent := entity.Define("ssoft", entity.EntityConfig{
		Table:        "ssoft",
		Fields:       []schema.Field{{Name: "user_id", Type: schema.String}, {Name: "title", Type: schema.String}},
		OwnerField:   "user_id",
		SearchFields: []string{"title"},
		SoftDelete:   true,
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	db.Exec(`INSERT INTO ssoft (id, user_id, title, deleted_at) VALUES ('s1', 'alice', 'visible go', NULL)`)
	db.Exec(`INSERT INTO ssoft (id, user_id, title, deleted_at) VALUES ('s2', 'alice', 'deleted go', '2025-01-01')`)

	req := withTestUser(httptest.NewRequest(http.MethodGet, "/api/ssoft?q=go", nil), "alice")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	lr := decodeListResponse(t, rec.Body.String())
	if len(lr.Data) != 1 {
		t.Fatalf("soft-delete search returned %d rows, want 1 (only non-deleted)", len(lr.Data))
	}
	if lr.Data[0]["id"] != "s1" {
		t.Fatalf("soft-deleted row appeared in search: %+v", lr.Data[0])
	}
}

// TestSearch_TenantScopeHolds: search results never cross tenant boundary.
func TestSearch_TenantScopeHolds(t *testing.T) {
	installOwnerExtractor(t)
	db := setupDB(t, `CREATE TABLE sten (
		id TEXT PRIMARY KEY,
		user_id TEXT,
		title TEXT,
		tenant_id TEXT
	)`)
	ent := entity.Define("sten", entity.EntityConfig{
		Table:        "sten",
		Fields:       []schema.Field{{Name: "user_id", Type: schema.String}, {Name: "title", Type: schema.String}, {Name: "tenant_id", Type: schema.String}},
		OwnerField:   "user_id",
		SearchFields: []string{"title"},
		MultiTenant:  true,
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	seedRows(t, db, "sten", []map[string]any{
		{"id": "t1", "user_id": "alice", "title": "go tenant a", "tenant_id": "ta"},
		{"id": "t2", "user_id": "bob", "title": "go tenant b", "tenant_id": "tb"},
	})

	ctx := signedIn("alice")
	ctx = tenant.SetTenantID(ctx, "ta")
	rows, err := ch.ListAll(ctx, ListOptions{Search: "go"})
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("tenant-scoped search returned %d rows, want 1", len(rows))
	}
	if rows[0]["tenant_id"] != "ta" {
		t.Fatalf("tenant-B row leaked: %+v", rows[0])
	}
}

// Ensure filter import is used.
var _ = filter.MaxSearchTerms
