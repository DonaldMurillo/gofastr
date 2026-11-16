package headless

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Disclosure parts, shared with Menu.
const (
	PartSummary Part = "summary"
	PartPanel   Part = "panel"
)

// DisclosureProps configures one native <details> disclosure.
type DisclosureProps struct {
	// Summary is the always-visible controller. Required and non-blank.
	Summary render.HTML
	// Content is the revealed panel.
	Content render.HTML
	// Open renders the details expanded.
	Open bool
	// Trap contains focus inside the open disclosure.
	Trap bool
	// Dismiss closes the open disclosure on a press outside it (light dismiss).
	Dismiss bool
	// Name is the details name attribute: the browser opens one disclosure of a
	// name at a time. Control bytes are refused.
	Name string
	// PersistKey keeps the open state across client-side navigation; empty means
	// a navigation closes it.
	PersistKey string

	ID         string
	ExtraAttrs html.Attrs

	Parts Parts
}

// Disclosure renders the native details disclosure.
func Disclosure(p DisclosureProps, s Classes) render.HTML {
	if strings.TrimSpace(string(p.Summary)) == "" {
		panic("headless: Disclosure requires Summary — a details whose summary says nothing is a button with no name")
	}
	if p.PersistKey != "" {
		checkStorageKey("Disclosure", p.PersistKey)
	}
	if p.Name != "" {
		checkNoControlBytes("Disclosure", "Name", p.Name)
	}
	b := p.Parts.Box(s, PartSummary, PartPanel)
	own := Merge(Safe(p.ExtraAttrs, "open", "name"), Attrs(map[string]string{
		"id":   p.ID,
		"name": p.Name,
	}))
	Mark(own, "data-hui-disclosure")
	if p.Trap {
		Mark(own, "data-hui-disclosure-trap")
	}
	if p.Dismiss {
		Mark(own, "data-hui-disclosure-dismiss")
	}
	if p.PersistKey != "" {
		own["data-hui-disclosure-persist"] = p.PersistKey
	}
	if p.Open {
		Mark(own, "open")
	}
	content := b.Fill(PartPanel, p.Content)
	panelOwn := Internal(nil)
	if content != "" {
		panelOwn = internalIf(ownedSlot(content), nil)
	}
	return b.El("details", PartRoot, own,
		b.El("summary", PartSummary, internalIf(ownedSlot(p.Summary), nil), b.Fill(PartSummary, p.Summary)),
		b.El("div", PartPanel, panelOwn, content),
	)
}

func init() {
	Register(Spec{
		Name:     "Disclosure",
		Anatomy:  []Part{PartRoot, PartSummary, PartPanel},
		Hooks:    []string{"data-hui-disclosure", "data-hui-disclosure-trap", "data-hui-disclosure-dismiss", "data-hui-disclosure-persist"},
		Fillable: []Part{PartSummary, PartPanel},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Disclosure(DisclosureProps{
				Summary: render.Text("Advanced options"),
				Content: render.Text("The panel the summary reveals."),
				Parts:   parts,
			}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "a closed disclosure",
				Why:  "the summary is a real controller and the panel is real content with no script — the module only keeps the accessibility state the platform leaves missing",
				HTML: Disclosure(DisclosureProps{Summary: render.Text("What is this?"),
					Content: render.Text("A native details element.")}, s),
			}, {
				Name: "open, and persistent",
				Why:  "an open state a navigation keeps is opt-in: the persist key names the store the module restores from, so a shell's section survives a client-side page turn",
				HTML: Disclosure(DisclosureProps{Summary: render.Text("Filters"), Open: true,
					Content: render.Text("Persistent across navigation."), PersistKey: "list.filters"}, s),
			}, {
				Name: "the trap posture",
				Why:  "a disclosure used as a drawer contains focus while open — Tab walks inside it — through the widget runtime's containment, released on close and on detach",
				HTML: Disclosure(DisclosureProps{Summary: render.Text("Menu"), Trap: true,
					Content: render.Text("A drawer-shaped disclosure.")}, s),
			}, {
				Name: "a popup",
				Why:  "a disclosure that floats over the page closes on a click outside it, the way a menu does; a section read beside the page does not ask for it",
				HTML: Disclosure(DisclosureProps{Summary: render.Text("Filters"), Dismiss: true,
					Content: render.Text("A panel over the page.")}, s),
			}, {
				Name: "an exclusive group",
				Why:  "two disclosures sharing a name open one at a time in the browser itself — the native accordion, no script on the page at all",
				HTML: render.Join(
					Disclosure(DisclosureProps{Name: "faq", Summary: render.Text("First question"),
						Content: render.Text("One answer.")}, s),
					Disclosure(DisclosureProps{Name: "faq", Summary: render.Text("Second question"),
						Content: render.Text("Another answer.")}, s),
				),
			}}
		},
	})
}
