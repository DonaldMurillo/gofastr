package crud

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
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
				t.Errorf("cursor is %d bytes; past the cap it should carry the key alone", len(page.Cursor))
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

// A key-only cursor (minted when the sort values overflow the cap) whose
// anchor row is gone answers 400 error.cursor_anchor_gone, so the client
// restarts from the first page instead of being served a page from an
// unknown position. A row the caller may not read is gone the same way,
// so a cursor naming another owner's row learns nothing about where that
// row sorts. A cursor that carries its sort values needs no anchor row
// and pages on after the delete.
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
		if code, next, body := anchorPage(t, ch, "alice", page.Cursor); code != http.StatusOK || len(next.Data) != 1 || next.Data[0]["id"] != "n2" {
			t.Errorf("full cursor after delete = %d %s, want n2", code, body)
		}
		for name, cur := range map[string]string{
			"deleted anchor":     pagination.EncodeCursor("id", "n1"),
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

// A key too long for the 16 KiB cursor cap is refused when the cursor
// would be minted, with a reason naming it, never handed out as a token
// the next request refuses as "too long" (round 3 review of D33). SQLite
// only: a Postgres btree key cannot hold a value this size.
func TestCursorRefusesKeyPastCap(t *testing.T) {
	db := setupDB(t, `CREATE TABLE lnotes (id TEXT PRIMARY KEY, body TEXT NOT NULL)`)
	ent := entity.Define("lnotes", entity.EntityConfig{
		Name: "lnotes", Table: "lnotes",
		Fields: []schema.Field{
			{Name: "id", Type: schema.Text},
			{Name: "body", Type: schema.String, Required: true},
		},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	for _, id := range []string{"a" + strings.Repeat("k", 13<<10), "b"} {
		if _, err := db.Exec("INSERT INTO lnotes (id, body) VALUES (?, ?)", id, "x"); err != nil {
			t.Fatal(err)
		}
	}
	req := withTestUser(httptest.NewRequest(http.MethodGet, "/lnotes?limit=1&cursor=", nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code == http.StatusOK {
		var page pagination.CursorPage
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		t.Fatalf("minted a %d-byte cursor the decoder refuses", len(page.Cursor))
	}
	if !strings.Contains(rec.Body.String(), "cursor_key_too_long") {
		t.Fatalf("got %d %.300s, want reason cursor_key_too_long", rec.Code, rec.Body.String())
	}
}

// A cursor v0.95.0 minted carried the values of its cursor fields: the
// one CursorField alone, CursorFields plus the key columns they left out,
// or the key. Today's cursor carries the cursor fields plus the key, or
// the key alone when those overflow the cap. A released token the decoder accepts must name the
// same columns in the same order as the new keyset, or it would resume
// under a different meaning; every other one is refused once (round 3
// review of D33).
func TestReleasedCursorKeepsItsMeaning(t *testing.T) {
	released := func(p entity.PaginationConfig, keys []string) []string {
		switch {
		case len(p.CursorFields) > 0:
			out := slices.Clone(p.CursorFields)
			for _, k := range keys {
				if !slices.Contains(out, k) {
					out = append(out, k)
				}
			}
			return out
		case p.CursorField != "":
			return []string{p.CursorField}
		}
		return keys
	}
	var accepted, refused int
	for _, key := range [][]string{{"id"}, {"a", "b"}} {
		for _, p := range []entity.PaginationConfig{
			{}, {CursorField: "id"}, {CursorField: "a"}, {CursorField: "body"},
			{CursorFields: []string{"body"}}, {CursorFields: []string{"body", "id"}},
			{CursorFields: []string{"id"}}, {CursorFields: []string{"a"}},
			{CursorFields: []string{"b", "a"}}, {CursorFields: []string{"body", "a"}},
		} {
			ent := entity.Define("rel", entity.EntityConfig{
				Name: "rel", Table: "rel", PrimaryKey: key,
				Fields: []schema.Field{
					{Name: "id", Type: schema.String, Required: true},
					{Name: "a", Type: schema.String, Required: true},
					{Name: "b", Type: schema.String, Required: true},
					{Name: "body", Type: schema.String, Required: true},
				},
				Pagination: &p,
			}.WithTimestamps(false))
			ch := NewCrudHandler(ent, nil)
			old := released(p, ch.keyCols())
			token := pagination.EncodeCursor(old[0], "v")
			if len(old) > 1 {
				row := map[string]any{}
				for _, f := range old {
					row[f] = "v"
				}
				token = pagination.EncodeMultiCursor(old, row)
			}
			fields, keys := ch.cursorFields(), ch.keyCols()
			if _, err := decodeCursorFor(token, fields, keys); err != nil {
				refused++
				continue
			}
			accepted++
			// Accepted as the full shape, the values page in the new
			// keyset's order; accepted as the key, the anchor row supplies
			// the sort values, so any key order names the same row.
			if !slices.Equal(old, fields) && !sameSet(old, keys) {
				t.Errorf("key %v, %+v: released cursor on %v accepted under the order %v", key, p, old, fields)
			}
		}
	}
	if accepted == 0 || refused == 0 {
		t.Fatalf("accepted %d, refused %d: the cases no longer cover both outcomes", accepted, refused)
	}
}

// Rows tied on a single CursorField all come back: the key is the
// tiebreak behind it, the way CursorFields already appended it. Paging
// compared body > "x" alone and skipped the second "x" (round 3 review).
func TestCursorFieldPagesThroughTies(t *testing.T) {
	anchorDialects(t, func(t *testing.T, ch *CrudHandler, db *sql.DB, ph func(int) string) {
		anchorSeed(t, db, ph, "n1", "alice", "x")
		anchorSeed(t, db, ph, "n2", "alice", "x")
		anchorSeed(t, db, ph, "n3", "alice", "y")
		var got []string
		cursor := ""
		for range 5 {
			code, page, body := anchorPage(t, ch, "alice", cursor)
			if code != http.StatusOK {
				t.Fatalf("page %d = %d: %s", len(got)+1, code, body)
			}
			for _, row := range page.Data {
				got = append(got, row["id"].(string))
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

// The anchor row's sort value changing between pages must not skip the
// rows after it. A key-only cursor read the anchor's new value back, so
// a row edited after page 1 (an updated_at feed) jumped the cursor past
// every row between (round 3 review of D33). The cursor carries the sort
// values whenever they fit the cap.
func TestCursorSurvivesMovedAnchor(t *testing.T) {
	anchorDialects(t, func(t *testing.T, ch *CrudHandler, db *sql.DB, ph func(int) string) {
		for i, id := range []string{"n1", "n2", "n3"} {
			anchorSeed(t, db, ph, id, "alice", string(rune('a'+i)))
		}
		code, page, body := anchorPage(t, ch, "alice", "")
		if code != http.StatusOK || page.Cursor == "" {
			t.Fatalf("first page = %d, cursor %q: %s", code, page.Cursor, body)
		}
		if _, err := db.Exec("UPDATE anotes SET body = "+ph(1)+" WHERE id = "+ph(2), "z", "n1"); err != nil {
			t.Fatal(err)
		}
		code, page, body = anchorPage(t, ch, "alice", page.Cursor)
		if code != http.StatusOK || len(page.Data) != 1 || page.Data[0]["id"] != "n2" {
			t.Fatalf("second page = %d %s, want n2", code, body)
		}
	})
}

// A key-only cursor's anchor row is looked up under the caller's scopes
// only, never the client's own ?where= or ?q=: the anchor fixes where the
// page starts, and a row that stopped matching the search still names
// that position (bug hunt r7 R7-001). The same search spelled as a plain
// ?body= filter never reached the lookup and paged on.
func TestCursorAnchorIgnoresClientWhere(t *testing.T) {
	anchorDialects(t, func(t *testing.T, ch *CrudHandler, db *sql.DB, ph func(int) string) {
		anchorSeed(t, db, ph, "n1", "alice", "a")
		anchorSeed(t, db, ph, "n2", "alice", "b")
		anchorSeed(t, db, ph, "n3", "alice", "c")
		where := url.QueryEscape(`{"field":"body","op":"in","values":["b","c"]}`)
		cur := url.QueryEscape(pagination.EncodeCursor("id", "n1"))
		req := withTestUser(httptest.NewRequest(http.MethodGet, "/anotes?limit=1&where="+where+"&cursor="+cur, nil), "alice")
		rec := httptest.NewRecorder()
		ch.List()(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("key-only cursor with ?where= = %d %.300s, want 200", rec.Code, rec.Body.String())
		}
		var page pagination.CursorPage
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		if len(page.Data) != 1 || page.Data[0]["id"] != "n2" {
			t.Errorf("page = %v, want n2", page.Data)
		}
	})
}
