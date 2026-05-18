package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type LightboxScreen struct{}

func (s *LightboxScreen) ScreenTitle() string { return "Lightbox" }
func (s *LightboxScreen) ScreenDescription() string {
	return "Click-to-zoom gallery; composes preset.Modal."
}
func (s *LightboxScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *LightboxScreen) Render() render.HTML {
	// Inline-data thumbnail placeholders so the demo doesn't need
	// binary assets in the repo. Each "image" is a 320×240 SVG.
	makeSrc := func(hue int, label string) string {
		return "data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 320 240'>" +
			"<rect width='320' height='240' fill='hsl(" + itoaScreen(hue) + ", 60%, 50%)'/>" +
			"<text x='50%25' y='52%25' text-anchor='middle' font-size='28' fill='white' font-family='system-ui'>" + label + "</text></svg>"
	}

	thumbs, _ := ui.Lightbox(ui.LightboxConfig{
		Name:  "components-lightbox-demo",
		Label: "Sample gallery",
		Pages: []string{"/components/lightbox"},
		Images: []ui.LightboxImage{
			{Src: makeSrc(220, "Mountain"), Alt: "Mountain at dawn"},
			{Src: makeSrc(140, "Forest"), Alt: "Misty forest"},
			{Src: makeSrc(40, "Desert"), Alt: "Desert sunset"},
			{Src: makeSrc(0, "Coral"), Alt: "Coral reef"},
		},
	})

	src := `thumbs, modal := ui.Lightbox(ui.LightboxConfig{
    Name:  "gallery",
    Label: "Sample gallery",
    Images: []ui.LightboxImage{
        {Src: "/photos/mountain.jpg", Alt: "Mountain at dawn"},
        {Src: "/photos/forest.jpg",   Alt: "Misty forest"},
        {Src: "/photos/desert.jpg",   Alt: "Desert sunset"},
    },
})

// Mount the modal once at app startup:
widget.Mount(r, modal.Build())

// Render thumbs anywhere on the page.
return thumbs`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Lightbox")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Click-to-zoom image gallery. Composes preset.Modal — ESC, click-outside, focus-trap, and return-focus all come free. Each thumb anchor carries href=<full> as the no-JS fallback (opens in a new tab) + data-fui-deeplink that mirrors src/alt onto the modal's signals on open.")),
		demoFrame(thumbs, src),
	)
}

// itoaScreen is a tiny inline int→string used by the SVG-data demo helper.
// Keeping it screen-local avoids polluting the package-level namespace.
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
