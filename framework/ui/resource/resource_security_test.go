package resource

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
)

// TestListCarryQueryKeepsFmtVerbs pins the production carry builder itself
// (resource.go table(): carry = QueryEscape'd search/facets off the query
// string, then SortHrefPattern = "?" + carry + "sort=%s&dir=%s" and
// Pagination HrefPattern = "?" + carry + "p=%d"). A request-derived value
// like ?q=a%26b must never inject fmt directives into those Sprintf
// patterns: today Encode()'s own %XX triples make fmt parse %26 as
// width+verb, so the sort verbs misalign, the page verb reports
// %!d(MISSING), and every sort/pagination link on the page navigates to a
// corrupted URL (silent filter/state loss on the CRUD list surface).
func TestListCarryQueryKeepsFmtVerbs(t *testing.T) {
	for _, tc := range []struct{ name, raw string }{
		{"search-amp", "/orders?q=a%26b"},
		{"search-percent", "/orders?q=50%25+off"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := &stubSource{rows: []map[string]any{
				{"id": "1", "name": "a"}, {"id": "2", "name": "b"},
				{"id": "3", "name": "c"}, {"id": "4", "name": "d"},
				{"id": "5", "name": "e"},
			}}
			cfg := Config{
				Entity: "orders", Title: "Orders", Singular: "Order",
				BasePath: "/orders", APIPath: "/api/orders",
				Crud: source, PageSize: 4,
				Fields: []Field{{Key: "name", Label: "Name", Type: "string"}},
			}
			req := httptest.NewRequest("GET", tc.raw, nil)
			s := string(cfg.List(appui.WithRequest(context.Background(), req)))
			// Show the sort-header region, not the page header, as evidence.
			sortAt := strings.Index(s, "ui-data-table__sort")
			hrefRegion := s[:min(len(s), 400)]
			if sortAt >= 0 {
				lo := max(0, sortAt-40)
				hrefRegion = s[lo:min(len(s), sortAt+260)]
			}
			if strings.Contains(s, "%!") {
				t.Errorf("SECURITY: [fmt-carry] request %q: sort/pagination hrefs corrupted by fmt directives injected through the carry; sort href region: %s", tc.raw, hrefRegion)
			}
			if !strings.Contains(s, "sort=name") {
				t.Errorf("SECURITY: [fmt-carry] request %q: column key must land in its own sort= param, got: %s", tc.raw, hrefRegion)
			}
			if !strings.Contains(s, "dir=asc") {
				t.Errorf("SECURITY: [fmt-carry] request %q: direction must land in its own dir= param, got: %s", tc.raw, hrefRegion)
			}
			if !strings.Contains(s, "p=2") {
				t.Errorf("SECURITY: [fmt-carry] request %q: page-2 link must keep its p= param (5 rows / page size 4 = 2 pages), got: %s", tc.raw, hrefRegion)
			}
		})
	}
}
