package crud

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// listWhere drives the HTTP List handler as the given user with a
// ?where=<json> tree and returns the decoded rows.
func listWhere(t *testing.T, ch *CrudHandler, userID, whereJSON string) []map[string]any {
	t.Helper()
	req := httptest.NewRequest("GET", "/onotes?where="+url.QueryEscape(whereJSON), nil).
		WithContext(signedIn(userID))
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("List ?where= = %d, body=%s", rec.Code, rec.Body.String())
	}
	return decodeListResponse(t, rec.Body.String()).Data
}

// TestWhereTree_CannotWidenOwnerScope is THE security invariant for #52:
// a user's OR-group that matches other owners' rows must NOT return them —
// the owner scope is an outer AND clause the OR can never widen past.
func TestWhereTree_CannotWidenOwnerScope(t *testing.T) {
	installOwnerExtractor(t)
	ch, _ := setupOwnerReadInProcHandler(t) // alice: "Alpha", bob: "Beta"

	// alice asks for (title = Alpha OR title = Beta). Beta is bob's row.
	// Owner scoping must still restrict the result to alice's rows.
	rows := listWhere(t, ch, "alice",
		`{"or":[{"field":"title","value":"Alpha"},{"field":"title","value":"Beta"}]}`)

	if len(rows) != 1 {
		t.Fatalf("SECURITY: OR-tree widened owner scope — got %d rows, want 1 (alice-only)", len(rows))
	}
	if rows[0]["title"] != "Alpha" {
		t.Fatalf("SECURITY: returned another owner's row: %v", rows[0])
	}
}

// TestWhereTree_FiltersWithinScope proves the tree still filters correctly
// inside the owner scope (not a blanket no-op).
func TestWhereTree_FiltersWithinScope(t *testing.T) {
	installOwnerExtractor(t)
	ch, _ := setupOwnerReadInProcHandler(t)

	// alice asks for title = Beta (which is bob's) — she owns no such row,
	// so the AND of (owner=alice) AND (title=Beta) is empty.
	rows := listWhere(t, ch, "alice", `{"field":"title","value":"Beta"}`)
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows (alice owns no Beta), got %d", len(rows))
	}

	// title = Alpha matches alice's own row.
	rows = listWhere(t, ch, "alice", `{"field":"title","value":"Alpha"}`)
	if len(rows) != 1 || rows[0]["title"] != "Alpha" {
		t.Fatalf("expected alice's Alpha row, got %v", rows)
	}
}

// TestWhereTree_InvalidReturns400 confirms a bad tree is a client error,
// not a 500 or a silent full-table scan.
func TestWhereTree_InvalidReturns400(t *testing.T) {
	installOwnerExtractor(t)
	ch, _ := setupOwnerReadInProcHandler(t)

	for _, bad := range []string{
		`{"field":"nope","value":"x"}`,               // unknown field
		`{"field":"title","op":"regex","value":"x"}`, // unknown operator
		`{"field":`, // malformed JSON
	} {
		req := httptest.NewRequest("GET", "/onotes?where="+url.QueryEscape(bad), nil).
			WithContext(signedIn("alice"))
		rec := httptest.NewRecorder()
		ch.List()(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("bad where %q = %d, want 400 (body=%s)", bad, rec.Code, rec.Body.String())
		}
	}
}
