package ui

import (
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── Collapsible ────────────────────────────────────────────────────

// CollapsibleConfig configures an expand/collapse section.
// Uses the native <details> element with data-fui-disclosure for
// keyboard support (Escape to close, aria-expanded mirroring).
type CollapsibleConfig struct {
	Summary string // required — the always-visible header
	Open    bool   // optional — start expanded (default: collapsed)
	Class   string // optional — additional CSS classes
	ID      string // optional — element id
}

// Collapsible renders a <details> element with a clickable summary.
// The data-fui-disclosure attribute wires up keyboard accessibility
// via the runtime (Escape to close, aria-expanded mirroring).
//
// The body is wrapped in a fui-collapsible__content div so CSS can
// target the expandable region independently of the summary.
func Collapsible(cfg CollapsibleConfig, body ...render.HTML) render.HTML {
	if cfg.Summary == "" {
		panic("ui: Collapsible requires Summary")
	}

	attrs := map[string]string{
		"class":               "fui-collapsible",
		"data-fui-comp":       "fui-collapsible",
		"data-fui-disclosure": "",
	}
	if cfg.Open {
		attrs["open"] = ""
	}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	if cfg.Class != "" {
		attrs["class"] = "fui-collapsible " + cfg.Class
	}

	summary := render.Tag("summary", map[string]string{
		"class": "fui-collapsible__summary",
	}, render.Text(cfg.Summary))

	content := render.Tag("div", map[string]string{
		"class": "fui-collapsible__content",
	}, body...)

	return render.Tag("details", attrs, summary, content)
}
