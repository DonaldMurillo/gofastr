package headless

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// The rail: a sticky in-page navigation whose entries are real
// fragment links. Every entry is an anchor to an id on the page, so
// the whole contract works with no script — the module's contribution
// is only the active state, aria-current and a class on the link
// whose target is in view. The observer is owned by this package's
// headless-rail module and shared with the table of contents (one
// observer, not two drifting implementations).

// Rail parts. The label is a plain label, not a heading: the rail is
// a complementary landmark already named by its aria-label, and a
// heading here would inject a stray, out-of-order entry into the page
// outline.
const (
	PartRailList    Part = "rail-list"
	PartRailItem    Part = "rail-item"
	PartRailLink    Part = "rail-link"
	PartRailEyebrow Part = "rail-eyebrow"
	PartRailCount   Part = "rail-count"
)

// RailItem is one entry in the rail.
type RailItem struct {
	// Anchor is the fragment id of the section this entry links to,
	// without the leading #. Required: an entry that links nowhere is
	// not an entry.
	Anchor string
	// Text is the visible link label. Required.
	Text string
	// Eyebrow is the leading chip beside the label (a number, a
	// glyph); empty hides it.
	Eyebrow string
	// Count is the trailing chip (a document count); empty hides it.
	Count string
}

// RailProps configures a rail.
type RailProps struct {
	// Label names the landmark and heads the list. Required: a
	// complementary landmark with no name is a region a screen reader
	// cannot jump to by name.
	Label string
	// Items are the entries, in display order. Required and non-empty.
	Items []RailItem
	// ObserveSelector is the CSS selector of the region whose sections
	// the module watches to mark the active entry. Empty renders a
	// purely static rail: the links work, nothing is marked.
	ObserveSelector string
	// TargetSelector narrows which elements inside the observed region
	// count as sections. Empty takes the module's default (the
	// h2/h3/h4 elements that carry an id).
	TargetSelector string

	ID         string
	ExtraAttrs html.Attrs

	// Parts: attrs on the root and the entries. No part is fillable:
	// every element the rail draws carries its own guarantee.
	Parts Parts
}

// Rail renders the sticky in-page navigation.
func Rail(p RailProps, s Classes) render.HTML {
	checkLabel("Rail", "Label", p.Label)
	if len(p.Items) == 0 {
		panic("headless: Rail requires at least one item — an empty landmark is a name with nothing under it")
	}
	if p.ObserveSelector != "" {
		checkSelector("Rail", "ObserveSelector", p.ObserveSelector)
	}
	if p.TargetSelector != "" {
		checkSelector("Rail", "TargetSelector", p.TargetSelector)
	}
	ids := make([]string, 0, len(p.Items))
	for _, it := range p.Items {
		checkFragmentID("Rail item", "Anchor", it.Anchor)
		if scrubControlBytes(it.Text) == "" {
			panic("headless: Rail item with Anchor " + it.Anchor + " has no Text — a link with no label is not a link")
		}
		ids = append(ids, it.Anchor)
	}
	checkNoDuplicateIDs("Rail", ids)

	b := p.Parts.Box(s)
	own := Merge(Safe(p.ExtraAttrs, "aria-label"), Attrs(map[string]string{
		"aria-label": p.Label,
		"id":         p.ID,
	}))
	if p.ObserveSelector != "" {
		// The hook is the module's trigger, so it rides only on a rail
		// the module can do something with: a static rail keeps its
		// links and loads nothing.
		Mark(own, "data-hui-rail")
		own["data-hui-rail-observe"] = p.ObserveSelector
		if p.TargetSelector != "" {
			own["data-hui-rail-target"] = p.TargetSelector
		}
	}

	items := make([]render.HTML, 0, len(p.Items))
	for _, it := range p.Items {
		text := render.Text(scrubControlBytes(it.Text))
		var linkContent []render.HTML
		if eyebrow := scrubControlBytes(it.Eyebrow); eyebrow != "" {
			linkContent = append(linkContent, b.El("span", PartRailEyebrow, nil, render.Text(eyebrow)))
		}
		linkContent = append(linkContent, text)
		if count := scrubControlBytes(it.Count); count != "" {
			linkContent = append(linkContent, b.El("span", PartRailCount, nil, render.Text(count)))
		}
		items = append(items, b.El("li", PartRailItem, nil,
			b.El("a", PartRailLink, Attrs(map[string]string{"href": "#" + it.Anchor}),
				linkContent...)))
	}

	return b.El("aside", PartRoot, own,
		b.El("div", PartLabel, nil, render.Text(p.Label)),
		b.El("ol", PartRailList, nil, items...),
	)
}

func init() {
	Register(Spec{
		Name:    "Rail",
		Anatomy: []Part{PartRoot, PartLabel, PartRailList, PartRailItem, PartRailLink, PartRailEyebrow, PartRailCount},
		Hooks:   []string{"data-hui-rail", "data-hui-rail-observe", "data-hui-rail-target"},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Rail(RailProps{Label: "On this page", Items: []RailItem{
				{Anchor: "overview", Text: "Overview", Eyebrow: "01"},
				{Anchor: "details", Text: "Details", Eyebrow: "02", Count: "9"},
			}, ObserveSelector: "main", Parts: parts}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "an observed rail",
				Why:  "every entry is a real fragment link before any script runs, and the observer is armed by the module the root hook loads",
				HTML: Rail(RailProps{Label: "By intent", ObserveSelector: "#sections", TargetSelector: "section[id]", Items: []RailItem{
					{Anchor: "modeling", Text: "Modeling", Eyebrow: "01", Count: "9"},
					{Anchor: "serving", Text: "Serving", Eyebrow: "02", Count: "9"},
				}}, s),
			}, {
				Name: "a static rail",
				Why:  "with no observation selector the links still work and nothing is marked — the active state is enhancement, never the only state",
				HTML: Rail(RailProps{Label: "Sections", Items: []RailItem{
					{Anchor: "one", Text: "One"},
				}}, s),
			}, {
				Name: "a bare entry",
				Why:  "the eyebrow and the count are optional, so the same list serves the plain and the annotated rail without a second anatomy",
				HTML: Rail(RailProps{Label: "Steps", Items: []RailItem{
					{Anchor: "install", Text: "Install"},
					{Anchor: "run", Text: "Run"},
				}}, s),
			}}
		},
	})
}
