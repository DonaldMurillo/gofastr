package headless

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Card parts.
const (
	PartCardHeader Part = "card-header"
	PartCardBody   Part = "card-body"
)

// CardProps configures a card.
//
// A card is the one component here with almost no accessibility
// surface, and saying so is the point of the type: the heading level
// is the only decision in it that a screen reader can be hurt by, and
// it is a decision the card cannot make alone.
type CardProps struct {
	// Title renders as a heading. Empty omits it, along with the whole
	// header when Desc is empty too — an empty header is a stripe of
	// padding that looks like a mistake.
	Title string
	// TitleTag is the heading level, "h3" by default. A card does not
	// know how deep in the outline it sits, so a page that nests cards
	// under an h2 says so here rather than letting every card claim
	// the same level and leave the document with no structure to
	// navigate by.
	TitleTag string
	Desc     string
	Footer   render.HTML

	ID         string
	ExtraAttrs html.Attrs

	// Parts. The header is the one fillable part, and the rule it
	// comes from is worth stating: a slot exists only where the
	// component COMPOSES something a prop cannot express. A card's
	// header is built from a title string and a description string,
	// so a header that needs a control in it has no prop to arrive
	// through. The body and the footer already take caller content, so
	// a slot there would be a second way to do one thing — which is
	// worse than none, because half the call sites will use each.
	//
	// Attrs are the opposite: they apply to every part, on every
	// component, because adding an attribute cannot break a structure.
	Parts Parts
	// Strings are the strings this component says. Nil means the English
	// defaults; a layer above sets them from the request's language.
	Strings *Strings
}

// Card renders a card around its body.
func Card(p CardProps, s Classes, body ...render.HTML) render.HTML {
	// The one fillable part, the same list the spec declares: a text
	// Bind may replace what a Slot may (box.go), and nothing else in
	// a card may have its content rewritten.
	b := p.Parts.Box(s, PartCardHeader)
	kids := make([]render.HTML, 0, 3)
	if p.Title != "" || p.Desc != "" || b.Filled(PartCardHeader) {
		head := make([]render.HTML, 0, 2)
		if p.Title != "" {
			tag := orDefault(p.TitleTag, "h3")
			if len(tag) != 2 || tag[0] != 'h' || tag[1] < '1' || tag[1] > '6' {
				panic("headless: Card TitleTag must be h1 to h6, not " + strconv.Quote(tag))
			}
			head = append(head, b.El(tag, PartTitle, nil, render.Text(p.Title)))
		}
		if p.Desc != "" {
			head = append(head, b.El("p", PartDesc, nil, render.Text(p.Desc)))
		}
		kids = append(kids, b.El("div", PartCardHeader, nil, b.Fill(PartCardHeader, group(head...))))
	}
	kids = append(kids, b.El("div", PartCardBody, nil, body...))
	if p.Footer != "" {
		kids = append(kids, b.El("div", PartFooter, nil, p.Footer))
	}
	own := Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{"id": p.ID}))
	return b.El("div", PartRoot, own, kids...)
}

func init() {
	Register(Spec{
		Name:     "Card",
		Anatomy:  []Part{PartRoot, PartTitle, PartDesc, PartCardHeader, PartCardBody, PartFooter},
		Fillable: []Part{PartCardHeader},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Card(CardProps{Title: "Deployments", Parts: parts}, s, render.Text("body"))
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "titled",
				Why:  "the common card: a heading, a line of context, a body",
				HTML: Card(CardProps{Title: "Deployments", Desc: "Last 24 hours"}, s,
					render.HTML("<p>Nothing yet.</p>")),
			}, {
				Name: "bare",
				Why:  "no title and no description means no header — an empty header is a stripe of padding that reads as a mistake",
				HTML: Card(CardProps{}, s, render.HTML("<p>Body only.</p>")),
			}, {
				Name: "footed",
				Why:  "the footer is where the actions that apply to the whole card live",
				HTML: Card(CardProps{Title: "Restart policy", Footer: Button(ButtonProps{Label: "Save", Variant: "primary"}, k.For("Button"))}, s,
					render.HTML("<p>On failure, up to 3 times.</p>")),
			}}
		},
	})
}
