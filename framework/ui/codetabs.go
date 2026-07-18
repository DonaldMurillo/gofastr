package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/patterns/tabs"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
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
}

// CodeTabs renders the same snippet in several languages behind a tab strip
// — the "install this SDK in Go / TypeScript / curl" shape docs sites need.
// It is pure composition: patterns/tabs (zero-JS exclusive tabset) around
// one syntax-highlighted CodeBlock (with copy button) per sample.
//
// Selection is per-tabset — the native <details name=> mechanism has no
// page-wide state, so picking "TypeScript" in one group does not switch
// sibling groups.
func CodeTabs(cfg CodeTabsConfig, samples ...CodeSample) render.HTML {
	if cfg.Name == "" {
		panic("ui: CodeTabs requires Name")
	}
	if len(samples) == 0 {
		panic("ui: CodeTabs requires at least one CodeSample")
	}

	items := make([]tabs.Tab, 0, len(samples))
	for _, s := range samples {
		if s.Label == "" || s.Code == "" {
			panic("ui: CodeSample requires Label and Code")
		}
		items = append(items, tabs.Tab{
			Label: s.Label,
			Content: CodeBlock(CodeBlockConfig{
				Lines:       HighlightLines(s.Code, s.Language),
				Language:    s.Language,
				Filename:    s.Filename,
				ShowCopy:    true,
				LineNumbers: cfg.LineNumbers,
			}),
		})
	}

	cls := "ui-code-tabs"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	return codeTabsStyle.WrapHTML(render.Tag("div",
		map[string]string{"class": cls},
		tabs.New(tabs.Config{Name: cfg.Name, Label: cfg.Label, ID: cfg.ID}, items...),
	))
}

var codeTabsStyle = registry.RegisterStyle("ui-code-tabs", codeTabsCSS)

func codeTabsCSS(_ style.Theme) string {
	// The tabset's panel padding plus the CodeBlock frame double up;
	// tighten the seam so the block sits flush under the strip.
	return `
.ui-code-tabs .tabs > .tabs-panels { padding-top: var(--spacing-md, 8px); }
`
}
