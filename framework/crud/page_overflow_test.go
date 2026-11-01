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
// POSITIVE wrong value: (2^62+1)*4 = 2^64+4 → 4. Unguarded, the request
// served the page-2 window labelled page 2^62+2; the MaxInt64 flavor
// wraps NEGATIVE, which SQLite reads as 0 and Postgres refuses (a 500).
const hugePageWrapPositive = 4611686018427387906 // 2^62 + 2, wraps ×4

// A page whose skip overflows int is a page past the offset cap, and is
// refused as one (400 offset_too_large) on the buffered and the streaming
// path alike. pagination.OffsetForPage answers 0 on overflow, and a page or
// offset too long for strconv.Atoi parses as none at all, so each of these
// was served as page 1, labelled as the page asked for (2026-11-01 bug
// bash; before that, a wrapped window).
func TestListRefusesOverflowingPage(t *testing.T) {
	ch := pageOverflowHandler(t)
	queries := []string{
		fmt.Sprintf("page=%d&limit=4", hugePageWrapPositive),
		"page=9223372036854775807&limit=20",
		"page=99999999999999999999999&limit=20",
		"offset=99999999999999999999999&limit=20",
	}
	for _, stream := range []string{"", "&stream=true"} {
		for _, q := range queries {
			req := withTestUser(httptest.NewRequest("GET", "/items?sort=n&"+q+stream, nil), "u1")
			rec := httptest.NewRecorder()
			ch.List()(rec, req)
			if rec.Code != http.StatusBadRequest || !contains(rec.Body.String(), "offset_too_large") {
				t.Errorf("?%s%s = %d %s, want 400 offset_too_large like any page past the cap", q, stream, rec.Code, rec.Body.String())
			}
		}
	}
	// Within the cap still pages, past the last row included.
	for _, q := range []string{"page=2&limit=4", "page=5000&limit=20", "offset=4&limit=4"} {
		rec := httptest.NewRecorder()
		ch.List()(rec, withTestUser(httptest.NewRequest("GET", "/items?sort=n&"+q, nil), "u1"))
		if rec.Code != http.StatusOK {
			t.Errorf("?%s = %d %s, want 200", q, rec.Code, rec.Body.String())
		}
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
