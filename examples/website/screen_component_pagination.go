package main

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/elements"
	"github.com/gofastr/gofastr/core-ui/pagination"
	"github.com/gofastr/gofastr/core/render"
)

// PaginationScreen renders the pagination demo page.
//
// The "Live" demo is an island — clicking a page button fires an RPC
// to /islands/pagination-demo/page, which returns the new pagination
// HTML. The runtime swaps it into the data-fui-signal wrapper. URL is
// updated via the per-button data-fui-push-state attribute (no
// re-fetch). Refresh / share-link / browser-back all round-trip
// through the URL → Load(ctx) reads ?p → SSR ships the right page.
//
// See core-ui/ARCHITECTURE.md "URL params are the source of truth".
type PaginationScreen struct {
	page int
}

func (s *PaginationScreen) ScreenTitle() string        { return "Pagination" }
func (s *PaginationScreen) ScreenDescription() string  { return "Numeric pagination island — click swaps just the island, no full reload." }
func (s *PaginationScreen) ScreenType() app.ScreenType { return app.ScreenPage }

const paginationDemoSignal = "pagination-demo-rows"
const paginationDemoEndpoint = "/islands/pagination-demo/page"

func (s *PaginationScreen) Load(ctx context.Context) error {
	s.page = clampPage(app.QueryFromContext(ctx).Get("p"), 5)
	return nil
}

// renderLivePagination produces the island content (the pagination nav
// itself). Reused by both the initial SSR and the RPC handler so the
// markup is identical — that's how the runtime can swap it in place.
func renderLivePagination(page int) render.HTML {
	return pagination.New(pagination.Config{
		Total:          5,
		Current:        page,
		HrefPattern:    "?p=%d",
		IslandSignal:   paginationDemoSignal,
		IslandEndpoint: paginationDemoEndpoint,
	})
}

func (s *PaginationScreen) Render() render.HTML {
	livePage := s.page

	// Wrap the live demo in the signal-bound container — the RPC
	// response replaces this innerHTML on every click.
	liveIsland := render.Tag("div",
		map[string]string{
			"data-fui-signal":      paginationDemoSignal,
			"data-fui-signal-mode": "html",
		},
		renderLivePagination(livePage),
	)

	small := pagination.New(pagination.Config{
		Total: 5, Current: 3, HrefPattern: "?p=%d",
	})
	mid := pagination.New(pagination.Config{
		Total: 20, Current: 4, HrefPattern: "?p=%d",
	})
	large := pagination.New(pagination.Config{
		Total: 100, Current: 47, HrefPattern: "?p=%d", Window: 2,
	})
	noPrevNext := pagination.New(pagination.Config{
		Total: 12, Current: 6, HrefPattern: "?p=%d", OmitPrevNext: true,
	})
	atFirst := pagination.New(pagination.Config{
		Total: 8, Current: 1, HrefPattern: "?p=%d",
	})

	stack := render.Tag("div", map[string]string{"style": "display:grid;gap:1rem"},
		labeledRow(
			"Live (island) — click a page button. Server returns just this island; no full reload, URL updates via pushState.",
			liveIsland),
		labeledRow("5 pages, current=3 (no ellipsis)", small),
		labeledRow("8 pages, current=1 (Previous disabled at boundary)", atFirst),
		labeledRow("20 pages, current=4 (single ellipsis)", mid),
		labeledRow("100 pages, current=47, window=2 (two ellipses)", large),
		labeledRow("Numbers only — OmitPrevNext: true", noPrevNext),
	)

	source := `// Live demo (island mode):
pagination.New(pagination.Config{
    Total: 5, Current: page, HrefPattern: "?p=%d",
    IslandSignal:   "pagination-demo-rows",
    IslandEndpoint: "/islands/pagination-demo/page",
})

// Wrap in a signal-bound container; the RPC response replaces innerHTML.
<div data-fui-signal="pagination-demo-rows" data-fui-signal-mode="html">
    {pagination}
</div>

// Server-side handler returns the new island HTML on each click:
func paginationIslandHandler(w http.ResponseWriter, r *http.Request) {
    page := atoi(r.URL.Query().Get("p"), 1)
    render.RespondHTML(w, renderLivePagination(page))
}`

	return render.Tag("main", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		elements.Heading(elements.HeadingConfig{Level: 1}, render.Text("Pagination")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"A numeric pagination nav. Renders as plain <a href> links by default; pass IslandSignal+IslandEndpoint to switch to island mode (data-fui-rpc buttons that swap just the island, no page reload).")),
		demoFrame(stack, source),
	)
}

// PaginationIslandHandler serves /islands/pagination-demo/page.
// Returns the new island HTML for the requested page. The runtime
// applies the response to the data-fui-signal wrapper.
func PaginationIslandHandler(w http.ResponseWriter, r *http.Request) {
	page := clampPage(r.URL.Query().Get("p"), 5)
	render.RespondHTML(w, renderLivePagination(page))
}

func clampPage(s string, max int) int {
	v, err := strconv.Atoi(s)
	if err != nil || v < 1 {
		return 1
	}
	if v > max {
		return max
	}
	return v
}

func labeledRow(label string, body render.HTML) render.HTML {
	return render.Tag("div", nil,
		render.Tag("strong", map[string]string{"style": "display:block;margin-bottom:0.5rem;font-size:0.85rem;color:var(--color-text-muted)"},
			render.Text(label)),
		body,
	)
}
