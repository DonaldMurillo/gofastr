package ui

import (
	"math"
	"strings"
	"testing"
)

func searchFixture() SearchResultsConfig {
	return SearchResultsConfig{
		Action: "/search", Query: "form", Total: 1, Page: 1, PerPage: 20,
		Results: []SearchResult{{
			URL: "/docs/forms", Title: "Forms <b>", Group: "Docs",
			Excerpt: []SearchSegment{{Text: "Build a "}, {Text: "form", Hit: true}},
			Sections: []SearchSection{{URL: "/docs/forms#validation", Heading: "Validation",
				Excerpt: []SearchSegment{{Text: "a "}, {Text: "form", Hit: true}, {Text: " field"}}}},
		}},
	}
}

// A result renders as links a reader without script can follow, its
// text escaped and its hits marked.
func TestSearchResultsRendersLinks(t *testing.T) {
	out := string(SearchResults(searchFixture()))
	for _, want := range []string{
		`<form action="/search" class="fui-search__form" method="GET" role="search"`,
		`value="form"`,
		`href="/docs/forms"`, `Forms &lt;b&gt;`,
		`href="/docs/forms#validation"`,
		`Build a <mark>form</mark>`,
		`1 result for “form”`,
		`data-search-page="/search"`, `data-search-page-query="form"`,
		`data-search-page-list`, `data-search-page-status`,
		`autocapitalize="none"`, `autocorrect="off"`, `spellcheck="false"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "data-search-page-pager") {
		t.Error("one page of results drew pagination")
	}
}

// The status line counts the matches in the reader's words, and says
// nothing before a query.
func TestSearchResultsStatusWords(t *testing.T) {
	for _, c := range []struct {
		q     string
		total int
		want  string
	}{
		{"form", 0, "No pages match “form”"},
		{"form", 1, "1 result for “form”"},
		{"form", 12, "12 results for “form”"},
		{"  ", 3, ""},
	} {
		cfg := SearchResultsConfig{Action: "/search", Query: c.q, Total: c.total}
		out := string(SearchResults(cfg))
		i := strings.Index(out, "data-search-page-status")
		j := strings.Index(out[i:], ">")
		k := strings.Index(out[i+j:], "</p>")
		if got := out[i+j+1 : i+j+k]; got != c.want {
			t.Errorf("q %q total %d: status %q, want %q", c.q, c.total, got, c.want)
		}
	}
}

// More matches than a page holds draw pagination that keeps the query.
func TestSearchResultsPaginates(t *testing.T) {
	cfg := searchFixture()
	cfg.Total, cfg.PerPage, cfg.Page = 45, 20, 2
	out := string(SearchResults(cfg))
	if !strings.Contains(out, "data-search-page-pager") {
		t.Fatal("45 results at 20 a page drew no pagination")
	}
	if !strings.Contains(out, `href="/search?page=3&amp;q=form"`) {
		t.Errorf("no next-page link keeping the query in\n%s", out)
	}
}

// The page count must not wrap at the top of int.
func TestSearchResultsHugeTotal(t *testing.T) {
	cfg := searchFixture()
	cfg.Total, cfg.PerPage, cfg.Page = math.MaxInt, 20, 2
	if out := string(SearchResults(cfg)); !strings.Contains(out, "data-search-page-pager") {
		t.Fatal("no pagination for the largest total")
	}
}

// A result URL that is not a safe link renders no live href.
func TestSearchResultsCleansHrefs(t *testing.T) {
	cfg := searchFixture()
	cfg.Results[0].URL = "javascript:alert(1)"
	cfg.Results[0].Sections[0].URL = "data:text/html,x"
	out := string(SearchResults(cfg))
	if strings.Contains(out, "javascript:") || strings.Contains(out, "data:text") {
		t.Errorf("unsafe href rendered:\n%s", out)
	}
}

// ExtraAttrs cannot overwrite the hooks the page module reads.
func TestSearchResultsKeepsHooks(t *testing.T) {
	cfg := searchFixture()
	cfg.ExtraAttrs = map[string]string{"data-test": "x", "data-search-page-query": "evil", "DATA-SEARCH-PAGE": "/evil"}
	out := string(SearchResults(cfg))
	if !strings.Contains(out, `data-test="x"`) {
		t.Error("ExtraAttrs dropped a free key")
	}
	if strings.Contains(out, "evil") {
		t.Errorf("ExtraAttrs overwrote a hook:\n%s", out)
	}
}

func TestSearchResultsRequiresAction(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("no panic without Action")
		}
	}()
	SearchResults(SearchResultsConfig{})
}
