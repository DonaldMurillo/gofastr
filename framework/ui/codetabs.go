package ui

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// CodeSample is one language tab in a CodeTabs group: a label, a language
// key for syntax highlighting, and the raw source.
type CodeSample struct {
	// Label is the visible tab text ("Go", "TypeScript", "curl"). Required.
	Label string
	// Language is the HighlightLines language key (go, js, ts, sql, json,
	// yaml, shell, …). Unknown values fall back to plain escaped text.
	Language string
	// Code is the raw source; it is escaped/tokenized, never trusted HTML.
	// Required.
	Code string
	// Filename, when set, renders the CodeBlock's framed chrome header.
	Filename string
}

// CodeTabsConfig configures a CodeTabs group.
type CodeTabsConfig struct {
	// Name groups the tabs as one exclusive set (native <details name=>
	// exclusivity). Required and must be unique within the page.
	Name string
	// Label is an optional aria-label for the group.
	Label string
	// LineNumbers turns on the CodeBlock line-number gutter for every tab.
	LineNumbers bool
	ID          string
	Class       string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the tab group's root
	// element. Keys the component owns are dropped: class (use
	// Class), id (ID tags the inner tabset, not the root), and
	// data-fui-*.
	ExtraAttrs html.Attrs
}

// CodeTabs renders the same snippet in several languages behind a tab strip,
// the "install this SDK in Go / TypeScript / curl" shape docs sites need.
// It is pure composition: headless.Tabs (the zero-JS fragment contract)
// one syntax-highlighted CodeBlock (with copy button) per sample.
//
// Selection is per-tabset: the native <details name=> mechanism has no
// page-wide state, so picking "TypeScript" in one group does not switch
// sibling groups.
func CodeTabs(cfg CodeTabsConfig, samples ...CodeSample) render.HTML {
	if cfg.Name == "" {
		panic("ui: CodeTabs requires Name")
	}
	if len(samples) == 0 {
		panic("ui: CodeTabs requires at least one CodeSample")
	}

	items := make([]headless.Tab, 0, len(samples))
	for _, s := range samples {
		if s.Label == "" || s.Code == "" {
			panic("ui: CodeSample requires Label and Code")
		}
		items = append(items, headless.Tab{
			Label: s.Label,
			Panel: CodeBlock(CodeBlockConfig{
				Lines:       HighlightLines(s.Code, s.Language),
				Language:    s.Language,
				Filename:    s.Filename,
				ShowCopy:    true,
				LineNumbers: cfg.LineNumbers,
			}),
		})
	}

	cls := "fui-code-tabs"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	rootAttrs := html.SafeExtraAttrs(cfg.ExtraAttrs)
	if rootAttrs == nil {
		rootAttrs = map[string]string{}
	}
	rootAttrs["class"] = cls
	// The strip renders through headless.Tabs with the code-tabs
	// class map: anchor tabs with the signal contract, fragment hrefs
	// as the no-script path, the keyboard bound by headless-tabs.
	classes := headless.Classes{
		headless.PartRoot:      "fui-code-tabs__strip",
		headless.PartTabsNav:   "fui-code-tabs__nav",
		headless.PartTab:       "fui-code-tabs__tab",
		headless.PartTabPanel:  "fui-code-tabs__panel",
		headless.PartTabsPanel: "fui-code-tabs__panels",
	}
	inner := headless.Tabs(headless.TabsProps{Name: cfg.Name, ID: cfg.ID, Tabs: items}, classes)
	return codeTabsStyle.WrapHTML(render.Tag("div",
		rootAttrs,
		inner,
	))
}

var codeTabsStyle = registry.RegisterStyle("ui-code-tabs", codeTabsCSS)

func codeTabsCSS(_ style.Theme) string {
	// The strip carries the retired tabs pattern's look on the new
	// class names: spaced labels, a 2px underline on the active tab,
	// and only the active panel visible. Panel visibility keys on the
	// same data-active/data-fui-tab-index pair ui.Tabs generates its
	// rules from, pre-generated to the primitive's ceiling.
	var b strings.Builder
	b.WriteString(`
[data-fui-comp="ui-code-tabs"] .fui-code-tabs__nav {
  display: flex;
  flex-wrap: wrap;
  border-bottom: 1px solid var(--color-border, #E5E7EB);
  margin-bottom: 0;
}
[data-fui-comp="ui-code-tabs"] .fui-code-tabs__tab {
  display: inline-flex;
  align-items: center;
  padding: var(--spacing-md, 8px) var(--spacing-lg, 16px);
  font-weight: 500;
  font-size: var(--text-base, 1rem);
  color: var(--color-text-muted, #6B7280);
  border-bottom: 2px solid transparent;
  margin-bottom: -1px; /* overlap the strip's 1px border-bottom */
  transition: color 150ms ease, border-color 150ms ease;
  white-space: nowrap;
  text-decoration: none;
}
[data-fui-comp="ui-code-tabs"] .fui-code-tabs__tab:hover { color: var(--color-text, #1F2937); }
[data-fui-comp="ui-code-tabs"] .fui-code-tabs__tab:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: -2px;
  border-radius: var(--radii-md, 8px);
}
[data-fui-comp="ui-code-tabs"] .fui-code-tabs__panels {
  padding-top: var(--spacing-md, 8px);
  min-width: 0;
}
[data-fui-comp="ui-code-tabs"] .fui-code-tabs__panel { display: none; max-inline-size: 100%; }
`)
	for i := range headless.TabsMaxPanels() {
		b.WriteString(fmt.Sprintf(`[data-fui-comp="ui-code-tabs"] .fui-code-tabs__strip[data-active="%d"] .fui-code-tabs__tab[data-fui-tab-index="%d"]{color:var(--color-primary, #4F46E5);border-bottom-color:var(--color-primary, #4F46E5);font-weight:600}`,
			i, i))
		b.WriteString(fmt.Sprintf(`[data-fui-comp="ui-code-tabs"] .fui-code-tabs__strip[data-active="%d"] .fui-code-tabs__panel[data-fui-tab-index="%d"]{display:block}`,
			i, i))
	}
	b.WriteString("\n@media (prefers-reduced-motion: reduce) {\n" +
		`  [data-fui-comp="ui-code-tabs"] .fui-code-tabs__tab { transition: none; }` + "\n}\n")
	return b.String()
}
