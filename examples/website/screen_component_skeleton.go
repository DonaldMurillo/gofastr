package main

import (
	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/html"
	"github.com/gofastr/gofastr/core-ui/patterns/skeleton"
	"github.com/gofastr/gofastr/core/render"
)

type SkeletonScreen struct{}

func (s *SkeletonScreen) ScreenTitle() string        { return "Skeleton" }
func (s *SkeletonScreen) ScreenDescription() string  { return "Pure-CSS shimmer placeholders." }
func (s *SkeletonScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *SkeletonScreen) Render() render.HTML {
	// CSP-strict environments forbid inline style="…" attributes, so
	// the demo sticks to defaults. Hosts that need arbitrary
	// Width/Height can still set them; the skeleton package emits
	// them as inline style which works under permissive CSP. See
	// core-ui/ARCHITECTURE.md for the strict-CSP contract.
	multiline := skeleton.New(skeleton.Config{Variant: skeleton.Line, Count: 4})
	block := skeleton.New(skeleton.Config{Variant: skeleton.Block})
	circle := skeleton.New(skeleton.Config{Variant: skeleton.Circle})

	avatarRow := render.Tag("div", map[string]string{"class": "demo-row-tight"},
		circle,
		render.Tag("div", map[string]string{"class": "demo-flex-1"},
			skeleton.New(skeleton.Config{Variant: skeleton.Line}),
		),
	)

	stack := render.Tag("div", map[string]string{"class": "demo-stack demo-stack--lg"},
		render.Tag("div", nil,
			render.Tag("strong", nil, render.Text("Multi-line paragraph")),
			multiline,
		),
		render.Tag("div", nil,
			render.Tag("strong", nil, render.Text("Block (e.g. card cover)")),
			block,
		),
		render.Tag("div", nil,
			render.Tag("strong", nil, render.Text("Avatar + label")),
			avatarRow,
		),
	)

	source := `skeleton.New(skeleton.Config{Variant: skeleton.Line, Count: 4})
skeleton.New(skeleton.Config{Variant: skeleton.Block, Height: "120px"})
skeleton.New(skeleton.Config{Variant: skeleton.Circle, Width: "3rem"})`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Skeleton")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Three shimmer placeholders rendered with pure CSS. Variants: Line, Block, Circle.")),
		demoFrame(stack, source),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Accessibility")),
		render.Tag("p", nil, render.Text(
			"Every skeleton is aria-hidden=\"true\". Surface the loading state on the parent container (e.g. aria-busy=\"true\") so screen readers announce it once, not for every shimmer block.")),
	)
}
