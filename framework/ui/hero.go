package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// HeroConfig configures a Hero.
type HeroConfig struct {
	// Eyebrow is an optional kicker rendered as an accent pill above the title.
	Eyebrow string
	// Title is the display headline (rendered as the page <h1>).
	Title string
	// Subtitle is the supporting lede under the title.
	Subtitle string
	// Lede is an inline-markup lede that replaces Subtitle; it renders inside a <p>.
	Lede render.HTML
	// Actions are the call-to-action elements, in a wrapping row under the lede.
	Actions []render.HTML
	// Footer is optional content under the actions, as wide as the copy column.
	Footer render.HTML
	// Media is an optional visual beside the copy; it makes the hero a two-column split.
	Media render.HTML
	// AriaLabel names the hero <section>. Defaults to the Title.
	AriaLabel string
	// Class is appended to the ui-hero wrapper.
	Class string

	// ExtraAttrs is forwarded to the root without the keys the component owns.
	ExtraAttrs html.Attrs
}

// Hero renders a single-column (or copy+media split) hero section.
func Hero(cfg HeroConfig) render.HTML {
	var copyOwn, partsOwn html.Attrs
	if len(cfg.Actions) > 0 || cfg.Lede != "" || cfg.Footer != "" {
		partsOwn = html.Attrs{"data-cui-internal": ""}
	} else {
		copyOwn = html.Attrs{"data-cui-internal": ""}
	}
	copyParts := make([]render.HTML, 0, 4)
	if cfg.Eyebrow != "" {
		pill := StatusPill(StatusPillConfig{Label: cfg.Eyebrow, Tone: StatusPillAccent})
		if partsOwn != nil {
			pill = headless.Own(pill)
		}
		copyParts = append(copyParts, pill)
	}
	copyParts = append(copyParts, html.Heading(html.HeadingConfig{Level: 1, Class: "fui-hero__title", ExtraAttrs: partsOwn}, render.Text(cfg.Title)))
	if cfg.Lede != "" {
		copyParts = append(copyParts, html.Paragraph(html.TextConfig{Class: "fui-hero__lede"}, cfg.Lede))
	} else if cfg.Subtitle != "" {
		copyParts = append(copyParts, html.Paragraph(html.TextConfig{Class: "fui-hero__lede", ExtraAttrs: partsOwn}, render.Text(cfg.Subtitle)))
	}
	if len(cfg.Actions) > 0 {
		copyParts = append(copyParts, html.Div(html.DivConfig{Class: "fui-hero__actions"}, cfg.Actions...))
	}
	if cfg.Footer != "" {
		copyParts = append(copyParts, html.Div(html.DivConfig{Class: "fui-hero__footer"}, cfg.Footer))
	}
	copy := html.Div(html.DivConfig{Class: "fui-hero__copy", ExtraAttrs: copyOwn}, copyParts...)

	cls := "fui-hero"
	if cfg.Media != "" {
		cls += " fui-hero--split"
	}
	if cfg.Class != "" {
		cls = cls + " " + cfg.Class
	}
	label := cfg.AriaLabel
	if label == "" {
		label = cfg.Title
	}
	attrs := html.SafeExtraAttrs(cfg.ExtraAttrs, "aria-label")
	if attrs == nil {
		attrs = map[string]string{}
	}
	attrs["class"] = cls
	if label != "" {
		attrs["aria-label"] = label
	}
	children := []render.HTML{copy}
	if cfg.Media != "" {
		children = append(children, html.Div(html.DivConfig{Class: "fui-hero__media"}, cfg.Media))
	}
	return heroStyle.WrapHTML(render.Tag("section", attrs, children...))
}

var heroStyle = registry.RegisterStyle("ui-hero", heroCSS)

func heroCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-hero"] { display: flex; }
:where([data-cui-comp="ui-hero"]).fui-hero--split {
  display: grid;
  grid-template-columns: minmax(0, 1.2fr) minmax(0, 1fr);
  gap: var(--spacing-2xl, 32px);
  align-items: center;
}
[data-cui-comp="ui-hero"] .fui-hero__copy {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: var(--spacing-lg, 16px);
  max-width: var(--ui-hero-copy-max-width, 42rem);
}
[data-cui-comp="ui-hero"] .fui-hero__title {
  margin: 0;
  font-family: var(--font-heading, inherit);
  /* The preferred size mixes rem with vw so it still follows the
     reader's font-size setting; bare vw would ignore it. */
  font-size: clamp(calc(var(--text-3xl, 1.875rem) * 1.333), calc(1.5rem + 3vw), calc(var(--text-3xl, 1.875rem) * 2.133));
  line-height: calc(var(--leading-tight, 1.2) - 0.16);
  text-wrap: balance;
  letter-spacing: var(--tracking-tighter, -0.03em);
  font-weight: var(--font-weight-bold);
  color: var(--color-text, inherit);
}
[data-cui-comp="ui-hero"] .fui-hero__lede {
  margin: 0;
  font-size: clamp(var(--text-lg, 1.125rem), calc(0.875rem + 0.6vw), calc(var(--text-2xl, 1.5rem) * 0.917));
  line-height: var(--leading-normal, 1.5);
  color: var(--color-text-muted, inherit);
  max-width: 46ch;
}
[data-cui-comp="ui-hero"] .fui-hero__actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--spacing-md, 8px);
  margin-top: var(--spacing-xs, 2px);
}
[data-cui-comp="ui-hero"] .fui-hero__footer {
  align-self: stretch;
  min-inline-size: 0;
  display: flex;
  flex-direction: column;
  gap: var(--spacing-md, 8px);
  color: var(--color-text-muted, inherit);
}
[data-cui-comp="ui-hero"] .fui-hero__footer > p { margin: 0; }
[data-cui-comp="ui-hero"] .fui-hero__media img {
  inline-size: 100%;
  height: auto;
  border-radius: var(--radii-lg, 10px);
}
@media (max-width: 980px) {
  :where([data-cui-comp="ui-hero"]).fui-hero--split { grid-template-columns: minmax(0, 1fr); gap: var(--spacing-lg, 16px); }
}
`
}
