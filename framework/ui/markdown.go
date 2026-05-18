package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/markdown"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── Markdown ───────────────────────────────────────────────────────
//
// Themed wrapper over core/markdown. The core renderer produces
// semantic HTML; this component wraps that in a styled prose container
// so headings, lists, links, code blocks, and tables get theme-token
// colors + readable typography without the caller writing CSS.

// MarkdownConfig configures a Markdown renderer.
type MarkdownConfig struct {
	// Source is the raw markdown text (required).
	Source string
	// Compact tightens spacing — useful for inline previews where
	// hero-page paragraph rhythm would feel wrong.
	Compact bool
	ID      string
	Class   string
	Attrs   html.Attrs
}

// Markdown renders the given Markdown source as themed HTML.
func Markdown(cfg MarkdownConfig) render.HTML {
	if cfg.Source == "" {
		panic("ui: Markdown requires Source")
	}
	cls := "ui-markdown"
	if cfg.Compact {
		cls += " ui-markdown--compact"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs := html.Attrs{"class": cls}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	for k, v := range cfg.Attrs {
		attrs[k] = v
	}
	body := markdown.RenderHTML(cfg.Source)
	return markdownStyle.WrapHTML(render.Tag("div", attrs, body))
}

var markdownStyle = registry.RegisterStyle("ui-markdown", markdownCSS)

func markdownCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-markdown"] {
  display: block;
  color: var(--color-text, #18181B);
  line-height: 1.6;
}
[data-fui-comp="ui-markdown"] > * + * {
  margin-block-start: var(--spacing-md, 12px);
}
[data-fui-comp="ui-markdown"] h1,
[data-fui-comp="ui-markdown"] h2,
[data-fui-comp="ui-markdown"] h3,
[data-fui-comp="ui-markdown"] h4 {
  margin-block-start: var(--spacing-xl, 28px);
  margin-block-end: var(--spacing-sm, 8px);
  line-height: 1.25;
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-markdown"] h1 { font-size: 1.75rem; font-weight: 700; }
[data-fui-comp="ui-markdown"] h2 { font-size: 1.35rem; font-weight: 700; }
[data-fui-comp="ui-markdown"] h3 { font-size: 1.1rem; font-weight: 600; }
[data-fui-comp="ui-markdown"] h4 { font-size: 1rem; font-weight: 600; }

[data-fui-comp="ui-markdown"] p {
  margin: 0;
}
[data-fui-comp="ui-markdown"] a {
  color: var(--color-primary, #4F46E5);
  text-decoration: none;
}
[data-fui-comp="ui-markdown"] a:hover { text-decoration: underline; }

[data-fui-comp="ui-markdown"] ul,
[data-fui-comp="ui-markdown"] ol {
  margin: 0;
  padding-inline-start: var(--spacing-xl, 28px);
}
[data-fui-comp="ui-markdown"] li + li {
  margin-block-start: 4px;
}

[data-fui-comp="ui-markdown"] code {
  font-family: var(--font-mono, ui-monospace, monospace);
  background: var(--color-surface-soft, #F4F4F5);
  color: var(--color-text, #18181B);
  padding: 1px 5px;
  border-radius: var(--radii-sm, 4px);
  font-size: 0.9em;
}
[data-fui-comp="ui-markdown"] pre {
  margin: 0;
  padding: var(--spacing-md, 12px);
  background: var(--color-code-surface, #18181B);
  color: var(--color-code-text, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  overflow-x: auto;
  font-family: var(--font-mono, ui-monospace, monospace);
  font-size: 0.85rem;
  line-height: 1.5;
}
[data-fui-comp="ui-markdown"] pre code {
  background: transparent;
  color: inherit;
  padding: 0;
}

[data-fui-comp="ui-markdown"] blockquote {
  margin: 0;
  padding: var(--spacing-sm, 8px) var(--spacing-md, 12px);
  border-inline-start: 4px solid var(--color-border, #E4E4E7);
  color: var(--color-text-muted, #52525B);
  background: var(--color-surface-soft, #F4F4F5);
  border-radius: 0 var(--radii-md, 8px) var(--radii-md, 8px) 0;
}

[data-fui-comp="ui-markdown"] hr {
  border: 0;
  border-block-start: 1px solid var(--color-border, #E4E4E7);
  margin-block: var(--spacing-xl, 28px);
}

[data-fui-comp="ui-markdown"] table {
  border-collapse: collapse;
  margin: 0;
}
[data-fui-comp="ui-markdown"] th,
[data-fui-comp="ui-markdown"] td {
  padding: 6px var(--spacing-md, 12px);
  border-block-end: 1px solid var(--color-border, #E4E4E7);
  text-align: start;
}
[data-fui-comp="ui-markdown"] th {
  font-weight: 600;
  color: var(--color-text, #18181B);
}

/* Compact variant — tighter rhythm for inline previews. */
.ui-markdown.ui-markdown--compact > * + * {
  margin-block-start: var(--spacing-sm, 8px);
}
.ui-markdown.ui-markdown--compact h1,
.ui-markdown.ui-markdown--compact h2,
.ui-markdown.ui-markdown--compact h3,
.ui-markdown.ui-markdown--compact h4 {
  margin-block-start: var(--spacing-md, 12px);
}
.ui-markdown.ui-markdown--compact h1 { font-size: 1.25rem; }
.ui-markdown.ui-markdown--compact h2 { font-size: 1.1rem; }
.ui-markdown.ui-markdown--compact h3 { font-size: 1rem; }`
}
