package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/store"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
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
}

var tabsStyle = registry.RegisterStyle("fui-tabs", tabsCSS)

// tabsMaxPanels bounds how many tab indices the generated CSS covers.
// Both the active-button highlight and the visible-panel rule are
// emitted per index (the registered CSS is global — it can't know a
// given strip's tab count), so we cover a generous fixed ceiling and
// reject anything beyond it loudly rather than silently hiding panels.
const tabsMaxPanels = 24

// Tabs renders a signal-driven tab strip. Clicking a tab sets the signal;
// the runtime mirrors it to data-active on the wrapper, and CSS lights up
// both the matching button and panel — so the highlight moves with the
// selection.
//
// Panics if SignalName is empty, Tabs is empty, or there are more than
// tabsMaxPanels tabs.
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
	if len(cfg.Tabs) > tabsMaxPanels {
		panic(fmt.Sprintf("ui: Tabs supports at most %d tabs, got %d", tabsMaxPanels, len(cfg.Tabs)))
	}
	if active < 0 || active >= len(cfg.Tabs) {
		active = 0
	}

	var buttons []render.HTML
	for i, tab := range cfg.Tabs {
		buttons = append(buttons, render.Tag("button", map[string]string{
			"class":               "fui-tab",
			"data-fui-signal-set": name + ":" + strconv.Itoa(i),
			"role":                "tab",
			"aria-selected":       strconv.FormatBool(i == active),
			"data-fui-tab-index":  strconv.Itoa(i),
		}, render.Text(tab.Label)))
	}

	var panels []render.HTML
	for i, tab := range cfg.Tabs {
		panels = append(panels, render.Tag("div", map[string]string{
			"class":              "fui-tab-panel",
			"role":               "tabpanel",
			"data-fui-tab-index": strconv.Itoa(i),
		}, tab.Content))
	}

	nav := render.Tag("nav", map[string]string{
		"class": "fui-tabs-nav",
		"role":  "tablist",
	}, buttons...)

	content := render.Tag("div", map[string]string{
		"class": "fui-tabs-content",
	}, panels...)

	cls := "fui-tabs"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	// The signal binding lives on the outer wrapper — the common ancestor
	// of the nav buttons and the panels — so one data-active drives both.
	wrapper := render.Tag("div", map[string]string{
		"class":                cls,
		"data-fui-signal":      name,
		"data-fui-signal-mode": "attr",
		"data-fui-signal-attr": "data-active",
		"data-active":          strconv.Itoa(active),
	}, nav, content)

	return tabsStyle.WrapHTML(wrapper)
}

func tabsCSS(_ style.Theme) string {
	var b strings.Builder
	// WrapHTML stamps data-fui-comp onto the wrapper itself, so the
	// wrapper-targeting rules are compound (no descendant combinator).
	b.WriteString(`[data-fui-comp="fui-tabs"].fui-tabs{margin:0}`)
	b.WriteString(`[data-fui-comp="fui-tabs"] .fui-tabs-nav{display:flex;gap:0;border-bottom:1px solid var(--fui-border,#e2e8f0);margin-bottom:0}`)
	b.WriteString(`[data-fui-comp="fui-tabs"] .fui-tab{padding:.5rem 1rem;background:none;border:none;border-bottom:2px solid transparent;cursor:pointer;font-size:.875rem;font-weight:500;color:var(--fui-muted,#64748b);transition:color .15s,border-color .15s}`)
	b.WriteString(`[data-fui-comp="fui-tabs"] .fui-tab:hover{color:var(--fui-foreground,#0f172a)}`)
	b.WriteString(`[data-fui-comp="fui-tabs"] .fui-tab:focus-visible{outline:2px solid var(--fui-primary,#3b82f6);outline-offset:-2px;border-radius:2px}`)
	b.WriteString(`[data-fui-comp="fui-tabs"] .fui-tabs-content{padding-top:1rem}`)
	b.WriteString(`[data-fui-comp="fui-tabs"] .fui-tab-panel{display:none}`)
	// Active button + visible panel both keyed off the wrapper's
	// data-active so the highlight follows the selected tab.
	for i := 0; i < tabsMaxPanels; i++ {
		b.WriteString(fmt.Sprintf(`[data-fui-comp="fui-tabs"][data-active="%d"] .fui-tab[data-fui-tab-index="%d"]{color:var(--fui-primary,#3b82f6);border-bottom-color:var(--fui-primary,#3b82f6)}`, i, i))
		b.WriteString(fmt.Sprintf(`[data-fui-comp="fui-tabs"][data-active="%d"] .fui-tab-panel[data-fui-tab-index="%d"]{display:block}`, i, i))
	}
	return b.String()
}
