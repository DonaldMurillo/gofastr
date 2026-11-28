package headless

import (
	"log/slog"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Tooltip parts.
const (
	PartTooltipPop Part = "tooltip-pop"
)

// TooltipProps configures one tooltip: a short sentence that describes
// the trigger it wraps.
type TooltipProps struct {
	// Text is the sentence. Required and non-blank.
	Text string
	// Trigger is the element the tooltip describes; the ID joins the aria-describedby list on its first open tag. Required.
	Trigger render.HTML
	// Placement is the preferred side: "top" (or empty), "bottom", "left" or "right"; left and right are the reading direction's start and end.
	Placement string
	// ID is the pop's id, which the trigger's aria-describedby names. Required.
	ID string

	ExtraAttrs html.Attrs

	Parts Parts
}

// Tooltip renders the wrapper, the caller's trigger pointed at the pop
// by aria-describedby, and the role=tooltip pop. Without script the
// kit's sheet shows the pop on hover and focus; the headless-tooltip
// module raises it into the top layer as a manual popover, keeps it open
// while the pointer is over it and closes it on Escape.
func Tooltip(p TooltipProps, s Classes) render.HTML {
	if strings.TrimSpace(p.Text) == "" {
		panic("headless: Tooltip requires Text")
	}
	if strings.TrimSpace(string(p.Trigger)) == "" {
		panic("headless: Tooltip requires Trigger: a tooltip describes an element")
	}
	checkFragmentID("Tooltip", "ID", p.ID)
	placement := p.Placement
	if placement == "" {
		placement = "top"
	}
	checkEnum("Tooltip", "Placement", placement, "top", "bottom", "left", "right")
	b := p.Parts.Box(s)
	own := Safe(p.ExtraAttrs, "role")
	Mark(own, "data-hui-tooltip")
	own["data-hui-tooltip-placement"] = placement
	trigger, err := uiregistry.AddAttributeToken(p.Trigger, "aria-describedby", p.ID)
	if err != nil {
		warnUndescribedTrigger(p.ID, err)
	}
	popAttrs := Internal(Attrs(map[string]string{"id": p.ID, "role": "tooltip"}))
	Mark(popAttrs, "data-hui-tooltip-pop")
	return b.El("span", PartRoot, own,
		trigger,
		b.El("span", PartTooltipPop, popAttrs, render.Text(scrubControlBytes(p.Text))),
	)
}

// warnedTriggers holds the tooltip ids already warned about, so a page
// rendered on every request logs a trigger it cannot describe once.
var warnedTriggers sync.Map

func warnUndescribedTrigger(id string, err error) {
	if _, seen := warnedTriggers.LoadOrStore(id, true); seen {
		return
	}
	slog.Warn("headless: Tooltip trigger has no opening tag to carry aria-describedby, so assistive technology does not tie the tooltip to it; wrap the trigger in an element",
		"id", id, "err", err)
}

func init() {
	Register(Spec{
		Name:    "Tooltip",
		Anatomy: []Part{PartRoot, PartTooltipPop},
		Hooks:   []string{"data-hui-tooltip", "data-hui-tooltip-placement", "data-hui-tooltip-pop"},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Tooltip(TooltipProps{
				Text: "Copies the link", ID: "tip-parts",
				Trigger: Button(ButtonProps{Label: "Share", Variant: "secondary"}, nil), Parts: parts,
			}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "a tooltip on a button",
				Why:  "the pop is a description, not a name: the trigger keeps its own label and points at the pop with aria-describedby, so a screen reader says both and a sighted reader sees the pop on hover or focus",
				HTML: Tooltip(TooltipProps{Text: "Saves without closing", ID: "tip-save",
					Trigger: Button(ButtonProps{Label: "Save", Variant: "primary"}, k.For("Button"))}, s),
			}, {
				Name: "a preferred side",
				Why:  "the placement is a preference, not a promise: the pop flips to the opposite side when the preferred one has no room, so the hook says where to try first",
				HTML: Tooltip(TooltipProps{Text: "Opens the guide beside the page", ID: "tip-docs", Placement: "bottom",
					Trigger: Button(ButtonProps{Label: "Guide", Variant: "secondary"}, k.For("Button"))}, s),
			}}
		},
	})
}
