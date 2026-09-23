package headless

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// The disclosure: a native <details> whose open state is the browser's
// and whose accessibility state the module keeps honest. The no-script
// contract is the element itself — a summary a reader activates, a
// panel the UA shows — so the module adds only what the platform
// leaves missing: the aria-expanded mirror on the controller (a native
// summary reports as a button with no state), Escape closing the
// deepest open disclosure with focus returned to its controller, the
// close-on-navigation for disclosures that are not persistent, and the
// optional focus containment of the trap mode. The trap is the widget
// runtime's technique — Tab containment over the kernel's shared focus
// selector — not a second trap implementation.

// Disclosure parts. The summary and the panel are shared with Menu,
// which composes this anatomy for its dropdown panels.
const (
	PartSummary Part = "summary"
	PartPanel   Part = "panel"
)

// DisclosureProps configures one disclosure.
type DisclosureProps struct {
	// Summary is the always-visible controller. Required and
	// non-blank: a details whose summary says nothing is a button with
	// no name.
	Summary render.HTML
	// Content is the revealed panel.
	Content render.HTML
	// Open renders the details expanded.
	Open bool
	// Trap opts the open disclosure into focus containment: Tab walks
	// inside it and comes back, the drawer posture. The containment is
	// the widget runtime's own (the kernel's focus selector), armed by
	// the module while the disclosure is open.
	Trap bool
	// Name, when set, is the details element's name attribute: the
	// browser groups disclosures that share it and opens one at a time
	// (the native accordion) — opening one closes the others with no
	// script at all. Control bytes are refused: the value is a group
	// key the browser matches verbatim.
	Name string
	// PersistKey, when set, keeps the open state across client-side
	// navigation AND restores it on arrival from the session store,
	// namespaced and component-encoded so two disclosures never share
	// one key. Empty means the ordinary dialect: closed by a
	// navigation, whatever the reader left open.
	PersistKey string

	ID         string
	ExtraAttrs html.Attrs

	// Parts: attrs on the root, the summary and the panel. The summary
	// is fillable — its content is the caller's — and so is the panel.
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
	if p.PersistKey != "" {
		own["data-hui-disclosure-persist"] = p.PersistKey
	}
	if p.Open {
		Mark(own, "open")
	}
	return b.El("details", PartRoot, own,
		b.El("summary", PartSummary, nil, b.Fill(PartSummary, p.Summary)),
		b.El("div", PartPanel, nil, b.Fill(PartPanel, p.Content)),
	)
}

func init() {
	Register(Spec{
		Name:     "Disclosure",
		Anatomy:  []Part{PartRoot, PartSummary, PartPanel},
		Hooks:    []string{"data-hui-disclosure", "data-hui-disclosure-trap", "data-hui-disclosure-persist"},
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
