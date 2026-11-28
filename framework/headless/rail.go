package headless

import (
	"net/url"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

const (
	PartRailList    Part = "rail-list"
	PartRailItem    Part = "rail-item"
	PartRailLink    Part = "rail-link"
	PartRailEyebrow Part = "rail-eyebrow"
	PartRailCount   Part = "rail-count"
)

// RailItem is one entry in the rail.
type RailItem struct {
	// Anchor is the fragment id the entry links to, without the #. Required.
	Anchor string
	// Text is the visible link label. Required.
	Text string
	// Eyebrow is the optional leading chip beside the label.
	Eyebrow string
	// Count is the optional trailing chip.
	Count string
}

// RailProps configures a rail.
type RailProps struct {
	// Label names the landmark and heads the list. Required.
	Label string
	// Items are the entries, in display order. Required.
	Items []RailItem
	// ObserveSelector is the region whose sections mark the active entry; empty renders a static rail.
	ObserveSelector string
	// TargetSelector narrows the sections; empty is the module's default, h2 to h4 with an id.
	TargetSelector string

	ID         string
	ExtraAttrs html.Attrs

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
			b.El("a", PartRailLink, Attrs(map[string]string{"href": fragmentHref(it.Anchor)}),
				linkContent...)))
	}

	return b.El("aside", PartRoot, own,
		b.El("div", PartLabel, Internal(nil), render.Text(p.Label)),
		b.El("ol", PartRailList, Internal(nil), items...),
	)
}

func fragmentHref(id string) string {
	u := url.URL{Fragment: id}
	return u.String()
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
