package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type GalleryScreen struct{}

func (s *GalleryScreen) ScreenTitle() string { return "Gallery" }
func (s *GalleryScreen) ScreenDescription() string {
	return "Thumbnail surface; Grid / Strip / Masonry variants."
}
func (s *GalleryScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *GalleryScreen) Render() render.HTML {
	makeSrc := func(hue int, label string) string {
		return "data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 320 240'>" +
			"<rect width='320' height='240' fill='hsl(" + itoaScreen(hue) + ", 60%, 50%)'/>" +
			"<text x='50%25' y='52%25' text-anchor='middle' font-size='24' fill='white' font-family='system-ui'>" + label + "</text></svg>"
	}
	items := []ui.GalleryItem{
		{Src: makeSrc(220, "1"), Alt: "Mountain at dawn", Caption: "Sierra Nevada, 5:42 AM."},
		{Src: makeSrc(140, "2"), Alt: "Misty forest", Caption: "Pacific Northwest, post-rain."},
		{Src: makeSrc(40, "3"), Alt: "Desert sunset", Caption: "Mojave, golden hour."},
		{Src: makeSrc(0, "4"), Alt: "Coral reef", Caption: "Great Barrier Reef."},
		{Src: makeSrc(280, "5"), Alt: "Aurora", Caption: "Tromsø, 02:14."},
		{Src: makeSrc(90, "6"), Alt: "Volcano", Caption: "Iceland, sulphur plume."},
	}

	grid := ui.Gallery(ui.GalleryConfig{
		Items: items, Columns: 3,
	})
	strip := ui.Gallery(ui.GalleryConfig{
		Variant: ui.GalleryStrip, Items: items, Gap: ui.GapSM,
	})
	masonry := ui.Gallery(ui.GalleryConfig{
		Variant: ui.GalleryMasonry, Items: items, Columns: 3,
		CaptionMode: ui.GalleryCaptionOverlay,
	})

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Gallery")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Standalone thumbnail surface. Three variants — Grid (CSS Grid, configurable Columns + Gap), Strip (scroll-snap row), Masonry (CSS columns flow). Without a Lightbox target each item is a plain link; set Lightbox: \"<name>\" and items become triggers for the paired Lightbox modal.")),
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Grid (default)")),
		demoFrame(grid, `ui.Gallery(ui.GalleryConfig{
    Items: photos, Columns: 3,
})`),
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Strip — horizontal scroll-snap")),
		demoFrame(strip, `ui.Gallery(ui.GalleryConfig{
    Variant: ui.GalleryStrip,
    Items:   photos,
    Gap:     ui.GapSM,
})`),
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Masonry with hover captions")),
		demoFrame(masonry, `ui.Gallery(ui.GalleryConfig{
    Variant:     ui.GalleryMasonry,
    CaptionMode: ui.GalleryCaptionOverlay,
    Items:       photos,
    Columns:     3,
})`),
	)
}
