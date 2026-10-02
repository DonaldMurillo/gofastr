package headless

import (
	"net/url"
	"strings"
	"testing"
)

// The pager's own contract, the table's twin: typed page props, the
// window, the ends, and the hooks. The universal sweeps in
// harness_test.go cover the spec; these are the promises specific to
// turning pages.

// Every href is built through net/url from the typed props: the
// carried query survives a page turn, the page parameter is replaced
// rather than appended, and the parameter's name is the caller's.
func TestPaginationBuildsItsHrefsThroughNetURL(t *testing.T) {
	got := Pagination(PaginationProps{
		Page: 3, Pages: 9, Path: "/apps",
		Query:     url.Values{"q": {"caf\xc3\xa9"}, "page": {"7"}, "sort": {"name"}},
		PageParam: "page",
		AriaLabel: "Pages",
	}, nil)
	// The carried page parameter is the one a Set replaces: page=7 in
	// the carry must not survive beside the anchor's own page.
	hasNot(t, got, "page=7", "the carried page value survived beside the replaced one")
	for _, want := range []string{
		`href="/apps?page=1&amp;q=caf%C3%A9&amp;sort=name"`,
		`href="/apps?page=2&amp;q=caf%C3%A9&amp;sort=name"`,
		`href="/apps?page=4&amp;q=caf%C3%A9&amp;sort=name"`,
	} {
		has(t, got, want, "the carried query did not survive the page turn byte-for-byte")
	}
	if n := strings.Count(string(got), "q=caf%C3%A9"); n < 4 {
		t.Errorf("the carry reached only %d anchors, want at least the four around page 3", n)
	}

	// The parameter's name is the caller's: a pager on a screen whose
	// page is ?p= does not emit ?page=.
	named := Pagination(PaginationProps{Page: 1, Pages: 2, Path: "/apps",
		PageParam: "p", AriaLabel: "Pages"}, nil)
	has(t, named, `href="/apps?p=2"`, "the page parameter did not take the caller's name")

	// No Path at all is the current document: a relative "?query" href,
	// the shape a screen's own pager carries.
	rel := Pagination(PaginationProps{Page: 1, Pages: 2,
		Query: url.Values{"q": {"x"}}, AriaLabel: "Pages"}, nil)
	has(t, rel, `href="?p=2&amp;q=x"`, "an empty Path did not render the relative query href")
}

// A Path carrying its own query or fragment is refused, exactly as the
// table's is: the carry belongs in Query, where it survives the page
// turn, and a Path query is silently replaced — a value lost with no
// error is the defect the refusal exists to make structural.
func TestPaginationRefusesAPathWithItsOwnQueryOrFragment(t *testing.T) {
	refuse(t, "query or fragment", func() {
		Pagination(PaginationProps{Page: 1, Pages: 2, Path: "/apps?keep=1", AriaLabel: "Pages"}, nil)
	})
	refuse(t, "query or fragment", func() {
		Pagination(PaginationProps{Page: 1, Pages: 2, Path: "/apps#top", AriaLabel: "Pages"}, nil)
	})
}

// The bounds: no page zero, no page past the end, no empty run.
func TestPaginationRefusesPagesOutsideTheRun(t *testing.T) {
	refuse(t, "outside 1..2", func() {
		Pagination(PaginationProps{Page: 3, Pages: 2, Path: "/x", AriaLabel: "Pages"}, nil)
	})
	refuse(t, "outside 1..2", func() {
		Pagination(PaginationProps{Page: 0, Pages: 2, Path: "/x", AriaLabel: "Pages"}, nil)
	})
	refuse(t, "Pages >= 1", func() {
		Pagination(PaginationProps{Page: 1, Path: "/x", AriaLabel: "Pages"}, nil)
	})
}

// The window: first and last always, the current page's neighbourhood
// of the asked size, and a gap span marking each elision. Small runs
// show whole; a window large enough shows every page.
func TestPaginationWindowsItsPages(t *testing.T) {
	// Default window, middle of a long run: 1 … 4 5 6 … 12.
	mid := Pagination(PaginationProps{Page: 5, Pages: 12, Path: "/a", AriaLabel: "Pages"}, nil)
	for _, want := range []string{
		`>1</a>`, `>4</a>`, `>5</a>`, `>6</a>`, `>12</a>`, `aria-hidden="true">…</a>`,
	} {
		if want == `aria-hidden="true">…</a>` {
			hasNot(t, mid, want, "the gap rendered as an anchor")
			continue
		}
		has(t, mid, want, "the window dropped a page it must show")
	}
	hasNot(t, mid, `>7</a>`, "the default window showed a page outside it")
	if n := strings.Count(string(mid), "…"); n != 2 {
		t.Errorf("a middle window renders two gaps, found %d", n)
	}

	// Window 2 at the left edge: the window grows, the left gap goes.
	wide := Pagination(PaginationProps{Page: 2, Pages: 20, Window: 2, Path: "/a", AriaLabel: "Pages"}, nil)
	for _, want := range []string{`>1</a>`, `>2</a>`, `>3</a>`, `>4</a>`, `>20</a>`} {
		has(t, wide, want, "a window of two each side dropped a page it must show")
	}
	if n := strings.Count(string(wide), "…"); n != 1 {
		t.Errorf("page 2 of 20 at window 2 renders one gap, found %d", n)
	}

	// A window that covers the run shows it whole, no gaps.
	whole := Pagination(PaginationProps{Page: 4, Pages: 5, Window: 3, Path: "/a", AriaLabel: "Pages"}, nil)
	if strings.Contains(string(whole), "…") {
		t.Error("a window larger than the run still elided pages")
	}

	// A short run shows whole at the default window.
	short := Pagination(PaginationProps{Page: 1, Pages: 4, Path: "/a", AriaLabel: "Pages"}, nil)
	if strings.Contains(string(short), "…") {
		t.Error("a four-page run elided pages at the default window")
	}
}

// OmitPrevNext drops the two end anchors entirely; the numbered pages
// and the current-page marker stay.
func TestPaginationOmitsPrevNextWhenAsked(t *testing.T) {
	got := Pagination(PaginationProps{Page: 2, Pages: 3, OmitPrevNext: true,
		Path: "/a", AriaLabel: "Pages", PrevLabel: "Backwards", NextLabel: "Onwards"}, nil)
	has(t, got, `aria-current="page"`, "the current page lost its marker with the ends gone")
	// The labels are unique to this test, so their absence is the
	// ends' absence and not a default the run happened to elide.
	hasNot(t, got, "Backwards", "OmitPrevNext kept the previous anchor")
	hasNot(t, got, "Onwards", "OmitPrevNext kept the next anchor")
}

// The page hook is the island's alone: a plain pager renders no
// data-hui-page, and an island pager renders one on every enabled
// anchor — the ends included, whose page is the one they turn to.
func TestPaginationPageHookRendersOnlyWithAnIsland(t *testing.T) {
	plain := Pagination(PaginationProps{Page: 2, Pages: 3, Path: "/a", AriaLabel: "Pages"}, nil)
	hasNot(t, plain, "data-hui-page", "a plain pager rendered a hook nothing reads")

	island := Pagination(PaginationProps{Page: 2, Pages: 3, Path: "/a", AriaLabel: "Pages",
		Island: fixtureIsland}, nil)
	for _, want := range []string{
		`data-hui-page="1"`, `data-hui-page="2"`, `data-hui-page="3"`,
	} {
		has(t, island, want, "an island pager's anchors must all carry their page")
	}
	// The ends carry the page they turn to: Previous says 1, Next 3.
	// Attributes render sorted, so the hook sits directly beside the
	// href it belongs to.
	has(t, island, `data-hui-page="1" href="/a?p=1"`, "the Previous end did not name the page it turns to")
	has(t, island, `data-hui-page="3" href="/a?p=3"`, "the Next end did not name the page it turns to")
}

// A request-derived carry can corrupt neither the plain hrefs nor the
// island sinks. The core pattern's security tests pinned this against
// a "%d" HrefPattern Sprintf'd into data-fui-push-state and the RPC
// URL; under the typed props every one of those URLs is built through
// net/url from the scrubbed carry, so the percent signs in a hostile
// search are bytes of a value, never verbs — but the property is the
// same one and stays pinned here.
func TestPaginationCarryCannotCorruptTheIslandSinks(t *testing.T) {
	for _, tc := range []struct{ name, search string }{
		{"amp", "a&b"},
		{"percent", "50% off"},
		{"utf8", "café"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Pagination(PaginationProps{Page: 1, Pages: 3,
				Query:     url.Values{"q": {tc.search}},
				AriaLabel: "Pages", Island: fixtureIsland}, nil)
			s := string(got)
			if strings.Contains(s, "%!") {
				t.Errorf("a href or island URL carries a fmt directive: %s", s[:min(len(s), 400)])
			}
			for _, want := range []string{
				// The page anchor's three destinations all keep the
				// page parameter beside the carried search.
				`href="?p=2&amp;q=`,
				`data-fui-rpc="/island/apps?p=2&amp;q=`,
				`data-fui-push-state="?p=2&amp;q=`,
			} {
				has(t, got, want, "an island sink lost its page parameter")
			}
		})
	}
}
