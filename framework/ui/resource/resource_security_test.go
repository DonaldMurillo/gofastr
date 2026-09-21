package resource

import (
	"context"
	"errors"
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
// pager = the same carry plus the active sort, handed to the typed
// pager's Query with p replaced). A request-derived value like
// ?q=a%26b must never corrupt those hrefs: both are built by the
// primitive through net/url, never fmt or a pattern string —
// Encode()'s own %XX triples would read as flag/width/verb to
// Sprintf and every link on the page would navigate to a corrupted
// URL (silent filter/state loss on the CRUD list surface).
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

// currentPageAnchor finds the pager anchor the reader is on: the one
// carrying aria-current, its href parsed back.
var currentAnchorHref = regexp.MustCompile(`<a aria-current="page" href="([^"]*)"`)

// TestPageOutOfRangeIsTheLastPage pins the request-facing clamp: ?p=
// beyond the run is a URL anyone can type, and the typed pager refuses
// a page outside 1..Pages — the screen must answer with the last
// page's rows under a pager whose current page IS the last page, not a
// 500 (the panic escapes List otherwise and fails this test). ?p=0 and
// negative pages are page 1.
func TestPageOutOfRangeIsTheLastPage(t *testing.T) {
	fiveRows := []map[string]any{
		{"id": "1", "name": "a"}, {"id": "2", "name": "b"},
		{"id": "3", "name": "c"}, {"id": "4", "name": "d"},
		{"id": "5", "name": "e"},
	}
	for _, tc := range []struct {
		raw       string
		wantPage  string
		wantFirst string
	}{
		{"/orders?p=999", "2", "e"}, // 5 rows / size 4 → page 2 holds "e"
		{"/orders?p=2", "2", "e"},
		{"/orders?p=0", "1", "a"},
		{"/orders?p=-3", "1", "a"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			cfg := Config{
				Entity: "orders", Title: "Orders", Singular: "Order",
				BasePath: "/orders", APIPath: "/api/orders",
				Crud: &stubSource{rows: fiveRows}, PageSize: 4,
				Fields: []Field{{Key: "name", Label: "Name", Type: "string"}},
			}
			req := httptest.NewRequest("GET", tc.raw, nil)
			s := string(cfg.List(appui.WithRequest(context.Background(), req)))
			if !strings.Contains(s, ">"+tc.wantFirst+"<") {
				t.Fatalf("[page-clamp] request %q did not render page %s's first row %q:\n%s", tc.raw, tc.wantPage, tc.wantFirst, s[:min(len(s), 400)])
			}
			m := currentAnchorHref.FindStringSubmatch(s)
			if m == nil {
				t.Fatalf("[page-clamp] request %q rendered no current page anchor:\n%s", tc.raw, s[:min(len(s), 400)])
			}
			if q := parsedQuery(t, m[1]); q.Get("p") != tc.wantPage {
				t.Fatalf("[page-clamp] request %q: the pager's current page is %q, want %q", tc.raw, q.Get("p"), tc.wantPage)
			}
		})
	}
}

// TestAFailedCountDoesNotClampThePage: the clamp needs a real run to
// clamp into. When CountAll fails the total is 0 with an error, and a
// zero total must not turn ?p=3 into page 1's rows under a URL that
// says page 3: the rows are fetched at the page asked for, and no
// pager renders, so nothing is refused either.
func TestAFailedCountDoesNotClampThePage(t *testing.T) {
	source := &stubSource{rows: []map[string]any{{"id": "1", "name": "a"}}, countErr: errors.New("count refused")}
	cfg := Config{
		Entity: "orders", Title: "Orders", Singular: "Order",
		BasePath: "/orders", APIPath: "/api/orders",
		Crud: source, PageSize: 4,
		Fields: []Field{{Key: "name", Label: "Name", Type: "string"}},
	}
	req := httptest.NewRequest("GET", "/orders?p=3", nil)
	s := string(cfg.List(appui.WithRequest(context.Background(), req)))
	if len(source.listCalls) != 1 {
		t.Fatalf("[count-error] ListAll calls = %d, want 1", len(source.listCalls))
	}
	if got, want := source.listCalls[0].Offset, 8; got != want {
		t.Fatalf("[count-error] rows fetched at offset %d, want page 3's %d: a failed count clamped the page", got, want)
	}
	if strings.Contains(s, `aria-current="page"`) {
		t.Fatalf("[count-error] a pager rendered on an unknown total:\n%s", s[:min(len(s), 400)])
	}
}

// TestPagerCarriesTheActiveSort is defect 10's regression: the
// resource pager's carry kept the search and the facets but dropped
// the active sort, so turning a page silently lost the order the
// reader chose. The page-2 href must carry sort and dir beside the
// search, exactly as the battery's pager does.
func TestPagerCarriesTheActiveSort(t *testing.T) {
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
	req := httptest.NewRequest("GET", "/orders?q=ada&sort=name&dir=desc", nil)
	s := string(cfg.List(appui.WithRequest(context.Background(), req)))
	var pq url.Values
	for _, m := range anyHref.FindAllStringSubmatch(s, -1) {
		if q := parsedQuery(t, m[1]); q.Get("p") == "2" {
			pq = q
			break
		}
	}
	if pq == nil {
		t.Fatalf("[pager-sort-carry] no page-2 link rendered (5 rows / page size 4 = 2 pages):\n%s", s[:min(len(s), 400)])
	}
	if pq.Get("sort") != "name" || pq.Get("dir") != "desc" || len(pq["sort"]) != 1 || len(pq["dir"]) != 1 {
		t.Errorf("[pager-sort-carry] the page-2 href lost the active sort: sort=%q dir=%q values=%v",
			pq.Get("sort"), pq.Get("dir"), pq)
	}
	if pq.Get("q") != "ada" {
		t.Errorf("[pager-sort-carry] the page-2 href lost the search: q=%q", pq.Get("q"))
	}
}
