package headless

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Spinner and Skeleton parts.
const (
	PartSpinnerRing Part = "spinner-ring"
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

	ID         string
	ExtraAttrs html.Attrs
}

// Spinner renders the indicator.
//
// role="status" rather than role="progressbar": a progressbar promises
// a value, and a spinner has none — that is what makes it a spinner
// rather than a Progress. An indeterminate <progress> is the right
// element when the wait has a place in the layout; this is for the
// small inline case.
func Spinner(p SpinnerProps, s Classes) render.HTML {
	if p.Label == "" {
		panic("headless: Spinner requires Label — a moving shape says nothing on its own")
	}
	own := Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{"id": p.ID}))
	if cls := s.Variant(PartRoot, "size-"+p.Size); p.Size != "" && cls != "" {
		own["class"] = joinClasses(own["class"], cls)
	}
	if p.Announce {
		own["role"] = "status"
	}
	return El("span", s, PartRoot, own,
		// The ring is the animation, and it is hidden: what it means
		// is in the text beside it, and a spinning shape has no
		// meaning to announce.
		El("span", s, PartSpinnerRing, Attrs(map[string]string{"aria-hidden": "true"}), render.HTML("")),
		El("span", s, PartVisuallyHidden, nil, render.Text(p.Label)),
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
	bars := make([]render.HTML, 0, n+1)
	for i := 0; i < n; i++ {
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
		bars = append(bars, El("span", s, part, attrs, render.HTML("")))
	}
	own := Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{
		"id": p.ID, "data-hui-lines": strconv.Itoa(n),
	}))
	own["role"] = "status"
	return El("div", s, PartRoot, own,
		append(bars, El("span", s, PartVisuallyHidden, nil, render.Text(p.Label)))...)
}

func init() {
	Register(Spec{
		Name:    "Spinner",
		Anatomy: []Part{PartRoot, PartSpinnerRing, PartVisuallyHidden},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "inline wait",
				Why:  "role=status, not progressbar: a progressbar promises a value and a spinner has none — and the label says what is being waited for, because a moving shape says nothing on its own",
				HTML: Spinner(SpinnerProps{Label: "Checking the registry"}, s),
			}}
		},
	})

	Register(Spec{
		Name:    "Skeleton",
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
