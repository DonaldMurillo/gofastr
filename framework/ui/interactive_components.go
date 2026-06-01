package ui

import (
	"fmt"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
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
	SignalName string    // required
	Tabs       []TabItem // required, at least 1
	Class      string    // optional extra CSS class
}

var tabsStyle = registry.RegisterStyle("fui-tabs", tabsCSS)

// Tabs renders a signal-driven tab strip.
// Panics if SignalName is empty or Tabs is empty.
func Tabs(cfg TabsConfig) render.HTML {
	if cfg.SignalName == "" {
		panic("ui: Tabs requires SignalName")
	}
	if len(cfg.Tabs) == 0 {
		panic("ui: Tabs requires at least one TabItem")
	}

	var buttons []render.HTML
	for i, tab := range cfg.Tabs {
		cls := "fui-tab"
		if i == 0 {
			cls = "fui-tab fui-tab--active"
		}
		buttons = append(buttons, render.Tag("button", map[string]string{
			"class":               cls,
			"data-fui-signal-set": cfg.SignalName + ":" + strconv.Itoa(i),
			"role":                "tab",
			"aria-selected":       strconv.FormatBool(i == 0),
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
		"class":                "fui-tabs-content",
		"data-fui-signal":      cfg.SignalName,
		"data-fui-signal-mode": "attr",
		"data-fui-signal-attr": "data-active",
		"data-active":          "0",
	}, panels...)

	cls := "fui-tabs"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	wrapper := render.Tag("div", map[string]string{"class": cls}, nav, content)

	return tabsStyle.WrapHTML(wrapper)
}

func tabsCSS(_ style.Theme) string {
	css := `[data-fui-comp="fui-tabs"] .fui-tabs{margin:0}[data-fui-comp="fui-tabs"] .fui-tabs-nav{display:flex;gap:0;border-bottom:1px solid var(--fui-border,#e2e8f0);margin-bottom:0}[data-fui-comp="fui-tabs"] .fui-tab{padding:.5rem 1rem;background:none;border:none;border-bottom:2px solid transparent;cursor:pointer;font-size:.875rem;font-weight:500;color:var(--fui-muted,#64748b);transition:color .15s,border-color .15s}[data-fui-comp="fui-tabs"] .fui-tab:hover{color:var(--fui-foreground,#0f172a)}[data-fui-comp="fui-tabs"] .fui-tab:focus-visible{outline:2px solid var(--fui-primary,#3b82f6);outline-offset:-2px;border-radius:2px}[data-fui-comp="fui-tabs"] .fui-tab--active,[data-fui-comp="fui-tabs"] .fui-tab[aria-selected="true"]{color:var(--fui-primary,#3b82f6);border-bottom-color:var(--fui-primary,#3b82f6)}[data-fui-comp="fui-tabs"] .fui-tabs-content{padding-top:1rem}[data-fui-comp="fui-tabs"] .fui-tab-panel{display:none}`
	for i := 0; i < 6; i++ {
		css += fmt.Sprintf(`[data-fui-comp="fui-tabs"] .fui-tabs-content[data-active="%d"]>.fui-tab-panel[data-fui-tab-index="%d"]{display:block}`, i, i)
	}
	return css
}
