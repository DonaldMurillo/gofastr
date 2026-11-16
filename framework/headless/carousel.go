package headless

import (
	"context"
	"slices"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Carousel parts.
const (
	PartCarouselStage Part = "carousel-stage"
	PartCarouselTrack Part = "carousel-track"
	PartCarouselSlide Part = "carousel-slide"
	PartCarouselDot   Part = "carousel-dot"
	PartCarouselPrev  Part = "carousel-prev"
	PartCarouselNext  Part = "carousel-next"
)

// CarouselSlide is one entry.
type CarouselSlide struct {
	Content render.HTML // required: the slide body
	Label   string      // the slide's accessible name; Strings.CarouselSlide by default
	ID      string      // empty takes "<trackID>-slide-<n>"
}

// CarouselProps configures the carousel.
type CarouselProps struct {
	Label          string          // required: names the region
	Slides         []CarouselSlide // required, at least one
	Loop           bool            // Next on the last slide wraps to the first; otherwise the ends disable
	AutoRotateMS   int             // advances every N ms when positive; negative panics
	VisiblePerView int             // negative panics; the headless render draws nothing from it
	NoDots         bool
	NoArrows       bool

	ID         string
	ExtraAttrs html.Attrs

	Parts   Parts
	Strings *Strings

	Ctx context.Context // a derived id goes through its id scope; an explicit ID is never changed
}

// Carousel renders the slider.
func Carousel(p CarouselProps, s Classes) render.HTML {
	checkLabel("Carousel", "Label", p.Label)
	if len(p.Slides) == 0 {
		panic("headless: Carousel requires at least one slide — an empty track is not a carousel")
	}
	if p.AutoRotateMS < 0 {
		panic("headless: Carousel AutoRotateMS " + strconv.Itoa(p.AutoRotateMS) + " is negative — a rotation ahead of the clock is not a rotation")
	}
	if p.VisiblePerView < 0 {
		panic("headless: Carousel VisiblePerView " + strconv.Itoa(p.VisiblePerView) + " is negative — a viewport narrower than nothing is not a viewport")
	}
	if p.VisiblePerView == 0 {
		p.VisiblePerView = 1
	}
	if p.VisiblePerView > 8 {
		p.VisiblePerView = 8
	}
	w := p.Strings.Resolve()

	b := p.Parts.Box(s)
	family := []string{"-track"}
	for i, sl := range p.Slides {
		if sl.ID == "" {
			family = append(family, "-track-slide-"+strconv.Itoa(i))
		} else {
			render.ReserveID(p.Ctx, sl.ID)
		}
	}
	id := p.ID
	if id == "" {
		id = render.UniqueID(p.Ctx, "hui-carousel-"+menuShortHash(p.Label), family...)
	} else {
		render.ReserveID(p.Ctx, id, family...)
	}
	trackID := id + "-track"

	p.Slides = slices.Clone(p.Slides)
	ids := make([]string, 0, len(p.Slides))
	for i := range p.Slides {
		if p.Slides[i].ID == "" {
			p.Slides[i].ID = trackID + "-slide-" + strconv.Itoa(i)
		}
		checkFragmentID("Carousel slide", "ID", p.Slides[i].ID)
		ids = append(ids, p.Slides[i].ID)
	}
	checkNoDuplicateIDs("Carousel", ids)

	rootAttrs := Merge(Safe(p.ExtraAttrs, "role", "aria-roledescription"), html.Attrs{
		"role":                 "region",
		"aria-roledescription": w.CarouselRole,
		"aria-label":           scrubControlBytes(p.Label),
		"id":                   id,
	})
	Mark(rootAttrs, "data-hui-carousel")
	if p.AutoRotateMS > 0 {
		rootAttrs["data-hui-carousel-rotate-ms"] = strconv.Itoa(p.AutoRotateMS)
	}
	if p.Loop {
		Mark(rootAttrs, "data-hui-carousel-loop")
	}

	slides := make([]render.HTML, 0, len(p.Slides))
	for i, sl := range p.Slides {
		label := sl.Label
		if label == "" {
			label = w.fill(w.CarouselSlide, "n", i+1, "total", len(p.Slides))
		}
		own := html.Attrs{
			"id":                   sl.ID,
			"role":                 "group",
			"aria-roledescription": w.CarouselSlideRole,
			"aria-label":           scrubControlBytes(label),
		}
		if i == 0 {
			own["aria-current"] = "true"
		}
		slides = append(slides, b.El("div", PartCarouselSlide, own, sl.Content))
	}

	trackAttrs := Attrs(map[string]string{
		"id":       trackID,
		"tabindex": "0",
	})
	Mark(trackAttrs, "data-hui-carousel-track")
	status := w.fill(w.CarouselSlide, "n", 1, "total", len(p.Slides))
	trackAttrs["data-hui-carousel-status"] = status
	trackAttrs["data-hui-carousel-status-fmt"] = w.CarouselSlide + "|" + strconv.Itoa(len(p.Slides))
	hasContent := carouselSlidesHaveContent(p.Slides)
	arrowOwn := func(a html.Attrs) html.Attrs {
		if hasContent {
			return Internal(a)
		}
		return a
	}
	stage := []render.HTML{b.El("div", PartCarouselTrack, trackAttrs, slides...)}
	if !p.NoArrows {
		last := len(p.Slides) - 1
		stage = append(stage,
			b.El("a", PartCarouselPrev, arrowOwn(html.Attrs{
				"href":                   "#" + p.Slides[0].ID,
				"aria-label":             w.Previous,
				"data-hui-carousel-prev": "",
			}), render.Text(w.Previous)),
			b.El("a", PartCarouselNext, arrowOwn(html.Attrs{
				"href":                   "#" + p.Slides[last].ID,
				"aria-label":             w.Next,
				"data-hui-carousel-next": "",
			}), render.Text(w.Next)),
		)
	}
	stageOwn := html.Attrs(nil)
	if !hasContent {
		stageOwn = Internal(nil)
	}
	children := []render.HTML{b.El("div", PartCarouselStage, stageOwn, stage...),
		b.El("span", PartVisuallyHidden, Internal(html.Attrs{
			"aria-live":              carouselLive(p.AutoRotateMS),
			"aria-atomic":            "true",
			"data-hui-carousel-live": "",
		}), render.Text(status)),
	}

	if !p.NoDots {
		dots := make([]render.HTML, 0, len(p.Slides))
		for i, sl := range p.Slides {
			own := html.Attrs{
				"href":                   "#" + sl.ID,
				"aria-label":             w.fill(w.CarouselGoTo, "n", i+1),
				"data-hui-carousel-goto": strconv.Itoa(i),
			}
			if i == 0 {
				own["aria-current"] = "true"
			}
			dots = append(dots, b.El("a", PartCarouselDot, own, render.Text(w.Number(i+1))))
		}
		children = append(children, b.El("nav", PartMarker, Internal(html.Attrs{
			"aria-label": scrubControlBytes(p.Label),
		}), dots...))
	}
	return b.El("div", PartRoot, rootAttrs, children...)
}

// carouselLive is the live region's politeness at render: off while rotating, else polite.
func carouselLive(rotateMS int) string {
	if rotateMS > 0 {
		return "off"
	}
	return "polite"
}

// carouselSlidesHaveContent reports whether any slide carries the caller's content.
func carouselSlidesHaveContent(slides []CarouselSlide) bool {
	for _, sl := range slides {
		if sl.Content != "" {
			return true
		}
	}
	return false
}
