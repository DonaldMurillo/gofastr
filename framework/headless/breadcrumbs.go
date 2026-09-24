package headless

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// The breadcrumb trail: a navigation landmark whose ordered list walks
// the reader from the page they are on up the hierarchy that holds
// it. The whole trail is server-rendered — an ordered list, because
// the order IS the hierarchy — and the current page is the last item,
// marked aria-current="page" and rendered as text, not a link: a
// trail that links to itself is a trail with a dead end in it. No
// script: nothing here changes in-page.

// Breadcrumb parts.
const (
	PartBreadcrumbList      Part = "breadcrumb-list"
	PartBreadcrumbItem      Part = "breadcrumb-item"
	PartBreadcrumbLink      Part = "breadcrumb-link"
	PartBreadcrumbSeparator Part = "breadcrumb-separator"
)

// Breadcrumb is one step in the trail.
type Breadcrumb struct {
	// Text is the step's visible label. Required.
	Text string
	// Href is the step's destination. Empty on the last step (the
	// current page names itself, it does not link to itself); a value
	// on the last step links it unless Current is also set.
	Href string
	// Current marks the step as the current page whatever its Href:
	// rendered as text with aria-current="page" rather than a link,
	// for a page that appears in its own trail with a link.
	Current bool
}

// BreadcrumbsProps configures the trail.
type BreadcrumbsProps struct {
	// Label names the navigation landmark. Empty takes
	// Strings.BreadcrumbsLabel.
	Label string
	// Items are the steps, shallowest first. Required and non-empty:
	// a trail with no steps is a landmark that says nothing.
	Items []Breadcrumb

	ID         string
	ExtraAttrs html.Attrs

	// Parts: attrs on the root and the list. No part is fillable: the
	// trail is the contract.
	Parts   Parts
	Strings *Strings
}

// Breadcrumbs renders the trail.
func Breadcrumbs(p BreadcrumbsProps, s Classes) render.HTML {
	if len(p.Items) == 0 {
		panic("headless: Breadcrumbs requires Items — an empty trail is a navigation landmark that names nothing; render nothing at all instead")
	}
	w := p.Strings.Resolve()
	for i, it := range p.Items {
		checkLabel("Breadcrumb "+strconv.Itoa(i), "Text", it.Text)
	}
	label := p.Label
	if label == "" {
		label = w.BreadcrumbsLabel
	}

	b := p.Parts.Box(s)
	own := Merge(Safe(p.ExtraAttrs, "aria-label"), Attrs(map[string]string{
		"aria-label": label,
		"id":         p.ID,
	}))

	items := make([]render.HTML, 0, len(p.Items))
	for i, c := range p.Items {
		// A dangerous Href (javascript:, vbscript:, data:, a
		// protocol-relative host, a smuggled control byte) is dropped
		// and the step degrades to plain text rather than a clickable
		// XSS vector. An empty result means "no link", which folds
		// into the same plain-text path.
		href := urlsafe.CleanAnchor(c.Href)
		current := c.Current || href == ""
		kids := []render.HTML{}
		if i > 0 {
			// The separator is real markup so its glyph can be styled
			// without a pseudo-element, and aria-hidden so the trail
			// reads "Docs, Modeling, Entities" — the order already
			// says what the slashes would.
			kids = append(kids, b.El("span", PartBreadcrumbSeparator,
				Attrs(map[string]string{"aria-hidden": "true"}), render.Text("/")))
		}
		if current {
			kids = append(kids, b.El("span", PartBreadcrumbLink,
				Attrs(map[string]string{"aria-current": "page"}),
				render.Text(scrubControlBytes(c.Text))))
		} else {
			kids = append(kids, b.El("a", PartBreadcrumbLink,
				Attrs(map[string]string{"href": href}),
				render.Text(scrubControlBytes(c.Text))))
		}
		items = append(items, b.El("li", PartBreadcrumbItem, nil, kids...))
	}

	return b.El("nav", PartRoot, own,
		b.El("ol", PartBreadcrumbList, nil, items...),
	)
}

func init() {
	Register(Spec{
		Name:    "Breadcrumbs",
		Anatomy: []Part{PartRoot, PartBreadcrumbList, PartBreadcrumbItem, PartBreadcrumbLink, PartBreadcrumbSeparator},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Breadcrumbs(BreadcrumbsProps{
				Items: []Breadcrumb{
					{Text: "Docs", Href: "/docs/"},
					{Text: "Modeling", Href: "/docs/#modeling"},
					{Text: "Entities"},
				},
				Parts: parts,
			}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "a trail ending on the current page",
				Why:  "the ordered list is the hierarchy and the last step names the page as text with aria-current, so a reader knows where they are without a link that goes nowhere",
				HTML: Breadcrumbs(BreadcrumbsProps{Items: []Breadcrumb{
					{Text: "Home", Href: "/"},
					{Text: "Docs", Href: "/docs/"},
					{Text: "Entities"},
				}}, s),
			}, {
				Name: "the unlabelled landmark names itself",
				Why:  "a trail with no label of its own still names its navigation landmark — the default word is the one Strings carries, so a translated page says it in the reader's language",
				HTML: Breadcrumbs(BreadcrumbsProps{Items: []Breadcrumb{
					{Text: "Home", Href: "/"},
					{Text: "Here"},
				}}, s),
			}, {
				Name: "a page linked inside its own trail",
				Why:  "Current turns a linked step into the page's name for itself — the href the caller supplied stays on the steps around it, and the trail still ends in text",
				HTML: Breadcrumbs(BreadcrumbsProps{Items: []Breadcrumb{
					{Text: "Settings", Href: "/settings"},
					{Text: "Profile", Href: "/settings/profile", Current: true},
				}}, s),
			}}
		},
	})
}
