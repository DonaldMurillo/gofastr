package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// RailItem is one rail entry. Anchor (the fragment without #) and Text are
// required; an empty Eyebrow or a zero Count hides that chip.
type RailItem struct {
	Anchor  string // required, e.g. "modeling" → href="#modeling"
	Text    string // required, link label
	Eyebrow string // optional leading chip
	Count   int    // optional trailing chip (0 = hidden)
}

// AnchoredRailConfig configures the rail.
type AnchoredRailConfig struct {
	// Label is the visible heading and the aside's aria-label. Required.
	Label string

	// Items in display order. Required.
	Items []RailItem

	// ObserveSelector is the container the headless-rail module watches for the
	// section in view; empty leaves the rail static.
	ObserveSelector string

	// TargetSelector overrides the observed targets, .fui-section[id] by default.
	TargetSelector string

	// PhoneSheet folds the rail into a bottom sheet below SwapAt, opened by a pill
	// naming the section in view; set ObserveSelector too. Its id is ID plus "-sheet".
	PhoneSheet bool
	// SwapAt is the width below which PhoneSheet folds the rail, 900px by default.
	SwapAt interactive.SwapWidth
	// PhoneSheetTop pins the PhoneSheet pill as a bar under the page's sticky header.
	PhoneSheetTop bool

	// Class is appended to the <aside>'s class list.
	Class string

	// ID optionally tags the <aside>.
	ID string

	// ExtraAttrs is forwarded to the root without the keys the component owns.
	ExtraAttrs html.Attrs

	// Ctx carries the request context the counts are written in; nil is English.
	Ctx context.Context
}

// AnchoredRail renders a sticky in-page nav rail of fragment links.
func AnchoredRail(cfg AnchoredRailConfig) render.HTML {
	items := make([]headless.RailItem, len(cfg.Items))
	for i, it := range cfg.Items {
		count := ""
		if it.Count > 0 {
			count = i18n.Number(cfg.Ctx, it.Count)
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
		headless.PartRailEyebrow: "fui-anchored-rail__eyebrow " + interactive.SheetChipClass,
		headless.PartRailCount:   "fui-anchored-rail__count " + interactive.SheetChipClass,
	}
	if cfg.Class != "" {
		classes[headless.PartRoot] += " " + cfg.Class
	}
	target := cfg.TargetSelector
	if target == "" {
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
	rail = anchoredRailStyle.WrapHTML(rail)
	if !cfg.PhoneSheet {
		return rail
	}
	sheetID := "fui-anchored-rail-sheet"
	if cfg.ID != "" {
		sheetID = cfg.ID + "-sheet"
	}
	return interactive.PhoneSheet(interactive.PhoneSheetConfig{Ctx: cfg.Ctx, ID: sheetID, Title: cfg.Label, SwapAt: cfg.SwapAt, Top: cfg.PhoneSheetTop}, rail)
}

var anchoredRailStyle = registry.RegisterStyle("ui-anchored-rail", anchoredRailCSS)
