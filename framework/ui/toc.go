package ui

import (
	"context"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// TOCItem is one entry in the contents list.
type TOCItem struct {
	// ID is the heading's fragment id, without "#". Required.
	ID string
	// Label is the entry's visible text. Required.
	Label string
	// Level is the heading level, 1 to 6, that indents the entry; 0 is 2.
	Level int
}

// TOCConfig configures a TableOfContents.
type TOCConfig struct {
	// Ctx is the default label's language; nil is English.
	Ctx context.Context
	// Items are the entries, in document order. Required.
	Items []TOCItem
	// Target is the selector of the region whose headings mark the entry in view.
	Target string
	// Label names the nav. Defaults to "On this page".
	Label string
	// Sticky makes the nav sticky.
	Sticky bool
	// PhoneSheet folds the list below Breakpoint into a sheet whose pill follows Target.
	PhoneSheet bool
	// PhoneSheetTop pins the pill under the sticky header instead, beside a SectionMenu.
	PhoneSheetTop bool
	// CloseLabel names the phone sheet's dismiss control. Empty uses Close.
	CloseLabel string
	// Breakpoint is where PhoneSheet folds; match the ContentRow's.
	Breakpoint StackBreakpoint

	ID    string
	Class string
	// ExtraAttrs land on the nav; class, id, data-hui-* and aria-label are dropped.
	ExtraAttrs html.Attrs
}

// TableOfContents renders a server-built contents nav of Items.
func TableOfContents(cfg TOCConfig) render.HTML {
	items := make([]headless.TOCItem, len(cfg.Items))
	for i, it := range cfg.Items {
		items[i] = headless.TOCItem{ID: it.ID, Label: it.Label, Level: it.Level}
	}
	classes := map[headless.Part]string{
		headless.PartRoot:    "fui-toc",
		headless.PartTOCList: "fui-toc__list",
		headless.PartTOCItem: "fui-toc__item",
		headless.PartTOCLink: "fui-toc__link",
	}
	for l := 1; l <= 6; l++ {
		classes[headless.Part("toc-item--h"+strconv.Itoa(l))] = "fui-toc__item--h" + strconv.Itoa(l)
	}
	if cfg.Sticky {
		classes[headless.PartRoot] += " fui-toc--sticky"
	}
	if cfg.Class != "" {
		classes[headless.PartRoot] += " " + cfg.Class
	}
	nav := headless.TableOfContents(headless.TableOfContentsProps{
		Label:          cfg.Label,
		Items:          items,
		TargetSelector: cfg.Target,
		ID:             cfg.ID,
		ExtraAttrs:     headless.Safe(cfg.ExtraAttrs, "class", "id", "aria-label"),
		Strings:        StringsFor(cfg.Ctx),
	}, classes)
	nav = tocStyle.WrapHTML(nav)
	if !cfg.PhoneSheet {
		return nav
	}
	checkStackBreakpoint(cfg.Breakpoint)
	label := cfg.Label
	if label == "" {
		label = i18nui.HuiTableOfContentsLabel.Text(cfg.Ctx)
	}
	sheetID := "fui-toc-sheet"
	if cfg.ID != "" {
		sheetID = cfg.ID + "-sheet"
	}
	swap := interactive.SwapMD
	if cfg.Breakpoint == StackBelowLG {
		swap = interactive.SwapLG
	}
	return interactive.PhoneSheet(interactive.PhoneSheetConfig{ID: sheetID, Title: label, SwapAt: swap, Top: cfg.PhoneSheetTop, CloseLabel: cfg.CloseLabel, Ctx: cfg.Ctx}, nav)
}

var tocStyle = registry.RegisterStyle("ui-toc", tocCSS)

func tocCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-toc"] {
  display: block;
  font-size: var(--text-sm, 0.875rem);
}
:where([data-cui-comp="ui-toc"]).fui-toc--sticky {
  position: sticky;
  /* --ui-sticky-top is where sticky parts start: under a StickyHeader
     page's header, or the window's top. */
  inset-block-start: calc(var(--ui-sticky-top, 0px) + var(--spacing-lg, 16px));
  align-self: start;
  max-block-size: calc(100vh - var(--ui-sticky-top, 0px) - var(--spacing-sm, 4px) * 16);
  overflow-y: auto;
}
[data-cui-comp="ui-toc"]::before {
  content: attr(aria-label);
  display: block;
  font-size: var(--text-sm, 0.875rem);
  font-weight: var(--font-weight-medium);
  color: var(--color-text, #09090B);
  margin-block-end: var(--spacing-sm, 4px);
}
[data-cui-comp="ui-toc"] .fui-toc__list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: var(--spacing-xs, 2px);
}
[data-cui-comp="ui-toc"] .fui-toc__item {
  margin: 0;
}
[data-cui-comp="ui-toc"] .fui-toc__item--h3 {
  margin-inline-start: var(--spacing-md, 8px);
}
[data-cui-comp="ui-toc"] .fui-toc__item--h4,
[data-cui-comp="ui-toc"] .fui-toc__item--h5,
[data-cui-comp="ui-toc"] .fui-toc__item--h6 {
  margin-inline-start: calc(var(--spacing-md, 8px) * 2);
}
[data-cui-comp="ui-toc"] .fui-toc__link {
  display: block;
  padding: var(--spacing-sm, 4px) var(--spacing-sm, 4px);
  border-radius: var(--radii-sm, 6px);
  border-inline-start: var(--stroke-thick, 2px) solid transparent;
  color: var(--color-text-muted, #52525B);
  text-decoration: none;
  line-height: var(--leading-snug, 1.4);
}
[data-cui-comp="ui-toc"] .fui-toc__link:hover {
  color: var(--color-text, #09090B);
  background: var(--color-surface-soft, #F4F4F5);
}
[data-cui-comp="ui-toc"] .fui-toc__link.is-active,
[data-cui-comp="ui-toc"] .fui-toc__link[aria-current="true"] {
  color: var(--color-text, #09090B);
  border-inline-start-color: var(--color-text, #09090B);
  font-weight: var(--font-weight-medium);
}
[data-cui-comp="ui-toc"] .fui-toc__link:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: var(--stroke-focus-offset, 2px);
}
/* In an open phone sheet (PhoneSheet) the sheet's title row names the
   list and the sheet scrolls it; the rows grow to a thumb's height. A
   list in any other popover keeps its own label and stickiness. */
.cui-phone-sheet__sheet:popover-open [data-cui-comp="ui-toc"]::before { display: none; }
:where(.cui-phone-sheet__sheet:popover-open) :where([data-cui-comp="ui-toc"]).fui-toc--sticky {
  position: static;
  max-block-size: none;
  overflow: visible;
}
.cui-phone-sheet__sheet:popover-open [data-cui-comp="ui-toc"] .fui-toc__link { padding-block: var(--spacing-md, 8px); }`
}
