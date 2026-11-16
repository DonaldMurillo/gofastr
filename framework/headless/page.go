package headless

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// PageHeader parts.
const (
	PartPageEyebrow  Part = "page-eyebrow"
	PartPageText     Part = "page-text"
	PartPageSubtitle Part = "page-subtitle"
	PartPageActions  Part = "page-actions"
	PartPageTitleRow Part = "page-title-row"
)

// PageHeaderProps is the top of a page: its heading, qualifiers and page-level actions.
type PageHeaderProps struct {
	Title    string      // required: the page's heading
	Level    int         // the heading level, 1 by default
	Eyebrow  string      // a kicker above the title, hidden from assistive tech
	Subtitle string      // the supporting line under the title
	Badge    render.HTML // status beside the heading, outside its accessible name
	Actions  render.HTML // the page-level controls in the trailing slot

	ID         string
	ExtraAttrs html.Attrs

	Parts Parts
}

// PageHeader renders a plain <header> that claims no role.
func PageHeader(p PageHeaderProps, s Classes) render.HTML {
	b := p.Parts.Box(s)
	if p.Title == "" {
		panic("headless: PageHeader requires Title — a header that names nothing is padding above the content")
	}
	textOwn, partOwn := Internal(nil), html.Attrs(nil)
	if p.Badge != "" {
		textOwn, partOwn = nil, Internal(nil)
	}
	kids := make([]render.HTML, 0, 3)
	if p.Eyebrow != "" {
		kids = append(kids, b.El("p", PartPageEyebrow,
			Merge(Attrs(map[string]string{"aria-hidden": "true"}), partOwn), render.Text(p.Eyebrow)))
	}
	level := p.Level
	if level < 1 || level > 6 {
		level = 1
	}
	title := b.El(headingTag(level), PartTitle, partOwn, render.Text(p.Title))
	if p.Badge != "" {
		title = b.El("div", PartPageTitleRow, nil, title, p.Badge)
	}
	kids = append(kids, title)
	if p.Subtitle != "" {
		kids = append(kids, b.El("p", PartPageSubtitle, partOwn, render.Text(p.Subtitle)))
	}
	text := b.El("div", PartPageText, textOwn, kids...)

	out := []render.HTML{text}
	if p.Actions != "" {
		out = append(out, b.El("div", PartPageActions, nil, p.Actions))
	}
	return b.El("header", PartRoot,
		Merge(Safe(p.ExtraAttrs, "role"), Attrs(map[string]string{"id": p.ID})),
		out...)
}

func init() {
	Register(Spec{
		Name: "PageHeader",
		WithParts: func(s Classes, parts Parts) render.HTML {
			return PageHeader(PageHeaderProps{Title: "Apps", Badge: Badge(BadgeProps{Label: "Ready"}, nil), Parts: parts}, s)
		},
		Anatomy: []Part{PartRoot, PartPageEyebrow, PartTitle, PartPageTitleRow, PartPageSubtitle, PartPageText, PartPageActions},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "page top",
				Why:  "the title is the page's h1 and the actions beside it are the page's own — a reader who lands mid-scroll jumps here to learn where they are",
				HTML: PageHeader(PageHeaderProps{Title: "Apps", Subtitle: "Eleven apps across three workers.",
					Actions: Button(ButtonProps{Label: "New app", Variant: "primary"}, k.For("Button"))}, s),
			}, {
				Name: "status beside the title",
				Why:  "a badge is the page's state, not part of its name: it rides beside the heading where the eye lands, and the screen reader hears the title alone",
				HTML: PageHeader(PageHeaderProps{Title: "Billing", Badge: Badge(BadgeProps{Label: "On track"}, k.For("Badge"))}, s),
			}, {
				Name: "eyebrow",
				Why:  "a kicker above the title is hidden from the tree: it repeats what the title or the navigation already says, and hearing the page's name twice is the alternative",
				HTML: PageHeader(PageHeaderProps{Eyebrow: "Customers", Title: "Northwind Traders"}, s),
			}, {
				Name: "deeper in the outline",
				Why:  "a header for a sub-page inside a page claims level 2 rather than a second h1 — the outline is how a screen reader user moves, and two h1s on one page is a hole in it",
				HTML: PageHeader(PageHeaderProps{Title: "Deployment history", Level: 2}, s),
			}}
		},
	})
}

// EmptyState parts.
const (
	PartEmptyTitle Part = "empty-title"
	PartEmptyDesc  Part = "empty-desc"
	PartEmptyAct   Part = "empty-action"
)

// EmptyStateProps is what a list with nothing in it shows instead of the list.
type EmptyStateProps struct {
	Title       string      // required: the heading and the region's name
	Level       int         // the heading level, 3 by default
	Description string      // what would be here, or what to do about it
	Action      render.HTML // the way out: "New app", "Clear the filter"

	ID         string
	ExtraAttrs html.Attrs

	Parts Parts
}

// EmptyState renders a role=region named by its heading: through aria-labelledby
// "<ID>-title" with an ID, through an aria-label of Title without one.
func EmptyState(p EmptyStateProps, s Classes) render.HTML {
	b := p.Parts.Box(s)
	if p.Title == "" {
		panic("headless: EmptyState requires Title — the title is also the region's name, and an unnamed region is a hole with a border")
	}

	level := p.Level
	if level < 1 || level > 6 {
		level = 3
	}
	titleAttrs := Internal(nil)
	own := Merge(Safe(p.ExtraAttrs, "role", "aria-label", "aria-labelledby"), Attrs(map[string]string{
		"id": p.ID,
	}))
	own["role"] = "region"
	if p.ID != "" {
		titleAttrs = Internal(Attrs(map[string]string{"id": p.ID + "-title"}))
		own["aria-labelledby"] = p.ID + "-title"
	} else {
		own["aria-label"] = p.Title
	}
	kids := []render.HTML{
		b.El(headingTag(level), PartEmptyTitle, titleAttrs, render.Text(p.Title)),
	}
	if p.Description != "" {
		kids = append(kids, b.El("p", PartEmptyDesc, Internal(nil), render.Text(p.Description)))
	}
	if p.Action != "" {
		kids = append(kids, b.El("div", PartEmptyAct, nil, p.Action))
	}
	return b.El("div", PartRoot, own, kids...)
}

func init() {
	Register(Spec{
		Name: "EmptyState",
		WithParts: func(s Classes, parts Parts) render.HTML {
			return EmptyState(EmptyStateProps{Title: "No apps", Parts: parts}, s)
		},
		Anatomy: []Part{PartRoot, PartEmptyTitle, PartEmptyDesc, PartEmptyAct},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "nothing here yet",
				Why:  "the region is named by its own heading, so a reader who lands on it is told what is absent and offered the way out — a dead end with no action is a page the back button owns",
				HTML: EmptyState(EmptyStateProps{Title: "No apps yet",
					Description: "Deploy your first app and it will appear here.",
					Action:      Button(ButtonProps{Label: "New app", Variant: "primary"}, k.For("Button"))}, s),
			}, {
				Name: "an empty result",
				Why:  "a search that matched nothing names the absence after itself; with no explicit ID the name is an aria-label, so two panels that share a title share no id",
				HTML: EmptyState(EmptyStateProps{Title: "No apps match", Level: 2,
					Description: "Clear a filter or try another name."}, s),
			}}
		},
	})
}

// StatCard parts.
const (
	PartStatValue Part = "stat-value"
	PartStatTrend Part = "stat-trend"
	PartStatLink  Part = "stat-link"
)

// StatCardProps is one metric held up for the page: a labelled value and how it moves.
type StatCardProps struct {
	Label     string      // required: what is counted
	Value     string      // required: the number, formatted by the caller
	Trend     string      // the movement, in words ("+12% vs. last week")
	Direction string      // "up", "down" or "flat": a variant hint, never the only carrier
	Href      string      // links the label to what the number counts
	Icon      render.HTML // a decorative glyph in the card's head
	Action    render.HTML // a control in the head, outside the label's link

	ID         string
	ExtraAttrs html.Attrs

	Parts Parts
}

// StatCard renders label, value and trend, in that reading order.
func StatCard(p StatCardProps, s Classes) render.HTML {
	b := p.Parts.Box(s)
	if p.Label == "" {
		panic("headless: StatCard requires Label — a value with no label is a number the reader has to guess at")
	}
	if p.Value == "" {
		panic("headless: StatCard requires Value — a label with no value is a box with a caption")
	}
	switch p.Direction {
	case "", "up", "down", "flat":
	default:
		panic(`headless: StatCard Direction must be "up", "down" or "flat", not ` + strconv.Quote(p.Direction) + " — a direction the trend cannot mean paints a movement the reader cannot trust")
	}
	trendAttrs := Attrs(nil)
	if p.Trend != "" && p.Direction != "" {
		trendAttrs["data-direction"] = p.Direction
		if cls := s.Variant(PartStatTrend, p.Direction); cls != "" {
			trendAttrs["class"] = cls
		}
	}
	label := render.Text(p.Label)
	if p.Href != "" {
		href := urlsafe.CleanAnchor(p.Href)
		if href == "" {
			panic("headless: StatCard Href " + strconv.Quote(p.Href) + " is not a URL the anchor policy allows")
		}
		label = b.El("a", PartStatLink, html.Attrs{"href": href}, label)
	}
	labelEl := b.El("p", PartLabel, Internal(nil), label)
	var kids []render.HTML
	if p.Icon != "" || p.Action != "" {
		var head []render.HTML
		if p.Icon != "" {
			head = append(head, b.El("span", PartIcon, html.Attrs{"aria-hidden": "true"}, p.Icon))
		}
		head = append(head, labelEl, p.Action)
		kids = append(kids, b.El("div", PartHeader, nil, head...))
	} else {
		kids = append(kids, labelEl)
	}
	kids = append(kids, b.El("p", PartStatValue, Internal(nil), render.Text(p.Value)))
	if p.Trend != "" {
		kids = append(kids, b.El("p", PartStatTrend, Internal(trendAttrs), render.Text(p.Trend)))
	}
	return b.El("div", PartRoot,
		Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{"id": p.ID})), kids...)
}

func init() {
	Register(Spec{
		Name: "StatCard",
		WithParts: func(s Classes, parts Parts) render.HTML {
			return StatCard(StatCardProps{Label: "Builds", Value: "12", Parts: parts}, s)
		},
		Anatomy: []Part{PartRoot, PartHeader, PartIcon, PartLabel, PartStatLink, PartStatValue, PartStatTrend},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "with a trend",
				Why:  "label before value, and the trend's direction carried by its own words — the colour the class map paints from Direction is emphasis, never the only carrier",
				HTML: StatCard(StatCardProps{Label: "Active users", Value: "12,483",
					Trend: "+12% vs. last week", Direction: "up"}, s),
			}, {
				Name: "the number alone",
				Why:  "a card with no trend renders without one, and the label still arrives before the value it names",
				HTML: StatCard(StatCardProps{Label: "Queued builds", Value: "2"}, s),
			}, {
				Name: "linked, with an icon and an action",
				Why:  "the label links to what it counts, the icon is hidden from assistive technology, and the action sits in the head outside the link so no interactive element nests in another",
				HTML: StatCard(StatCardProps{Label: "Invoices", Value: "75", Href: "/admin/entities/invoices",
					Icon:   SpecimenGlyph,
					Action: Button(ButtonProps{Label: "New", Variant: "secondary"}, k.For("Button"))}, s),
			}}
		},
	})
}

// DetailList parts.
const (
	PartDetailRow   Part = "detail-row"
	PartDetailTerm  Part = "detail-term"
	PartDetailValue Part = "detail-value"
)

// DetailRow is one labelled value: the term in a <dt>, the value in the <dd> after it.
type DetailRow struct {
	Label string      // required: the term
	Value render.HTML // required: render the empty-value dash for a missing value
}

// DetailListProps is a record read as label/value pairs.
type DetailListProps struct {
	Rows []DetailRow // in reading order; at least one

	ID         string
	ExtraAttrs html.Attrs

	Parts Parts
}

// DetailList renders the record as a <dl>, each dt/dd pair in a row div.
func DetailList(p DetailListProps, s Classes) render.HTML {
	b := p.Parts.Box(s)
	if len(p.Rows) == 0 {
		panic("headless: DetailList requires at least one Row — an empty description list is invalid markup a screen reader announces as nothing")
	}
	rows := make([]render.HTML, 0, len(p.Rows))
	for _, r := range p.Rows {
		if r.Label == "" {
			panic("headless: DetailRow requires Label — a value with no term is a fact nobody can look up")
		}
		if r.Value == "" {
			panic("headless: DetailRow requires Value — render the empty-value dash for a missing value, so the absence is deliberate")
		}
		rows = append(rows, b.El("div", PartDetailRow, internalIf(ownedSlot(r.Value), nil),
			b.El("dt", PartDetailTerm, Internal(nil), render.Text(r.Label)),
			b.El("dd", PartDetailValue, nil, r.Value),
		))
	}
	return b.El("dl", PartRoot,
		Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{"id": p.ID})), rows...)
}

func init() {
	Register(Spec{
		Name: "DetailList",
		WithParts: func(s Classes, parts Parts) render.HTML {
			return DetailList(DetailListProps{Rows: []DetailRow{{Label: "Name", Value: render.Text("blog")}}, Parts: parts}, s)
		},
		Anatomy: []Part{PartRoot, PartDetailRow, PartDetailTerm, PartDetailValue},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "a record",
				Why:  "a dt and the dd after it are one pair to assistive technology in a way a grid of divs never is, and the pair survives every restyling because the relationship is the element",
				HTML: DetailList(DetailListProps{Rows: []DetailRow{
					{Label: "Name", Value: render.Text("blog")},
					{Label: "Status", Value: Badge(BadgeProps{Label: "running"}, k.For("Badge"))},
					{Label: "Restarts", Value: render.Text("263")},
				}}, s),
			}}
		},
	})
}
