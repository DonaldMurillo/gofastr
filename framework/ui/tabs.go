package ui

import (
	"context"
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

	// StateAttrs adds data-state="active"/"inactive" to every tab.
	StateAttrs bool
	// ID is the tab and panel id prefix; empty derives it from the signal.
	ID string
	// VacateHidden ships hidden panels empty, their content in a JSON stash.
	VacateHidden bool
	// ExtraAttrs land on the root; class, id, data-hui-* and data-active are dropped.
	ExtraAttrs html.Attrs
	// Ctx scopes a derived id prefix to the page and reads a Slice's value.
	Ctx context.Context
}

var tabsStyle = registry.RegisterStyle("fui-tabs", tabsCSS)

var tabsMaxPanels = headless.TabsMaxPanels()

// Tabs renders a signal-driven tab strip through headless.Tabs.
func Tabs(cfg TabsConfig) render.HTML {
	name := cfg.SignalName
	active := 0
	if cfg.Slice != nil {
		name = cfg.Slice.Name()
		active = cfg.Slice.Value(cfg.Ctx)
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
		Ctx:          cfg.Ctx,
	}, classes)
	return tabsStyle.WrapHTML(out)
}

func tabsCSS(_ style.Theme) string {
	var b strings.Builder
	b.WriteString(`:where([data-cui-comp="fui-tabs"]).fui-tabs{margin:0}`)
	b.WriteString(`[data-cui-comp="fui-tabs"] .fui-tabs-nav{display:inline-flex;flex-wrap:nowrap;max-inline-size:100%;overflow-x:auto;scrollbar-width:thin;gap:0;padding:var(--spacing-sm, 4px);background:var(--fui-muted-bg, var(--color-surface-soft, #F4F4F5));border-radius:var(--radii-lg, 10px);margin-bottom:0}`)
	b.WriteString(`[data-cui-comp="fui-tabs"] .fui-tab{display:inline-flex;align-items:center;justify-content:center;min-block-size:calc(var(--spacing-touch-target, 44px) - 2 * var(--spacing-sm, 4px));padding:0 calc(var(--spacing-sm, 4px) * 3);background:transparent;border:var(--stroke-thin, 1px) solid transparent;border-radius:var(--radii-md, 8px);cursor:pointer;font-size:var(--text-sm, .875rem);font-weight:var(--font-weight-medium);color:var(--fui-muted, var(--color-text-muted, #52525B));transition:color var(--duration-fast, 150ms),background-color var(--duration-fast, 150ms),box-shadow var(--duration-fast, 150ms);text-decoration:none;white-space:nowrap}`)
	b.WriteString(`[data-cui-comp="fui-tabs"] .fui-tab:hover{color:var(--fui-foreground, var(--color-text, #09090B))}`)
	b.WriteString(`[data-cui-comp="fui-tabs"] .fui-tab:focus-visible{outline:var(--stroke-focus, 2px) solid var(--color-text-subtle);outline-offset:var(--stroke-focus-offset, 2px)}`)
	b.WriteString(`[data-cui-comp="fui-tabs"] .fui-tabs-content{padding-top:var(--spacing-lg, 16px)}`)
	// Forced colours drop the selected tab's background and shadow, so it
	// takes a Highlight underline keyed on the state assistive technology reads.
	b.WriteString(`@media (forced-colors: active){[data-cui-comp="fui-tabs"] .fui-tab{border-color:Canvas}[data-cui-comp="fui-tabs"] .fui-tab[aria-selected="true"]{border-color:Highlight;border-block-end-width:var(--stroke-thick, 2px)}}`)
	for i := range tabsMaxPanels {
		b.WriteString(fmt.Sprintf(`[data-cui-comp="fui-tabs"][data-active="%d"]>.fui-tabs-nav>.fui-tab[data-cui-tab-index="%d"]{color:var(--fui-foreground, var(--color-text, #09090B));background:var(--fui-surface, var(--color-surface, #FFFFFF));box-shadow:var(--shadow-xs)}`, i, i))
	}
	return b.String()
}
