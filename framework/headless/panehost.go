package headless

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

const (
	PartPane Part = "pane"
)

// PaneHostProps configures the host.
type PaneHostProps struct {
	// Primary is the always-visible pane. Required.
	Primary render.HTML
	// Secondary is the first optional side pane.
	Secondary render.HTML
	// Tertiary is the second optional side pane.
	Tertiary render.HTML
	// SecondaryOpen / TertiaryOpen set the SSR initial open state.
	SecondaryOpen bool
	TertiaryOpen  bool
	// SecondaryLabel / TertiaryLabel name the side panes' regions; empty takes Strings.
	SecondaryLabel string
	TertiaryLabel  string
	// DeepLinkParam names the query parameter carrying pane state; it must be a bare token.
	DeepLinkParam string

	ID         string
	ExtraAttrs html.Attrs

	Parts   Parts
	Strings *Strings
}

// PaneHost renders the shell.
func PaneHost(p PaneHostProps, s Classes) render.HTML {
	if p.Primary == "" {
		panic("headless: PaneHost requires Primary — the host is the primary pane's frame; without it there is no host")
	}
	if p.DeepLinkParam != "" {
		checkNoControlBytes("PaneHost", "DeepLinkParam", p.DeepLinkParam)
		if len(p.DeepLinkParam) != len(trimAllSpace(p.DeepLinkParam)) || p.DeepLinkParam == "" {
			panic("headless: PaneHost DeepLinkParam must be a bare token, not " + p.DeepLinkParam)
		}
	}
	b := p.Parts.Box(s)

	secondaryOpen := p.Secondary != "" && p.SecondaryOpen
	tertiaryOpen := p.Tertiary != "" && p.TertiaryOpen

	rootAttrs := Merge(Safe(p.ExtraAttrs, "class"), html.Attrs{
		"id": p.ID,
	})
	Mark(rootAttrs, "data-hui-panehost")
	open := make([]string, 0, 2)
	if secondaryOpen {
		open = append(open, "secondary")
	}
	if tertiaryOpen {
		open = append(open, "tertiary")
	}
	if len(open) > 0 {
		rootAttrs["data-hui-pane-open"] = strings.Join(open, " ")
	}
	if p.DeepLinkParam != "" {
		rootAttrs["data-hui-pane-deeplink"] = p.DeepLinkParam
	}

	primaryOwn := html.Attrs{"data-hui-pane": "primary"}
	if cls := b.Classes.Variant(PartPane, "primary"); cls != "" {
		primaryOwn["class"] = cls
	}
	children := []render.HTML{b.El("div", PartPane, primaryOwn, p.Primary)}
	w := p.Strings.Resolve()
	if p.Secondary != "" {
		children = append(children, paneHostSlot(b, "secondary", w.PaneSecondary, p.SecondaryLabel, secondaryOpen, p.Secondary))
	}
	if p.Tertiary != "" {
		children = append(children, paneHostSlot(b, "tertiary", w.PaneTertiary, p.TertiaryLabel, tertiaryOpen, p.Tertiary))
	}
	return b.El("div", PartRoot, rootAttrs, children...)
}

func paneHostSlot(b Box, slot, defLabel string, label string, open bool, content render.HTML) render.HTML {
	if label != "" && strings.TrimSpace(label) == "" {
		panic("headless: PaneHost " + slot + " label is only whitespace — a region's label has to say something")
	}
	if label == "" {
		label = defLabel
	}
	own := html.Attrs{
		"role":          "region",
		"aria-label":    scrubControlBytes(label),
		"data-hui-pane": slot,
	}
	if cls := b.Classes.Variant(PartPane, slot); cls != "" {
		own["class"] = cls
	}
	if !open {
		Mark(own, "hidden")
	}
	return b.El("div", PartPane, own, content)
}

func trimAllSpace(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r', '\v', '\f':
		default:
			out = append(out, r)
		}
	}
	return string(out)
}

func init() {
	Register(Spec{
		Name:    "PaneHost",
		Anatomy: []Part{PartRoot, PartPane},
		Hooks:   []string{"data-hui-panehost", "data-hui-pane-open", "data-hui-pane-deeplink", "data-hui-pane", "data-hui-pane-open-control", "data-hui-pane-host-target"},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return PaneHost(PaneHostProps{Primary: render.Text("The list"), Parts: parts}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "a primary pane alone",
				Why:  "the host is the primary pane's frame; with no side panes there is nothing to open and no script needed",
				HTML: PaneHost(PaneHostProps{Primary: render.Text("Customers")}, s),
			}, {
				Name: "a secondary pane, open, with a deep link",
				Why:  "the open state ships in the markup so first paint matches the server, and the deep-link parameter names the query key the runtime records the open pane in",
				HTML: PaneHost(PaneHostProps{
					Primary:        render.Text("Customers"),
					Secondary:      render.Text("Ticket detail"),
					SecondaryOpen:  true,
					SecondaryLabel: "Details",
					DeepLinkParam:  "pane",
				}, s),
			}, {
				Name: "an outside trigger addressing its host by id",
				Why:  "a trigger beyond every host (a toolbar above the panes) drives the host its data-hui-pane-host-target names — the control attribute beside it is the same one an in-host trigger carries",
				HTML: render.Join(
					PaneHost(PaneHostProps{
						ID:        "host",
						Primary:   render.Text("Customers"),
						Secondary: render.Text("Ticket detail"),
					}, s),
					Button(ButtonProps{
						Label: "Ticket detail",
						Action: html.Attrs{
							"data-hui-pane-open-control": "secondary",
							"data-hui-pane-host-target":  "host",
						},
					}, k.For("Button"))),
			}, {
				Name: "two side panes named by Strings",
				Why:  "a side pane with no label of its own is named by the Strings default (Secondary, Tertiary), so a translated page names its regions in its own language",
				HTML: PaneHost(PaneHostProps{
					Primary:   render.Text("Customers"),
					Secondary: render.Text("Ticket detail"),
					Tertiary:  render.Text("Customer detail"),
				}, s),
			}}
		},
	})
}
