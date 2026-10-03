package ui

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// AnchoredRail is a sticky in-page nav rail with an active-entry state
// the headless-rail module tracks.
//
// Renders headless.Rail dressed with the fui-anchored-rail class map:
// a labelled <aside> with an ordered list of "#anchor" links, each
// optionally carrying an eyebrow chip and a trailing count. The
// entries are real fragment links, so the whole rail works with no
// script — the module only marks the link whose target section is in
// view (aria-current and a class, never a style).
//
// Compose with ui.Section + auto-id-from-Heading for the cheapest
// possible hook-up:
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
	// "By intent", "The path"). Required: also doubles as the
	// aria-label on the underlying <aside>.
	Label string

	// Items in display order. Required.
	Items []RailItem

	// ObserveSelector is the CSS selector for the container the
	// headless-rail module watches for in-view sections. Typically the
	// id of a wrapper around the sections (e.g. "#docs-sections"). If
	// empty, the rail is purely static: the links work and nothing is
	// marked.
	ObserveSelector string

	// TargetSelector overrides the module's default heading targets.
	// Set it when the sections aren't headings (e.g. ".intent[id]").
	TargetSelector string

	// Class is appended to the <aside>'s class list.
	Class string

	// ID optionally tags the <aside>.
	ID string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the rail's root <aside>.
	// Keys the component owns are dropped: class and id (use Class /
	// ID), data-hui-* (the rail wiring), and aria-label (use Label).
	ExtraAttrs html.Attrs
}

// AnchoredRail returns the rail HTML. When ObserveSelector is set the
// root carries the observer hooks the headless-rail module binds.
func AnchoredRail(cfg AnchoredRailConfig) render.HTML {
	items := make([]headless.RailItem, len(cfg.Items))
	for i, it := range cfg.Items {
		count := ""
		if it.Count > 0 {
			count = strconv.Itoa(it.Count)
		}
		items[i] = headless.RailItem{
			Anchor:  it.Anchor,
			Text:    it.Text,
			Eyebrow: it.Eyebrow,
			Count:   count,
		}
	}
	classes := map[headless.Part]string{
		headless.PartRoot:        "fui-anchored-rail",
		headless.PartLabel:       "fui-anchored-rail__label",
		headless.PartRailList:    "fui-anchored-rail__list",
		headless.PartRailItem:    "fui-anchored-rail__item",
		headless.PartRailLink:    "fui-anchored-rail__link",
		headless.PartRailEyebrow: "fui-anchored-rail__eyebrow",
		headless.PartRailCount:   "fui-anchored-rail__count",
	}
	if cfg.Class != "" {
		classes[headless.PartRoot] += " " + cfg.Class
	}
	target := cfg.TargetSelector
	if target == "" {
		// Match what ui.Section emits: auto-slugged-from-heading
		// sections land as .fui-section[id], so the default selector
		// picks them up without the caller having to spell it out.
		target = ".fui-section[id]"
	}
	rail := headless.Rail(headless.RailProps{
		Label:           cfg.Label,
		Items:           items,
		ObserveSelector: cfg.ObserveSelector,
		TargetSelector:  target,
		ID:              cfg.ID,
		ExtraAttrs:      headless.Safe(cfg.ExtraAttrs, "class", "id", "aria-label"),
	}, classes)
	return anchoredRailStyle.WrapHTML(rail)
}

var anchoredRailStyle = registry.RegisterStyle("ui-anchored-rail", anchoredRailCSS)
