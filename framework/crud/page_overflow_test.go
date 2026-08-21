package crud

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// pageOverflowHandler: plain offset-paginated entity.
func pageOverflowHandler(t *testing.T) *CrudHandler {
	t.Helper()
	db := setupDB(t, `CREATE TABLE items (id TEXT PRIMARY KEY, n INTEGER)`)
	ent := entity.Define("items", entity.EntityConfig{
		Name: "items", Table: "items",
		Fields: []schema.Field{{Name: "n", Type: schema.Int}},
	}.WithTimestamps(false))
	ent.SetDB(db)
	rows := make([]map[string]any, 8)
	for i := range rows {
		rows[i] = map[string]any{"id": fmt.Sprintf("i-%d", i+1), "n": i + 1}
	}
	seedRows(t, db, "items", rows)
	return NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
}

// hugePageWrapPositive is a page number parsePaginationValues accepts
// (>0, fits int) whose (page-1)*limit product wraps int arithmetic to a
// POSITIVE wrong value: (2^62+1)*4 = 2^64+4 → 4. The request then
// silently serves the page-2 window while labelling it page 2^62+2.
// (The MaxInt64 flavor wraps NEGATIVE: SQLite treats a negative OFFSET
// as 0, masking the bug, while Postgres rejects it, turning the
// request into a 500.)
const hugePageWrapPositive = 4611686018427387906 // 2^62 + 2, wraps ×4

// TestListHugePageNotWrappedWindow: an overflowing page number must be
// clamped by the same guard pagination.ParsePagination applies, not
// wrapped to an arbitrary window by int overflow.
func TestListHugePageNotWrappedWindow(t *testing.T) {
	ch := pageOverflowHandler(t)

	req := withTestUser(httptest.NewRequest("GET",
		fmt.Sprintf("/items?page=%d&limit=4&sort=n", hugePageWrapPositive), nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("huge page = %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeListResponse(t, rec.Body.String())
	if len(resp.Data) == 0 {
		t.Fatal("expected the first window of rows, got none")
	}
	first, _ := resp.Data[0]["n"].(float64)
	if first != 1 {
		t.Fatalf("overflowing page wrapped to window starting at n=%v — int overflow in (page-1)*limit; want the guarded first window", first)
	}
}

// TestStreamingListHugePageNotWrappedWindow: the streaming list path
// computes its own (page-1)*limit offset and must apply the same
// overflow guard.
func TestStreamingListHugePageNotWrappedWindow(t *testing.T) {
	ch := pageOverflowHandler(t)

	req := withTestUser(httptest.NewRequest("GET",
		fmt.Sprintf("/items?stream=true&page=%d&limit=4&sort=n", hugePageWrapPositive), nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("streaming huge page = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// Buggy offset 4 streams rows 5..8; the guarded path streams 1..4.
	want := `"n":1`
	for i := 1; i <= 4; i++ {
		if !contains(body, fmt.Sprintf(`"n":%d`, i)) {
			t.Fatalf("streaming huge page missed row n=%d (wrapped window): %s", i, body)
		}
	}
	_ = want
	if contains(body, `"n":5`) {
		t.Fatalf("streaming huge page served the wrapped window (n=5 present): %s", body)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
