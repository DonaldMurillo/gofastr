package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// CodeSample is one language tab in a CodeTabs group.
type CodeSample struct {
	// Label is the visible tab text (required).
	Label string
	// Language is the HighlightLines language key.
	Language string
	// Code is the raw source, escaped and tokenized (required).
	Code string
	// Filename, when set, renders the CodeBlock's framed header.
	Filename string
}

// CodeTabsConfig configures a CodeTabs group.
type CodeTabsConfig struct {
	// Name groups the tabs as one set, unique within the page (required).
	Name string
	// Label is the tablist's optional aria-label.
	Label string
	// LineNumbers turns on the line-number gutter for every tab.
	LineNumbers bool
	// ID tags the inner tabset, not the root.
	ID    string
	Class string
	// ExtraAttrs lands on the root; class, id and data-cui-* are dropped.
	ExtraAttrs html.Attrs
	// Ctx is the request context the code blocks' words are said in.
	Ctx context.Context
}

// CodeTabs renders one highlighted CodeBlock per sample behind a tab
// strip; picking a tab in one group never switches another group. It
// panics without Name, without samples, or on a sample with no Label or Code.
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
				Ctx:         cfg.Ctx,
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
	classes := headless.Classes{
		headless.PartRoot:      "fui-code-tabs__strip",
		headless.PartTabsNav:   "fui-code-tabs__nav",
		headless.PartTab:       "fui-code-tabs__tab",
		headless.PartTabPanel:  "fui-code-tabs__panel",
		headless.PartTabsPanel: "fui-code-tabs__panels",
	}
	var parts headless.Parts
	if cfg.Label != "" {
		parts.Attrs = headless.PartAttrs{headless.PartTabsNav: {"aria-label": cfg.Label}}
	}
	inner := headless.Tabs(headless.TabsProps{Name: cfg.Name, ID: cfg.ID, Tabs: items, Parts: parts, Ctx: cfg.Ctx}, classes)
	inner = headless.Own(inner)
	return codeTabsStyle.WrapHTML(render.Tag("div",
		rootAttrs,
		inner,
	))
}

var codeTabsStyle = registry.RegisterStyle("ui-code-tabs", codeTabsCSS)

func codeTabsCSS(_ style.Theme) string {
	var b strings.Builder
	b.WriteString(`
[data-cui-comp="ui-code-tabs"] .fui-code-tabs__nav {
  display: flex;
  flex-wrap: nowrap;
  overflow-x: auto;
  scrollbar-width: thin;
  /* The rule is an inset shadow, not a border: a scroll container clips
     what lies outside its padding box, so the selected tab's underline
     could no longer overlap a border; it paints over this instead. */
  box-shadow: inset 0 calc(-1 * var(--stroke-thin, 1px)) 0 var(--color-border, #E4E4E7);
  margin-bottom: 0;
}
[data-cui-comp="ui-code-tabs"] .fui-code-tabs__tab {
  display: inline-flex;
  align-items: center;
  padding: var(--spacing-md, 8px) var(--spacing-lg, 16px);
  font-weight: var(--font-weight-medium);
  font-size: var(--text-base, 1rem);
  color: var(--color-text-muted, #52525B);
  border-bottom: var(--stroke-thick, 2px) solid transparent;
  transition: color var(--duration-fast, 150ms) var(--easing-ease-in-out, ease), border-color var(--duration-fast, 150ms) var(--easing-ease-in-out, ease);
  white-space: nowrap;
  text-decoration: none;
}
[data-cui-comp="ui-code-tabs"] .fui-code-tabs__tab:hover { color: var(--color-text, #09090B); }
[data-cui-comp="ui-code-tabs"] .fui-code-tabs__tab:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: calc(-1 * var(--stroke-focus-offset, 2px));
  border-radius: var(--radii-md, 8px);
}
[data-cui-comp="ui-code-tabs"] .fui-code-tabs__panels {
  padding-top: var(--spacing-md, 8px);
  min-width: 0;
}
[data-cui-comp="ui-code-tabs"] .fui-code-tabs__panel { max-inline-size: 100%; }
`)
	for i := range headless.TabsMaxPanels() {
		b.WriteString(fmt.Sprintf(`[data-cui-comp="ui-code-tabs"] .fui-code-tabs__strip[data-active="%d"] .fui-code-tabs__tab[data-cui-tab-index="%d"]{color:var(--color-primary, #18181B);border-bottom-color:var(--color-primary, #18181B);font-weight:var(--font-weight-semibold)}`,
			i, i))
	}
	b.WriteString("\n@media (prefers-reduced-motion: reduce) {\n" +
		`  [data-cui-comp="ui-code-tabs"] .fui-code-tabs__tab { transition: none; }` + "\n}\n")
	return b.String()
}
