package headless

import (
	"math"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Progress parts.
const (
	PartProgressValue Part = "progress-value"
)

// ProgressProps configures one native <progress> bar.
type ProgressProps struct {
	// Value is the progress, clamped to Max; a negative or non-finite value renders indeterminate.
	Value float64
	// Max is the ceiling; zero, negative or non-finite takes 100.
	Max float64
	// Label names the bar for assistive tech. Required.
	Label string
	// LabelVisible renders the label as text wired through aria-labelledby.
	LabelVisible bool
	// Description is an optional sentence beside the bar, scrubbed of control bytes.
	Description string

	ID         string
	ExtraAttrs html.Attrs

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
			b.El("span", PartLabel, Internal(Attrs(map[string]string{"id": labelID})), render.Text(label)),
			b.El("progress", PartProgressValue, Internal(barAttrs)),
		}
		if d := scrubControlBytes(p.Description); d != "" {
			parts = append(parts, b.El("span", PartDesc, Internal(nil), render.Text(d)))
		}
		return b.El("div", PartRoot,
			Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{"id": p.ID})), parts...)
	}
	barAttrs["aria-label"] = label
	parts := []render.HTML{b.El("progress", PartProgressValue, Internal(barAttrs))}
	if d := scrubControlBytes(p.Description); d != "" {
		parts = append(parts, b.El("span", PartDesc, Internal(nil), render.Text(d)))
	}
	return b.El("div", PartRoot,
		Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{"id": p.ID})), parts...)
}

func clampMax(max float64) float64 {
	if math.IsNaN(max) || math.IsInf(max, 0) || max <= 0 {
		return 100
	}
	return max
}

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
