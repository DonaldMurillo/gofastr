package headless

import (
	"math"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// The progress bar: the native <progress> element, named for a screen
// reader and clamped for the DOM. The element carries the whole
// accessibility contract itself — role, value, maximum — so the
// component's job is naming it and keeping the numbers honest: a
// value the request carried is clamped into [0, Max] rather than
// trusted, because a hostile or garbled number must never panic a
// render or paint a bar past its own end. A negative value is the
// documented indeterminate posture: no value attribute, the browser
// animates the bar, and the reader hears the label alone. Distinct
// from the step wizard's ProgressSteps (framework/ui), which walks
// named stages; this is one fraction. No script.

// Progress parts. The bar is the element; the description is the
// human sentence beside it.
const (
	PartProgressValue Part = "progress-value"
)

// ProgressProps configures one bar.
type ProgressProps struct {
	// Value is the current progress. 0 to Max renders a determinate
	// bar; a negative value renders an indeterminate one. A value
	// past Max is clamped to Max at render; NaN and the infinities
	// render indeterminate, because a number that is not a number
	// cannot label progress.
	Value float64
	// Max is the ceiling. 0 (and any non-finite or negative value)
	// takes 100.
	Max float64
	// Label names the bar for assistive tech. Required: a progress
	// bar with no name is a stripe a screen reader cannot identify.
	Label string
	// LabelVisible renders the label as text above the bar (wired to
	// the element through aria-labelledby) instead of an aria-label.
	LabelVisible bool
	// Description is an optional human sentence beside the bar ("73
	// of 100", "Uploading…"). Scrubbed, never refused: it is data a
	// request can carry.
	Description string

	ID         string
	ExtraAttrs html.Attrs

	// Parts: attrs on the root and the value. No part is fillable.
	Parts Parts
}

// Progress renders the bar.
func Progress(p ProgressProps, s Classes) render.HTML {
	checkLabel("Progress", "Label", p.Label)
	max := clampMax(p.Max)
	value, determinate := progressValue(p.Value, max)
	b := p.Parts.Box(s)

	label := scrubControlBytes(p.Label)
	barAttrs := Attrs(map[string]string{
		"max": strconv.FormatFloat(max, 'f', -1, 64),
	})
	if determinate {
		barAttrs["value"] = strconv.FormatFloat(value, 'f', -1, 64)
	}
	if p.LabelVisible {
		labelID := p.ID
		if labelID == "" {
			labelID = "hui-progress-" + menuShortHash(label+"\x00"+p.Description)
		}
		labelID += "-label"
		barAttrs["aria-labelledby"] = labelID
		parts := []render.HTML{
			b.El("span", PartLabel, Attrs(map[string]string{"id": labelID}), render.Text(label)),
			b.El("progress", PartProgressValue, barAttrs),
		}
		if d := scrubControlBytes(p.Description); d != "" {
			parts = append(parts, b.El("span", PartDesc, nil, render.Text(d)))
		}
		return b.El("div", PartRoot,
			Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{"id": p.ID})), parts...)
	}
	barAttrs["aria-label"] = label
	parts := []render.HTML{b.El("progress", PartProgressValue, barAttrs)}
	if d := scrubControlBytes(p.Description); d != "" {
		parts = append(parts, b.El("span", PartDesc, nil, render.Text(d)))
	}
	return b.El("div", PartRoot,
		Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{"id": p.ID})), parts...)
}

// clampMax returns the ceiling a bar renders with: the caller's when
// it is a positive finite number, 100 otherwise.
func clampMax(max float64) float64 {
	if math.IsNaN(max) || math.IsInf(max, 0) || max <= 0 {
		return 100
	}
	return max
}

// progressValue clamps the bar's value into [0, max] and reports
// whether the bar is determinate. NaN and the infinitudes are
// indeterminate rather than clamped: Infinity past the max is a
// number that never arrived intact.
func progressValue(value, max float64) (float64, bool) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0, false
	}
	if value > max {
		return max, true
	}
	return value, true
}

func init() {
	Register(Spec{
		Name:    "Progress",
		Anatomy: []Part{PartRoot, PartLabel, PartProgressValue, PartDesc},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Progress(ProgressProps{
				Value: 73, Max: 100, Label: "Upload progress",
				Description: "73 of 100", Parts: parts,
			}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "a determinate bar",
				Why:  "the native element carries the value and the maximum; the component's whole job is the name and the clamp that keeps the fraction honest",
				HTML: Progress(ProgressProps{Value: 73, Max: 100, Label: "Upload progress", Description: "73 of 100"}, s),
			}, {
				Name: "an indeterminate bar",
				Why:  "a negative value is the documented posture for work whose fraction is not known: no value attribute, the browser animates, the label alone names it",
				HTML: Progress(ProgressProps{Value: -1, Label: "Working"}, s),
			}, {
				Name: "a visible label",
				Why:  "a label a reader can see as well as hear: the same name rendered as text and wired through aria-labelledby, for a bar whose purpose deserves the space",
				HTML: Progress(ProgressProps{Value: 18, Max: 100, Label: "Storage used", LabelVisible: true, Description: "18% of 1 TB"}, s),
			}}
		},
	})
}
