package ui

import (
	"context"
	"encoding/json"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"

	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// ─── CopyButton ─────────────────────────────────────────────────────
//
// A "copy to clipboard" button that targets another element via CSS
// selector. The runtime's existing data-fui-copy-text-from handler
// performs the clipboard write; this component adds:
//
//   - Visible label/copied-label swap driven by the `.fui-copied`
//     class the runtime toggles for 1.2s.
//   - A visually-hidden role="status" sibling that the runtime
//     populates via data-fui-copy-status on success so screen-reader
//     users hear "Copied" without focus-loss.
//   - Token-driven min tap target (inherits .ui-button-style sizing).
//
// Pairs naturally with CodeBlock (Target="#my-code"), but works for
// any element with a stable selector.

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
	// the page's existing ToastStack (or auto-created one) — no extra
	// wiring required. Use ToastTitle / ToastBody / ToastVariant to
	// configure the message; sensible defaults if left blank.
	ToastOnCopy bool

	// ToastTitle is the toast title when ToastOnCopy=true. Default "Copied".
	ToastTitle string

	// ToastBody is the toast body when ToastOnCopy=true. Default empty.
	ToastBody string

	// ToastVariant maps to the toast's variant — "success" (default),
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

	cls := "ui-copy-btn"
	if cfg.IconOnly {
		cls += " ui-copy-btn--icon"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	btnAttrs := html.Attrs{
		"data-fui-copy-text-from": cfg.Target,
		"data-fui-copy-announce":  announce,
		"type":                    "button",
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
			btnAttrs["data-fui-copy-toast"] = string(b)
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
			html.Span(html.TextConfig{Class: "ui-copy-btn__label"}, render.Text(label)),
			html.Span(html.TextConfig{
				Class:      "ui-copy-btn__copied",
				ExtraAttrs: html.Attrs{"aria-hidden": "true"},
			}, render.Text(copied)),
		}
	} else {
		// Icon-only: no visible label, just an inline check / clipboard glyph.
		inner = []render.HTML{
			render.Raw(`<span class="ui-copy-btn__icon" aria-hidden="true">⧉</span>`),
		}
	}

	btn := render.Tag("button", flattenAttrs(html.MergeAttrs(html.Attrs{"class": cls, "id": cfg.ID}, btnAttrs)),
		inner...)

	// The wrapper holds the button AND the SR-only status span so the
	// runtime can find it via parentElement.querySelector. Wrapper is
	// a plain inline-block; CSS class lets consumers target it.
	status := html.Span(html.TextConfig{
		Class: "ui-visually-hidden",
		ExtraAttrs: html.Attrs{
			"role":                 "status",
			"aria-live":            "polite",
			"data-fui-copy-status": "",
		},
	})

	return copyButtonStyle.WrapHTML(html.Span(html.TextConfig{
		Class: "ui-copy-btn-wrap",
	}, btn, status))
}

var copyButtonStyle = registry.RegisterStyle("ui-copy-btn", copyButtonCSS)

func copyButtonCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-copy-btn"] {
  display: inline-block;
}
[data-fui-comp="ui-copy-btn"] .ui-copy-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: var(--spacing-xs, 4px);
  min-height: var(--spacing-touch-target, 44px);
  min-width: var(--spacing-touch-target, 44px);
  padding: 6px var(--spacing-md, 12px);
  border: 1px solid var(--color-border, #d0d0d8);
  border-radius: var(--radii-md, 6px);
  background: var(--color-surface, #fff);
  color: var(--color-text, #111);
  font: inherit;
  font-size: var(--text-sm, 0.85rem);
  cursor: pointer;
  transition: background-color 150ms ease, border-color 150ms ease;
}
[data-fui-comp="ui-copy-btn"] .ui-copy-btn:hover {
  background: var(--color-muted, #f3f3f5);
}
[data-fui-comp="ui-copy-btn"] .ui-copy-btn:focus-visible {
  outline: none;
  box-shadow: 0 0 0 2px var(--color-surface, #fff), 0 0 0 4px var(--color-primary, #4F46E5);
}
[data-fui-comp="ui-copy-btn"] .ui-copy-btn .ui-copy-btn__copied { display: none; }
[data-fui-comp="ui-copy-btn"] .ui-copy-btn.fui-copied { background: var(--color-success-bg, #e7f8ee); border-color: var(--color-success, #16a34a); }
[data-fui-comp="ui-copy-btn"] .ui-copy-btn.fui-copied .ui-copy-btn__label { display: none; }
[data-fui-comp="ui-copy-btn"] .ui-copy-btn.fui-copied .ui-copy-btn__copied { display: inline; color: var(--color-success, #16a34a); }
[data-fui-comp="ui-copy-btn"] .ui-copy-btn--icon { padding: 6px 10px; }
`
}
