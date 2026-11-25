package headless

import (
	"regexp"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Card parts.
const (
	PartCardHeader Part = "card-header"
	PartCardBody   Part = "card-body"
	PartCardInner  Part = "card-inner"
	PartCardAction Part = "card-action"
	// PartCardTitleLink is the link TitleHref wraps the title's words in.
	PartCardTitleLink Part = "card-title-link"
)

// CardProps configures a card.
type CardProps struct {
	Title        string      // the heading; with Desc empty too, no header renders
	TitleContent render.HTML // composed heading content; wins over Title
	TitleTag     string      // "h1" to "h6", "h3" by default
	Desc         string
	Href         string // makes the whole card one link; a URL the anchor policy refuses panics
	Footer       render.HTML
	Action       render.HTML // a control at the header's end; refused with Href

	// TitleHref links the title's words, the card's one link, which the
	// styling layer can stretch over the whole card while the card's
	// other controls stay reachable. Refused with Href, without a title,
	// with a TitleContent holding a link, or for a URL the anchor policy
	// refuses.
	TitleHref string
	// Article renders the root as an <article>: a card that stands on its
	// own, one of a feed or a grid of items. Refused with Href, whose root
	// is the link.
	Article bool

	ID         string // with a title, the title's id is "<ID>-title"
	ExtraAttrs html.Attrs

	Parts   Parts    // PartCardHeader is the one fillable part
	Strings *Strings // nil means the English defaults
}

// Card renders a card around its body.
func Card(p CardProps, s Classes, body ...render.HTML) render.HTML {
	if p.Action != "" && p.Href != "" {
		panic("headless: Card Action and Href together nest a control inside the card's link")
	}
	if p.Article && p.Href != "" {
		panic("headless: Card Article and Href together — a linked card's root is the link; give an article its link with TitleHref")
	}
	var titleHref string
	if p.TitleHref != "" {
		switch {
		case p.Href != "":
			panic("headless: Card TitleHref and Href together nest the title's link inside the card's link")
		case p.Title == "" && p.TitleContent == "":
			panic("headless: Card TitleHref needs a Title — the link's words are the title's")
		case interactiveContentRe.MatchString(string(p.TitleContent)):
			panic("headless: Card TitleHref with a TitleContent that holds a link, a control or a tabindex nests interactive content in the title's link")
		}
		titleHref = urlsafe.CleanAnchor(p.TitleHref)
		if titleHref == "" {
			panic("headless: Card TitleHref " + strconv.Quote(p.TitleHref) + " is not a URL the anchor policy allows")
		}
	}
	b := p.Parts.Box(s, PartCardHeader)
	mark := p.Href == ""
	kids := make([]render.HTML, 0, 3)
	if p.Title != "" || p.TitleContent != "" || p.Desc != "" || p.Action != "" || b.Filled(PartCardHeader) {
		head := make([]render.HTML, 0, 3)
		if p.Title != "" || p.TitleContent != "" {
			tag := orDefault(p.TitleTag, "h3")
			if len(tag) != 2 || tag[0] != 'h' || tag[1] < '1' || tag[1] > '6' {
				panic("headless: Card TitleTag must be h1 to h6, not " + strconv.Quote(tag))
			}
			title := p.TitleContent
			if title == "" {
				title = render.Text(p.Title)
			}
			if titleHref != "" {
				title = b.El("a", PartCardTitleLink, Attrs(map[string]string{"href": titleHref}), title)
			}
			var titleOwn html.Attrs
			if p.ID != "" {
				titleOwn = Attrs(map[string]string{"id": p.ID + "-title"})
			}
			// TitleContent is the caller's markup: a title holding it is never marked.
			head = append(head, b.El(tag, PartTitle, internalIf(mark && p.Action != "" && p.TitleContent == "", titleOwn), title))
		}
		if p.Desc != "" {
			head = append(head, b.El("p", PartDesc, internalIf(mark && p.Action != "", nil), render.Text(p.Desc)))
		}
		if p.Action != "" {
			head = append(head, b.El("div", PartCardAction, nil, p.Action))
		}
		headOwn := internalIf(mark && p.Action == "" && !b.Filled(PartCardHeader), nil)
		kids = append(kids, b.El("div", PartCardHeader, headOwn, b.Fill(PartCardHeader, group(head...))))
	}
	kids = append(kids, b.El("div", PartCardBody, internalIf(mark && len(body) == 0, nil), body...))
	if p.Footer != "" {
		kids = append(kids, b.El("div", PartFooter, nil, p.Footer))
	}
	own := Merge(Safe(p.ExtraAttrs, "href"), Attrs(map[string]string{"id": p.ID}))
	if p.Href != "" {
		href := urlsafe.CleanAnchor(p.Href)
		if href == "" {
			panic("headless: Card Href " + strconv.Quote(p.Href) + " is not a URL the anchor policy allows")
		}
		own["href"] = href
		var innerOwn html.Attrs
		if !b.Filled(PartCardHeader) && p.TitleContent == "" && len(body) == 0 && p.Footer == "" {
			innerOwn = Internal(nil)
		}
		inner := b.El("div", PartCardInner, innerOwn, kids...)
		return b.El("a", PartRoot, own, inner)
	}
	tag := "div"
	if p.Article {
		tag = "article"
	}
	return b.El(tag, PartRoot, own, kids...)
}

func init() {
	Register(Spec{
		Name:     "Card",
		Anatomy:  []Part{PartRoot, PartTitle, PartCardTitleLink, PartDesc, PartCardHeader, PartCardAction, PartCardBody, PartCardInner, PartFooter},
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
			}, {
				Name: "with a header action",
				Why:  "a feed's way to its full list sits at the header's end, beside the title it belongs to, not in a footer under the last row",
				HTML: Card(CardProps{Title: "Recent activity", Action: Button(ButtonProps{Label: "Audit log", Href: "/audit", Variant: "ghost"}, k.For("Button"))}, s,
					render.HTML("<p>Ada created INV-1.</p>")),
			}, {
				Name: "an article whose title is its link",
				Why:  "a card that is one item of a feed is an article, and its title carries the one link a styling layer stretches over the card, so the link's name is the title rather than every word on the card and the header action stays its own control",
				HTML: Card(CardProps{Title: "Release 0.98", Desc: "Nov 23", TitleHref: "/blog/0-98", Article: true,
					Action: Button(ButtonProps{Label: "Share", Variant: "ghost"}, k.For("Button"))}, s,
					render.HTML("<p>Breadcrumbs, copy and cards.</p>")),
			}, {
				Name: "the whole card is the link",
				Why:  "an href on the card makes the surface itself the affordance — one focusable link wrapping what the div showed, so the click target is the card a reader can see rather than a Read more crammed into the footer",
				HTML: Card(CardProps{Title: "blog", Desc: "2 vCPU, 4 GB", Href: "/apps/blog"}, s,
					render.HTML("<p>Deployed 3 days ago.</p>")),
			}}
		},
	})
}

// interactiveContentRe matches what an <a> may not hold: an opening
// link, control or embedded-media tag in any letter case (and not
// <abbr>, <article>, <aside> and the like, whose names only start the
// same), or a tabindex or contenteditable attribute.
var interactiveContentRe = regexp.MustCompile(`(?i)<(a|button|input|select|textarea|label|details|summary|iframe|embed|object|audio|video)[\s>/]|\s(tabindex|contenteditable)\s*=`)
