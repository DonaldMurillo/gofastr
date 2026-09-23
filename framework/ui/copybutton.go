package ui

import (
	"context"
	"strings"

	"encoding/json"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"

	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// ─── CopyButton ─────────────────────────────────────────────────────
//
// A "copy to clipboard" button that targets another element by id.
// The headless-feedback module's [data-hui-copy] reader performs the
// clipboard write; this component adds:
//
//   - Visible label/copied-label swap driven by the `.fui-copied`
//     class the module toggles for 1.2s.
//   - A visually-hidden role="status" sibling the module populates
//     through data-hui-copy-status on success so screen-reader users
//     hear "Copied" without focus-loss.
//   - Token-driven min tap target (the button family's token-driven sizing).
//
// Pairs naturally with CodeBlock (Target="#my-code"), but works for
// any element with a stable id.

// CopyButtonConfig configures the copy button.
type CopyButtonConfig struct {
	// Target is a CSS selector that identifies the element whose
	// textContent will be copied. Required.
	Target string

	// Label is the visible button text before copying. Default "Copy".
	Label string

	// CopiedLabel is the visible text shown briefly after success.
	// Default "Copied".
	CopiedLabel string

	// IconOnly hides the visible label but keeps the SR-only label
	// (via AriaLabel or default). Use when the button is icon-only.
	IconOnly bool

	// AriaLabel overrides the screen-reader name. When IconOnly is
	// true and AriaLabel is empty, defaults to "Copy to clipboard".
	AriaLabel string

	// AnnounceText is the message written into the role=status span
	// on copy success. Default "Copied".
	AnnounceText string

	// ToastOnCopy, when true, fires a toast on copy success. The toast
	// is dispatched via window.__gofastr.toast({...}) so it stacks in
	// the page's existing ToastStack (or auto-created one), no extra
	// wiring required. Use ToastTitle / ToastBody / ToastVariant to
	// configure the message; sensible defaults if left blank.
	ToastOnCopy bool

	// ToastTitle is the toast title when ToastOnCopy=true. Default "Copied".
	ToastTitle string

	// ToastBody is the toast body when ToastOnCopy=true. Default empty.
	ToastBody string

	// ToastVariant maps to the toast's variant: "success" (default),
	// "info", "warning", "danger".
	ToastVariant string

	// ToastTTLms is the toast auto-dismiss timeout in milliseconds.
	// Default 3000.
	ToastTTLms int

	// Ctx carries the per-request context used to resolve the
	// Copy/Copied/clipboard labels. When nil, English fallbacks apply.
	Ctx context.Context

	ID    string
	Class string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the root wrapper (the
	// span carrying data-fui-comp). Keys the component owns are
	// dropped: class, id (ID lands on the button, not the wrapper),
	// and data-fui-*.
	ExtraAttrs html.Attrs
}

// CopyButton renders the button.
func CopyButton(cfg CopyButtonConfig) render.HTML {
	if cfg.Target == "" {
		panic("ui: CopyButton requires Target")
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	label := cfg.Label
	if label == "" {
		label = i18nui.T(ctx, i18nui.KeyCopyCopy)
	}
	copied := cfg.CopiedLabel
	if copied == "" {
		copied = i18nui.T(ctx, i18nui.KeyCopyCopied)
	}
	announce := cfg.AnnounceText
	if announce == "" {
		announce = i18nui.T(ctx, i18nui.KeyCopyCopied)
	}

	cls := "fui-copy-btn"
	if cfg.IconOnly {
		cls += " fui-copy-btn--icon"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	btnAttrs := html.Attrs{
		"type": "button",
	}
	if cfg.ToastOnCopy {
		variant := cfg.ToastVariant
		if variant == "" {
			variant = "success"
		}
		title := cfg.ToastTitle
		if title == "" {
			title = i18nui.T(ctx, i18nui.KeyCopyCopied)
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
			// A toast on copy rides the feedback module's toast API;
			// the config travels on the wrapper the module resolves.
			btnAttrs["data-hui-copy-toast"] = string(b)
		}
	}
	if cfg.IconOnly {
		al := cfg.AriaLabel
		if al == "" {
			al = i18nui.T(ctx, i18nui.KeyCopyToClipboard)
		}
		btnAttrs["aria-label"] = al
	} else if cfg.AriaLabel != "" {
		btnAttrs["aria-label"] = cfg.AriaLabel
	}

	// Visible labels (one shown via CSS at a time based on .fui-copied).
	var inner []render.HTML
	if !cfg.IconOnly {
		inner = []render.HTML{
			html.Span(html.TextConfig{Class: "fui-copy-btn__label"}, render.Text(label)),
			html.Span(html.TextConfig{
				Class:      "fui-copy-btn__copied",
				ExtraAttrs: html.Attrs{"aria-hidden": "true"},
			}, render.Text(copied)),
		}
	} else {
		// Icon-only: no visible label, just an inline check / clipboard glyph.
		inner = []render.HTML{
			render.Raw(`<span class="fui-copy-btn__icon" aria-hidden="true">⧉</span>`),
		}
	}

	btn := render.Tag("button", flattenAttrs(html.MergeAttrs(html.Attrs{"class": cls, "id": cfg.ID}, btnAttrs)),
		inner...)

	// The wrapper holds the button AND the SR-only status span so the
	// runtime can find it via parentElement.querySelector. Wrapper is
	// a plain inline-block; CSS class lets consumers target it.
	status := html.Span(html.TextConfig{
		Class: "fui-visually-hidden",
		ExtraAttrs: html.Attrs{
			"role":                 "status",
			"aria-live":            "polite",
			"data-hui-copy-status": "",
		},
	})

	// The hook carries an ELEMENT ID, not a selector: the module
	// resolves it with getElementById, so a value carrying "#" (the
	// old selector spelling) loses its prefix here.
	targetID := strings.TrimPrefix(cfg.Target, "#")
	wrapAttrs := html.Attrs{
		"data-hui-copy":          "",
		"data-hui-copy-target":   targetID,
		"data-hui-copy-name":     targetID,
		"data-hui-copy-copied":   copied,
		"data-hui-copy-back":     label,
		"data-hui-copy-sentence": announce,
	}
	for k, v := range html.SafeExtraAttrs(cfg.ExtraAttrs) {
		wrapAttrs[k] = v
	}
	if cfg.ID != "" {
		wrapAttrs["id"] = cfg.ID
	}
	return copyButtonStyle.WrapHTML(html.Span(html.TextConfig{
		Class:      "fui-copy-btn-wrap",
		ExtraAttrs: wrapAttrs,
	}, btn, status))
}

var copyButtonStyle = registry.RegisterStyle("ui-copy-btn", copyButtonCSS)

func copyButtonCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-copy-btn"] {
  display: inline-block;
}
[data-fui-comp="ui-copy-btn"] .fui-copy-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: var(--spacing-xs, 2px);
  min-height: var(--spacing-touch-target, 44px);
  min-width: var(--spacing-touch-target, 44px);
  padding: 6px var(--spacing-md, 8px);
  border: 1px solid var(--color-border, #d0d0d8);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #fff);
  color: var(--color-text, #111);
  font: inherit;
  font-size: var(--text-sm, 0.875rem);
  cursor: pointer;
  transition: background-color 150ms ease, border-color 150ms ease;
}
/* Hover keeps the theme's own surface pair: surface-soft is defined by every
   theme in both schemes, so text set to --color-text stays readable. (The old
   rule referenced --color-muted — a token that does not exist — so the light
   #f3f3f5 fallback always applied and dark themes got near-white on white.) */
[data-fui-comp="ui-copy-btn"] .fui-copy-btn:hover {
  background: var(--color-surface-soft, #f3f3f5);
  border-color: var(--color-border-strong, var(--color-border, #d0d0d8));
}
[data-fui-comp="ui-copy-btn"] .fui-copy-btn:focus-visible {
  outline: none;
  box-shadow: 0 0 0 2px var(--color-surface, #fff), 0 0 0 4px var(--color-primary, #4F46E5);
}
[data-fui-comp="ui-copy-btn"] .fui-copy-btn .fui-copy-btn__copied { display: none; }
/* Success tint mixes the theme's own success color over the surface, so it
   adapts to light and dark schemes alike (--color-success-bg was never a real
   token; its light fallback always applied). */
[data-fui-comp="ui-copy-btn"] .fui-copy-btn.fui-copied { background: color-mix(in srgb, var(--color-success, #16a34a) 14%, transparent); border-color: var(--color-success, #16a34a); }
[data-fui-comp="ui-copy-btn"] .fui-copy-btn.fui-copied .fui-copy-btn__label { display: none; }
[data-fui-comp="ui-copy-btn"] .fui-copy-btn.fui-copied .fui-copy-btn__copied { display: inline; color: var(--color-success, #16a34a); }
[data-fui-comp="ui-copy-btn"] .fui-copy-btn--icon { padding: 6px 10px; }
/* The icon glyph: one line-box tall so the aria-hidden ⧉ never stretches
   the icon-only button past the touch target the base rule sets. */
[data-fui-comp="ui-copy-btn"] .fui-copy-btn__icon {
  line-height: 1;
}
`
}
