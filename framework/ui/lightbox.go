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
			Scope: "[data-cui-widget]:not([hidden]) [data-fui-lightbox]",
			Keys:  []string{"ArrowLeft", "ArrowRight"},
		},
	),
)

// LightboxConfig configures a Lightbox.
type LightboxConfig struct {
	// Name is the page-unique modal name (required) that data-cui-open names.
	Name string
	// Label names the open modal. Default "Image viewer".
	Label string
	// NavArrows adds Prev/Next buttons and arrow keys over triggers
	// sharing one data-fui-lightbox-group.
	NavArrows bool
	// ShowCaption adds a <figcaption> bound to the deeplink's caption.
	ShowCaption bool
	// AllowDownload adds a Download anchor bound to the current src.
	AllowDownload bool
	// Pages, when non-empty, scopes the modal mount to those routes.
	Pages []string
	// ExtraAttrs lands on the viewer panel; class, id, data-cui-* and data-hui-* are dropped.
	ExtraAttrs html.Attrs
}

// Lightbox returns the zoom-overlay modal, opened by any element with
// data-cui-open="<Name>" and a data-cui-deeplink of src, alt, caption
// and group. It panics without Name.
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

type lightboxSlot struct {
	name          string
	label         string
	navArrows     bool
	showCaption   bool
	allowDownload bool
	extraAttrs    html.Attrs
}

var lightboxClasses = headless.Classes{
	headless.PartRoot:           "fui-lightbox cui-slot-bare",
	headless.PartVisuallyHidden: "fui-visually-hidden",
	headless.PartFigure:         "fui-lightbox__figure",
	headless.PartImage:          "fui-lightbox__full",
	headless.PartCaption:        "fui-lightbox__caption",
	headless.PartToolbar:        "fui-lightbox__toolbar",
	headless.PartPrev:           "fui-lightbox__nav fui-lightbox__nav--prev",
	headless.PartNext:           "fui-lightbox__nav fui-lightbox__nav--next",
	headless.PartDownload:       "fui-lightbox__download",
}

// Render is RenderCtx with no reader: the English viewer.
func (s *lightboxSlot) Render() render.HTML { return s.RenderCtx(context.Background()) }

// RenderCtx draws the viewer with its words in the request's locale.
func (s *lightboxSlot) RenderCtx(ctx context.Context) render.HTML {
	if ctx == nil {
		ctx = context.Background()
	}
	return lightboxStyle.WrapHTML(headless.LightboxViewer(headless.LightboxViewerProps{
		Name:         s.name,
		Label:        s.label,
		Nav:          s.navArrows,
		Caption:      s.showCaption,
		Download:     s.allowDownload,
		PrevIcon:     Icon("chevron-left", IconConfig{Size: "22"}),
		NextIcon:     Icon("chevron-right", IconConfig{Size: "22"}),
		DownloadIcon: Icon("download", IconConfig{Size: "22"}),
		Wiring:       headless.LightboxWiring{Viewer: s.name, Nav: s.navArrows},
		ExtraAttrs:   s.extraAttrs,
		Strings:      StringsFor(ctx),
	}, lightboxClasses))
}

var _ component.ContextComponent = (*lightboxSlot)(nil)

var lightboxStyle = registry.RegisterStyle("ui-lightbox", lightboxCSS)

func lightboxCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-lightbox"] {
  display: grid;
  gap: var(--spacing-md, 8px);
  place-items: center;
  inline-size: min(90vw, var(--ui-lightbox-max-width, 1200px));
}
[data-cui-comp="ui-lightbox"] .fui-lightbox__figure {
  margin: 0;
  display: grid;
  gap: var(--spacing-sm, 4px);
  place-items: center;
}
[data-cui-comp="ui-lightbox"] .fui-lightbox__full {
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
[data-cui-comp="ui-lightbox"] .fui-lightbox__full[data-fui-zoomed] {
  cursor: grab;
}
[data-cui-comp="ui-lightbox"] .fui-lightbox__full[data-fui-zoomed]:active {
  cursor: grabbing;
}
[data-cui-comp="ui-lightbox"] .fui-lightbox__caption {
  margin: 0;
  font-size: var(--text-sm, 0.875rem);
  text-align: center;
  color: var(--color-text-muted, #52525B);
  max-inline-size: 60ch;
}
[data-cui-comp="ui-lightbox"] .fui-lightbox__caption:empty { display: none; }

[data-cui-comp="ui-lightbox"] .fui-lightbox__toolbar {
  display: flex;
  gap: var(--spacing-sm, 4px);
  align-items: center;
  justify-content: center;
}
[data-cui-comp="ui-lightbox"] .fui-lightbox__nav,
[data-cui-comp="ui-lightbox"] .fui-lightbox__download {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-block-size: var(--spacing-touch-target, 44px);
  min-inline-size: var(--spacing-touch-target, 44px);
  border-radius: var(--radii-full, 9999px);
  border: 0;
  background: var(--color-surface-soft, #F4F4F5);
  color: var(--color-text, #09090B);
  cursor: pointer;
  text-decoration: none;
}
[data-cui-comp="ui-lightbox"] .fui-lightbox__nav:hover,
[data-cui-comp="ui-lightbox"] .fui-lightbox__download:hover {
  background: var(--color-border, #E4E4E7);
}
[data-cui-comp="ui-lightbox"] .fui-lightbox__nav:focus-visible,
[data-cui-comp="ui-lightbox"] .fui-lightbox__download:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: var(--stroke-focus-offset, 2px);
}
/* Right to left the toolbar puts Prev on the right and the arrow keys
   follow it, so each chevron mirrors to point the way its button goes. */
[data-cui-comp="ui-lightbox"] .fui-lightbox__nav:dir(rtl) > svg { transform: scaleX(-1); }`
}
