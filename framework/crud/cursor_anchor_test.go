package crud

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/pagination"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// anchorDialects runs fn on SQLite and on Postgres (skipped when no
// Postgres is reachable) with a handler over an owner-scoped notes table
// cursored on its body, a Text column.
func anchorDialects(t *testing.T, fn func(t *testing.T, ch *CrudHandler, db *sql.DB, ph func(int) string)) {
	cfg := makeEntityConfig("anotes", "anotes", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "body", Type: schema.Text, Required: true},
	}, func(c *entity.EntityConfig) {
		c.Pagination = &entity.PaginationConfig{CursorField: "body"}
	})
	ddl := `CREATE TABLE anotes (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, body TEXT NOT NULL)`
	for _, d := range []struct {
		name string
		open func(t *testing.T) *sql.DB
		ph   func(int) string
	}{
		{"sqlite", func(t *testing.T) *sql.DB { return setupDB(t) }, func(int) string { return "?" }},
		{"postgres", pgtest.DB, func(n int) string { return "$" + string(rune('0'+n)) }},
	} {
		t.Run(d.name, func(t *testing.T) {
			db := d.open(t)
			if _, err := db.Exec(ddl); err != nil {
				t.Fatalf("exec DDL: %v", err)
			}
			ent := entity.Define(cfg.Name, cfg)
			ent.SetDB(db)
			installSecurityOwnerExtractor(t)
			fn(t, NewCrudHandler(ent, db).WithJSONCase(CaseSnake), db, d.ph)
		})
	}
}

func anchorSeed(t *testing.T, db *sql.DB, ph func(int) string, id, user, body string) {
	t.Helper()
	q := "INSERT INTO anotes (id, user_id, body) VALUES (" + ph(1) + ", " + ph(2) + ", " + ph(3) + ")"
	if _, err := db.Exec(q, id, user, body); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

// anchorPage requests one cursor page as user and returns the status and
// the decoded page.
func anchorPage(t *testing.T, ch *CrudHandler, user, cursor string) (int, pagination.CursorPage, string) {
	t.Helper()
	req := withTestUser(httptest.NewRequest(http.MethodGet, "/anotes?limit=1&cursor="+url.QueryEscape(cursor), nil), user)
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	var page pagination.CursorPage
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatalf("decode page: %v", err)
		}
	}
	return rec.Code, page, rec.Body.String()
}

// A sort value longer than the 16 KiB cursor cap used to go into the
// cursor, so the next page answered 400 and paging stopped partway
// (2026-10-27 bug bash D33). The cursor carries the key now.
func TestCursorPagesPastLongSortValue(t *testing.T) {
	anchorDialects(t, func(t *testing.T, ch *CrudHandler, db *sql.DB, ph func(int) string) {
		for i, id := range []string{"n1", "n2", "n3"} {
			anchorSeed(t, db, ph, id, "alice", string(rune('a'+i))+strings.Repeat("x", 20<<10))
		}
		var got []string
		cursor := ""
		for range 4 {
			code, page, body := anchorPage(t, ch, "alice", cursor)
			if code != http.StatusOK {
				t.Fatalf("page %d = %d: %.200s", len(got)+1, code, body)
			}
			for _, row := range page.Data {
				got = append(got, row["id"].(string))
			}
			if len(page.Cursor) > 1024 {
				t.Errorf("cursor is %d bytes; it should carry the key, not the sort value", len(page.Cursor))
			}
			if !page.HasMore {
				break
			}
			cursor = page.Cursor
		}
		if strings.Join(got, ",") != "n1,n2,n3" {
			t.Errorf("paged %v, want n1,n2,n3", got)
		}
	})
}

// The anchor row deleted between pages answers 400 error.cursor_anchor_gone,
// so the client restarts from the first page instead of being served a
// page from an unknown position. A row the caller may not read is gone
// the same way, so a cursor naming another owner's row learns nothing
// about where that row sorts.
func TestCursorAnchorGoneRefused(t *testing.T) {
	anchorDialects(t, func(t *testing.T, ch *CrudHandler, db *sql.DB, ph func(int) string) {
		anchorSeed(t, db, ph, "n1", "alice", "a")
		anchorSeed(t, db, ph, "n2", "alice", "b")
		anchorSeed(t, db, ph, "b1", "bob", "a5")
		code, page, body := anchorPage(t, ch, "alice", "")
		if code != http.StatusOK || page.Cursor == "" {
			t.Fatalf("first page = %d, cursor %q: %s", code, page.Cursor, body)
		}
		if _, err := db.Exec("DELETE FROM anotes WHERE id = "+ph(1), "n1"); err != nil {
			t.Fatal(err)
		}
		for name, cur := range map[string]string{
			"deleted anchor":     page.Cursor,
			"other owner's row":  pagination.EncodeCursor("id", "b1"),
			"row that never was": pagination.EncodeCursor("id", "zz"),
		} {
			code, _, body := anchorPage(t, ch, "alice", cur)
			if code != http.StatusBadRequest || !strings.Contains(body, "cursor_anchor_gone") {
				t.Errorf("%s: %d %s, want 400 error.cursor_anchor_gone", name, code, body)
			}
		}
	})
}
