package framework

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/gofastr/gofastr/core/schema"
	_ "github.com/mattn/go-sqlite3"
)

// setupCursorDB seeds a posts table with N rows whose ids sort lexically:
// "p001", "p002", … so cursor pagination's ORDER BY id ASC has a stable order.
func setupCursorDB(t *testing.T, n int) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT NOT NULL)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	for i := 1; i <= n; i++ {
		id := fmt.Sprintf("p%03d", i)
		if _, err := db.Exec("INSERT INTO posts(id, title) VALUES (?, ?)", id, fmt.Sprintf("Post %d", i)); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	return db
}

func cursorApp(t *testing.T, db *sql.DB) *App {
	t.Helper()
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.Entity("posts", EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
		},
	}.WithTimestamps(false))
	return app
}

func decodeCursorPage(t *testing.T, body string) CursorPage {
	t.Helper()
	var p CursorPage
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatalf("decode CursorPage: %v\n%s", err, body)
	}
	return p
}

// ============================================================================
// Test: First page returns first N items, hasMore=true, cursor present
// ============================================================================

func TestCursor_FirstPage(t *testing.T) {
	db := setupCursorDB(t, 25)
	ta := TestHarness(t, cursorApp(t, db))

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
}

// ============================================================================
// Test: Following the cursor walks the dataset to the end
// ============================================================================

func TestCursor_WalksToLastPage(t *testing.T) {
	db := setupCursorDB(t, 25)
	ta := TestHarness(t, cursorApp(t, db))

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
}

// ============================================================================
// Test: Last page reports hasMore=false and empty cursor
// ============================================================================

func TestCursor_LastPageHasNoMore(t *testing.T) {
	db := setupCursorDB(t, 12)
	ta := TestHarness(t, cursorApp(t, db))

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
}

// ============================================================================
// Test: Invalid cursor → 400
// ============================================================================

func TestCursor_InvalidCursor_400(t *testing.T) {
	db := setupCursorDB(t, 5)
	ta := TestHarness(t, cursorApp(t, db))

	resp := ta.Get("/posts?cursor=" + url.QueryEscape("not-base64-!@#"))
	resp.AssertStatus(t, http.StatusBadRequest).
		AssertBodyContains(t, "invalid cursor")
}

// ============================================================================
// Test: No cursor key → falls back to offset (ListResponse) — regression pin
// ============================================================================

func TestCursor_AbsentCursor_UsesOffset(t *testing.T) {
	db := setupCursorDB(t, 5)
	ta := TestHarness(t, cursorApp(t, db))

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
}

// ============================================================================
// Test: Cursor mode respects filters
// ============================================================================

func TestCursor_RespectsFilters(t *testing.T) {
	db := setupCursorDB(t, 25)
	ta := TestHarness(t, cursorApp(t, db))

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
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
