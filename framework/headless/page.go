package headless

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// The page-level surfaces: the header that names a page, the state
// that says a page has nothing in it, the one metric a dashboard
// holds up, and the label/value pairs a record screen reads from.
//
// What they share is the reason they are primitives at all: each one
// names itself for assistive technology in a way the elements alone
// cannot. A PageHeader is the heading a screen reader jumps to first;
// an EmptyState is a region named by its own heading, so "no results"
// is a findable, nameable place rather than a hole; a DetailList is a
// description list, where the dt/dd pairing is the contract; a
// StatCard is the one member with no contract at all — its label,
// value and trend are plain text in reading order — and it is here
// so the styled layer composes it rather than hand-rolling it.

// PageHeader parts.
const (
	PartPageEyebrow  Part = "page-eyebrow"
	PartPageText     Part = "page-text"
	PartPageSubtitle Part = "page-subtitle"
	PartPageActions  Part = "page-actions"
)

// PageHeaderProps is the top of a page: a heading, the words that
// qualify it, and the actions that apply to the whole page.
type PageHeaderProps struct {
	// Title is the page's heading. Required: the header exists to
	// name the page, and a header with no heading is padding.
	Title string
	// Level is the heading level, 1 by default. The page's own header
	// is the h1; a header for a sub-page inside a page says 2 here
	// rather than leaving the outline to guess.
	Level int
	// Eyebrow is a short kicker above the title ("Customers"). It is
	// aria-hidden: it repeats what the title or the navigation
	// already says, in fewer words, and hearing both is hearing the
	// page's name twice.
	Eyebrow string
	// Subtitle is the supporting line under the title.
	Subtitle string
	// Actions are the page-level controls — New, Import, Filter — in
	// the trailing slot. They apply to the page, not to an item in
	// it; an action about one row belongs on that row.
	Actions render.HTML

	ID         string
	ExtraAttrs html.Attrs

	// Parts: attrs and binds on every part the header draws.
	Parts Parts
}

// PageHeader renders the page top.
//
// The element is a plain <header>: role="banner" is the top-level
// page header's to claim, and whether this header is that one is a
// decision the page makes, not the header — so the component claims
// no role and the browser's header semantics stand.
func PageHeader(p PageHeaderProps, s Classes) render.HTML {
	b := p.Parts.Box(s)
	if p.Title == "" {
		panic("headless: PageHeader requires Title — a header that names nothing is padding above the content")
	}
	kids := make([]render.HTML, 0, 3)
	if p.Eyebrow != "" {
		kids = append(kids, b.El("p", PartPageEyebrow,
			Attrs(map[string]string{"aria-hidden": "true"}), render.Text(p.Eyebrow)))
	}
	level := p.Level
	if level < 1 || level > 6 {
		level = 1
	}
	kids = append(kids, b.El(headingTag(level), PartTitle, nil, render.Text(p.Title)))
	if p.Subtitle != "" {
		kids = append(kids, b.El("p", PartPageSubtitle, nil, render.Text(p.Subtitle)))
	}
	text := b.El("div", PartPageText, nil, kids...)

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
			return PageHeader(PageHeaderProps{Title: "Apps", Parts: parts}, s)
		},
		Anatomy: []Part{PartRoot, PartPageEyebrow, PartTitle, PartPageSubtitle, PartPageText, PartPageActions},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "page top",
				Why:  "the title is the page's h1 and the actions beside it are the page's own — a reader who lands mid-scroll jumps here to learn where they are",
				HTML: PageHeader(PageHeaderProps{Title: "Apps", Subtitle: "Eleven apps across three workers.",
					Actions: Button(ButtonProps{Label: "New app", Variant: "primary"}, k.For("Button"))}, s),
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

// ─── EmptyState ─────────────────────────────────────────────────────

// EmptyState parts.
const (
	PartEmptyTitle Part = "empty-title"
	PartEmptyDesc  Part = "empty-desc"
	PartEmptyAct   Part = "empty-action"
)

// EmptyStateProps is the page a list with nothing in it shows instead
// of the list.
type EmptyStateProps struct {
	// Title is the heading. Required: it is also the region's name.
	Title string
	// Level is the heading level, 3 by default. An empty state nests
	// inside a section; one mounted as the whole page under the h1
	// says 2 here.
	Level int
	// Description is the supporting line: what would be here, or what
	// to do about it.
	Description string
	// Action is the way out — "New app", "Clear the filter". A dead
	// end with no action is a page the reader can only leave with the
	// back button.
	Action render.HTML

	ID         string
	ExtraAttrs html.Attrs

	// Parts: attrs and binds on every part the state draws.
	Parts Parts
}

// EmptyState renders the nothing-here.
//
// The root is role="region", because an empty result is a place the
// reader arrives at and needs to recognise — "this is the empty
// state, its name is the reason" — and an unnamed div is a place
// nothing can name. An explicit ID names the heading `<ID>-title`
// and points the region's aria-labelledby at it, so the name and the
// heading cannot disagree; without one the region is named by an
// aria-label equal to the Title and the heading carries no id — a
// title-derived id is not unique, and two empty panels with one
// title on a page would collide.
func EmptyState(p EmptyStateProps, s Classes) render.HTML {
	b := p.Parts.Box(s)
	if p.Title == "" {
		panic("headless: EmptyState requires Title — the title is also the region's name, and an unnamed region is a hole with a border")
	}

	level := p.Level
	if level < 1 || level > 6 {
		level = 3
	}
	var titleAttrs html.Attrs
	own := Merge(Safe(p.ExtraAttrs, "role", "aria-label", "aria-labelledby"), Attrs(map[string]string{
		"id": p.ID,
	}))
	own["role"] = "region"
	if p.ID != "" {
		titleAttrs = Attrs(map[string]string{"id": p.ID + "-title"})
		own["aria-labelledby"] = p.ID + "-title"
	} else {
		own["aria-label"] = p.Title
	}
	kids := []render.HTML{
		b.El(headingTag(level), PartEmptyTitle, titleAttrs, render.Text(p.Title)),
	}
	if p.Description != "" {
		kids = append(kids, b.El("p", PartEmptyDesc, nil, render.Text(p.Description)))
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

// ─── StatCard ───────────────────────────────────────────────────────

// StatCard parts.
const (
	PartStatValue Part = "stat-value"
	PartStatTrend Part = "stat-trend"
)

// StatCardProps is one metric held up for the page: a labelled value,
// and how it is moving.
type StatCardProps struct {
	// Label says what is counted. Required.
	Label string
	// Value is the number, formatted by the caller. Required: a stat
	// card with no value is a label in a box.
	Value string
	// Trend is the movement ("+12% vs. last week"). Optional.
	Trend string
	// Direction names which way the trend reads — "up", "down" or
	// "flat". The trend's colour is the class map's to draw from it;
	// the direction must also be in the Trend's own words for a
	// reader who cannot see the colour, which is why Direction is a
	// variant hint and never the only carrier of meaning.
	Direction string

	ID         string
	ExtraAttrs html.Attrs

	// Parts: attrs and binds on every part the card draws. The value
	// is the part a dashboard binds to a signal.
	Parts Parts
}

// StatCard renders the metric.
//
// Label, value and trend are read in that order and the label is
// first for the same reason a form label precedes its control: the
// name arrives before the number, so "MRR: 48,200" is one fact
// instead of a number the reader has to look up.
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
		// The direction rides as data for the styled layer and as a
		// class-map variant for the class map; either can key on it,
		// and neither replaces the Trend's own words.
		trendAttrs["data-direction"] = p.Direction
		if cls := s.Variant(PartStatTrend, p.Direction); cls != "" {
			trendAttrs["class"] = cls
		}
	}
	kids := []render.HTML{
		b.El("p", PartLabel, nil, render.Text(p.Label)),
		b.El("p", PartStatValue, nil, render.Text(p.Value)),
	}
	if p.Trend != "" {
		kids = append(kids, b.El("p", PartStatTrend, trendAttrs, render.Text(p.Trend)))
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
		Anatomy: []Part{PartRoot, PartLabel, PartStatValue, PartStatTrend},
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
			}}
		},
	})
}

// ─── DetailList ─────────────────────────────────────────────────────

// DetailList parts.
const (
	PartDetailRow   Part = "detail-row"
	PartDetailTerm  Part = "detail-term"
	PartDetailValue Part = "detail-value"
)

// DetailRow is one labelled value: the term in a <dt>, the value in
// the <dd> that follows it.
type DetailRow struct {
	// Label is the term. Required: a value with no term is a fact
	// nobody can look up.
	Label string
	// Value is the value, and it may be markup — a status badge, a
	// link, an empty-value dash. Required: render the empty-value
	// dash for a missing value, so the absence is deliberate.
	Value render.HTML
}

// DetailListProps is a record read as label/value pairs.
type DetailListProps struct {
	// Rows are the pairs, in reading order. At least one.
	Rows []DetailRow

	ID         string
	ExtraAttrs html.Attrs

	// Parts: attrs and binds on the list, its rows and their terms
	// and values.
	Parts Parts
}

// DetailList renders the record as a <dl>.
//
// The description list is the contract: a <dt> and the <dd> after it
// are one pair to assistive technology in a way a grid of divs never
// is, and the pair survives every restyling because the relationship
// is the element, not the layout. A row wrapper keeps each pair
// addressable for the class map without breaking that pairing — a
// div between dt and dd is valid HTML and keeps the pair's reading
// order intact.
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
		rows = append(rows, b.El("div", PartDetailRow, nil,
			b.El("dt", PartDetailTerm, nil, render.Text(r.Label)),
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
