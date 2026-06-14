package ui

// Hero — a single-column editorial hero for marketing/landing surfaces:
// an optional eyebrow pill, a large display title, a supporting lede, and a
// row of call-to-action buttons. When Media is supplied it becomes a
// two-column split (copy + media) that collapses to one column on small
// screens. The framework owns the type scale, spacing, and responsive
// behaviour so consumers don't hand-roll hero CSS.

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// HeroConfig configures a Hero.
type HeroConfig struct {
	// Eyebrow is an optional short kicker rendered as an accent pill above
	// the title (e.g. "Billing & revenue").
	Eyebrow string
	// Title is the display headline (rendered as the page <h1>).
	Title string
	// Subtitle is the supporting lede under the title.
	Subtitle string
	// Actions are the call-to-action elements (usually ui.LinkButton),
	// laid out in a wrapping row beneath the lede.
	Actions []render.HTML
	// Media is an optional visual rendered beside the copy. When set, the
	// hero becomes a two-column split; when empty it's a single column.
	Media render.HTML
	// AriaLabel names the hero <section>. Defaults to the Title.
	AriaLabel string
	// Class is appended to the ui-hero wrapper.
	Class string
}

// Hero renders a single-column (or copy+media split) hero section.
func Hero(cfg HeroConfig) render.HTML {
	copyParts := make([]render.HTML, 0, 4)
	if cfg.Eyebrow != "" {
		copyParts = append(copyParts, StatusPill(StatusPillConfig{Label: cfg.Eyebrow, Tone: StatusPillAccent}))
	}
	copyParts = append(copyParts, html.Heading(html.HeadingConfig{Level: 1, Class: "ui-hero__title"}, render.Text(cfg.Title)))
	if cfg.Subtitle != "" {
		copyParts = append(copyParts, html.Paragraph(html.TextConfig{Class: "ui-hero__lede"}, render.Text(cfg.Subtitle)))
	}
	if len(cfg.Actions) > 0 {
		copyParts = append(copyParts, html.Div(html.DivConfig{Class: "ui-hero__actions"}, cfg.Actions...))
	}
	copy := html.Div(html.DivConfig{Class: "ui-hero__copy"}, copyParts...)

	cls := "ui-hero"
	if cfg.Media != "" {
		cls += " ui-hero--split"
	}
	if cfg.Class != "" {
		cls = cls + " " + cfg.Class
	}
	label := cfg.AriaLabel
	if label == "" {
		label = cfg.Title
	}
	attrs := map[string]string{"class": cls}
	if label != "" {
		attrs["aria-label"] = label
	}
	children := []render.HTML{copy}
	if cfg.Media != "" {
		children = append(children, html.Div(html.DivConfig{Class: "ui-hero__media"}, cfg.Media))
	}
	return heroStyle.WrapHTML(render.Tag("section", attrs, children...))
}

var heroStyle = registry.RegisterStyle("ui-hero", heroCSS)

func heroCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-hero"] { display: flex; }
[data-fui-comp="ui-hero"].ui-hero--split {
  display: grid;
  grid-template-columns: minmax(0, 1.2fr) minmax(0, 1fr);
  gap: var(--spacing-2xl, 48px);
  align-items: center;
}
[data-fui-comp="ui-hero"] .ui-hero__copy {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: var(--spacing-lg, 24px);
  max-width: 42rem;
}
[data-fui-comp="ui-hero"] .ui-hero__title {
  margin: 0;
  font-family: var(--font-heading, inherit);
  font-size: clamp(2.5rem, 6vw, 4rem);
  line-height: 1.04;
  letter-spacing: -0.03em;
  font-weight: 700;
  color: var(--color-text, inherit);
}
[data-fui-comp="ui-hero"] .ui-hero__lede {
  margin: 0;
  font-size: clamp(1.125rem, 2.2vw, 1.375rem);
  line-height: 1.5;
  color: var(--color-text-muted, inherit);
  max-width: 46ch;
}
[data-fui-comp="ui-hero"] .ui-hero__actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--spacing-sm, 12px);
  margin-top: var(--spacing-xs, 4px);
}
[data-fui-comp="ui-hero"] .ui-hero__media img {
  inline-size: 100%;
  height: auto;
  border-radius: var(--radius-lg, 16px);
}
@media (max-width: 980px) {
  [data-fui-comp="ui-hero"].ui-hero--split { grid-template-columns: 1fr; gap: var(--spacing-lg, 24px); }
}
`
}
