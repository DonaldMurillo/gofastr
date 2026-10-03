package headless

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// The pane host: a layout shell with an always-visible primary pane
// and one or two openable side panes. The panes are labelled regions
// whose open state ships server-rendered (hidden on closed panes), so
// first paint matches server state with no script; the module adds the
// open/close lifecycle, the focus handoff and restore, the responsive
// drawer below 768px (its own Tab trap over the kernel's focus
// selector and the kernel's refcounted scroll lock), and the optional
// query deep link. Nothing here fetches pane content: a trigger that
// loads content uses the island/RPC rails inside the pane's region.

// PaneHost parts. A slot's modifier (pane--primary, pane--secondary,
// pane--tertiary) resolves from the same class map through
// Classes.Variant(PartPane, slot), the way every variant-taking part
// in this package resolves.
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
	// SecondaryLabel / TertiaryLabel label each side pane's region.
	// Empty takes "Secondary" / "Tertiary".
	SecondaryLabel string
	TertiaryLabel  string
	// DeepLinkParam names the query parameter that carries pane state
	// for refresh/share/Back parity. Empty leaves the URL alone. A
	// non-token value (control bytes, markup, whitespace) is refused.
	DeepLinkParam string

	ID         string
	ExtraAttrs html.Attrs

	// Parts: attrs on the root and each pane.
	Parts Parts
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
	// data-hui-pane-open is the space-separated list of open side
	// panes, in slot order — the state the module maintains and the
	// sheet's column rules key off, so both open panes must be named
	// at render, not just the secondary.
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
	if p.Secondary != "" {
		children = append(children, paneHostSlot(b, "secondary", "Secondary", p.SecondaryLabel, secondaryOpen, p.Secondary))
	}
	if p.Tertiary != "" {
		children = append(children, paneHostSlot(b, "tertiary", "Tertiary", p.TertiaryLabel, tertiaryOpen, p.Tertiary))
	}
	return b.El("div", PartRoot, rootAttrs, children...)
}

func paneHostSlot(b Box, slot, defLabel string, label string, open bool, content render.HTML) render.HTML {
	// A label that is blank once trimmed is neither the default nor a
	// label: a caller who typed spaces meant something, and the region
	// must not ship an accessible name of spaces.
	// A label that is blank once trimmed is neither the default nor a
	// label: a caller who typed spaces meant something, and the region
	// must not ship an accessible name of spaces.
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

// trimAllSpace drops every whitespace rune (a parameter name with an
// inner space is not a token).
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
			}}
		},
	})
}
