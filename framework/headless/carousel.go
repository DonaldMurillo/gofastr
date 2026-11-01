package headless

import (
	"context"
	"slices"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// The carousel: a labelled region whose track is a real scrollable
// list of slides. Without script the track scrolls (native
// scroll-snap) and the controls are fragment anchors to the slide
// ids; with script the module owns the active slide (aria-current +
// a class), the prev/next stepping, the dot anchors, the slide status
// through the server's sentence, the auto-rotation (paused on hover,
// focus, hidden tab and reduced motion), and the reduced-motion gate.

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
	// Content is the slide body. Required.
	Content render.HTML
	// Label is the slide's accessible label. Defaults to
	// Strings.CarouselSlide ("Slide <n> of <total>") at render.
	Label string
	// ID is the slide's stable id. Empty takes "<trackID>-slide-<n>".
	ID string
}

// CarouselProps configures the carousel.
type CarouselProps struct {
	// Label names the region. Required.
	Label string
	// Slides, in order. Required, at least one.
	Slides []CarouselSlide
	// Loop makes Next on the last wrap to the first (and vice versa).
	// Default: the ends disable.
	Loop bool
	// AutoRotateMS, when > 0, advances every N ms. Negative refused.
	AutoRotateMS int
	// VisiblePerView shows N slides side-by-side (default 1; 0 or
	// negative refused — a viewport of nothing is not a viewport).
	VisiblePerView int
	// NoDots hides the dot anchors.
	NoDots bool
	// NoArrows hides the prev/next controls.
	NoArrows bool

	ID         string
	ExtraAttrs html.Attrs

	// Parts: attrs on the root, the track, the slides and the
	// controls.
	Parts   Parts
	Strings *Strings

	// Ctx is the render context. An id this component derives (no ID
	// given) is handed out through its id scope (render.UniqueID), so a
	// repeat on the page gets a -2 suffix; an explicit ID is never
	// changed. With no Ctx the derived id is used as is.
	Ctx context.Context
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
		// Carried data clamps, never refuses (the generated CSS covers
		// eight columns): a wider count renders the ceiling.
		p.VisiblePerView = 8
	}
	w := p.Strings.Resolve()

	b := p.Parts.Box(s)
	// The family: the track and every slide without its own id. A slide
	// with its own id is an explicit id like any other: reserved, so a
	// derived id elsewhere on the page that would repeat it moves.
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

	// Slides: stable ids, labels through the Strings sentence. The
	// track and slides are DIVs, not ul/li: slides are not list items,
	// and a ul whose children carry role=group trips axe's list rule
	// besides the allowed-role one the coordinator's site sweep caught.
	// The derived ids go on a copy: the caller's slice may feed
	// another carousel with a different ID.
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
	// The status format travels beside the initial sentence: the module
	// re-says the sentence through the format after every step, so the
	// words stay the server's while the number stays live.
	status := w.fill(w.CarouselSlide, "n", 1, "total", len(p.Slides))
	trackAttrs["data-hui-carousel-status"] = status
	trackAttrs["data-hui-carousel-status-fmt"] = w.CarouselSlide + "|" + strconv.Itoa(len(p.Slides))
	// The stage is the positioning context the overlaid controls
	// hang from: it wraps exactly the track, so an absolutely
	// positioned prev/next centres on the slides and can never grow
	// down over the dot row (a WCAG 2.2 target-size failure the flat
	// structure brought with it).
	// hasContent says whether any slide carries content. When none do,
	// the stage is marked as a whole below and the prev/next controls
	// — already fully generated, never the caller's — sit under that
	// mark instead of carrying their own; marking both would be
	// marking inside an already-marked subtree.
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
		// No slide carries content — a degenerate carousel, but a
		// legal one — so the stage (the track's slides plus the
		// prev/next controls) is entirely the component's own.
		stageOwn = Internal(nil)
	}
	children := []render.HTML{b.El("div", PartCarouselStage, stageOwn, stage...),
		// The live region the slide status is said through: the module
		// writes the sentence here after each step. A rotating carousel
		// renders it quiet (aria-live="off", the APG carousel pattern),
		// so an automatic step is not an interruption; the module turns
		// it polite while the rotation is paused (hover, focus, reduced
		// motion), which is when a step is the reader's own.
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

// carouselLive is the live region's politeness at render: quiet for a
// carousel that rotates on its own, polite for one that moves only
// when the reader moves it.
func carouselLive(rotateMS int) string {
	if rotateMS > 0 {
		return "off"
	}
	return "polite"
}

// carouselSlidesHaveContent reports whether any slide carries content —
// the only caller content the stage can hold.
func carouselSlidesHaveContent(slides []CarouselSlide) bool {
	for _, sl := range slides {
		if sl.Content != "" {
			return true
		}
	}
	return false
}
