package ui

// AnchoredRail — sticky in-page nav rail with scrollspy-tracked active state.
//
// Bundles the rail markup (a labelled <aside> with an ordered list of
// "#anchor" links, each optionally carrying an eyebrow number + trailing
// count) with core-ui/patterns/scrollspy.Wrap so the runtime sets
// aria-current="true" on the link whose target section is currently in
// view.
//
// Replaces the hand-rolled pattern that was rebuilt three times in the
// product site (categories rail on /components/, intent rail on /docs/,
// step rail on /get-started/) with hardcoded SSR ".active" hints that
// stayed stuck on item 01 forever.
//
// Compose with ui.Section + auto-id-from-Heading for the cheapest possible
// hook-up:
//
//	rail := ui.AnchoredRail(ui.AnchoredRailConfig{
//	    Label: "By intent",
//	    Items: []ui.RailItem{
//	        {Eyebrow: "01", Text: "Modeling", Anchor: "modeling", Count: 9},
//	        {Eyebrow: "02", Text: "Serving",  Anchor: "serving",  Count: 9},
//	    },
//	    ObserveSelector: "#docs-sections",
//	})
//
// And on the section side, since ui.Section auto-slugs Heading → ID:
//
//	ui.Section(ui.SectionConfig{Heading: "Modeling"}, …)  // ID="modeling"

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/patterns/scrollspy"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// RailItem is one entry in the rail.
//
// Anchor is required (the fragment without the leading #). Text is the
// visible link label. Eyebrow is the leading mono chip (e.g. "01" /
// "01 / overview"); empty hides the chip column. Count is the trailing
// numeric chip (e.g. doc count per section); 0 hides the column.
type RailItem struct {
	Anchor  string // required, e.g. "modeling" → href="#modeling"
	Text    string // required, link label
	Eyebrow string // optional leading chip
	Count   int    // optional trailing chip (0 = hidden)
}

// AnchoredRailConfig configures the rail.
type AnchoredRailConfig struct {
	// Label is the visible heading above the rail (e.g. "Categories",
	// "By intent", "The path"). Required — also doubles as the
	// aria-label on the underlying <aside>.
	Label string

	// Items in display order. Required.
	Items []RailItem

	// ObserveSelector is the CSS selector for the container the
	// scrollspy runtime watches for in-view sections. Typically the
	// id of a wrapper around the sections (e.g. "#docs-sections"). If
	// empty, scrollspy is skipped and the rail is purely static — the
	// runtime then can't track active state.
	ObserveSelector string

	// TargetSelector overrides the default ".ui-section[id]" — set it
	// when the sections aren't ui.Section calls.
	TargetSelector string

	// Class is appended to the <aside>'s class list.
	Class string

	// ID optionally tags the <aside>.
	ID string
}

// AnchoredRail returns the rail HTML. When ObserveSelector is set, the
// rail is wrapped with scrollspy so the runtime tracks active state.
//
// The default CSS class is "ui-anchored-rail" — re-style with a Class
// override and a scoped block in the host's stylesheet.
func AnchoredRail(cfg AnchoredRailConfig) render.HTML {
	if cfg.Label == "" {
		panic("ui: AnchoredRail requires Label")
	}
	if len(cfg.Items) == 0 {
		panic("ui: AnchoredRail requires at least one Item")
	}

	listItems := make([]render.HTML, 0, len(cfg.Items))
	for _, it := range cfg.Items {
		if it.Anchor == "" {
			panic("ui: AnchoredRail item missing Anchor")
		}
		if it.Text == "" {
			panic("ui: AnchoredRail item missing Text")
		}
		var content []render.HTML
		if it.Eyebrow != "" {
			content = append(content, html.Span(
				html.TextConfig{Class: "ui-anchored-rail__eyebrow"},
				render.Text(it.Eyebrow),
			))
		}
		content = append(content, render.Text(it.Text))
		if it.Count > 0 {
			content = append(content, html.Span(
				html.TextConfig{Class: "ui-anchored-rail__count"},
				render.Text(strconv.Itoa(it.Count)),
			))
		}
		listItems = append(listItems, html.ListItem(html.ListItemConfig{},
			html.LinkHTML(html.LinkHTMLConfig{
				Href:    "#" + it.Anchor,
				Content: render.Join(content...),
			}),
		))
	}

	asideClass := "ui-anchored-rail"
	if cfg.Class != "" {
		asideClass += " " + cfg.Class
	}
	asideAttrs := map[string]string{
		"class":      asideClass,
		"aria-label": cfg.Label,
	}
	if cfg.ID != "" {
		asideAttrs["id"] = cfg.ID
	}
	rail := render.Tag("aside", asideAttrs,
		// A plain label, NOT a heading: the rail is a complementary landmark
		// already named by Label (aria-label above), and emitting an <h6>
		// here injected a stray, out-of-order heading into the page outline
		// (h1 → h6 → h2…). Same fix StepRail already made. The label keeps
		// the visual + the landmark name without polluting the heading
		// hierarchy.
		render.Tag("div", map[string]string{"class": "ui-anchored-rail__label"},
			render.Text(cfg.Label),
		),
		render.Tag("ol", map[string]string{"class": "ui-anchored-rail__list"},
			listItems...,
		),
	)

	// Stamp the rail FIRST so data-fui-comp="ui-anchored-rail" lands on
	// the <aside>. If we wrapped with scrollspy first and then WrapHTML
	// over that, the registry's injectMarker would see scrollspy's
	// outermost data-fui-comp already present and skip ours — leaving
	// our CSS un-bundled by the SSR scanner.
	marked := anchoredRailStyle.WrapHTML(rail)

	if cfg.ObserveSelector == "" {
		return marked
	}

	target := cfg.TargetSelector
	if target == "" {
		// Match what ui.Section emits — auto-slugged-from-heading sections
		// land as .ui-section[id="…"], so the default selector picks them
		// up without the caller having to spell it out.
		target = ".ui-section[id]"
	}
	return scrollspy.Wrap(scrollspy.Config{
		ObserveSelector: cfg.ObserveSelector,
		TargetSelector:  target,
	}, marked)
}
