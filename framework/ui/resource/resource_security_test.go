package resource

import (
	"context"
	stdhtml "html"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
)

// TestListCarryQueryKeepsFmtVerbs pins the production carry builders
// themselves (resource.go table(): Query = the search and active facets
// as url.Values, handed to the typed DataTable sort props, and the
// pagination HrefPattern = "?" + Query.Encode() + "&p=%d"). A
// request-derived value like ?q=a%26b must never corrupt those hrefs:
// the sort anchors are built by the primitive through net/url, and the
// pager pattern is substituted by pagination's strings.Replace, never
// fmt — Encode()'s own %XX triples would read as flag/width/verb to
// Sprintf and every link on the page would navigate to a corrupted URL
// (silent filter/state loss on the CRUD list surface).
func TestListCarryQueryKeepsFmtVerbs(t *testing.T) {
	for _, tc := range []struct{ name, raw, search string }{
		{"search-amp", "/orders?q=a%26b", "a&b"},
		{"search-percent", "/orders?q=50%25+off", "50% off"},
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
			if strings.Contains(s, "%!") {
				t.Errorf("SECURITY: [fmt-carry] request %q: a href on the page carries a fmt directive: %s", tc.raw, s[:min(len(s), 400)])
			}
			// The sort anchor and the page-2 link are parsed back, not
			// matched as substrings: a "sort=name" anywhere on the page
			// would satisfy a substring, and the property is that THESE
			// hrefs carry the search beside their own parameters.
			sortHref := sortAnchorHref.FindStringSubmatch(s)
			if sortHref == nil {
				t.Fatalf("SECURITY: [fmt-carry] request %q: no sort anchor rendered", tc.raw)
			}
			sq := parsedQuery(t, sortHref[1])
			if sq.Get("q") != tc.search {
				t.Errorf("SECURITY: [fmt-carry] request %q: the sort href lost or corrupted the search: q=%q want %q", tc.raw, sq.Get("q"), tc.search)
			}
			if sq.Get("sort") != "name" || sq.Get("dir") != "asc" || len(sq["sort"]) != 1 {
				t.Errorf("SECURITY: [fmt-carry] request %q: the sort href's own parameters are wrong: %v", tc.raw, sq)
			}
			var pq url.Values
			for _, m := range anyHref.FindAllStringSubmatch(s, -1) {
				if q := parsedQuery(t, m[1]); q.Get("p") == "2" {
					pq = q
					break
				}
			}
			if pq == nil {
				t.Fatalf("SECURITY: [fmt-carry] request %q: no page-2 link rendered (5 rows / page size 4 = 2 pages)", tc.raw)
			}
			if pq.Get("q") != tc.search {
				t.Errorf("SECURITY: [fmt-carry] request %q: the page-2 href lost or corrupted the search: q=%q want %q", tc.raw, pq.Get("q"), tc.search)
			}
		})
	}
}

// sortAnchorHref matches the DataTable's sort anchor; attributes render
// sorted, so class precedes href. anyHref matches every href on the
// page; the page-2 link is the one whose parsed query says p=2.
var (
	sortAnchorHref = regexp.MustCompile(`<a class="ui-data-table__sort" href="([^"]*)"`)
	anyHref        = regexp.MustCompile(`href="([^"]*)"`)
)

// parsedQuery unescapes an attribute value and parses its query, so the
// assertions read values rather than byte order.
func parsedQuery(t *testing.T, href string) url.Values {
	t.Helper()
	u, err := url.Parse(stdhtml.UnescapeString(href))
	if err != nil {
		t.Fatalf("href %q does not parse: %v", href, err)
	}
	return u.Query()
}
