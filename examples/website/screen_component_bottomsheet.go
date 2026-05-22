package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type BottomSheetScreen struct{}

func (s *BottomSheetScreen) ScreenTitle() string { return "Bottom Sheet" }
func (s *BottomSheetScreen) ScreenDescription() string {
	return "Mobile-friendly bottom-anchored variant of Drawer."
}
func (s *BottomSheetScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *BottomSheetScreen) Render() render.HTML {
	openBtn := ui.Button(ui.ButtonConfig{
		Label: "Open bottom sheet",
		Attrs: html.Attrs{"data-fui-open": "components-bottomsheet-demo"},
	})

	src := `// Register once at app startup.
sheet := preset.BottomSheet("share-sheet").
    Hidden().
    Pages("/post/:id").
    LabelledBy("share-sheet-title").
    Slot("body", shareSheetBody{}).
    Build()
widget.Mount(r, &sheet)

// Trigger anywhere:
<button data-fui-open="share-sheet">Share</button>`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Bottom Sheet")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"preset.BottomSheet is a bottom-edge sibling of Drawer. Same dismiss affordances (backdrop, ESC, click-outside, focus-trap), mounted on the bottom edge with a slide-from-bottom animation. Designed for mobile detail panels, share sheets, action menus — anywhere a side-drawer would feel wrong on small screens.")),
		demoFrame(openBtn, src),
		render.Tag("p", nil, render.Text(
			"Pointer drag-to-dismiss is wired: drag the handle bar down past ~80px (or release with downward velocity) to close. Shorter drags snap back. ESC + backdrop click remain available for keyboard + pointer dismiss.")),
	)
}
