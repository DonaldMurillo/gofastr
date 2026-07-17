package crud

// Real-Postgres round-trip coverage for the CRUD SQL-generation core. The
// rest of this package's matrix runs on in-memory SQLite; these tests prove
// the same generated SQL ($N placeholders, ON CONFLICT, keyset cursors,
// tx rollback, scope predicates) executes against a live Postgres with the
// values actually coming back out. One focused suite — representative paths
// only, no duplication of the SQLite matrix.
//
// Skips automatically when Postgres is unreachable (see internal/pgtest).

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/pagination"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// pgCrudSetup provisions a schema-scoped live-Postgres database (skipping
// when none is reachable), runs the DDL, and returns a snake-cased
// CrudHandler over the entity — the PG mirror of setupSecurityTestHandler.
func pgCrudSetup(t *testing.T, cfg entity.EntityConfig, ddl string) (*CrudHandler, *sql.DB) {
	t.Helper()
	db := pgtest.DB(t)
	if _, err := db.Exec(ddl); err != nil {
		t.Fatalf("exec DDL: %v", err)
	}
	ent := entity.Define(cfg.Name, cfg)
	ent.SetDB(db)
	installSecurityOwnerExtractor(t)
	return NewCrudHandler(ent, db).WithJSONCase(CaseSnake), db
}

// pgSeed inserts rows using $N placeholders (lib/pq rejects `?`).
func pgSeed(t *testing.T, db *sql.DB, table string, rows []map[string]any) {
	t.Helper()
	for _, row := range rows {
		var cols []string
		var ph []string
		var vals []any
		for col, val := range row {
			cols = append(cols, col)
			ph = append(ph, fmt.Sprintf("$%d", len(ph)+1))
			vals = append(vals, val)
		}
		stmt := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
			table, strings.Join(cols, ", "), strings.Join(ph, ", "))
		if _, err := db.Exec(stmt, vals...); err != nil {
			t.Fatalf("seed %s: %v", table, err)
		}
	}
}

// listIDs runs a List request and returns the id of every returned row,
// failing hard on any non-200.
func listIDs(t *testing.T, ch *CrudHandler, path string) []string {
	t.Helper()
	resp := listOK(t, ch, path)
	ids := make([]string, 0, len(resp.Data))
	for _, row := range resp.Data {
		ids = append(ids, fmt.Sprint(row["id"]))
	}
	return ids
}

// listOK runs a List request and decodes the ListResponse, failing on
// any non-200.
func listOK(t *testing.T, ch *CrudHandler, path string) ListResponse {
	t.Helper()
	req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: path, UserID: "u1"})
	rr := httptest.NewRecorder()
	ch.List()(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200; body=%s", path, rr.Code, rr.Body.String())
	}
	return decodeListResponse(t, rr.Body.String())
}

// pgBooksCfg is the shared books entity for the read-path tests.
func pgBooksCfg(name string) entity.EntityConfig {
	return entity.EntityConfig{
		Name:  name,
		Table: name,
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "pages", Type: schema.Int},
			{Name: "genre", Type: schema.String},
		},
	}.WithTimestamps(false)
}

func pgBooksDDL(name string) string {
	return fmt.Sprintf(
		`CREATE TABLE %s (id TEXT PRIMARY KEY, title TEXT, pages INT, genre TEXT)`, name)
}

func seedBooks(t *testing.T, db *sql.DB, table string) {
	t.Helper()
	pgSeed(t, db, table, []map[string]any{
		{"id": "b1", "title": "Go in Action", "pages": 300, "genre": "tech"},
		{"id": "b2", "title": "Dune", "pages": 600, "genre": "scifi"},
		{"id": "b3", "title": "Untitled Draft", "pages": 100, "genre": nil},
		{"id": "b4", "title": "Go Web Dev", "pages": 450, "genre": "tech"},
	})
}

func TestPG_ListFilters(t *testing.T) {
	ch, db := pgCrudSetup(t, pgBooksCfg("pgf_books"), pgBooksDDL("pgf_books"))
	seedBooks(t, db, "pgf_books")

	// String equality — and the NULL-genre row (b3) must NOT match:
	// `genre = 'tech'` is NULL-false in real PG three-valued logic.
	got := listIDs(t, ch, "/pgf_books?genre=tech&sort=id")
	if want := []string{"b1", "b4"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("genre=tech → %v, want %v", got, want)
	}

	// Numeric comparison: the text query param must round-trip against a
	// real INT column (PG infers the cast; no SQLite type-affinity help).
	got = listIDs(t, ch, "/pgf_books?pages_gte=300&sort=id")
	if want := []string{"b1", "b2", "b4"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("pages_gte=300 → %v, want %v", got, want)
	}

	// LIKE (contains) with the framework's ESCAPE clause on real PG.
	got = listIDs(t, ch, "/pgf_books?title_like=Go&sort=id")
	if want := []string{"b1", "b4"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("title_like=Go → %v, want %v", got, want)
	}

	// The NULL-genre row is still reachable by a non-genre predicate.
	got = listIDs(t, ch, "/pgf_books?pages_lt=200")
	if want := []string{"b3"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("pages_lt=200 → %v, want %v", got, want)
	}
}

func TestPG_SortDesc(t *testing.T) {
	ch, db := pgCrudSetup(t, pgBooksCfg("pgs_books"), pgBooksDDL("pgs_books"))
	seedBooks(t, db, "pgs_books")

	got := listIDs(t, ch, "/pgs_books?sort=-pages")
	if want := []string{"b2", "b4", "b1", "b3"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("sort=-pages → %v, want %v", got, want)
	}
}

func TestPG_OffsetPageCount(t *testing.T) {
	ch, db := pgCrudSetup(t, pgBooksCfg("pgo_books"), pgBooksDDL("pgo_books"))
	seedBooks(t, db, "pgo_books")

	resp := listOK(t, ch, "/pgo_books?sort=id&limit=2&page=2")
	if resp.Total != 4 || resp.TotalPages != 2 || resp.Page != 2 || resp.PerPage != 2 {
		t.Errorf("envelope = total %d pages %d page %d perPage %d, want 4/2/2/2",
			resp.Total, resp.TotalPages, resp.Page, resp.PerPage)
	}
	ids := make([]string, 0, len(resp.Data))
	for _, row := range resp.Data {
		ids = append(ids, fmt.Sprint(row["id"]))
	}
	if want := []string{"b3", "b4"}; fmt.Sprint(ids) != fmt.Sprint(want) {
		t.Errorf("page 2 → %v, want %v", ids, want)
	}
}

func TestPG_CursorPaging(t *testing.T) {
	ch, db := pgCrudSetup(t, pgBooksCfg("pgc_books"), pgBooksDDL("pgc_books"))
	seedBooks(t, db, "pgc_books")
	pgSeed(t, db, "pgc_books", []map[string]any{
		{"id": "b5", "title": "Last", "pages": 10, "genre": "tech"},
	})

	fetch := func(path string) pagination.CursorPage {
		t.Helper()
		req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: path, UserID: "u1"})
		rr := httptest.NewRecorder()
		ch.List()(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("GET %s = %d; body=%s", path, rr.Code, rr.Body.String())
		}
		var page pagination.CursorPage
		if err := json.Unmarshal(rr.Body.Bytes(), &page); err != nil {
			t.Fatalf("decode cursor page: %v", err)
		}
		return page
	}
	ids := func(p pagination.CursorPage) string {
		out := make([]string, 0, len(p.Data))
		for _, row := range p.Data {
			out = append(out, fmt.Sprint(row["id"]))
		}
		return fmt.Sprint(out)
	}

	p1 := fetch("/pgc_books?cursor=&limit=2")
	if ids(p1) != fmt.Sprint([]string{"b1", "b2"}) || !p1.HasMore || p1.Cursor == "" {
		t.Fatalf("page1 = %s hasMore=%v cursor=%q, want [b1 b2] true non-empty",
			ids(p1), p1.HasMore, p1.Cursor)
	}
	p2 := fetch("/pgc_books?cursor=" + p1.Cursor + "&limit=2")
	if ids(p2) != fmt.Sprint([]string{"b3", "b4"}) || !p2.HasMore {
		t.Fatalf("page2 = %s hasMore=%v, want [b3 b4] true", ids(p2), p2.HasMore)
	}
	p3 := fetch("/pgc_books?cursor=" + p2.Cursor + "&limit=2")
	if ids(p3) != fmt.Sprint([]string{"b5"}) || p3.HasMore {
		t.Fatalf("page3 = %s hasMore=%v, want [b5] false", ids(p3), p3.HasMore)
	}
}

func TestPG_BatchRollback(t *testing.T) {
	ch, db := pgCrudSetup(t, entity.EntityConfig{
		Name:  "pgb_tasks",
		Table: "pgb_tasks",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
		},
	}.WithTimestamps(false),
		`CREATE TABLE pgb_tasks (id TEXT PRIMARY KEY, title TEXT)`)

	// Second item fails Required validation — the whole real-PG
	// transaction must roll back, including the already-inserted first.
	req := makeRequest(t, RequestOpts{
		Method: http.MethodPost,
		Path:   "/pgb_tasks/_batch",
		Body:   `{"items":[{"title":"ok"},{"title":""}]}`,
	})
	rr := httptest.NewRecorder()
	ch.BatchCreate()(rr, req)

	var resp BatchResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode batch response: %v; body=%s", err, rr.Body.String())
	}
	if resp.Committed {
		t.Errorf("batch with invalid item reported committed=true")
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM pgb_tasks").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("%d rows persisted after failed batch, want 0 (rollback)", count)
	}
}

func TestPG_Upsert(t *testing.T) {
	ch, db := pgCrudSetup(t, entity.EntityConfig{
		Name:  "pgu_kv",
		Table: "pgu_kv",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String},
		},
	}.WithTimestamps(false),
		`CREATE TABLE pgu_kv (id TEXT PRIMARY KEY, name TEXT)`)

	ctx := context.Background()
	if _, err := ch.UpsertOne(ctx, map[string]any{"id": "k1", "name": "first"}); err != nil {
		t.Fatalf("upsert insert: %v", err)
	}
	res, err := ch.UpsertOne(ctx, map[string]any{"id": "k1", "name": "second"})
	if err != nil {
		t.Fatalf("upsert update: %v", err)
	}
	if got := fmt.Sprint(res["name"]); got != "second" {
		t.Errorf("upsert returned name=%q, want second", got)
	}
	var count int
	var name string
	if err := db.QueryRow("SELECT COUNT(*) FROM pgu_kv").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if err := db.QueryRow("SELECT name FROM pgu_kv WHERE id = 'k1'").Scan(&name); err != nil {
		t.Fatalf("select: %v", err)
	}
	if count != 1 || name != "second" {
		t.Errorf("table holds %d rows, name=%q; want 1 row with name=second", count, name)
	}
}

func TestPG_IncludeBelongsTo(t *testing.T) {
	authorsEnt := entity.Define("pgi_authors", entity.EntityConfig{
		Name:  "pgi_authors",
		Table: "pgi_authors",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String},
		},
	}.WithTimestamps(false))

	ch, db := pgCrudSetup(t, entity.EntityConfig{
		Name:  "pgi_posts",
		Table: "pgi_posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "author_id", Type: schema.String},
		},
		Relations: []entity.Relation{
			entity.BelongsTo("author", "pgi_authors", "author_id"),
		},
	}.WithTimestamps(false),
		`CREATE TABLE pgi_authors (id TEXT PRIMARY KEY, name TEXT);
		 CREATE TABLE pgi_posts (id TEXT PRIMARY KEY, title TEXT, author_id TEXT)`)
	authorsEnt.SetDB(db)
	ch.Registry = stubRegistry{byName: map[string]*entity.Entity{"pgi_authors": authorsEnt}}

	pgSeed(t, db, "pgi_authors", []map[string]any{{"id": "a1", "name": "ann"}})
	pgSeed(t, db, "pgi_posts", []map[string]any{{"id": "p1", "title": "hello", "author_id": "a1"}})

	resp := listOK(t, ch, "/pgi_posts?include=author")
	if len(resp.Data) != 1 {
		t.Fatalf("got %d rows, want 1", len(resp.Data))
	}
	author, ok := resp.Data[0]["author"].(map[string]any)
	if !ok {
		t.Fatalf("author not eager-loaded: row=%v", resp.Data[0])
	}
	if got := fmt.Sprint(author["name"]); got != "ann" {
		t.Errorf("author.name = %q, want ann", got)
	}
}

func TestPG_SoftDelete(t *testing.T) {
	ch, db := pgCrudSetup(t, entity.EntityConfig{
		Name:  "pgd_docs",
		Table: "pgd_docs",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
		},
		SoftDelete: true,
	}.WithTimestamps(false),
		`CREATE TABLE pgd_docs (id TEXT PRIMARY KEY, title TEXT, deleted_at TIMESTAMPTZ)`)

	pgSeed(t, db, "pgd_docs", []map[string]any{
		{"id": "d1", "title": "keep"},
		{"id": "d2", "title": "trash"},
	})

	req := makeRequest(t, RequestOpts{Method: http.MethodDelete, Path: "/pgd_docs/d2", UserID: "u1"})
	req.SetPathValue("id", "d2")
	rr := httptest.NewRecorder()
	ch.Delete()(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d, want 204; body=%s", rr.Code, rr.Body.String())
	}

	// List must filter the trashed row out — but the row itself must
	// survive in the table with deleted_at stamped (soft, not hard).
	if got := listIDs(t, ch, "/pgd_docs"); fmt.Sprint(got) != fmt.Sprint([]string{"d1"}) {
		t.Errorf("list after soft delete → %v, want [d1]", got)
	}
	var stamped bool
	if err := db.QueryRow(
		"SELECT deleted_at IS NOT NULL FROM pgd_docs WHERE id = 'd2'").Scan(&stamped); err != nil {
		t.Fatalf("row d2 vanished from table (hard delete?): %v", err)
	}
	if !stamped {
		t.Errorf("deleted_at not set on soft-deleted row")
	}
}

func TestPG_OwnerTenantFailClosed(t *testing.T) {
	ch, db := pgCrudSetup(t, entity.EntityConfig{
		Name:  "pgn_notes",
		Table: "pgn_notes",
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "body", Type: schema.String},
		},
		OwnerField:  "user_id",
		MultiTenant: true,
	}.WithTimestamps(false),
		`CREATE TABLE pgn_notes (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, tenant_id TEXT NOT NULL, body TEXT)`)

	pgSeed(t, db, "pgn_notes", []map[string]any{
		{"id": "n1", "user_id": "alice", "tenant_id": "t1", "body": "a-t1"},
		{"id": "n2", "user_id": "alice", "tenant_id": "t2", "body": "a-t2"},
		{"id": "n3", "user_id": "bob", "tenant_id": "t1", "body": "b-t1"},
	})

	// No user → 401, never an unscoped result set.
	req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/pgn_notes"})
	rr := httptest.NewRecorder()
	ch.List()(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("anonymous list = %d, want 401", rr.Code)
	}

	// User but no tenant → still 401 (tenant gate fails closed).
	req = makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/pgn_notes", UserID: "alice"})
	rr = httptest.NewRecorder()
	ch.List()(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("tenantless list = %d, want 401", rr.Code)
	}

	// Both present → exactly the (alice, t1) row; count scoped too.
	req = makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/pgn_notes", UserID: "alice"})
	req = req.WithContext(tenant.SetTenantID(req.Context(), "t1"))
	rr = httptest.NewRecorder()
	ch.List()(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("scoped list = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	resp := decodeListResponse(t, rr.Body.String())
	if len(resp.Data) != 1 || resp.Total != 1 {
		t.Fatalf("scoped list → %d rows total=%d, want 1/1: %v", len(resp.Data), resp.Total, resp.Data)
	}
	if got := fmt.Sprint(resp.Data[0]["body"]); got != "a-t1" {
		t.Errorf("scoped row body = %q, want a-t1", got)
	}
}

// TestPG_SearchFields exercises ?q= free-text search against real
// Postgres LOWER() — proving the LOWER(col) LIKE $N ESCAPE pattern
// works on PG's locale-aware LOWER (vs SQLite's ASCII-only). The
// pgBooksCfg entity is reused but with SearchFields on title+genre.
func TestPG_SearchFields(t *testing.T) {
	cfg := pgBooksCfg("pgsrch_books")
	cfg.SearchFields = []string{"title", "genre"}
	ch, db := pgCrudSetup(t, cfg, pgBooksDDL("pgsrch_books"))
	seedBooks(t, db, "pgsrch_books")

	// "go" matches title "Go in Action" and "Go Web Dev" (case-insensitive).
	got := listIDs(t, ch, "/pgsrch_books?q=go&sort=id")
	if want := []string{"b1", "b4"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("q=go → %v, want %v", got, want)
	}

	// "tech" matches genre "tech" on b1 + b4.
	got = listIDs(t, ch, "/pgsrch_books?q=TECH&sort=id")
	if want := []string{"b1", "b4"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("q=TECH → %v, want %v (case-insensitive)", got, want)
	}

	// Multi-token AND: "go tech" → both tokens must match (title OR genre).
	got = listIDs(t, ch, "/pgsrch_books?q=go%20tech&sort=id")
	if want := []string{"b1", "b4"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("q='go tech' → %v, want %v (AND composition)", got, want)
	}

	// No match: "python" matches nothing.
	got = listIDs(t, ch, "/pgsrch_books?q=python&sort=id")
	if len(got) != 0 {
		t.Errorf("q=python → %v, want empty", got)
	}
}
