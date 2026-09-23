package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// collapsibleStyle registers the scoped CSS for fui-collapsible so the
// host emits it for any page that renders a Collapsible.
var collapsibleStyle = registry.RegisterStyle("fui-collapsible", collapsibleCSS)

// collapsibleClasses dresses headless.Disclosure's parts in this
// package's own vocabulary.
var collapsibleClasses = headless.Classes{
	headless.PartRoot:    "fui-collapsible",
	headless.PartSummary: "fui-collapsible__summary",
	headless.PartPanel:   "fui-collapsible__content",
}

func collapsibleCSS(_ style.Theme) string {
	// Token chain: --fui-* (the interactive set's host override bridge,
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
// Uses the native <details> element; the headless-disclosure module
// supplies the accessibility behaviour (Escape to close,
// aria-expanded mirroring) through the data-hui-disclosure hook.
type CollapsibleConfig struct {
	Summary string // required:  the always-visible header
	Open    bool   // optional:  start expanded (default: collapsed)
	Class   string // optional:  additional CSS classes
	ID      string // optional:  element id

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the root <details>. Keys
	// the component owns are dropped: class and id (use Class / ID),
	// the data-hui-* wiring, and open (use Open).
	ExtraAttrs html.Attrs
}

// Collapsible renders a <details> element with a clickable summary.
// The data-hui-disclosure hook wires up keyboard accessibility via
// the runtime (Escape to close, aria-expanded mirroring).
//
// The body is wrapped in a fui-collapsible__content div so CSS can
// target the expandable region independently of the summary.
func Collapsible(cfg CollapsibleConfig, body ...render.HTML) render.HTML {
	classes := map[headless.Part]string{
		headless.PartRoot:    "fui-collapsible",
		headless.PartSummary: "fui-collapsible__summary",
		headless.PartPanel:   "fui-collapsible__content",
	}
	if cfg.Class != "" {
		classes[headless.PartRoot] += " " + cfg.Class
	}
	out := headless.Disclosure(headless.DisclosureProps{
		Summary:    render.Text(cfg.Summary),
		Content:    render.Join(body...),
		Open:       cfg.Open,
		ID:         cfg.ID,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "open"),
	}, classes)
	return collapsibleStyle.WrapHTML(out)
}
