package main

import (
	"context"
	"strconv"

	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/elements"
	"github.com/gofastr/gofastr/core-ui/pagination"
	"github.com/gofastr/gofastr/core/render"
)

// PaginationScreen now reads ?p from the URL and threads it into the
// "Live" demo so clicking a page link actually advances the table —
// the screen Load() pulls the value via app.QueryFromContext.
type PaginationScreen struct {
	page int
}

func (s *PaginationScreen) ScreenTitle() string        { return "Pagination" }
func (s *PaginationScreen) ScreenDescription() string  { return "Numeric pagination with ARIA." }
func (s *PaginationScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *PaginationScreen) Load(ctx context.Context) error {
	s.page = 1
	if v, err := strconv.Atoi(app.QueryFromContext(ctx).Get("p")); err == nil && v > 0 {
		s.page = v
	}
	return nil
}

func (s *PaginationScreen) Render() render.HTML {
	livePage := s.page
	if livePage < 1 || livePage > 5 {
		livePage = 1
	}
	live := pagination.New(pagination.Config{
		Total: 5, Current: livePage, HrefPattern: "?p=%d",
	})
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
			"Live — current page is "+strconv.Itoa(livePage)+" (click any number to navigate)",
			live),
		labeledRow("5 pages, current=3 (no ellipsis)", small),
		labeledRow("8 pages, current=1 (Previous disabled at boundary)", atFirst),
		labeledRow("20 pages, current=4 (single ellipsis)", mid),
		labeledRow("100 pages, current=47, window=2 (two ellipses)", large),
		labeledRow("Numbers only — OmitPrevNext: true", noPrevNext),
	)

	source := `pagination.New(pagination.Config{
    Total: 100, Current: 47,
    HrefPattern: "/items?page=%d",
    Window: 2,
})`

	return render.Tag("main", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		elements.Heading(elements.HeadingConfig{Level: 1}, render.Text("Pagination")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"A numeric pagination nav. Always shows first, last, a window around the current page, and ellipses for gaps.")),
		demoFrame(stack, source),
	)
}

func labeledRow(label string, body render.HTML) render.HTML {
	return render.Tag("div", nil,
		render.Tag("strong", map[string]string{"style": "display:block;margin-bottom:0.5rem;font-size:0.85rem;color:var(--color-text-muted)"},
			render.Text(label)),
		body,
	)
}
