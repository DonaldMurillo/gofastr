// Package disclosure renders a single styled <details>/<summary>
// disclosure section. Use it for one-off "click to reveal" content
// (FAQ rows, expandable help, advanced-options panels). Accordion
// composes Group + Stack of these; this is the primitive.
//
// Output:
//
//	<details data-fui-comp="ui-disclosure" class="ui-disclosure">
//	  <summary class="ui-disclosure__summary">Title</summary>
//	  <div class="ui-disclosure__body">…</div>
//	</details>
//
// Native semantics — keyboard, screen reader, "find in page"
// expansion all work without JavaScript.
package disclosure

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Config configures a Disclosure.
type Config struct {
	// Title is the always-visible summary text (required).
	Title string
	// Open opts the disclosure into rendering open by default.
	Open bool
	// ID / Class / Attrs are passed through to the <details>.
	ID    string
	Class string
	Attrs html.Attrs
}

// Render renders the disclosure with the given body content.
func Render(cfg Config, body ...render.HTML) render.HTML {
	if cfg.Title == "" {
		panic("disclosure: Title required")
	}
	cls := "ui-disclosure"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs := html.Attrs{"class": cls}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	if cfg.Open {
		attrs["open"] = ""
	}
	for k, v := range cfg.Attrs {
		attrs[k] = v
	}
	return disclosureStyle.WrapHTML(render.Tag("details", attrs,
		render.Tag("summary",
			map[string]string{"class": "ui-disclosure__summary"},
			render.Text(cfg.Title)),
		render.Tag("div",
			map[string]string{"class": "ui-disclosure__body"},
			body...),
	))
}

var disclosureStyle = registry.RegisterStyle("ui-disclosure", disclosureCSS)

func disclosureCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-disclosure"] {
  display: block;
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFFFFF);
  overflow: hidden;
}
[data-fui-comp="ui-disclosure"] .ui-disclosure__summary {
  display: flex;
  align-items: center;
  gap: var(--spacing-sm, 8px);
  min-block-size: var(--spacing-touch-target, 44px);
  padding: var(--spacing-sm, 8px) var(--spacing-md, 12px);
  font-weight: 600;
  color: var(--color-text, #18181B);
  cursor: pointer;
  user-select: none;
  list-style: none;
}
[data-fui-comp="ui-disclosure"] .ui-disclosure__summary::-webkit-details-marker {
  display: none;
}
[data-fui-comp="ui-disclosure"] .ui-disclosure__summary::before {
  content: "▸";
  font-size: 0.7rem;
  color: var(--color-text-muted, #52525B);
  transition: transform 120ms ease;
}
[data-fui-comp="ui-disclosure"][open] .ui-disclosure__summary::before {
  transform: rotate(90deg);
}
[data-fui-comp="ui-disclosure"] .ui-disclosure__summary:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: -2px;
}
[data-fui-comp="ui-disclosure"] .ui-disclosure__body {
  padding: 0 var(--spacing-md, 12px) var(--spacing-md, 12px);
  color: var(--color-text, #18181B);
  border-top: 1px solid var(--color-border, #E4E4E7);
  padding-block-start: var(--spacing-md, 12px);
}
[data-fui-comp="ui-disclosure"]:not([open]) .ui-disclosure__body {
  display: none;
}`
}
