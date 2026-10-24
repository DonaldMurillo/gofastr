package sitesearch

// The search page: Screen renders the battery's results as a page of
// its own, with framework/ui components only. A reader without script
// searches through its GET form and pages through its links; with
// script, the sitesearch-page module updates the list in place as the
// reader types. The host mounts it the way it mounts any other screen:
//
//	site.Register("/search", sitesearch.Screen(bat), layout)

import (
	"context"
	"strconv"
	"strings"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

const (
	// perPage is how many results a search page shows.
	perPage = 20
	// maxPage bounds the page parameter, so the offset it makes cannot
	// overflow.
	maxPage = 10000
)

// ScreenOption configures the screen Screen builds.
type ScreenOption func(*searchScreen)

// ScreenPath names the path the host mounts the screen on ("/search",
// the default). The form submits to it and pagination links to it. It
// panics on a path that is not absolute.
func ScreenPath(path string) ScreenOption {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		panic("sitesearch: ScreenPath must be an absolute path, got " + strconv.Quote(path))
	}
	return func(s *searchScreen) { s.path = path }
}

// Screen builds the search results page for b, reading the query from
// ?q= and the page from ?page=, 20 results a page. It tells crawlers
// noindex, which also keeps it out of the index.
func Screen(b *Battery, opts ...ScreenOption) *searchScreen {
	s := &searchScreen{b: b, path: "/search"}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// searchScreen is the mounted page. The per-request copy the router
// makes shares b and path, both fixed after construction.
type searchScreen struct {
	component.ContextOnly
	b    *Battery
	path string
}

// ScreenTitle names the page with its heading's word.
func (s *searchScreen) ScreenTitle(ctx context.Context) string { return i18nui.SearchLabel.Text(ctx) }

// ScreenRobots keeps the page out of crawlers and out of the index.
func (s *searchScreen) ScreenRobots() string { return "noindex" }

// RenderCtx renders the form and, for a query, its page of results.
// With no query it never asks for the index: the index build renders
// this page too, and must not wait on itself.
func (s *searchScreen) RenderCtx(ctx context.Context) render.HTML {
	params := coreapp.QueryFromContext(ctx)
	q := params.Get("q")
	if len(q) > maxQueryBytes {
		q = ""
	}
	page, err := strconv.Atoi(params.Get("page"))
	if err != nil || page < 1 {
		page = 1
	}
	page = min(page, maxPage)
	cfg := ui.SearchResultsConfig{Action: s.path, Query: q, Page: page, PerPage: perPage, Ctx: ctx}
	if strings.TrimSpace(q) != "" {
		if idx, err := s.b.Index(ctx); err == nil {
			results, total := idx.SearchPage(q, (page-1)*perPage, perPage)
			cfg.Results, cfg.Total = uiResults(results), total
		}
	}
	// A padded page column: the gutter against the viewport, which an
	// app frame's content area supplies itself.
	return ui.Container(ui.ContainerConfig{Width: ui.ContainerPage, Pad: ui.ContainerPadPage},
		ui.Stack(ui.StackConfig{Gap: ui.GapLG},
			ui.PageHeader(ui.PageHeaderConfig{Title: i18nui.SearchLabel.Text(ctx)}),
			ui.SearchResults(cfg),
		),
	)
}

// uiResults maps the battery's results onto the kit's.
func uiResults(rs []Result) []ui.SearchResult {
	out := make([]ui.SearchResult, len(rs))
	for i, r := range rs {
		out[i] = ui.SearchResult{URL: r.URL, Title: r.Title, Group: r.Group, Excerpt: uiSegments(r.Excerpt)}
		for _, sec := range r.Sections {
			u := r.URL
			if sec.ID != "" {
				u += "#" + sec.ID
			}
			out[i].Sections = append(out[i].Sections, ui.SearchSection{URL: u, Heading: sec.Heading, Excerpt: uiSegments(sec.Excerpt)})
		}
	}
	return out
}

func uiSegments(segs []Segment) []ui.SearchSegment {
	out := make([]ui.SearchSegment, len(segs))
	for i, s := range segs {
		out[i] = ui.SearchSegment{Text: s.Text, Hit: s.Hit}
	}
	return out
}
