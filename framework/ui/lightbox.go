package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// ─── Lightbox ───────────────────────────────────────────────────────
//
// Standalone zoom overlay. Composes preset.Modal — ESC, click-outside,
// focus-trap, return-focus all come free. Lightbox does NOT render any
// trigger surface itself; any element on the page can open it via
// `data-fui-open="<lightbox-name>" data-fui-deeplink="src=…&alt=…&caption=…&group=<id>"`.
//
// Pairs cleanly with framework/ui.Gallery (set its Lightbox field to
// this Lightbox's Name and each gallery item becomes a trigger) but
// works equally well standalone — markdown-content authors, inline
// figures, custom photo feeds, etc. can all trigger the same overlay.
//
// Optional features:
//   - NavArrows     — Prev/Next buttons + ArrowLeft/Right keyboard nav
//                     across siblings sharing `data-fui-lightbox-group`.
//   - ShowCaption   — `<figcaption>` slot bound to a "caption" signal.
//   - AllowDownload — visible Download button bound to current src.
//
// All three live in the optional runtime module
// core-ui/runtime/src/lightbox.js, which auto-loads when the modal is
// rendered with any of these features enabled.

// LightboxConfig configures a Lightbox.
type LightboxConfig struct {
	// Name is the unique widget name (required) used as the
	// preset.Modal name. Page-unique. Any element with
	// data-fui-open="<this Name>" opens the overlay.
	Name string
	// Label is the accessible name for the open modal. Defaults to
	// "Image viewer".
	Label string
	// NavArrows renders Prev/Next buttons inside the modal AND wires
	// ArrowLeft/Right keyboard nav over siblings sharing the same
	// data-fui-lightbox-group attribute.
	NavArrows bool
	// ShowCaption adds a <figcaption> bound to the "caption" signal.
	// Triggers pass caption=<text> in their data-fui-deeplink.
	ShowCaption bool
	// AllowDownload renders a visible "Download" anchor inside the
	// modal whose href is bound to the current src signal.
	AllowDownload bool
	// Pages, when non-empty, scopes the modal mount to those routes.
	Pages []string

	// Ctx carries the per-request context used to resolve i18n strings
	// (Prev/Next nav aria-labels, Download aria-label). When nil,
	// context.Background() is used and English fallbacks are returned —
	// preserving today's behaviour.
	Ctx context.Context
}

// Lightbox returns a *widget.Builder for the zoom-overlay modal.
// Mount once at app startup; trigger from anywhere via data-fui-open.
func Lightbox(cfg LightboxConfig) *widget.Builder {
	if cfg.Name == "" {
		panic("ui: Lightbox requires Name")
	}
	label := cfg.Label
	if label == "" {
		label = "Image viewer"
	}

	slot := &lightboxSlot{
		name:          cfg.Name,
		label:         label,
		navArrows:     cfg.NavArrows,
		showCaption:   cfg.ShowCaption,
		allowDownload: cfg.AllowDownload,
		ctx:           cfg.Ctx,
	}
	titleID := cfg.Name + "-title"
	mb := preset.Modal(cfg.Name).
		Hidden().
		LabelledBy(titleID).
		DeepLinkParam("src").
		DeepLinkParam("alt").
		DeepLinkParam("caption").
		DeepLinkParam("group").
		Signal("src", widget.SignalFunc(func() (any, error) { return "", nil })).
		Signal("alt", widget.SignalFunc(func() (any, error) { return "", nil })).
		Signal("caption", widget.SignalFunc(func() (any, error) { return "", nil })).
		Signal("group", widget.SignalFunc(func() (any, error) { return "", nil })).
		Slot("body", slot)
	if len(cfg.Pages) > 0 {
		mb = mb.Pages(cfg.Pages...)
	}
	return mb
}

// lightboxSlot renders the open-modal contents.
type lightboxSlot struct {
	name          string
	label         string
	navArrows     bool
	showCaption   bool
	allowDownload bool
	// ctx carries the per-request locale for i18n resolution. nil = context.Background().
	ctx context.Context
}

func (s *lightboxSlot) Render() render.HTML {
	ctx := s.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	figChildren := []render.HTML{
		// SR-only title — Modal.LabelledBy() points here.
		html.Span(html.TextConfig{
			ID:         s.name + "-title",
			Class:      "ui-visually-hidden",
			ExtraAttrs: html.Attrs{"data-fui-signal": "alt"},
		}, render.Text(s.label)),
		// Image — runtime writes signal value into the src attr.
		render.Tag("img", map[string]string{
			"class":                "ui-lightbox__full",
			"alt":                  "",
			"data-fui-signal":      "src",
			"data-fui-signal-mode": "attr",
			"data-fui-signal-attr": "src",
		}),
	}
	if s.showCaption {
		figChildren = append(figChildren,
			render.Tag("figcaption", map[string]string{
				"class":           "ui-lightbox__caption",
				"data-fui-signal": "caption",
			}, render.HTML("")))
	}

	// Toolbar — Prev/Next + Download.
	toolbar := []render.HTML{}
	if s.navArrows {
		toolbar = append(toolbar,
			render.Tag("button", map[string]string{
				"type":                   "button",
				"class":                  "ui-lightbox__nav ui-lightbox__nav--prev",
				"aria-label":             i18nui.T(ctx, i18nui.KeyLightboxPrev),
				"data-fui-lightbox-prev": s.name,
			}, render.HTML(`<svg width="22" height="22" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M15 18l-6-6 6-6" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`)),
			render.Tag("button", map[string]string{
				"type":                   "button",
				"class":                  "ui-lightbox__nav ui-lightbox__nav--next",
				"aria-label":             i18nui.T(ctx, i18nui.KeyLightboxNext),
				"data-fui-lightbox-next": s.name,
			}, render.HTML(`<svg width="22" height="22" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M9 6l6 6-6 6" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`)),
		)
	}
	if s.allowDownload {
		toolbar = append(toolbar,
			render.Tag("a", map[string]string{
				"class":                "ui-lightbox__download",
				"aria-label":           i18nui.T(ctx, i18nui.KeyLightboxDownload),
				"download":             "",
				"data-fui-signal":      "src",
				"data-fui-signal-mode": "attr",
				"data-fui-signal-attr": "href",
			}, render.HTML(`<svg width="22" height="22" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M12 3v12m0 0l-4-4m4 4l4-4M5 21h14" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`)),
		)
	}

	viewerChildren := []render.HTML{
		render.Tag("figure", map[string]string{"class": "ui-lightbox__figure"}, figChildren...),
	}
	if len(toolbar) > 0 {
		viewerChildren = append(viewerChildren,
			render.Tag("div", map[string]string{"class": "ui-lightbox__toolbar"}, toolbar...))
	}

	wrapAttrs := map[string]string{
		"class":             "ui-lightbox__viewer",
		"data-fui-lightbox": s.name,
	}
	if s.navArrows {
		wrapAttrs["data-fui-lightbox-nav"] = "true"
	}

	return lightboxStyle.WrapHTML(render.Tag("div", wrapAttrs, viewerChildren...))
}

var _ component.Component = (*lightboxSlot)(nil)

var lightboxStyle = registry.RegisterStyle("ui-lightbox", lightboxCSS)

func lightboxCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-lightbox"] {
  display: grid;
  gap: var(--spacing-md, 12px);
  place-items: center;
  inline-size: min(90vw, 1200px);
}
[data-fui-comp="ui-lightbox"] .ui-lightbox__figure {
  margin: 0;
  display: grid;
  gap: var(--spacing-sm, 8px);
  place-items: center;
}
[data-fui-comp="ui-lightbox"] .ui-lightbox__full {
  display: block;
  max-inline-size: 100%;
  max-block-size: min(75vh, 80vh);
  object-fit: contain;
  border-radius: var(--radii-md, 8px);
  /* touch-action: none lets the pinch-zoom runtime own all gestures
     on the image without the browser claiming pinch as a page zoom. */
  touch-action: none;
  user-select: none;
  -webkit-user-drag: none;
  will-change: transform;
  cursor: zoom-in;
}
[data-fui-comp="ui-lightbox"] .ui-lightbox__full[data-fui-zoomed] {
  cursor: grab;
}
[data-fui-comp="ui-lightbox"] .ui-lightbox__full[data-fui-zoomed]:active {
  cursor: grabbing;
}
[data-fui-comp="ui-lightbox"] .ui-lightbox__caption {
  margin: 0;
  font-size: var(--text-sm, 0.9rem);
  text-align: center;
  color: var(--color-text-muted, #52525B);
  max-inline-size: 60ch;
}
[data-fui-comp="ui-lightbox"] .ui-lightbox__caption:empty { display: none; }

[data-fui-comp="ui-lightbox"] .ui-lightbox__toolbar {
  display: flex;
  gap: var(--spacing-sm, 8px);
  align-items: center;
  justify-content: center;
}
[data-fui-comp="ui-lightbox"] .ui-lightbox__nav,
[data-fui-comp="ui-lightbox"] .ui-lightbox__download {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-block-size: var(--spacing-touch-target, 44px);
  min-inline-size: var(--spacing-touch-target, 44px);
  border-radius: 999px;
  border: 0;
  background: var(--color-surface-soft, #F4F4F5);
  color: var(--color-text, #18181B);
  cursor: pointer;
  text-decoration: none;
}
[data-fui-comp="ui-lightbox"] .ui-lightbox__nav:hover,
[data-fui-comp="ui-lightbox"] .ui-lightbox__download:hover {
  background: var(--color-border, #E4E4E7);
}
[data-fui-comp="ui-lightbox"] .ui-lightbox__nav:focus-visible,
[data-fui-comp="ui-lightbox"] .ui-lightbox__download:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 2px;
}`
}
