package main

import (
	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/elements"
	"github.com/gofastr/gofastr/core-ui/skeleton"
	"github.com/gofastr/gofastr/core/render"
)

type SkeletonScreen struct{}

func (s *SkeletonScreen) ScreenTitle() string        { return "Skeleton" }
func (s *SkeletonScreen) ScreenDescription() string  { return "Pure-CSS shimmer placeholders." }
func (s *SkeletonScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *SkeletonScreen) Render() render.HTML {
	multiline := skeleton.New(skeleton.Config{Variant: skeleton.Line, Count: 4})
	block := skeleton.New(skeleton.Config{Variant: skeleton.Block, Width: "100%", Height: "120px"})
	circle := skeleton.New(skeleton.Config{Variant: skeleton.Circle, Width: "3rem"})

	avatarRow := render.Tag("div", map[string]string{"style": "display:flex;gap:0.75rem;align-items:center"},
		circle,
		render.Tag("div", map[string]string{"style": "flex:1"},
			skeleton.New(skeleton.Config{Variant: skeleton.Line, Width: "40%"}),
		),
	)

	stack := render.Tag("div", map[string]string{"style": "display:grid;gap:1.25rem"},
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

	return render.Tag("main", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		elements.Heading(elements.HeadingConfig{Level: 1}, render.Text("Skeleton")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Three shimmer placeholders rendered with pure CSS. Variants: Line, Block, Circle.")),
		demoFrame(stack, source),

		elements.Heading(elements.HeadingConfig{Level: 2}, render.Text("Accessibility")),
		render.Tag("p", nil, render.Text(
			"Every skeleton is aria-hidden=\"true\". Surface the loading state on the parent container (e.g. aria-busy=\"true\") so screen readers announce it once, not for every shimmer block.")),
	)
}
