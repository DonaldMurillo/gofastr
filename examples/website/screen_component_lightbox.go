package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// itoaScreen is a tiny inline int→string used by the SVG-data demo
// helpers in Lightbox / Gallery / Carousel screens.
func itoaScreen(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	out := make([]byte, 0, 4)
	for i > 0 {
		out = append([]byte{byte('0' + i%10)}, out...)
		i /= 10
	}
	if neg {
		out = append([]byte{'-'}, out...)
	}
	return string(out)
}

type LightboxScreen struct{}

func (s *LightboxScreen) ScreenTitle() string { return "Lightbox" }
func (s *LightboxScreen) ScreenDescription() string {
	return "Zoom overlay built on preset.Modal; pairs with Gallery."
}
func (s *LightboxScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *LightboxScreen) Render() render.HTML {
	makeSrc := func(hue int, label string) string {
		return "data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 320 240'>" +
			"<rect width='320' height='240' fill='hsl(" + itoaScreen(hue) + ", 60%, 50%)'/>" +
			"<text x='50%25' y='52%25' text-anchor='middle' font-size='28' fill='white' font-family='system-ui'>" + label + "</text></svg>"
	}

	items := []ui.GalleryItem{
		{Src: makeSrc(220, "Mountain"), Alt: "Mountain at dawn", Caption: "Sierra Nevada, 5:42 AM."},
		{Src: makeSrc(140, "Forest"), Alt: "Misty forest", Caption: "Pacific Northwest, post-rain."},
		{Src: makeSrc(40, "Desert"), Alt: "Desert sunset", Caption: "Mojave, golden hour."},
		{Src: makeSrc(0, "Coral"), Alt: "Coral reef", Caption: "Great Barrier Reef, 30 ft down."},
	}

	gridGallery := ui.Gallery(ui.GalleryConfig{
		ID:       "lightbox-demo-gallery",
		Items:    items,
		Columns:  4,
		Lightbox: "components-lightbox-demo",
	})

	masonryGallery := ui.Gallery(ui.GalleryConfig{
		ID:          "lightbox-demo-masonry",
		Variant:     ui.GalleryMasonry,
		Items:       items,
		Columns:     3,
		CaptionMode: ui.GalleryCaptionOverlay,
		Lightbox:    "components-lightbox-demo",
	})

	src := `// 1. Mount the Lightbox once at app startup.
modal := ui.Lightbox(ui.LightboxConfig{
    Name:          "photo-viewer",
    NavArrows:     true,
    ShowCaption:   true,
    AllowDownload: true,
})
widget.Mount(r, modal.Build())

// 2. Render any Gallery with Lightbox: "photo-viewer" — each thumb
//    becomes a trigger. ArrowLeft/Right cycle siblings; Esc closes.
ui.Gallery(ui.GalleryConfig{
    ID:       "trip-photos",
    Items:    photos,
    Columns:  4,
    Lightbox: "photo-viewer",
})`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Lightbox")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Click-to-zoom overlay built on preset.Modal — ESC, click-outside, focus-trap, return-focus all come free. Lightbox is standalone: any element on the page with data-fui-open + data-fui-deeplink triggers it. Pairs cleanly with Gallery but works equally well as a target for inline figures, markdown content, or custom photo feeds.")),
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Grid Gallery + Lightbox")),
		demoFrame(gridGallery, src),
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Masonry Gallery, same Lightbox, hover captions")),
		demoFrame(masonryGallery, `ui.Gallery(ui.GalleryConfig{
    Variant:     ui.GalleryMasonry,
    CaptionMode: ui.GalleryCaptionOverlay,
    Columns:     3,
    Lightbox:    "photo-viewer",
    Items:       photos,
})`),
	)
}
