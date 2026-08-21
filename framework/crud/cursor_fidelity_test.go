package crud

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/pagination"
)

// cursorFidelityHandler: entity cursored on a TEXT column (title), the
// shape that loses rows when the cursor decode mutates the keyset value.
func cursorFidelityHandler(t *testing.T) *CrudHandler {
	t.Helper()
	db := setupDB(t, `CREATE TABLE docs (id TEXT PRIMARY KEY, title TEXT)`)
	ent := entity.Define("docs", entity.EntityConfig{
		Name: "docs", Table: "docs",
		Pagination: &entity.PaginationConfig{CursorField: "title"},
		Fields:     []schema.Field{{Name: "title", Type: schema.String, Required: true}},
	}.WithTimestamps(false))
	ent.SetDB(db)
	return NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
}

// TestCursorPagingServesEveryRowOnce: page through rows whose sort keys
// include zero-width / bidi codepoints and assert the union of pages is
// exactly the seeded set, no duplicates, no missing rows. DecodeCursor
// used to strip those codepoints, so the keyset resumed BEFORE the
// stripped row and re-served it (and its predecessors' tail) twice.
func TestCursorPagingServesEveryRowOnce(t *testing.T) {
	ch := cursorFidelityHandler(t)
	titles := []string{"aa", "a\u200bb", "a\ufeffb", "mm", "zz"}
	for _, ti := range titles {
		req := withTestUser(httptest.NewRequest("POST", "/docs",
			strings.NewReader(fmt.Sprintf(`{"title":%s}`, mustJSONString(ti)))), "u1")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		ch.Create()(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("seed %q: %d %s", ti, rec.Code, rec.Body.String())
		}
	}

	seen := map[string]int{}
	cursor := ""
	pages := 0
	for {
		path := "/docs?cursor=" + pagination.EncodeCursor("title", cursor) + "&limit=2"
		if cursor == "" {
			path = "/docs?cursor=&limit=2"
		}
		req := withTestUser(httptest.NewRequest("GET", path, nil), "u1")
		rec := httptest.NewRecorder()
		ch.List()(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("page %d: %d %s", pages, rec.Code, rec.Body.String())
		}
		var page pagination.CursorPage
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatalf("decode page: %v", err)
		}
		pages++
		for _, row := range page.Data {
			title, _ := row["title"].(string)
			seen[title]++
		}
		if !page.HasMore || page.Cursor == "" {
			break
		}
		cursor = decodeCursorValue(t, page.Cursor)
		if pages > 10 {
			t.Fatal("paging did not terminate")
		}
	}

	var dupes, missing []string
	for _, want := range titles {
		n := seen[want]
		if n == 0 {
			missing = append(missing, want)
		} else if n > 1 {
			dupes = append(dupes, want)
		}
	}
	if len(dupes) > 0 || len(missing) > 0 {
		t.Fatalf("cursor paging corrupted: dupes=%v missing=%v seen=%v", dupes, missing, seen)
	}
}

// decodeCursorValue extracts the value half of an emitted cursor so the
// test can follow the paging chain without depending on the token shape.
func decodeCursorValue(t *testing.T, cur string) string {
	t.Helper()
	_, v, err := pagination.DecodeCursor(cur)
	if err != nil {
		t.Fatalf("decode cursor: %v", err)
	}
	return v
}

func mustJSONString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
