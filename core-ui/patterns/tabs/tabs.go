// Package tabs provides a tabbed-content layout with zero JavaScript.
//
// Implementation: each tab is a <details> sharing a name= attribute
// (so opening one closes the others — native exclusivity). CSS Grid
// then arranges the summaries in row 1 and panels in row 2 spanning
// the full width, producing the visual shape of a tab strip.
//
// Trade-off: assistive tech announces the widget as a disclosure, not
// an ARIA tablist. We chose this over a JS-driven tablist because:
//  1. Zero JavaScript, zero CSP complications.
//  2. The disclosure pattern is honest about what's happening — there
//     is no tab/panel separation, only "show one, hide others".
//  3. Native keyboard support (Tab between summaries, Enter/Space
//     activates, focus is automatic).
//
// If you need full WAI-ARIA tablist semantics (arrow-key tab cycling,
// activation modes), build a custom widget on top of core-ui/widget
// instead.
package tabs

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Style is the registered stylesheet handle. New's wrapping <div>
// goes through Style.WrapHTML so the data-fui-comp marker is emitted
// and the runtime auto-loads the CSS on first appearance.
var Style = registry.RegisterStyle("tabs", styleFn)

func styleFn(_ style.Theme) string { return buildCSS() }

// Tab is one entry in a tabset.
type Tab struct {
	Label   string      // required — visible tab summary text
	Content render.HTML // required — panel content
	Open    bool        // initially active
	ID      string
}

// Config configures the tabset.
type Config struct {
	// Name groups the tabs as a single exclusive set. Required.
	// Must be unique within the page.
	Name string

	Label string // optional aria-label for the wrapper
	Class string
	ID    string
}

// New renders a tabset.
//
// Output structure (panels live in a sibling container so the strip and
// the panel area are separate flex children of `.tabs` — needed because
// `display: contents` does not propagate flex `order` reliably in
// Chrome, which broke the strip-then-panel layout when both lived
// inside `<details>`):
//
//	<div class="tabs">
//	  <details name="X"><summary>A</summary></details>
//	  <details name="X"><summary>B</summary></details>
//	  <div class="tabs-panels">
//	    <div class="tabs-panel" data-for="…">A content</div>
//	    <div class="tabs-panel" data-for="…">B content</div>
//	  </div>
//	</div>
//
// Panel visibility is driven by `:has(> details:nth-child(N)[open])`
// rules in BaseCSS — pure CSS, no JS.
func New(cfg Config, tabs ...Tab) render.HTML {
	if cfg.Name == "" {
		panic("tabs: New requires Name")
	}
	if len(tabs) == 0 {
		panic("tabs: New requires at least one Tab")
	}
	if len(tabs) > maxTabs {
		panic(fmt.Sprintf("tabs: New supports at most %d tabs, got %d — the panel-visibility CSS is pre-generated per index (raise maxTabs in core-ui/patterns/tabs if you genuinely need more)", maxTabs, len(tabs)))
	}

	cls := "tabs"
	if cfg.Class != "" {
		cls = cls + " " + cfg.Class
	}
	wrapAttrs := map[string]string{"class": cls, "role": "group"}
	if cfg.ID != "" {
		wrapAttrs["id"] = cfg.ID
	}
	if cfg.Label != "" {
		wrapAttrs["aria-label"] = cfg.Label
	}

	hasOpen := false
	for _, t := range tabs {
		if t.Open {
			hasOpen = true
			break
		}
	}

	children := make([]render.HTML, 0, len(tabs)+1)
	panels := make([]render.HTML, 0, len(tabs))
	for i, t := range tabs {
		if t.Label == "" {
			panic("tabs: Tab requires Label")
		}
		if t.Content == "" {
			panic("tabs: Tab requires Content")
		}
		open := t.Open
		if !hasOpen && i == 0 {
			open = true
		}
		children = append(children, renderTabHead(t, cfg.Name, open))
		panels = append(panels, render.Tag("div",
			map[string]string{"class": "tabs-panel"}, t.Content))
	}
	children = append(children, render.Tag("div",
		map[string]string{"class": "tabs-panels"}, panels...))
	return Style.WrapHTML(render.Tag("div", wrapAttrs, children...))
}

func renderTabHead(t Tab, name string, open bool) render.HTML {
	attrs := map[string]string{"class": "tabs-tab", "name": name}
	if t.ID != "" {
		attrs["id"] = t.ID
	}
	if open {
		attrs["open"] = ""
	}
	summary := render.Tag("summary", map[string]string{"class": "tabs-summary"},
		render.Text(t.Label))
	return render.Tag("details", attrs, summary)
}

// baseCSS is the stylesheet for tabs. Tokens: --color-border,
// --color-surface, --color-primary, --color-text, --color-text-muted,
// --spacing-md, --spacing-lg, --radii-md.
//
// Layout: flex-wrap. Summaries are inline flex items along the top
// row; the active panel forces a full-width wrap (`flex: 1 0 100%`)
// onto a new row. `order: 999` keeps the panel below every summary.
// This survives narrow containers gracefully (tabs wrap onto multiple
// lines when needed) and avoids the grid-overflow clipping that the
// previous implementation had inside narrow demo frames.
// maxTabs bounds how many tabs one tabset may hold. The registered CSS
// is global (it can't know a given tabset's size), so panel-visibility
// rules are pre-generated per index up to this ceiling and New panics
// loudly past it instead of silently never showing panel 17.
const maxTabs = 16

func buildCSS() string {
	// Panel-visibility rules: when the Nth <details> is open, show the
	// Nth .tabs-panel. Pre-generated up to maxTabs; New rejects larger
	// tabsets. Using `:has()` keeps the cascade purely CSS.
	var panelRules strings.Builder
	for i := 1; i <= maxTabs; i++ {
		if i > 1 {
			panelRules.WriteString(",\n")
		}
		fmt.Fprintf(&panelRules,
			".tabs:has(> details:nth-of-type(%d)[open]) > .tabs-panels > .tabs-panel:nth-of-type(%d)", i, i)
	}
	panelRules.WriteString(" { display: block; }\n")

	return `
.tabs {
  display: flex;
  flex-wrap: wrap;
  align-items: stretch;
  border-bottom: 1px solid var(--color-border, #E5E7EB);
}
.tabs > details {
  flex: 0 0 auto;
}
.tabs > details > .tabs-summary {
  /* Override <summary>'s default display: list-item so it lays out
     inline as a tab. */
  display: inline-flex;
  align-items: center;
  list-style: none;
  cursor: pointer;
  padding: var(--spacing-md, 8px) var(--spacing-lg, 16px);
  font-weight: 500;
  font-size: var(--text-base, 0.95rem);
  color: var(--color-text-muted, #6B7280);
  border-bottom: 2px solid transparent;
  margin-bottom: -1px; /* overlap the strip's 1px border-bottom */
  transition: color 150ms ease, border-color 150ms ease;
  user-select: none;
  white-space: nowrap;
}
.tabs > details > .tabs-summary::-webkit-details-marker { display: none; }
.tabs > details > .tabs-summary::marker { content: ''; }
.tabs > details > .tabs-summary:hover {
  color: var(--color-text, #1F2937);
}
.tabs > details > .tabs-summary:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: -2px;
  border-radius: var(--radii-md, 8px);
}
.tabs > details[open] > .tabs-summary {
  color: var(--color-primary, #4F46E5);
  border-bottom-color: var(--color-primary, #4F46E5);
  font-weight: 600;
}
.tabs > .tabs-panels {
  /* Forces a flex-wrap break: panels area takes a full row below the
     summary strip. */
  flex: 1 0 100%;
  padding: var(--spacing-lg, 16px) 0 0;
}
.tabs > .tabs-panels > .tabs-panel { display: none; }
` + panelRules.String() + `
@media (prefers-reduced-motion: reduce) {
  .tabs > details > .tabs-summary { transition: none; }
}
`
}
