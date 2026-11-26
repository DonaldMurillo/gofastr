package ui

import (
	"context"
	"strconv"
	"strings"

	"encoding/json"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"

	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// CopyButtonConfig configures the copy button.
type CopyButtonConfig struct {
	// Target is the id of the element whose text is copied; a leading "#" is dropped. Required.
	Target string
	// Label is the text before copying. Default "Copy".
	Label string
	// CopiedLabel is the text shown briefly after success. Default "Copied".
	CopiedLabel string
	// FailedLabel is the text shown for a few seconds when the clipboard
	// refuses the copy. Default "Not copied".
	FailedLabel string
	// IconOnly hides the visible label and keeps an accessible name.
	IconOnly bool
	// Icon names a registered icon drawn in place of the copy glyph, or before the label.
	Icon string
	// Inline sits the button in a line of text with a 24px target.
	Inline bool
	// Ghost draws the button with no border, fill or shadow until hover.
	Ghost bool
	// Tile draws a full-width tile with the icon above the label; it excludes IconOnly, Inline and Ghost.
	Tile bool
	// AriaLabel overrides the accessible name. IconOnly defaults it to "Copy to clipboard".
	AriaLabel string
	// AnnounceText is the status message on success. Default "Copied".
	AnnounceText string
	// ToastOnCopy also fires a toast on success.
	ToastOnCopy bool
	// ToastTitle defaults to "Copied".
	ToastTitle string
	// ToastBody defaults to empty.
	ToastBody string
	// ToastVariant is "success" (default), "info", "warning" or "danger".
	ToastVariant string
	// ToastTTLms is the toast timeout. Default 3000.
	ToastTTLms int
	// Ctx resolves the default words. Nil uses English.
	Ctx context.Context
	// ID lands on the button, not the wrapper.
	ID    string
	Class string
	// ExtraAttrs land on the wrapper; class, id and data-cui-* are dropped.
	ExtraAttrs html.Attrs
}

// CopyButton renders the button.
func CopyButton(cfg CopyButtonConfig) render.HTML {
	if cfg.Tile && (cfg.IconOnly || cfg.Inline || cfg.Ghost) {
		panic("ui: CopyButton Tile cannot be combined with IconOnly, Inline or Ghost")
	}
	if cfg.Target == "" {
		panic("ui: CopyButton requires Target")
	}
	targetID := strings.TrimPrefix(cfg.Target, "#")
	if targetID == "" || strings.ContainsAny(targetID, " \t\n\r\f.>[:#") {
		panic("ui: CopyButton Target must be an element id (a leading # is allowed), not a CSS selector: " +
			strconv.Quote(cfg.Target))
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	label := cfg.Label
	if label == "" {
		label = i18nui.CopyCopy.Text(ctx)
	}
	copied := cfg.CopiedLabel
	if copied == "" {
		copied = i18nui.CopyCopied.Text(ctx)
	}
	failedLabel := cfg.FailedLabel
	if failedLabel == "" {
		failedLabel = i18nui.CopyNotCopied.Text(ctx)
	}
	announce := cfg.AnnounceText
	if announce == "" {
		announce = i18nui.CopyCopied.Text(ctx)
	}

	cls := "fui-copy-btn"
	if cfg.IconOnly {
		cls += " fui-copy-btn--icon"
	}
	if cfg.Inline {
		cls += " fui-copy-btn--inline"
	}
	if cfg.Ghost {
		cls += " fui-copy-btn--ghost"
	}
	if cfg.Tile {
		cls += " fui-copy-btn--tile"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	btnAttrs := html.Attrs{
		"type":              "button",
		"data-cui-internal": "",
	}
	if cfg.Tile {
		btnAttrs["title"] = label
	}
	if cfg.ToastOnCopy {
		variant := cfg.ToastVariant
		if variant == "" {
			variant = "success"
		}
		title := cfg.ToastTitle
		if title == "" {
			title = i18nui.CopyCopied.Text(ctx)
		}
		ttl := cfg.ToastTTLms
		if ttl <= 0 {
			ttl = 3000
		}
		toastCfg := map[string]any{
			"variant": variant,
			"title":   title,
			"ttl":     ttl,
		}
		if cfg.ToastBody != "" {
			toastCfg["body"] = cfg.ToastBody
		}
		if b, err := json.Marshal(toastCfg); err == nil {
			btnAttrs["data-hui-copy-toast"] = string(b)
		}
	}
	if cfg.IconOnly {
		al := cfg.AriaLabel
		if al == "" {
			al = i18nui.CopyToClipboard.Text(ctx)
		}
		btnAttrs["aria-label"] = al
	} else if cfg.AriaLabel != "" {
		btnAttrs["aria-label"] = cfg.AriaLabel
	}

	var inner []render.HTML
	if !cfg.IconOnly {
		if cfg.Icon != "" && IconRegistered(cfg.Icon) {
			size := "16"
			if cfg.Tile {
				size = "24"
			}
			inner = append(inner, html.Span(html.TextConfig{Class: "fui-copy-btn__icon", ExtraAttrs: html.Attrs{"aria-hidden": "true"}},
				Icon(cfg.Icon, IconConfig{Size: size})))
		}
		inner = append(inner,
			html.Span(html.TextConfig{Class: "fui-copy-btn__label"}, render.Text(label)),
			html.Span(html.TextConfig{
				Class:      "fui-copy-btn__copied",
				ExtraAttrs: html.Attrs{"aria-hidden": "true"},
			}, render.Text(copied)),
			html.Span(html.TextConfig{
				Class:      "fui-copy-btn__failed",
				ExtraAttrs: html.Attrs{"aria-hidden": "true"},
			}, render.Text(failedLabel)))
	} else {
		inner = []render.HTML{
			render.Raw(`<span class="fui-copy-btn__icon" aria-hidden="true">⧉</span>`),
		}
		if cfg.Icon != "" && IconRegistered(cfg.Icon) {
			inner = []render.HTML{html.Span(html.TextConfig{Class: "fui-copy-btn__icon", ExtraAttrs: html.Attrs{"aria-hidden": "true"}},
				Icon(cfg.Icon, IconConfig{Size: "16"}))}
		}
	}

	if cfg.ID != "" {
		btnAttrs["id"] = cfg.ID
	}
	btn := render.Tag("button", flattenAttrs(html.MergeAttrs(html.Attrs{"class": cls}, btnAttrs)),
		inner...)

	status := html.Span(html.TextConfig{
		Class: "fui-visually-hidden",
		ExtraAttrs: html.Attrs{
			"role":                 "status",
			"aria-live":            "polite",
			"data-hui-copy-status": "",
			"data-cui-internal":    "",
		},
	})

	wrapAttrs := html.Attrs{
		"data-hui-copy":          "",
		"data-hui-copy-target":   targetID,
		"data-hui-copy-name":     targetID,
		"data-hui-copy-copied":   copied,
		"data-hui-copy-back":     label,
		"data-hui-copy-sentence": announce,
		"data-hui-copy-failed":   i18nui.CopyFailed.Text(ctx),
	}
	for k, v := range html.SafeExtraAttrs(cfg.ExtraAttrs) {
		wrapAttrs[k] = v
	}
	wrapClass := "fui-copy-btn-wrap"
	if cfg.Tile {
		wrapClass += " fui-copy-btn-wrap--tile"
	}
	return copyButtonStyle.WrapHTML(html.Span(html.TextConfig{
		Class:      wrapClass,
		ExtraAttrs: wrapAttrs,
	}, btn, status))
}

var copyButtonStyle = registry.RegisterStyle("ui-copy-btn", copyButtonCSS)

func copyButtonCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-copy-btn"] {
  display: inline-block;
}
/* The --ui-copy-btn-* knobs let a host surface restyle the button
   without reaching into it: the framed CodeBlock head sets them for a
   quiet button on its dark chrome. */
[data-cui-comp="ui-copy-btn"] .fui-copy-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: var(--spacing-sm, 4px);
  min-height: var(--ui-copy-btn-size, var(--spacing-touch-target, 44px));
  min-width: var(--ui-copy-btn-size, var(--spacing-touch-target, 44px));
  padding: calc(var(--spacing-sm, 4px) * 1.5) var(--spacing-md, 8px);
  border: var(--stroke-thin, 1px) solid var(--ui-copy-btn-border, var(--color-border, #E4E4E7));
  border-radius: var(--radii-md, 8px);
  background: var(--ui-copy-btn-bg, var(--color-surface, #fff));
  color: var(--ui-copy-btn-color, var(--color-text, #09090B));
  box-shadow: var(--ui-copy-btn-shadow, var(--shadow-xs));
  font: inherit;
  font-size: var(--text-sm, 0.875rem);
  font-weight: var(--font-weight-medium);
  cursor: pointer;
  transition: background-color var(--duration-fast, 150ms) var(--easing-ease-in-out, ease), border-color var(--duration-fast, 150ms) var(--easing-ease-in-out, ease), color var(--duration-fast, 150ms) var(--easing-ease-in-out, ease), box-shadow var(--duration-fast, 150ms) var(--easing-ease-in-out, ease), translate var(--duration-fast, 150ms) var(--easing-ease-in-out, ease);
}
/* Hover keeps the theme's own surface pair: surface-soft is defined by every
   theme in both schemes, so text set to --color-text stays readable (a rule
   that names an undefined token silently renders its fallback constant
   instead, which is how dark themes once got near-white on white here). */
[data-cui-comp="ui-copy-btn"] .fui-copy-btn:hover {
  background: var(--ui-copy-btn-hover-bg, var(--color-surface-soft, #F4F4F5));
  color: var(--ui-copy-btn-hover-color, var(--ui-copy-btn-color, var(--color-text, #09090B)));
  box-shadow: var(--ui-copy-btn-hover-shadow, var(--ui-press-hover-shadow, var(--ui-copy-btn-shadow, var(--shadow-xs))));
  translate: var(--ui-copy-btn-hover-translate, var(--ui-press-hover-translate, none));
}
/* Knobs: --ui-copy-btn-hover-shadow / -active-shadow and
   --ui-copy-btn-hover-translate / -active-translate over the shared
   --ui-press-* knobs. */
[data-cui-comp="ui-copy-btn"] .fui-copy-btn:active {
  box-shadow: var(--ui-copy-btn-active-shadow, var(--ui-press-active-shadow, var(--ui-copy-btn-hover-shadow, var(--ui-press-hover-shadow, var(--ui-copy-btn-shadow, var(--shadow-xs))))));
  translate: var(--ui-copy-btn-active-translate, var(--ui-press-active-translate, var(--ui-copy-btn-hover-translate, var(--ui-press-hover-translate, none))));
}
[data-cui-comp="ui-copy-btn"] .fui-copy-btn:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: var(--stroke-focus-offset, 2px);
}
[data-cui-comp="ui-copy-btn"] .fui-copy-btn .fui-copy-btn__copied,
[data-cui-comp="ui-copy-btn"] .fui-copy-btn .fui-copy-btn__failed { display: none; }
/* Success tint mixes the theme's own success color over the surface, so it
   adapts to light and dark schemes alike (--color-success-bg was never a real
   token; its light fallback always applied). */
[data-cui-comp="ui-copy-btn"][data-hui-copy-state="done"] .fui-copy-btn { background: color-mix(in oklab, var(--color-success, #166534) 14%, transparent); border-color: var(--color-success, #166534); }
/* The label leaves the eye but not the accessible name while the
   copied or failed word shows: that word is aria-hidden, so hiding
   the label with display none left the button unnamed. */
[data-cui-comp="ui-copy-btn"]:is([data-hui-copy-state="done"], [data-hui-copy-state="failed"]) .fui-copy-btn .fui-copy-btn__label {
  position: absolute;
  inline-size: 1px;
  block-size: 1px;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
}
[data-cui-comp="ui-copy-btn"][data-hui-copy-state="done"] .fui-copy-btn .fui-copy-btn__copied { display: inline; color: var(--color-success, #166534); }
/* A refused clipboard is shown, not only announced: the danger tint and,
   on a labelled button, the short failed label for the module's window. */
[data-cui-comp="ui-copy-btn"][data-hui-copy-state="failed"] .fui-copy-btn { background: color-mix(in oklab, var(--color-danger, #B91C1C) 12%, transparent); border-color: var(--color-danger, #B91C1C); }
[data-cui-comp="ui-copy-btn"][data-hui-copy-state="failed"] .fui-copy-btn .fui-copy-btn__failed { display: inline; color: var(--color-danger, #B91C1C); }
[data-cui-comp="ui-copy-btn"] .fui-copy-btn--icon { padding: calc(var(--spacing-sm, 4px) * 1.5) calc(var(--spacing-sm, 4px) * 2.5); }
/* Inline: the knobs set on the button itself, so the base rules draw
   it quiet; WCAG 2.5.8's 24px floor stands in for the touch target. */
[data-cui-comp="ui-copy-btn"] .fui-copy-btn--inline {
  --ui-copy-btn-size: 1.5rem;
  --ui-copy-btn-border: transparent;
  --ui-copy-btn-bg: transparent;
  --ui-copy-btn-shadow: none;
  --ui-copy-btn-color: var(--color-text-muted, #52525b);
  --ui-copy-btn-hover-color: var(--color-text, #09090B);
  padding: var(--spacing-xs, 2px);
}
/* Ghost: the knobs set on the button itself, sized and padded like a
   small icon Button so a row of them lines up. */
[data-cui-comp="ui-copy-btn"] .fui-copy-btn--ghost {
  --ui-copy-btn-size: auto;
  --ui-copy-btn-border: transparent;
  --ui-copy-btn-bg: transparent;
  --ui-copy-btn-shadow: none;
  padding: calc(var(--spacing-sm, 4px) * 1.5);
}
/* The icon glyph: one line-box tall so the aria-hidden ⧉ never stretches
   the icon-only button past the touch target the base rule sets. */
[data-cui-comp="ui-copy-btn"] .fui-copy-btn__icon {
  line-height: 1;
}
/* Copyable catalog tiles share the feedback and keyboard behavior of the
   regular button. Keep one glyph, a quiet label and an even footprint. */
:where([data-cui-comp="ui-copy-btn"]).fui-copy-btn-wrap--tile {
  display: block;
  block-size: 100%;
}
[data-cui-comp="ui-copy-btn"] .fui-copy-btn--tile {
  display: flex;
  flex-direction: column;
  inline-size: 100%;
  min-inline-size: 0;
  min-block-size: var(--ui-copy-btn-tile-min-height, 6rem);
  block-size: 100%;
  gap: var(--spacing-md, 8px);
  padding: var(--spacing-lg, 16px) var(--spacing-sm, 4px);
  box-shadow: none;
  font-size: var(--text-xs, 0.75rem);
  font-weight: var(--font-weight-normal, 400);
}
[data-cui-comp="ui-copy-btn"] .fui-copy-btn--tile .fui-copy-btn__icon {
  display: flex;
}
[data-cui-comp="ui-copy-btn"] .fui-copy-btn--tile :is(.fui-copy-btn__label, .fui-copy-btn__copied) {
  max-inline-size: 100%;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
`
}
