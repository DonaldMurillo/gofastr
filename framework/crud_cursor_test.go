package framework

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/gofastr/gofastr/core/schema"
	"github.com/gofastr/gofastr/framework/entity"
	"github.com/gofastr/gofastr/framework/pagination"
)

// seedCursorDB creates the posts table on db and inserts N rows whose ids
// sort lexically ("p001", "p002", …) so keyset pagination has a stable order
// across both dialects.
func seedCursorDB(t *testing.T, db *sql.DB, n int) {
	t.Helper()
	if _, err := db.Exec(`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT NOT NULL)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	for i := 1; i <= n; i++ {
		id := fmt.Sprintf("p%03d", i)
		if _, err := db.Exec("INSERT INTO posts(id, title) VALUES ($1, $2)", id, fmt.Sprintf("Post %d", i)); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
}

// runCursorTest fans the body across both dialects. n controls how many rows
// to seed; 0 means leave the table empty.
func runCursorTest(t *testing.T, n int, body func(t *testing.T, ta *TestApp)) {
	t.Helper()
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedCursorDB(t, db, n)
		app := cursorApp(t, db)
		ta := TestHarness(t, app)
		body(t, ta)
	})
}

func cursorApp(t *testing.T, db *sql.DB) *App {
	t.Helper()
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.Entity("posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
		},
	}.WithTimestamps(false))
	return app
}

func decodeCursorPage(t *testing.T, body string) pagination.CursorPage {
	t.Helper()
	var p pagination.CursorPage
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatalf("decode CursorPage: %v\n%s", err, body)
	}
	return p
}

// ============================================================================
// Test: First page returns first N items, hasMore=true, cursor present
// ============================================================================

func TestCursor_FirstPage(t *testing.T) {
	runCursorTest(t, 25, func(t *testing.T, ta *TestApp) {
		resp := ta.Get("/posts?cursor=&limit=10")
		resp.AssertStatus(t, http.StatusOK)

		page := decodeCursorPage(t, resp.Body())
		if len(page.Data) != 10 {
			t.Fatalf("expected 10 items, got %d", len(page.Data))
		}
		if !page.HasMore {
			t.Fatal("expected hasMore=true")
		}
		if page.Cursor == "" {
			t.Fatal("expected non-empty cursor")
		}
		if got := page.Data[0]["id"]; got != "p001" {
			t.Fatalf("expected first id p001, got %v", got)
		}
		if got := page.Data[9]["id"]; got != "p010" {
			t.Fatalf("expected last id p010, got %v", got)
		}
	})
}

// ============================================================================
// Test: Following the cursor walks the dataset to the end
// ============================================================================

func TestCursor_WalksToLastPage(t *testing.T) {
	runCursorTest(t, 25, func(t *testing.T, ta *TestApp) {

		cursor := ""
		seen := []string{}
		for page := 0; page < 5; page++ {
			// Always include cursor= so cursor mode is engaged
			path := "/posts?limit=10&cursor=" + url.QueryEscape(cursor)
			resp := ta.Get(path)
			resp.AssertStatus(t, http.StatusOK)
			got := decodeCursorPage(t, resp.Body())
			for _, row := range got.Data {
				seen = append(seen, fmt.Sprintf("%v", row["id"]))
			}
			if !got.HasMore {
				// final page
				cursor = got.Cursor
				break
			}
			cursor = got.Cursor
		}

		if len(seen) != 25 {
			t.Fatalf("expected to walk 25 rows, walked %d (last cursor=%q)", len(seen), cursor)
		}
		// Order check: lexical
		for i := 1; i <= 25; i++ {
			want := fmt.Sprintf("p%03d", i)
			if seen[i-1] != want {
				t.Fatalf("row %d: expected %s, got %s", i, want, seen[i-1])
			}
		}
	})
}

// ============================================================================
// Test: Last page reports hasMore=false and empty cursor
// ============================================================================

func TestCursor_LastPageHasNoMore(t *testing.T) {
	runCursorTest(t, 12, func(t *testing.T, ta *TestApp) {

		first := decodeCursorPage(t, ta.Get("/posts?cursor=&limit=10").Body())
		if !first.HasMore {
			t.Fatal("expected first page hasMore=true")
		}
		if first.Cursor == "" {
			t.Fatal("expected first page cursor non-empty")
		}

		second := decodeCursorPage(t, ta.Get("/posts?cursor="+url.QueryEscape(first.Cursor)+"&limit=10").Body())
		if len(second.Data) != 2 {
			t.Fatalf("expected 2 items on last page, got %d", len(second.Data))
		}
		if second.HasMore {
			t.Fatal("expected hasMore=false on last page")
		}
		if second.Cursor != "" {
			t.Fatalf("expected empty cursor on last page, got %q", second.Cursor)
		}
	})
}

// ============================================================================
// Test: Invalid cursor → 400
// ============================================================================

func TestCursor_InvalidCursor_400(t *testing.T) {
	runCursorTest(t, 5, func(t *testing.T, ta *TestApp) {

		resp := ta.Get("/posts?cursor=" + url.QueryEscape("not-base64-!@#"))
		resp.AssertStatus(t, http.StatusBadRequest).
			AssertBodyContains(t, "invalid cursor")
	})
}

// ============================================================================
// Test: No cursor key → falls back to offset (ListResponse) — regression pin
// ============================================================================

func TestCursor_AbsentCursor_UsesOffset(t *testing.T) {
	runCursorTest(t, 5, func(t *testing.T, ta *TestApp) {

		resp := ta.Get("/posts?limit=10")
		resp.AssertStatus(t, http.StatusOK)

		var off ListResponse
		if err := json.Unmarshal([]byte(resp.Body()), &off); err != nil {
			t.Fatalf("decode ListResponse: %v\n%s", err, resp.Body())
		}
		if off.Total != 5 {
			t.Fatalf("expected total=5 from offset envelope, got %d", off.Total)
		}
		if off.Page != 1 || off.PerPage != 10 {
			t.Fatalf("expected page=1 perPage=10 (offset shape), got %+v", off)
		}
	})
}

// ============================================================================
// Test: Cursor mode respects filters
// ============================================================================

func TestCursor_RespectsFilters(t *testing.T) {
	runCursorTest(t, 25, func(t *testing.T, ta *TestApp) {

		// Filter title_like contains "Post 2" → matches p002, p020-p025 (7 rows)
		resp := ta.Get("/posts?cursor=&limit=10&title_like=" + url.QueryEscape("Post 2"))
		resp.AssertStatus(t, http.StatusOK)
		page := decodeCursorPage(t, resp.Body())
		if len(page.Data) == 0 {
			t.Fatal("expected filtered cursor results, got 0")
		}
		for _, row := range page.Data {
			title := fmt.Sprintf("%v", row["title"])
			if !contains(title, "Post 2") {
				t.Fatalf("filter violated: row title=%q does not contain 'Post 2'", title)
			}
		}
	})
}

// ============================================================================
// Test: EntityConfig.CursorField overrides the PK for keyset pagination
// ============================================================================

func TestCursor_PerEntityCursorField(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		// Two-column table where created_at is the cursor; ids are inserted out
		// of created_at order so PK-keyset would walk a different sequence.
		if _, err := db.Exec(`CREATE TABLE events (
			id TEXT PRIMARY KEY,
			created_at TEXT NOT NULL UNIQUE,
			label TEXT NOT NULL
		)`); err != nil {
			t.Fatalf("create: %v", err)
		}
		seeds := []struct {
			id, ca, label string
		}{
			{"z", "2026-01-01T00:00:00Z", "first"},
			{"y", "2026-01-02T00:00:00Z", "second"},
			{"x", "2026-01-03T00:00:00Z", "third"},
			{"w", "2026-01-04T00:00:00Z", "fourth"},
			{"v", "2026-01-05T00:00:00Z", "fifth"},
		}
		for _, s := range seeds {
			if _, err := db.Exec("INSERT INTO events(id, created_at, label) VALUES ($1, $2, $3)", s.id, s.ca, s.label); err != nil {
				t.Fatalf("seed: %v", err)
			}
		}

		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Entity("events", entity.EntityConfig{
			Table:       "events",
			CursorField: "created_at",
			Fields: []schema.Field{
				{Name: "created_at", Type: schema.String, Required: true},
				{Name: "label", Type: schema.String, Required: true},
			},
		}.WithTimestamps(false))
		ta := TestHarness(t, app)

		// First page — should be ordered by created_at ASC, not by id.
		first := decodeCursorPage(t, ta.Get("/events?cursor=&limit=3").Body())
		if len(first.Data) != 3 {
			t.Fatalf("expected 3 items, got %d", len(first.Data))
		}
		// Expected created_at order: 2026-01-01, 02, 03.
		want := []string{"first", "second", "third"}
		for i, w := range want {
			label, _ := first.Data[i]["label"].(string)
			if label != w {
				t.Fatalf("row %d: expected label %q, got %q (data=%v)", i, w, label, first.Data[i])
			}
		}

		// Walk the cursor — must finish at 5 rows, label order preserved.
		cursor := first.Cursor
		seen := append([]string{}, want...)
		for hops := 0; hops < 5 && cursor != ""; hops++ {
			next := decodeCursorPage(t, ta.Get("/events?cursor="+cursor+"&limit=3").Body())
			for _, row := range next.Data {
				seen = append(seen, fmt.Sprintf("%v", row["label"]))
			}
			if !next.HasMore {
				break
			}
			cursor = next.Cursor
		}
		expected := []string{"first", "second", "third", "fourth", "fifth"}
		if len(seen) != len(expected) {
			t.Fatalf("expected %d rows walked, got %d (seen=%v)", len(expected), len(seen), seen)
		}
		for i, e := range expected {
			if seen[i] != e {
				t.Fatalf("walk position %d: expected %q, got %q", i, e, seen[i])
			}
		}
	})
}

// ============================================================================
// Test: composite cursor — ORDER BY (created_at DESC, id DESC) with tuple
// comparison handles ties on the leading column gracefully.
// ============================================================================

func TestCursor_Composite(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		// Two rows share the same created_at to force the tiebreak.
		if _, err := db.Exec(`CREATE TABLE feed (
			id TEXT PRIMARY KEY,
			created_at TEXT NOT NULL,
			label TEXT NOT NULL
		)`); err != nil {
			t.Fatalf("create: %v", err)
		}
		seeds := []struct {
			id, ca, label string
		}{
			{"a", "2026-01-01T00:00:00Z", "a"},
			{"b", "2026-01-02T00:00:00Z", "b"}, // shares timestamp with c
			{"c", "2026-01-02T00:00:00Z", "c"},
			{"d", "2026-01-03T00:00:00Z", "d"},
			{"e", "2026-01-04T00:00:00Z", "e"},
		}
		for _, s := range seeds {
			if _, err := db.Exec("INSERT INTO feed(id, created_at, label) VALUES ($1, $2, $3)",
				s.id, s.ca, s.label); err != nil {
				t.Fatalf("seed: %v", err)
			}
		}

		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Entity("feed", entity.EntityConfig{
			Table:        "feed",
			CursorFields: []string{"created_at", "id"},
			Fields: []schema.Field{
				{Name: "created_at", Type: schema.String, Required: true},
				{Name: "label", Type: schema.String, Required: true},
			},
		}.WithTimestamps(false))
		ta := TestHarness(t, app)

		first := decodeCursorPage(t, ta.Get("/feed?cursor=&limit=2").Body())
		if len(first.Data) != 2 {
			t.Fatalf("first page: expected 2, got %d", len(first.Data))
		}
		// ORDER BY created_at ASC, id ASC. Expected: a (2026-01-01), b (2026-01-02 + id=b).
		if first.Data[0]["label"] != "a" || first.Data[1]["label"] != "b" {
			t.Fatalf("unexpected first page order: %+v", first.Data)
		}
		if !first.HasMore {
			t.Fatal("expected hasMore=true after first page")
		}

		// Walk to the end.
		seen := []string{"a", "b"}
		cur := first.Cursor
		for cur != "" {
			page := decodeCursorPage(t, ta.Get("/feed?cursor="+cur+"&limit=2").Body())
			for _, row := range page.Data {
				seen = append(seen, fmt.Sprintf("%v", row["label"]))
			}
			if !page.HasMore {
				break
			}
			cur = page.Cursor
		}
		want := []string{"a", "b", "c", "d", "e"}
		if len(seen) != len(want) {
			t.Fatalf("expected walk len=%d, got %d (seen=%v)", len(want), len(seen), seen)
		}
		for i, l := range want {
			if seen[i] != l {
				t.Fatalf("walk pos %d: want %q, got %q", i, l, seen[i])
			}
		}
	})
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
