package ui

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/store"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// TabItem is a single tab with a label and content.
type TabItem struct {
	Label   string
	Content render.HTML
}

// TabsConfig configures a signal-driven tab strip.
type TabsConfig struct {
	SignalName string            // required unless Slice is set
	Slice      *store.Slice[int] // optional; supplies the signal name + initial active index, takes precedence
	Tabs       []TabItem         // required, at least 1
	Class      string            // optional extra CSS class

	// StateAttrs adds data-state="active"/"inactive" to every tab,
	// the attribute contract Radix-style ports pin their test
	// locators to. The headless-tabs module keeps data-state in step
	// with the selection after client-side switches. Zero value: no
	// data-state anywhere.
	StateAttrs bool

	// ID overrides the id prefix the tab strip would derive from the
	// signal name: each tab gets id "<ID>-tab-<i>" plus aria-controls
	// "<ID>-panel-<i>", each panel gets id "<ID>-panel-<i>".
	ID string

	// VacateHidden ships hidden panels EMPTY, their server-rendered
	// content parked in an adjacent JSON stash, so page-scoped test
	// locators cannot match text inside hidden panels. The
	// headless-tabs module restores a panel's content on first show
	// and moves the live nodes out and back afterwards. See the
	// headless.TabsProps doc for the timing trade-offs.
	VacateHidden bool

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the tab strip's root
	// wrapper. Keys the component owns are dropped: class, id, the
	// data-hui-* wiring, and data-active (the signal mirror).
	ExtraAttrs html.Attrs
}

var tabsStyle = registry.RegisterStyle("fui-tabs", tabsCSS)

// tabsMaxPanels bounds how many tab indices the generated CSS covers.
// tabsMaxPanels mirrors the primitive ceiling; the generated CSS below covers exactly this many indices.
var tabsMaxPanels = headless.TabsMaxPanels()

// Tabs renders a signal-driven tab strip through headless.Tabs:
// anchors carrying the kernel's signal contract (click sets the
// signal; the runtime mirrors it to data-active and CSS lights both
// the tab and the panel), roving tabindex with the full keyboard
// contract bound by the headless-tabs module, fragment hrefs as the
// no-script path.
func Tabs(cfg TabsConfig) render.HTML {
	name := cfg.SignalName
	active := 0
	if cfg.Slice != nil {
		name = cfg.Slice.Name()
		active = cfg.Slice.Default()
	}
	if name == "" {
		panic("ui: Tabs requires SignalName or Slice")
	}
	if len(cfg.Tabs) == 0 {
		panic("ui: Tabs requires at least one TabItem")
	}
	tabs := make([]headless.Tab, len(cfg.Tabs))
	for i, t := range cfg.Tabs {
		tabs[i] = headless.Tab{Label: t.Label, Panel: t.Content}
	}
	classes := headless.Classes{
		headless.PartRoot:      "fui-tabs",
		headless.PartTabsNav:   "fui-tabs-nav",
		headless.PartTab:       "fui-tab",
		headless.PartTabPanel:  "fui-tab-panel",
		headless.PartTabsPanel: "fui-tabs-content",
	}
	if cfg.Class != "" {
		classes[headless.PartRoot] += " " + cfg.Class
	}
	out := headless.Tabs(headless.TabsProps{
		Name:         name,
		ID:           cfg.ID,
		Tabs:         tabs,
		Active:       active,
		VacateHidden: cfg.VacateHidden,
		StateAttrs:   cfg.StateAttrs,
		ExtraAttrs:   headless.Safe(cfg.ExtraAttrs, "class", "data-active"),
	}, classes)
	return tabsStyle.WrapHTML(out)
}

func tabsCSS(_ style.Theme) string {
	var b strings.Builder
	b.WriteString(`:where([data-cui-comp="fui-tabs"]).fui-tabs{margin:0}`)
	// The strip is a muted track with raised segments, the same shape as
	// SegmentedControl and the FilterToolbar pills, so every "pick one of
	// these" control on a page reads as one family.
	b.WriteString(`[data-cui-comp="fui-tabs"] .fui-tabs-nav{display:inline-flex;flex-wrap:wrap;gap:0;padding:var(--spacing-sm, 4px);background:var(--fui-muted-bg, var(--color-surface-soft, #F4F4F5));border-radius:var(--radii-lg, 10px);margin-bottom:0}`)
	b.WriteString(`[data-cui-comp="fui-tabs"] .fui-tab{display:inline-flex;align-items:center;justify-content:center;min-block-size:calc(var(--spacing-touch-target, 44px) - 2 * var(--spacing-sm, 4px));padding:0 12px;background:transparent;border:var(--stroke-thin, 1px) solid transparent;border-radius:var(--radii-md, 8px);cursor:pointer;font-size:var(--text-sm, .875rem);font-weight:var(--font-weight-medium);color:var(--fui-muted, var(--color-text-muted, #52525B));transition:color var(--duration-fast, 150ms),background-color var(--duration-fast, 150ms),box-shadow var(--duration-fast, 150ms);text-decoration:none;white-space:nowrap}`)
	b.WriteString(`[data-cui-comp="fui-tabs"] .fui-tab:hover{color:var(--fui-foreground, var(--color-text, #09090B))}`)
	b.WriteString(`[data-cui-comp="fui-tabs"] .fui-tab:focus-visible{outline:var(--stroke-focus, 2px) solid var(--color-text-subtle);outline-offset:var(--stroke-focus-offset, 2px)}`)
	b.WriteString(`[data-cui-comp="fui-tabs"] .fui-tabs-content{padding-top:var(--spacing-lg, 16px)}`)
	b.WriteString(`[data-cui-comp="fui-tabs"] .fui-tab-panel{display:none}`)
	for i := range tabsMaxPanels {
		b.WriteString(fmt.Sprintf(`[data-cui-comp="fui-tabs"][data-active="%d"] .fui-tab[data-cui-tab-index="%d"]{color:var(--fui-foreground, var(--color-text, #09090B));background:var(--fui-surface, var(--color-surface, #FFFFFF));box-shadow:var(--shadow-xs)}`, i, i))
		b.WriteString(fmt.Sprintf(`[data-cui-comp="fui-tabs"][data-active="%d"] .fui-tab-panel[data-cui-tab-index="%d"]{display:block}`, i, i))
	}
	return b.String()
}
