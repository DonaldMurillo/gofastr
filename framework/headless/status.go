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
	// Label says what is being waited for. Required.
	Label string
	// Announce renders role=status, for a spinner that replaces content after an action.
	Announce bool
	// Size is a class-map hint ("sm", "lg").
	Size string
	// Variant is the shape: "" a ring, "dots" or "grid"; every shape is aria-hidden.
	Variant string

	ID         string
	ExtraAttrs html.Attrs

	Parts Parts
}

// Spinner renders the indicator.
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
	hidden := Internal(Attrs(map[string]string{"aria-hidden": "true"}))
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
		shape = b.El("span", PartSpinnerRing, hidden, render.HTML(""))
	}
	return b.El("span", PartRoot, own,
		shape,
		b.El("span", PartVisuallyHidden, Internal(nil), render.Text(p.Label)),
	)
}

// SkeletonProps is the placeholder shown while content loads.
type SkeletonProps struct {
	// Label is what is loading, announced once. Required.
	Label string
	// Lines is how many bars to draw. Zero means one.
	Lines int
	// Shape is a class-map hint: "text", "title", "block", "circle".
	Shape string

	ID         string
	ExtraAttrs html.Attrs

	Parts Parts
}

// Skeleton renders the placeholder: aria-hidden bars and the label in a status region.
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
		attrs := Internal(Attrs(map[string]string{"aria-hidden": "true"}))
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
		append(bars, b.El("span", PartVisuallyHidden, Internal(nil), render.Text(p.Label)))...)
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
