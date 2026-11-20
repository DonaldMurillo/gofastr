package sitesearch

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
	perPage = 20
	maxPage = 10000
)

// ScreenOption configures the screen Screen builds.
type ScreenOption func(*searchScreen)

// ScreenPath names the path the host mounts the screen on ("/search", the
// default).
func ScreenPath(path string) ScreenOption {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		panic("sitesearch: ScreenPath must be an absolute path, got " + strconv.Quote(path))
	}
	return func(s *searchScreen) { s.path = path }
}

// Screen builds the search results page for b, reading the query from ?q=
// and the page from ?page=, 20 results a page.
func Screen(b *Battery, opts ...ScreenOption) *searchScreen {
	s := &searchScreen{b: b, path: "/search"}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

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
func (s *searchScreen) RenderCtx(ctx context.Context) render.HTML {
	params := coreapp.QueryFromContext(ctx)
	q := params.Get("q")
	tooLong := len(q) > maxQueryBytes
	if tooLong {
		q = ""
	}
	page, err := strconv.Atoi(params.Get("page"))
	if err != nil || page < 1 {
		page = 1
	}
	page = min(page, maxPage)
	cfg := ui.SearchResultsConfig{Action: s.path, Query: q, Page: page, PerPage: perPage, Ctx: ctx}
	body := []render.HTML{ui.PageHeader(ui.PageHeaderConfig{Title: i18nui.SearchLabel.Text(ctx)})}
	if tooLong {
		body = append(body, ui.Callout(ui.CalloutConfig{Variant: ui.StatusWarning}, render.Text(reasonQueryTooLong.Text(ctx))))
	}
	if strings.TrimSpace(q) != "" {
		idx, err := s.b.Index(ctx)
		if err != nil {
			body = append(body, ui.Callout(ui.CalloutConfig{Variant: ui.StatusDanger}, render.Text(reasonIndexUnavailable.Text(ctx))))
		} else {
			results, total := idx.SearchPage(q, (page-1)*perPage, perPage)
			cfg.Results, cfg.Total = uiResults(results), total
		}
	}
	return ui.Container(ui.ContainerConfig{Width: ui.ContainerPage, Pad: ui.ContainerPadPage},
		ui.Stack(ui.StackConfig{Gap: ui.GapLG}, append(body, ui.SearchResults(cfg))...),
	)
}

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
