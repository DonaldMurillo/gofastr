package ui

import (
	"net/url"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── Lightbox ───────────────────────────────────────────────────────
//
// Click-to-zoom image gallery built on top of preset.Modal. No new
// runtime module: the framework already gives us
//
//   - ESC + click-outside dismiss (preset.Modal)
//   - Focus trap + return-focus (preset.Modal)
//   - URL deeplink mirroring (DeepLinkParam → signal)
//   - Signal → attribute binding (data-fui-signal-mode="attr")
//
// Composition flow:
//   1. Render N thumbnail anchors, each carrying
//      data-fui-open="<name>" + data-fui-deeplink="src=<full>&alt=<text>".
//      The bare <a href=…> is the no-JS fallback (opens full-size in a
//      new tab).
//   2. Return a *widget.Builder for a preset.Modal named "<name>" with
//      DeepLinkParam("src") + DeepLinkParam("alt") and Signal
//      definitions so click → URL/state mirror → signals update.
//   3. The modal slot renders <img data-fui-signal="src"
//      data-fui-signal-mode="attr" data-fui-signal-attr="src">; the
//      runtime mirrors the chosen image's URL into the src attribute.

// LightboxImage is one entry in a gallery.
type LightboxImage struct {
	// Src is the full-resolution image URL (required).
	Src string
	// Thumb is the thumbnail URL. Defaults to Src.
	Thumb string
	// Alt is the accessible image description (required).
	Alt string
	// Width / Height of the thumbnail in CSS pixels. Default 120×120.
	Width  int
	Height int
}

// LightboxConfig configures a Lightbox gallery.
type LightboxConfig struct {
	// Name is the unique gallery name (required). Used as the widget
	// name for the paired preset.Modal.
	Name string
	// Label is the accessible label for the thumbnail strip.
	// Defaults to "Image gallery".
	Label string
	// ModalLabel is the accessible label for the open lightbox modal.
	// Defaults to Label.
	ModalLabel string
	// Pages, when non-empty, scopes the modal mount to those routes
	// (passed through to preset.Modal's .Pages). Default: site-wide.
	Pages []string
	// Images are the entries (≥1).
	Images []LightboxImage
	ID     string
	Class  string
	Attrs  html.Attrs
}

// Lightbox returns the gallery HTML AND a *widget.Builder for the
// modal it pairs with. Mount the modal once at app startup:
//
//	thumbs, modal := ui.Lightbox(ui.LightboxConfig{...})
//	widget.Mount(r, modal.Build())
//
// Then render `thumbs` anywhere on the page.
func Lightbox(cfg LightboxConfig) (render.HTML, *widget.Builder) {
	if cfg.Name == "" {
		panic("ui: Lightbox requires Name")
	}
	if len(cfg.Images) == 0 {
		panic("ui: Lightbox requires ≥1 Image")
	}
	label := cfg.Label
	if label == "" {
		label = "Image gallery"
	}
	modalLabel := cfg.ModalLabel
	if modalLabel == "" {
		modalLabel = label
	}

	cls := "ui-lightbox"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	// Use native <ul> / <li> instead of role=list / role=listitem —
	// axe rejects role=listitem on an <a> (aria-allowed-role), and
	// native semantics also win for no-JS / SR fallback.
	attrs := html.Attrs{
		"class":      cls,
		"aria-label": label,
	}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	for k, v := range cfg.Attrs {
		attrs[k] = v
	}

	items := make([]render.HTML, 0, len(cfg.Images))
	for _, img := range cfg.Images {
		if img.Src == "" {
			panic("ui: Lightbox image requires Src")
		}
		if img.Alt == "" {
			panic("ui: Lightbox image requires Alt (a meaningful description; the open zoomed image inherits this as its accessible name)")
		}
		thumb := img.Thumb
		if thumb == "" {
			thumb = img.Src
		}
		w := img.Width
		if w == 0 {
			w = 120
		}
		h := img.Height
		if h == 0 {
			h = 120
		}
		// data-fui-deeplink mirrors src + alt onto the modal's signals
		// when this anchor opens the modal. URL-encode the values so
		// embedded "&" / "=" don't break the deeplink parser.
		dl := url.Values{}
		dl.Set("src", img.Src)
		dl.Set("alt", img.Alt)

		items = append(items, render.Tag("li", map[string]string{"class": "ui-lightbox__row"},
			render.Tag("a", map[string]string{
				"href":              img.Src, // no-JS fallback
				"target":            "_blank",
				"rel":               "noopener",
				"class":             "ui-lightbox__item",
				"aria-label":        img.Alt,
				"data-fui-open":     cfg.Name,
				"data-fui-deeplink": dl.Encode(),
			},
				render.Tag("img", map[string]string{
					"src":     thumb,
					"alt":     img.Alt,
					"width":   strconv.Itoa(w),
					"height":  strconv.Itoa(h),
					"loading": "lazy",
					"class":   "ui-lightbox__thumb",
				}),
			),
		))
	}

	// Native <ul> root keeps role=list implicit; per-item <li> wraps
	// each <a>. List padding/markers are reset via the registered CSS.
	thumbs := lightboxStyle.WrapHTML(render.Tag("ul", attrs, items...))

	// Compose the modal. Pages() scopes the mount; the modal carries
	// a single <img> bound to the "src" signal via
	// data-fui-signal-mode="attr" data-fui-signal-attr="src".
	slot := &lightboxSlot{name: cfg.Name, label: modalLabel}
	titleID := cfg.Name + "-title"
	mb := preset.Modal(cfg.Name).
		Hidden().
		LabelledBy(titleID).
		DeepLinkParam("src").
		DeepLinkParam("alt").
		Signal("src", widget.SignalFunc(func() (any, error) { return "", nil })).
		Signal("alt", widget.SignalFunc(func() (any, error) { return "", nil })).
		Slot("body", slot)
	if len(cfg.Pages) > 0 {
		mb = mb.Pages(cfg.Pages...)
	}
	return thumbs, mb
}

// lightboxSlot renders the open-modal contents: a centered <img>
// bound to the "src" signal + an SR-only title bound to "alt".
type lightboxSlot struct {
	name  string
	label string
}

func (s *lightboxSlot) Render() render.HTML {
	return render.Tag("div", map[string]string{"class": "ui-lightbox__viewer"},
		// SR-only title — Modal.LabelledBy() points here. The runtime
		// mirrors the "alt" signal into this element's textContent on
		// each open, so the announced label matches the displayed image.
		html.Span(html.TextConfig{
			ID:    s.name + "-title",
			Class: "ui-visually-hidden",
			Attrs: html.Attrs{"data-fui-signal": "alt"},
		}, render.Text(s.label)),
		// Image. data-fui-signal-mode="attr" + data-fui-signal-attr="src"
		// → runtime writes the signal value into the img's src attr.
		render.Tag("img", map[string]string{
			"class":                "ui-lightbox__full",
			"alt":                  "",
			"data-fui-signal":      "src",
			"data-fui-signal-mode": "attr",
			"data-fui-signal-attr": "src",
		}),
	)
}

var _ component.Component = (*lightboxSlot)(nil)

var lightboxStyle = registry.RegisterStyle("ui-lightbox", lightboxCSS)

func lightboxCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-lightbox"] {
  display: flex;
  flex-wrap: wrap;
  gap: var(--spacing-sm, 8px);
  list-style: none;
  margin: 0;
  padding: 0;
}
[data-fui-comp="ui-lightbox"] .ui-lightbox__row {
  margin: 0;
  padding: 0;
}
[data-fui-comp="ui-lightbox"] .ui-lightbox__item {
  display: inline-block;
  border-radius: var(--radii-md, 8px);
  overflow: hidden;
  border: 1px solid var(--color-border, #E4E4E7);
  cursor: zoom-in;
  background: var(--color-surface, #FFFFFF);
  transition: border-color 120ms ease;
}
[data-fui-comp="ui-lightbox"] .ui-lightbox__item:hover {
  border-color: var(--color-primary, #4F46E5);
}
[data-fui-comp="ui-lightbox"] .ui-lightbox__item:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 2px;
}
[data-fui-comp="ui-lightbox"] .ui-lightbox__thumb {
  display: block;
  object-fit: cover;
}

/* Inside the open modal — image fills the dialog up to viewport
   bounds. The modal preset already handles backdrop + centering. */
.ui-lightbox__viewer {
  display: grid;
  place-items: center;
  inline-size: min(90vw, 1200px);
  block-size: min(85vh, 90vh);
}
.ui-lightbox__full {
  display: block;
  max-inline-size: 100%;
  max-block-size: 100%;
  object-fit: contain;
  border-radius: var(--radii-md, 8px);
}`
}
