package resource

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
)

// TestTableHugePageOffsetGuarded: the admin table computes
// Offset: (page-1)*limit from ?p= with no upper bound on page. For
// page = 2^62+2 and a page size of 4, (page-1)*limit wraps int
// arithmetic to +4 — the grid silently renders the second window while
// its pager claims the astronomically-large page. The offset handed to
// ListAll must be overflow-guarded (0), matching pagination's guard.
func TestTableHugePageOffsetGuarded(t *testing.T) {
	source := &stubSource{}
	cfg := Config{
		Entity:   "orders",
		Title:    "Orders",
		Singular: "Order",
		BasePath: "/orders",
		APIPath:  "/api/orders",
		Crud:     source,
		PageSize: 4,
		Fields:   []Field{{Key: "name", Label: "Name", Type: "string"}},
	}
	req := httptest.NewRequest("GET", fmt.Sprintf("/orders?p=%d", int64(1)<<62+2), nil)
	cfg.List(appui.WithRequest(context.Background(), req))

	if len(source.listCalls) != 1 {
		t.Fatalf("ListAll calls = %d, want 1", len(source.listCalls))
	}
	if got := source.listCalls[0].Offset; got != 0 {
		t.Fatalf("?p=2^62+2 with page size 4 wrapped (page-1)*limit to offset %d, want 0 (overflow-guarded)", got)
	}
}
