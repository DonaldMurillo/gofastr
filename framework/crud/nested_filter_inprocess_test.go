package crud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// TestListAll_NestedFilterParity asserts that an in-process ListAll with a
// NestedFilter returns the same rows the HTTP ?author.name= path returns.
func TestListAll_NestedFilterParity(t *testing.T) {
	ch, _, _ := covRelWorld(t)
	ctx := context.Background()

	// HTTP path: ?author.name=alice → both posts (both authored by alice).
	req := httptest.NewRequest(http.MethodGet, "/posts?author.name=alice", nil)
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, body=%s", rec.Code, rec.Body.String())
	}
	httpResp := decodeListResponse(t, rec.Body.String())

	// In-process path.
	rows, err := ch.ListAll(ctx, ListOptions{
		NestedFilters: []NestedFilter{
			{Relation: "author", Field: "name", Op: filter.OpEq, Value: "alice"},
		},
	})
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(rows) != len(httpResp.Data) {
		t.Fatalf("in-process rows = %d, HTTP rows = %d", len(rows), len(httpResp.Data))
	}

	// And a non-matching value returns nothing, matching HTTP semantics.
	none, err := ch.ListAll(ctx, ListOptions{
		NestedFilters: []NestedFilter{
			{Relation: "author", Field: "name", Op: filter.OpEq, Value: "nobody"},
		},
	})
	if err != nil {
		t.Fatalf("ListAll(none): %v", err)
	}
	if len(none) != 0 {
		t.Errorf("non-matching nested filter returned %d rows, want 0", len(none))
	}
}

// TestCountAll_NestedFilter asserts CountAll honours NestedFilters.
func TestCountAll_NestedFilter(t *testing.T) {
	ch, _, _ := covRelWorld(t)
	n, err := ch.CountAll(context.Background(), ListOptions{
		NestedFilters: []NestedFilter{
			{Relation: "author", Field: "name", Op: filter.OpEq, Value: "alice"},
		},
	})
	if err != nil {
		t.Fatalf("CountAll: %v", err)
	}
	if n != 2 {
		t.Errorf("CountAll nested = %d, want 2", n)
	}
}

// TestListAll_NestedFilterUnknownRelation errors on an unknown relation,
// mirroring the HTTP 400.
func TestListAll_NestedFilterUnknownRelation(t *testing.T) {
	ch, _, _ := covRelWorld(t)
	_, err := ch.ListAll(context.Background(), ListOptions{
		NestedFilters: []NestedFilter{{Relation: "ghost", Field: "x", Op: filter.OpEq, Value: "y"}},
	})
	if err == nil {
		t.Error("expected error for unknown relation")
	}
}
