package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type CarouselScreen struct{}

func (s *CarouselScreen) ScreenTitle() string { return "Carousel" }
func (s *CarouselScreen) ScreenDescription() string {
	return "Horizontal scroll-snap slider with Prev/Next + dots + optional AutoRotate."
}
func (s *CarouselScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *CarouselScreen) Render() render.HTML {
	makeSlideContent := func(hue int, label, body string) render.HTML {
		return render.Tag("div", map[string]string{"class": "demo-carousel-slide"},
			render.Tag("div", map[string]string{
				"class": "demo-carousel-slide__art",
			}, render.HTML(
				"<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 600 200' width='100%' height='160'>"+
					"<rect width='600' height='200' fill='hsl("+itoaScreen(hue)+", 60%, 50%)'/>"+
					"<text x='50%' y='52%' text-anchor='middle' font-size='32' fill='white' font-family='system-ui'>"+label+"</text>"+
					"</svg>")),
			html.Heading(html.HeadingConfig{Level: 3, Class: "demo-carousel-slide__title"}, render.Text(label)),
			html.Paragraph(html.TextConfig{Class: "demo-carousel-slide__body"}, render.Text(body)),
		)
	}

	basic := ui.Carousel(ui.CarouselConfig{
		Label: "Featured products",
		Slides: []ui.CarouselSlide{
			{Content: makeSlideContent(220, "Aurora Bundle", "Limited edition. Free shipping through Sunday.")},
			{Content: makeSlideContent(140, "Forest Picks", "Hand-curated for the rainy season.")},
			{Content: makeSlideContent(40, "Desert Collection", "Earth tones and slow textures.")},
			{Content: makeSlideContent(0, "Coral Drops", "Bright accents for short days.")},
		},
	})

	autoRotate := ui.Carousel(ui.CarouselConfig{
		Label:        "Customer testimonials",
		AutoRotateMs: 4000,
		Loop:         true,
		Slides: []ui.CarouselSlide{
			{Content: makeSlideContent(280, "Ada L.", "“Changed how I think about full-stack Go.”")},
			{Content: makeSlideContent(180, "Grace H.", "“Read the source. Then I shipped a thing the same day.”")},
			{Content: makeSlideContent(90, "Linus T.", "“Boring in the best way.”")},
		},
	})

	multi := ui.Carousel(ui.CarouselConfig{
		Label:          "Related articles",
		VisiblePerView: 3,
		Slides: []ui.CarouselSlide{
			{Content: makeSlideContent(220, "Why SSR", "First-paint speed in 2026.")},
			{Content: makeSlideContent(140, "Hydration", "Selective islands done right.")},
			{Content: makeSlideContent(40, "RPC at scale", "When 1k req/sec stops being interesting.")},
			{Content: makeSlideContent(0, "Edge mounts", "Drawer, modal, popover — pick wisely.")},
			{Content: makeSlideContent(280, "Theming", "One token, six skins.")},
		},
	})

	// Virtual-scroll demo — 60 slides, first 5 hydrated, rest deferred.
	virtualSlides := make([]ui.CarouselSlide, 0, 60)
	for i := 0; i < 60; i++ {
		hue := (i * 17) % 360
		virtualSlides = append(virtualSlides, ui.CarouselSlide{
			Content: makeSlideContent(hue, "Image "+itoaScreen(i+1), "Lazily hydrated when scrolled into view."),
		})
	}
	virtual := ui.Carousel(ui.CarouselConfig{
		ID:                       "demo-virtual-carousel",
		Label:                    "Archive — 60 slides (lazy)",
		VirtualScroll:            true,
		VirtualWindow:            5,
		VirtualPlaceholderHeight: "220px",
		Slides:                   virtualSlides,
	})

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Carousel")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Horizontal scroll-snap slider. Prev/Next buttons + pagination dots ship by default; ArrowLeft/ArrowRight nav when the carousel is focused. AutoRotate is opt-in and pauses on hover, focus, prefers-reduced-motion, and background-tab visibility. Native scroll-snap means users can ALSO drag/swipe natively.")),
		// Demo slide content carries <h3>; an <h2> here keeps WCAG
		// 1.3.1 heading order monotonic.
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Basic")),
		demoFrame(basic, `ui.Carousel(ui.CarouselConfig{
    Label:  "Featured products",
    Slides: slides,
})`),
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("AutoRotate + Loop")),
		demoFrame(autoRotate, `ui.Carousel(ui.CarouselConfig{
    Label:        "Customer testimonials",
    AutoRotateMs: 4000,
    Loop:         true,
    Slides:       slides,
})`),
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Multi-slide view")),
		demoFrame(multi, `ui.Carousel(ui.CarouselConfig{
    Label:          "Related articles",
    VisiblePerView: 3,
    Slides:         slides,
})`),
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Virtual scroll (60 slides)")),
		render.Tag("p", nil, render.Text(
			"VirtualScroll renders only the initial window inline (5 slides here); the rest emit as same-width placeholders paired with a JSON manifest of deferred HTML. The runtime hydrates each placeholder via IntersectionObserver as it scrolls into the track viewport, plus a one-window read-ahead buffer. Use for image-heavy archives where rendering every slide upfront is wasteful.")),
		demoFrame(virtual, `ui.Carousel(ui.CarouselConfig{
    Label:                    "Archive",
    VirtualScroll:            true,
    VirtualWindow:            5,
    VirtualPlaceholderHeight: "220px",
    Slides:                   sixtySlides,
})`),
	)
}
