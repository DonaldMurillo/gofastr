package headless

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Spinner and Skeleton parts.
const (
	PartSpinnerRing Part = "spinner-ring"
	PartSpinnerDots Part = "spinner-dots"
	PartSpinnerDot  Part = "spinner-dot"
	PartSpinnerGrid Part = "spinner-grid"
	PartSpinnerCell Part = "spinner-cell"
	PartSkeleton    Part = "skeleton-line"
)

// SpinnerProps is the "working" indicator.
type SpinnerProps struct {
	// Label says what is being waited for — "Loading apps". Required,
	// and it is not decoration: a spinner with no label is a moving
	// shape that tells a screen reader user nothing at all, and the
	// one thing they need to know is that waiting is the correct thing
	// to be doing right now.
	Label string
	// Announce puts the label in a live region, so it is read when the
	// spinner appears. Off by default: a spinner rendered WITH the
	// page has not "happened" — the same rule Alert follows — and
	// announcing it on load talks over the page.
	//
	// Turn it on for a spinner that replaces content after an action.
	Announce bool
	// Size is a class-map hint ("sm", "lg").
	Size string
	// Variant is the animated shape, a class-map hint: "" draws a
	// bordered ring, "dots" three pulsing dots, "grid" a rippling
	// square of nine. Every shape is aria-hidden — the shape is a
	// picture of waiting and the label is what waiting is FOR.
	Variant string

	ID         string
	ExtraAttrs html.Attrs

	// Parts: attrs and binds on the root and the shape.
	Parts Parts
}

// Spinner renders the indicator.
//
// role="status" rather than role="progressbar": a progressbar promises
// a value, and a spinner has none — that is what makes it a spinner
func Spinner(p SpinnerProps, s Classes) render.HTML {
	if p.Label == "" {
		panic("headless: Spinner requires Label — a moving shape says nothing on its own")
	}
	b := p.Parts.Box(s)
	own := Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{"id": p.ID}))
	if cls := s.Variant(PartRoot, "size-"+p.Size); p.Size != "" && cls != "" {
		own["class"] = joinClasses(own["class"], cls)
	}
	if cls := s.Variant(PartRoot, p.Variant); p.Variant != "" && cls != "" {
		own["class"] = joinClasses(own["class"], cls)
	}
	if p.Announce {
		own["role"] = "status"
	}
	hidden := Attrs(map[string]string{"aria-hidden": "true"})
	var shape render.HTML
	switch p.Variant {
	case "dots":
		dots := make([]render.HTML, 3)
		for i := range dots {
			dots[i] = b.El("span", PartSpinnerDot, nil)
		}
		shape = b.El("span", PartSpinnerDots, hidden, dots...)
	case "grid":
		cells := make([]render.HTML, 9)
		for i := range cells {
			cells[i] = b.El("span", PartSpinnerCell, nil)
		}
		shape = b.El("span", PartSpinnerGrid, hidden, cells...)
	default:
		// The ring is the animation, and it is hidden: what it means
		// is in the text beside it, and a spinning shape has no
		// meaning to announce.
		shape = b.El("span", PartSpinnerRing, hidden, render.HTML(""))
	}
	return b.El("span", PartRoot, own,
		shape,
		b.El("span", PartVisuallyHidden, nil, render.Text(p.Label)),
	)
}

// SkeletonProps is the placeholder shown while content loads.
type SkeletonProps struct {
	// Label is what is loading, announced once through a live region
	// — "Loading apps". Required for the same reason Spinner's is.
	Label string
	// Lines is how many bars to draw. Zero means one.
	Lines int
	// Shape is a class map hint: "text", "title", "block", "circle".
	Shape string

	ID         string
	ExtraAttrs html.Attrs

	// Parts: attrs and binds on the root and the bars.
	Parts Parts
}

// Skeleton renders the placeholder.
//
// The bars are aria-hidden, every one of them. A skeleton is a picture
// of content that does not exist yet, and a screen reader reading out
// eight empty boxes — or worse, announcing each shimmer as it
// animates — is strictly worse than silence. What it does instead is
// say "Loading apps" once, politely, and then wait.
//
// This is also why the bars are not <p> or <div> full of nbsp: there
// is no text to read, so there should be no text.
func Skeleton(p SkeletonProps, s Classes) render.HTML {
	if p.Label == "" {
		panic("headless: Skeleton requires Label")
	}
	n := p.Lines
	if n <= 0 {
		n = 1
	}
	b := p.Parts.Box(s)
	bars := make([]render.HTML, 0, n+1)
	for i := range n {
		attrs := Attrs(map[string]string{"aria-hidden": "true"})
		// The last line of a paragraph is short, and a skeleton that
		// draws every line full width reads as a block, not as text.
		if n > 1 && i == n-1 {
			attrs["data-hui-skeleton-last"] = ""
		}
		part := PartSkeleton
		if p.Shape != "" {
			part = Part(string(PartSkeleton) + "--" + p.Shape)
		}
		bars = append(bars, b.El("span", part, attrs, render.HTML("")))
	}
	own := Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{
		"id": p.ID, "data-hui-lines": strconv.Itoa(n),
	}))
	own["role"] = "status"
	return b.El("div", PartRoot, own,
		append(bars, b.El("span", PartVisuallyHidden, nil, render.Text(p.Label)))...)
}

func init() {
	Register(Spec{
		Name: "Spinner",
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Spinner(SpinnerProps{Label: "Checking", Parts: parts}, s)
		},
		Anatomy: []Part{PartRoot, PartSpinnerRing, PartSpinnerDots, PartSpinnerDot, PartSpinnerGrid, PartSpinnerCell, PartVisuallyHidden},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "inline wait",
				Why:  "role=status, not progressbar: a progressbar promises a value and a spinner has none — and the label says what is being waited for, because a moving shape says nothing on its own",
				HTML: Spinner(SpinnerProps{Label: "Checking the registry"}, s),
			}, {
				Name: "dots",
				Why:  "the shape is the class map's choice and the meaning never moves: the dots are hidden and the label is what the wait is for",
				HTML: Spinner(SpinnerProps{Label: "Saving", Variant: "dots"}, s),
			}, {
				Name: "grid",
				Why:  "a heavy wait says so with a busier picture, and the picture is still hidden: the nine cells are motion and the label is the meaning",
				HTML: Spinner(SpinnerProps{Label: "Restoring the backup", Variant: "grid"}, s),
			}}
		},
	})

	Register(Spec{
		Name: "Skeleton",
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Skeleton(SkeletonProps{Label: "Loading apps", Lines: 2, Parts: parts}, s)
		},
		Anatomy: []Part{PartRoot, PartSkeleton, PartVisuallyHidden},
		Hooks:   []string{"data-hui-lines", "data-hui-skeleton-last"},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "three lines",
				Why:  "every bar is hidden from the tree: a screen reader reading out eight empty boxes, or announcing each shimmer, is strictly worse than one polite \"Loading apps\" and silence",
				HTML: Skeleton(SkeletonProps{Label: "Loading apps", Lines: 3}, s),
			}}
		},
	})
}
