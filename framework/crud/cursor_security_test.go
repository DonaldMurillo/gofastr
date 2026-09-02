package crud

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/pagination"
)

type cursorWireField struct {
	Name  string `json:"n"`
	Value string `json:"v"`
}

type cursorWireToken struct {
	Fields []cursorWireField `json:"f"`
}

func encodeMultiCursor(fields ...cursorWireField) string {
	b, _ := json.Marshal(cursorWireToken{Fields: fields})
	return base64.StdEncoding.EncodeToString(b)
}

// TestDecodeCursor_RejectsMismatchedFields pins the contract that the
// decoded cursor's column names must exact-match the consumer's
// expected set. Without this check, a cursor with mis-cased,
// whitespace-padded, or extra fields would let an attacker widen the
// ORDER BY / WHERE clause beyond the API contract.
func TestDecodeCursor_RejectsMismatchedFields(t *testing.T) {
	cases := map[string]struct {
		cursor string
		fields []string
	}{
		"missing-second-field": {
			cursor: encodeMultiCursor(cursorWireField{Name: "created_at", Value: "x"}),
			fields: []string{"created_at", "id"},
		},
		"extra-field": {
			cursor: encodeMultiCursor(
				cursorWireField{Name: "created_at", Value: "x"},
				cursorWireField{Name: "rogue", Value: "y"},
				cursorWireField{Name: "id", Value: "z"},
			),
			fields: []string{"created_at", "id"},
		},
		"duplicate-field": {
			cursor: encodeMultiCursor(
				cursorWireField{Name: "created_at", Value: "x"},
				cursorWireField{Name: "created_at", Value: "y"},
			),
			fields: []string{"created_at", "id"},
		},
		"case-mismatch": {
			cursor: encodeMultiCursor(cursorWireField{Name: "ID", Value: "1"}),
			fields: []string{"id"},
		},
		"single-fallback-for-composite": {
			cursor: pagination.EncodeCursor("created_at", "x"),
			fields: []string{"created_at", "id"},
		},
		"composite-for-single": {
			cursor: encodeMultiCursor(
				cursorWireField{Name: "id", Value: "1"},
				cursorWireField{Name: "extra", Value: "2"},
			),
			fields: []string{"id"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := decodeCursorAny(tc.cursor, tc.fields)
			if err == nil {
				t.Fatalf("decodeCursorAny accepted mismatched cursor; got %+v", out)
			}
		})
	}
}

// TestDecodeCursor_AcceptsExactMatch sanity-checks that a properly
// shaped cursor still decodes.
func TestDecodeCursor_AcceptsExactMatch(t *testing.T) {
	c := encodeMultiCursor(
		cursorWireField{Name: "created_at", Value: "2026-01-01"},
		cursorWireField{Name: "id", Value: "post-1"},
	)
	out, err := decodeCursorAny(c, []string{"created_at", "id"})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["created_at"] != "2026-01-01" || out["id"] != "post-1" {
		t.Fatalf("unexpected decoded cursor: %+v", out)
	}
}

// TestCursorSingleFieldForeignNameRejected: the exact-match contract
// covers the single-field encoding too. decodeCursorAny's fallback
// branch discards the decoded field name (`_, val, err :=`), so a
// cursor token naming ANY column ("evil_col") is accepted against a
// single-field consumer and its value is bound under the expected
// keyset column. The value is a bound arg so this is not injection,
// but the doc contract ("decoded field names MUST exact-match the
// expected fields set") is violated, and the accepted cursor makes
// the consumer silently keyset on a foreign value: state confusion
// across cursor revisions, the failure mode the mismatch checks on
// the multi-field path exist to prevent.
func TestCursorSingleFieldForeignNameRejected(t *testing.T) {
	shapes := []string{
		pagination.EncodeCursor("evil_col", "x"),   // entirely foreign name
		pagination.EncodeCursor("", "x"),           // empty name
		pagination.EncodeCursor("ID", "x"),         // case-mismatched name
		pagination.EncodeCursor("created_at", "x"), // another real column
		encodeMultiCursor(),                        // empty composite: no names at all
	}
	for _, c := range shapes {
		out, err := decodeCursorAny(c, []string{"id"})
		if err == nil {
			t.Fatalf("decodeCursorAny accepted single-field cursor with foreign name (out=%v); names must exact-match the expected keyset", out)
		}
	}
}

// TestCursorListClampsPerEntityLimit: the per-entity MaxListLimit must
// hold on the keyset path too. ParseCursorPagination clamps only to the
// global MaxPageSize; serveCursorList re-clamps to listLimitCap, and
// without it ?cursor= would be a MaxListLimit bypass (one oversized
// page per request instead of the offset path's enforced window).
// Asserted on both the first page (empty cursor key) and a follow-on
// page carrying a real cursor.
func TestCursorListClampsPerEntityLimit(t *testing.T) {
	cfg := makeEntityConfig("items", "items", "", []schema.Field{
		{Name: "title", Type: schema.String},
	}, func(c *entity.EntityConfig) {
		c.Pagination = &entity.PaginationConfig{MaxListLimit: 5}
	})
	ch, db := setupSecurityTestHandler(t, cfg,
		`CREATE TABLE items (id TEXT PRIMARY KEY, title TEXT)`)
	rows := make([]map[string]any, 0, 8)
	for i := range 8 {
		rows = append(rows, map[string]any{"id": fmt.Sprintf("i-%02d", i), "title": "t"})
	}
	seedRows(t, db, "items", rows)

	get := func(t *testing.T, query string) pagination.CursorPage {
		t.Helper()
		req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/items?" + query, UserID: "alice"})
		rr := httptest.NewRecorder()
		ch.List()(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("GET /items?%s = %d (body=%s)", query, rr.Code, rr.Body.String())
		}
		var page pagination.CursorPage
		if err := json.Unmarshal(rr.Body.Bytes(), &page); err != nil {
			t.Fatalf("decode cursor page: %v (body=%s)", err, rr.Body.String())
		}
		return page
	}

	first := get(t, "cursor=&limit=1000")
	if len(first.Data) != 5 {
		t.Fatalf("first keyset page carried %d rows, want the per-entity cap 5 (?cursor= as MaxListLimit bypass)", len(first.Data))
	}
	if !first.HasMore || first.Cursor == "" {
		t.Fatalf("first page HasMore=%v cursor=%q, want a continuation", first.HasMore, first.Cursor)
	}

	second := get(t, "cursor="+url.QueryEscape(first.Cursor)+"&limit=1000")
	if len(second.Data) != 3 {
		t.Fatalf("follow-on keyset page carried %d rows, want the remaining 3 under the per-entity cap", len(second.Data))
	}
}

// TestCursorModeStillRefusesBadSort: keyset mode ignores ?sort= for
// ordering purposes (the cursor fields own ORDER BY), but it must
// still REFUSE a sort every other query surface refuses. Skipping the
// check made ?cursor=&sort=<NoQuery> answer 200 where ?sort=<NoQuery>
// answers 400 — one empty parameter away from an exception to the
// "every query surface refuses it" contract. Asserted across the
// refusal family: Hidden, NoQuery, unknown, and control-byte sorts.
func TestCursorModeStillRefusesBadSort(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, makeEntityConfig("items", "items", "", []schema.Field{
		{Name: "title", Type: schema.String},
		{Name: "secret_key", Type: schema.String, Hidden: true},
		{Name: "card", Type: schema.String, NoQuery: true},
	}), `CREATE TABLE items (id TEXT PRIMARY KEY, title TEXT, secret_key TEXT, card TEXT)`)
	seedRows(t, db, "items", []map[string]any{{"id": "i-1", "title": "a", "secret_key": "s", "card": "c"}})

	cases := []string{
		"cursor=&sort=secret_key",
		"cursor=&sort=card",
		"cursor=&sort=-card",
		"cursor=&sort=nonexistent",
		"cursor=&sort=title%0Aadmin",
		"cursor=&sort=title%00x",
	}
	for _, q := range cases {
		req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/items?" + q, UserID: "alice"})
		rr := httptest.NewRecorder()
		ch.List()(rr, req)
		if rr.Code == http.StatusOK {
			t.Errorf("?%s answered 200 in cursor mode; a sort refused elsewhere must be refused here too (body=%s)", q, rr.Body.String())
		}
	}

	// Control: a clean sort is accepted in cursor mode (and ignored for
	// ordering — the cursor fields own it), so the refusals above are
	// the property, not a blanket sort ban.
	req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/items?cursor=&sort=title", UserID: "alice"})
	rr := httptest.NewRecorder()
	ch.List()(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("clean sort in cursor mode = %d (body=%s)", rr.Code, rr.Body.String())
	}
}
