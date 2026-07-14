package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// collapsibleStyle registers the scoped CSS for fui-collapsible so the
// host emits it for any page that renders a Collapsible.
var collapsibleStyle = registry.RegisterStyle("fui-collapsible", collapsibleCSS)

func collapsibleCSS(_ style.Theme) string {
	// Token chain: --fui-* (the interactive set's host override bridge —
	// see TestFuiBridgeChainsToColorTokens) wins when a host sets it, then
	// the canonical adaptive --color-* theme, then the light literal.
	return `[data-fui-comp="fui-collapsible"]{border:1px solid var(--fui-border, var(--color-border, #e2e8f0));border-radius:var(--radii-md,.5rem);background:var(--fui-surface, var(--color-surface, #fff));color:var(--fui-foreground, var(--color-text, #0f172a));overflow:hidden}` +
		`[data-fui-comp="fui-collapsible"] .fui-collapsible__summary{padding:.75rem var(--spacing-lg, 1rem);cursor:pointer;font-weight:600;color:var(--fui-foreground, var(--color-text, #0f172a));list-style:none;display:flex;align-items:center;justify-content:space-between;user-select:none}` +
		`[data-fui-comp="fui-collapsible"] .fui-collapsible__summary::-webkit-details-marker{display:none}` +
		`[data-fui-comp="fui-collapsible"] .fui-collapsible__summary::after{content:"\25B8";transition:transform var(--duration-fast,150ms) var(--easing-standard,ease);color:var(--fui-muted, var(--color-text-muted, #64748b))}` +
		`[data-fui-comp="fui-collapsible"][open] .fui-collapsible__summary::after{transform:rotate(90deg)}` +
		`[data-fui-comp="fui-collapsible"] .fui-collapsible__summary:focus-visible{outline:2px solid var(--fui-primary, var(--color-primary, #3b82f6));outline-offset:-2px}` +
		`[data-fui-comp="fui-collapsible"] .fui-collapsible__content{padding:.75rem var(--spacing-lg, 1rem);color:var(--fui-foreground, var(--color-text, #0f172a));border-top:1px solid var(--fui-border, var(--color-border, #e2e8f0))}`
}

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
