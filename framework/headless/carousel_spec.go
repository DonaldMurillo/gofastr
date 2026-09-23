package headless

import (
	"github.com/DonaldMurillo/gofastr/core/render"
)

func init() {
	Register(Spec{
		Name: "Carousel",
		Anatomy: []Part{PartRoot, PartCarouselStage, PartCarouselTrack, PartCarouselSlide,
			PartCarouselDot, PartCarouselPrev, PartCarouselNext, PartMarker},
		Hooks: []string{"data-hui-carousel", "data-hui-carousel-rotate-ms",
			"data-hui-carousel-loop", "data-hui-carousel-track",
			"data-hui-carousel-status", "data-hui-carousel-status-fmt",
			"data-hui-carousel-goto",
			"data-hui-carousel-prev", "data-hui-carousel-next"},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Carousel(CarouselProps{Label: "Highlights",
				Slides: []CarouselSlide{
					{Content: render.Text("First")},
					{Content: render.Text("Second")},
				}, Parts: parts}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "a carousel with every control",
				Why:  "the track is a real scrollable list, the dots and arrows are fragment anchors to the slide ids, and the first slide is current — the whole thing works with no script",
				HTML: Carousel(CarouselProps{Label: "Featured work", Slides: []CarouselSlide{
					{Content: render.Text("Slide one")},
					{Content: render.Text("Slide two")},
					{Content: render.Text("Slide three")},
				}}, s),
			}, {
				Name: "bare, rotating and looping",
				Why:  "without dots or arrows the track and the status are the whole contract, and the rotation flags ride the root for the module to pause on hover, focus, hidden and reduced motion",
				HTML: Carousel(CarouselProps{Label: "Hero", NoDots: true, NoArrows: true, Loop: true, AutoRotateMS: 6000,
					Slides: []CarouselSlide{{Content: render.Text("Only")}}}, s),
			}}
		},
	})
}
