package headless

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// The lightbox viewer: the anatomy inside a modal that shows one image
// at full size, with the optional parts a gallery walks — the prev and
// next buttons, the caption, the download anchor. The modal itself
// (open and close, ESC, backdrop, focus trap, the deeplink→signal
// pipeline) is widget machinery and lives below this package; this
// component is the body a widget mounts.
//
// The viewer publishes its structural lookups as data-hui-* hooks, the
// way every component here does, so a host writing its own viewer
// behaviour binds the same markup its class map dresses. The two
// vocabularies are mutually exclusive: zero Wiring renders the hui
// family, and a set Wiring renders the data-fui-lightbox* family for
// the framework's own module — framework/ui's registered lightbox
// behaviour, which binds data-fui-* hooks only — and suppresses the
// hui twins, because a host module binding those beside the
// framework's would double-bind the gallery the shipped module steps
// and fight its pinch-zoom on the same image.

// Lightbox viewer parts.
const (
	PartFigure   Part = "figure"
	PartImage    Part = "image"
	PartCaption  Part = "caption"
	PartToolbar  Part = "toolbar"
	PartPrev     Part = "prev"
	PartNext     Part = "next"
	PartDownload Part = "download"
)

// LightboxWiring is the framework binder's spelling of the viewer's
// own facts. The data-hui-* hooks are this package's contract with any
// runtime a host chooses, and they render exactly when Wiring is zero:
// a host shipping its own viewer module against the hooks leaves
// Wiring zero and none of the framework's attributes render. A set
// Wiring renders the data-fui-lightbox* family instead — for
// framework/ui's lightbox module, which binds data-fui-* hooks only —
// and suppresses the hui twins: the two vocabularies name the same
// facts, and a viewer that rendered both would invite a host module to
// double-bind the gallery the framework module steps.
type LightboxWiring struct {
	// Viewer is the data-fui-lightbox value: the lightbox widget's
	// name, carried by the root and the nav buttons, and (as a bare
	// marker) by the image the module's pinch-zoom owns.
	Viewer string
	// Nav renders data-fui-lightbox-nav="true" beside the hook that
	// states the same fact: the opt-in that makes prev/next and the
	// arrow keys step the gallery group.
	Nav bool
}

// LightboxViewerProps configure the viewer.
type LightboxViewerProps struct {
	// Name is the lightbox's identity (required): the value its hooks
	// carry, and the source of the title span's id (<Name>-title),
	// which the surrounding modal's aria-labelledby points at.
	Name string
	// Label is the accessible name of the open viewer. Empty means
	// Strings.LightboxViewerLabel.
	Label string
	// Nav renders the prev/next buttons.
	Nav bool
	// Caption renders a <figcaption> bound to the viewer's caption
	// signal.
	Caption bool
	// Download renders an anchor that saves the image being viewed,
	// its href bound to the viewer's src signal.
	Download bool
	// PrevIcon, NextIcon and DownloadIcon are the buttons' glyphs.
	// Decorative; the accessible names come from Strings.
	PrevIcon     render.HTML
	NextIcon     render.HTML
	DownloadIcon render.HTML
	// Wiring renders the framework module's data-fui-lightbox*
	// attributes IN PLACE OF the hooks. See LightboxWiring.
	Wiring LightboxWiring

	ID         string
	ExtraAttrs html.Attrs

	// Parts: attrs on every part. Nothing here is fillable — the
	// caption and the image are signal-bound, and the buttons are the
	// nav contract; a slot would fight the binding it replaced.
	Parts Parts
	// Strings are the strings this component says. Nil means the
	// English defaults; a layer above sets them from the request's
	// language.
	Strings *Strings
}

// LightboxViewer renders the open viewer's body.
//
// The three rendered signals — alt on the visually-hidden title, src on
// the image's src attribute and (for the download anchor) its href,
// caption on the figcaption — are the lightbox widget contract's own
// names: the deeplink a trigger carries (src=…&alt=…&caption=…) lands
// in them, and a viewer that renamed them would render a widget that
// never updates. The group parameter is not rendered: the behaviour
// module reads it from the signal store, not from markup.
func LightboxViewer(p LightboxViewerProps, s Classes) render.HTML {
	if p.Name == "" {
		panic("headless: LightboxViewer requires Name — it is the value the hooks carry and the title id is built from")
	}
	if p.Wiring.Nav && p.Wiring.Viewer == "" {
		panic("headless: LightboxViewer Wiring.Nav without Wiring.Viewer — the nav opt-in is the framework binder's attribute; name the viewer it belongs to")
	}
	w := p.Strings.Resolve()
	b := p.Parts.Box(s)
	label := p.Label
	if label == "" {
		label = w.LightboxViewerLabel
	}

	wired := p.Wiring.Viewer != ""
	own := Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{
		"id": p.ID,
	}))
	if wired {
		own["data-fui-lightbox"] = p.Wiring.Viewer
		if p.Wiring.Nav {
			own["data-fui-lightbox-nav"] = "true"
		}
	} else {
		own["data-hui-lightbox"] = p.Name
		if p.Nav {
			own["data-hui-lightbox-nav"] = "true"
		}
	}

	figKids := []render.HTML{
		b.El("span", PartVisuallyHidden,
			Merge(Attrs(map[string]string{"id": p.Name + "-title"}),
				Bind{Signal: "alt"}.attrs()),
			render.Text(label)),
	}
	zoomAttr := "data-hui-lightbox-image"
	if wired {
		zoomAttr = "data-fui-lightbox-image"
	}
	img := Mark(Attrs(map[string]string{"alt": ""}), zoomAttr)
	figKids = append(figKids,
		b.El("img", PartImage, Merge(img, Bind{Signal: "src", Mode: "attr", Attr: "src"}.attrs())))
	if p.Caption {
		figKids = append(figKids,
			b.El("figcaption", PartCaption, Bind{Signal: "caption"}.attrs()))
	}

	tools := []render.HTML{}
	if p.Nav {
		prevAttr, nextAttr := "data-hui-lightbox-prev", "data-hui-lightbox-next"
		prevName, nextName := p.Name, p.Name
		if wired {
			prevAttr, nextAttr = "data-fui-lightbox-prev", "data-fui-lightbox-next"
			prevName, nextName = p.Wiring.Viewer, p.Wiring.Viewer
		}
		prev := Attrs(map[string]string{
			"type":       "button",
			"aria-label": w.LightboxPrevious,
			prevAttr:     prevName,
		})
		next := Attrs(map[string]string{
			"type":       "button",
			"aria-label": w.LightboxNext,
			nextAttr:     nextName,
		})
		tools = append(tools,
			b.El("button", PartPrev, prev, p.PrevIcon),
			b.El("button", PartNext, next, p.NextIcon))
	}
	if p.Download {
		dl := Mark(Attrs(map[string]string{"aria-label": w.LightboxDownload}), "download")
		tools = append(tools,
			b.El("a", PartDownload, Merge(dl, Bind{Signal: "src", Mode: "attr", Attr: "href"}.attrs()), p.DownloadIcon))
	}

	kids := []render.HTML{b.El("figure", PartFigure, nil, figKids...)}
	if len(tools) > 0 {
		kids = append(kids, b.El("div", PartToolbar, nil, tools...))
	}
	return b.El("div", PartRoot, own, kids...)
}

func init() {
	Register(Spec{
		Name: "LightboxViewer",
		Anatomy: []Part{PartRoot, PartVisuallyHidden, PartFigure, PartImage, PartCaption,
			PartToolbar, PartPrev, PartNext, PartDownload},
		Hooks: []string{
			"data-hui-lightbox",
			"data-hui-lightbox-nav",
			"data-hui-lightbox-image",
			"data-hui-lightbox-prev",
			"data-hui-lightbox-next",
		},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return LightboxViewer(LightboxViewerProps{
				Name: "specimen", Nav: true, Caption: true, Download: true,
				PrevIcon: SpecimenGlyph, NextIcon: SpecimenGlyph, DownloadIcon: SpecimenGlyph,
				Wiring: LightboxWiring{Viewer: "specimen", Nav: true},
				Parts:  parts,
			}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "full",
				Why: "the working end of a gallery: nav, caption and download all on, so every hook, " +
					"binding and button a module or a signal could lose is on the page",
				HTML: LightboxViewer(LightboxViewerProps{
					Name: "photos", Nav: true, Caption: true, Download: true,
					PrevIcon: SpecimenGlyph, NextIcon: SpecimenGlyph, DownloadIcon: SpecimenGlyph,
				}, s),
			}, {
				Name: "bare",
				Why: "the minimum a viewer is: a named image and its accessible name, nothing optional — " +
					"a markdown figure or a single inline photo needs no nav and saves the buttons",
				HTML: LightboxViewer(LightboxViewerProps{Name: "figure"}, s),
			}, {
				Name: "framework wiring",
				Why: "the binder's spellings instead of the hooks: the same identity, nav opt-in, buttons and " +
					"zoom target as data-fui-lightbox*, for the framework module a styled lightbox ships — the hui " +
					"twins are suppressed here; they render exactly when the host's own module is the intended binder",
				HTML: LightboxViewer(LightboxViewerProps{
					Name: "photos", Nav: true, Download: true,
					Wiring: LightboxWiring{Viewer: "photos", Nav: true},
				}, s),
			}}
		},
	})
}
