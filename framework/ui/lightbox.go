package ui

import (
	"context"
	_ "embed"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ─── Lightbox ───────────────────────────────────────────────────────
//
// Standalone zoom overlay. Composes preset.Modal: ESC, click-outside,
// focus-trap, return-focus all come free. Lightbox does NOT render any
// trigger surface itself; any element on the page can open it via
// `data-fui-open="<lightbox-name>" data-fui-deeplink="src=…&alt=…&caption=…&group=<id>"`.
//
// Pairs cleanly with framework/ui.Gallery (set its Lightbox field to
// this Lightbox's Name and each gallery item becomes a trigger) but
// works equally well standalone: markdown-content authors, inline
// figures, custom photo feeds, etc. can all trigger the same overlay.
//
// Optional features:
//   - NavArrows:      Prev/Next buttons + ArrowLeft/Right keyboard nav
//                     across siblings sharing `data-fui-lightbox-group`.
//   - ShowCaption:    `<figcaption>` slot bound to a "caption" signal.
//   - AllowDownload:  visible Download button bound to current src.
//
// The markup is headless.LightboxViewer dressed with the fui-lightbox
// class map; the behaviour — gallery stepping, adjacent-image
// preloading, pinch-to-zoom — is the lightbox module this file
// registers below, which auto-loads when the viewer's
// [data-fui-lightbox] marker is in the DOM. The module needs the
// widgets runtime (it re-opens the widget to re-fire the deeplink
// signals), which its registration declares as Requires; its prev/next
// clicks and arrow keys are retained through the module's cold-cache
// fetch by the kernel's interaction bridge, from the interactions the
// registration declares — the kernel itself names no lightbox.

//go:embed lightbox.js
var lightboxJS string

var _ = registry.RegisterBehavior("lightbox", lightboxJS,
	registry.Markers("[data-fui-lightbox]"),
	registry.Requires("widgets"),
	registry.Interactions(
		registry.Interaction{
			Event:    "click",
			Selector: "[data-fui-lightbox-prev],[data-fui-lightbox-next]",
		},
		registry.Interaction{
			Event: "keydown",
			Scope: "[data-fui-widget]:not([hidden]) [data-fui-lightbox]",
			Keys:  []string{"ArrowLeft", "ArrowRight"},
		},
	),
)

// LightboxConfig configures a Lightbox.
type LightboxConfig struct {
	// Name is the unique widget name (required) used as the
	// preset.Modal name. Page-unique. Any element with
	// data-fui-open="<this Name>" opens the overlay.
	Name string
	// Label is the accessible name for the open modal. Defaults to
	// "Image viewer" (i18nui.KeyLightboxLabel through the strings
	// bridge).
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
	// (the viewer label, Prev/Next nav and Download aria-labels) through
	// the strings bridge. When nil, context.Background() is used and
	// English fallbacks are returned, preserving today's behaviour.
	Ctx context.Context

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) onto the lightbox's own root
	// (the fui-lightbox viewer panel; the modal chrome is widget
	// machinery, and triggers are caller-owned elements). Keys the
	// component owns are dropped: class, id, data-fui-* (the viewer and
	// nav wiring) and data-hui-* (the headless anatomy's hooks).
	ExtraAttrs html.Attrs
}

// Lightbox returns a *widget.Builder for the zoom-overlay modal.
// Mount once at app startup; trigger from anywhere via data-fui-open.
func Lightbox(cfg LightboxConfig) *widget.Builder {
	if cfg.Name == "" {
		panic("ui: Lightbox requires Name")
	}

	slot := &lightboxSlot{
		name:          cfg.Name,
		label:         cfg.Label,
		navArrows:     cfg.NavArrows,
		showCaption:   cfg.ShowCaption,
		allowDownload: cfg.AllowDownload,
		ctx:           cfg.Ctx,
		extraAttrs:    html.SafeExtraAttrs(cfg.ExtraAttrs),
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
	ctx        context.Context
	extraAttrs html.Attrs
}

// lightboxClasses dresses headless.LightboxViewer's parts in this
// package's own vocabulary. The pinch-zoom module and the stylesheet
// key off the data-fui-lightbox* attributes, never off these classes.
var lightboxClasses = headless.Classes{
	headless.PartRoot:           "fui-lightbox fui-slot-bare",
	headless.PartVisuallyHidden: "fui-visually-hidden",
	headless.PartFigure:         "fui-lightbox__figure",
	headless.PartImage:          "fui-lightbox__full",
	headless.PartCaption:        "fui-lightbox__caption",
	headless.PartToolbar:        "fui-lightbox__toolbar",
	headless.PartPrev:           "fui-lightbox__nav fui-lightbox__nav--prev",
	headless.PartNext:           "fui-lightbox__nav fui-lightbox__nav--next",
	headless.PartDownload:       "fui-lightbox__download",
}

func (s *lightboxSlot) Render() render.HTML {
	ctx := s.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return lightboxStyle.WrapHTML(headless.LightboxViewer(headless.LightboxViewerProps{
		Name:         s.name,
		Label:        s.label,
		Nav:          s.navArrows,
		Caption:      s.showCaption,
		Download:     s.allowDownload,
		PrevIcon:     lightboxChevronLeft,
		NextIcon:     lightboxChevronRight,
		DownloadIcon: lightboxDownloadIcon,
		Wiring:       headless.LightboxWiring{Viewer: s.name, Nav: s.navArrows},
		ExtraAttrs:   s.extraAttrs,
		Strings:      StringsFor(ctx),
	}, lightboxClasses))
}

var _ component.Component = (*lightboxSlot)(nil)

const lightboxChevronLeft render.HTML = `<svg width="22" height="22" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M15 18l-6-6 6-6" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`
const lightboxChevronRight render.HTML = `<svg width="22" height="22" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M9 6l6 6-6 6" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`
const lightboxDownloadIcon render.HTML = `<svg width="22" height="22" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M12 3v12m0 0l-4-4m4 4l4-4M5 21h14" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`

var lightboxStyle = registry.RegisterStyle("ui-lightbox", lightboxCSS)

func lightboxCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-lightbox"] {
  display: grid;
  gap: var(--spacing-md, 8px);
  place-items: center;
  inline-size: min(90vw, 1200px);
}
[data-fui-comp="ui-lightbox"] .fui-lightbox__figure {
  margin: 0;
  display: grid;
  gap: var(--spacing-sm, 4px);
  place-items: center;
}
[data-fui-comp="ui-lightbox"] .fui-lightbox__full {
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
[data-fui-comp="ui-lightbox"] .fui-lightbox__full[data-fui-zoomed] {
  cursor: grab;
}
[data-fui-comp="ui-lightbox"] .fui-lightbox__full[data-fui-zoomed]:active {
  cursor: grabbing;
}
[data-fui-comp="ui-lightbox"] .fui-lightbox__caption {
  margin: 0;
  font-size: var(--text-sm, 0.875rem);
  text-align: center;
  color: var(--color-text-muted, #52525B);
  max-inline-size: 60ch;
}
[data-fui-comp="ui-lightbox"] .fui-lightbox__caption:empty { display: none; }

[data-fui-comp="ui-lightbox"] .fui-lightbox__toolbar {
  display: flex;
  gap: var(--spacing-sm, 4px);
  align-items: center;
  justify-content: center;
}
[data-fui-comp="ui-lightbox"] .fui-lightbox__nav,
[data-fui-comp="ui-lightbox"] .fui-lightbox__download {
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
[data-fui-comp="ui-lightbox"] .fui-lightbox__nav:hover,
[data-fui-comp="ui-lightbox"] .fui-lightbox__download:hover {
  background: var(--color-border, #E4E4E7);
}
[data-fui-comp="ui-lightbox"] .fui-lightbox__nav:focus-visible,
[data-fui-comp="ui-lightbox"] .fui-lightbox__download:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 2px;
}`
}
