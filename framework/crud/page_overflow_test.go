package crud

import (
	"context"
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

// TestExplicitOffsetBeyondCapRejected: an explicit ?offset= beyond the
// handler's ceiling (MaxOffset, default the page cap × 1000 = 100,000)
// is refused with 400. LIMIT is clamped on every path; the skip side
// must be bounded too or one query param buys a per-request deep-skip
// scan on a populated table.
func TestExplicitOffsetBeyondCapRejected(t *testing.T) {
	ch, db := setupCamelDocsHandler(t)
	if _, err := db.Exec(`INSERT INTO reddocs (id, body_text) VALUES ('r1', 'x'), ('r2', 'y')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	req := withTestUser(httptest.NewRequest(http.MethodGet, "/api/reddocs?offset=9223372036854775807&limit=1", nil), "alice")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code == http.StatusOK {
		t.Errorf("SECURITY: [offset-bound] ?offset=9223372036854775807 was accepted with %d (limit is clamped to MaxPageSize, offset is not): a client can force a per-request full-table skip scan (requireBoundedOffset)", rec.Code)
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

// TestStreamingListZeroLimitNoPanic: ServeStreamingList is an EXPORTED
// in-process entrypoint that takes an arbitrary limit (its sibling
// OffsetForPage guards its own division for exactly that reason, and its
// doc names this method as the example). The trailing totalPages math
// divides by limit unguarded: a caller passing limit 0 gets
// "runtime error: integer divide by zero", taking down the goroutine
// (and any in-process caller that lacks a recover) instead of an
// error or a well-formed envelope.
func TestStreamingListZeroLimitNoPanic(t *testing.T) {
	ch := pageOverflowHandler(t)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("DATA: [stream-limit] ServeStreamingList panicked on limit=0: %v. Attack: exported in-process entrypoint performs unguarded total/limit division.", r)
		}
	}()
	req := withTestUser(httptest.NewRequest("GET", "/items?stream=true", nil), "u1")
	rec := httptest.NewRecorder()
	ch.ServeStreamingList(context.Background(), rec, req, ch.visibleFields(), nil, nil, nil, 1, 0, nil)
}
