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

	// ID wires tab↔panel semantics: each tab gets id "<ID>-tab-<i>"
	// plus aria-controls "<ID>-panel-<i>", each panel gets id
	// "<ID>-panel-<i>". With the headless primitive this is now
	// ALWAYS wired (the primitive owns the ids from the signal
	// name); the field is kept for config compatibility and used as
	// an override of the signal name for the id prefix.
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
	b.WriteString(`[data-fui-comp="fui-tabs"].fui-tabs{margin:0}`)
	b.WriteString(`[data-fui-comp="fui-tabs"] .fui-tabs-nav{display:flex;gap:0;border-bottom:1px solid var(--fui-border, var(--color-border, #e2e8f0));margin-bottom:0}`)
	b.WriteString(`[data-fui-comp="fui-tabs"] .fui-tab{padding:var(--spacing-md, .5rem) var(--spacing-lg, 1rem);background:none;border:none;border-bottom:2px solid transparent;cursor:pointer;font-size:var(--text-sm, .875rem);font-weight:500;color:var(--fui-muted, var(--color-text-muted, #64748b));transition:color .15s,border-color .15s;text-decoration:none;display:inline-block}`)
	b.WriteString(`[data-fui-comp="fui-tabs"] .fui-tab:hover{color:var(--fui-foreground, var(--color-text, #0f172a))}`)
	b.WriteString(`[data-fui-comp="fui-tabs"] .fui-tab:focus-visible{outline:2px solid var(--fui-primary, var(--color-primary, #3b82f6));outline-offset:-2px;border-radius:2px}`)
	b.WriteString(`[data-fui-comp="fui-tabs"] .fui-tabs-content{padding-top:1rem}`)
	b.WriteString(`[data-fui-comp="fui-tabs"] .fui-tab-panel{display:none}`)
	for i := range tabsMaxPanels {
		b.WriteString(fmt.Sprintf(`[data-fui-comp="fui-tabs"][data-active="%d"] .fui-tab[data-fui-tab-index="%d"]{color:var(--fui-primary, var(--color-primary, #3b82f6));border-bottom-color:var(--fui-primary, var(--color-primary, #3b82f6))}`, i, i))
		b.WriteString(fmt.Sprintf(`[data-fui-comp="fui-tabs"][data-active="%d"] .fui-tab-panel[data-fui-tab-index="%d"]{display:block}`, i, i))
	}
	return b.String()
}
