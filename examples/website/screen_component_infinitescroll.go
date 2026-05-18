package main

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/patterns/infinitescroll"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type InfiniteScrollScreen struct{}

func (s *InfiniteScrollScreen) ScreenTitle() string {
	return "Infinite Scroll"
}
func (s *InfiniteScrollScreen) ScreenDescription() string {
	return "Sentinel-based infinite scroll with <noscript> fallback."
}
func (s *InfiniteScrollScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *InfiniteScrollScreen) Render() render.HTML {
	items := []render.HTML{}
	for i := 1; i <= 5; i++ {
		items = append(items, render.Tag("article",
			map[string]string{"class": "demo-feed-item"},
			html.Heading(html.HeadingConfig{Level: 3}, render.Text("Post "+strconv.Itoa(i))),
			html.Paragraph(html.TextConfig{}, render.Text("Initial SSR-rendered feed entry — scroll inside the box to lazy-load more.")),
		))
	}
	feed := infinitescroll.Render(infinitescroll.Config{
		ID:        "feed-demo",
		RPCPath:   "/islands/new-components/feed-page",
		AriaLabel: "Demo activity feed",
		Items:     items,
		Cursor:    "5",
	})
	// Wrap the feed in a fixed-height scroll container so the demo
	// shows what infinite scroll actually does — without this, the
	// PAGE scrolls, the feed just expands, and the drain loop fires
	// all pages on first paint because the sentinel is always in
	// the page viewport.
	demo := render.Tag("div", map[string]string{"class": "demo-infinite-frame"}, feed)
	src := `infinitescroll.Render(infinitescroll.Config{
    ID:        "feed",
    RPCPath:   "/feed/page",
    AriaLabel: "Activity feed",
    Items:     firstPageHTML, // SSR-rendered first page
    Cursor:    "page-1-end",
})

// Server handler appends HTML, sets next cursor:
func feedPageHandler(w http.ResponseWriter, r *http.Request) {
    cursor := r.FormValue("cursor")
    // … fetch next page …
    w.Header().Set("X-Gofastr-Infinite-Cursor", nextCursor) // empty = end
    w.Write(htmlFragment)
}`
	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Infinite Scroll")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"role=\"feed\" container with IntersectionObserver-driven lazy fetch. aria-busy toggles during each request. <noscript> ships a \"Load more\" form so non-JS users get the same data, keyboard-operable.")),
		demoFrame(demo, src),
	)
}
