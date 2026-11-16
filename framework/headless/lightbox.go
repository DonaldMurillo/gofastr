package headless

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

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

// LightboxWiring renders the framework module's data-fui-lightbox* family in place of the data-hui-* hooks.
type LightboxWiring struct {
	Viewer string // the lightbox widget's name, on the root and the nav buttons
	Nav    bool   // data-fui-lightbox-nav="true": prev/next and the arrows step the group
}

// LightboxViewerProps configure the viewer.
type LightboxViewerProps struct {
	Name         string      // required: the hooks' value and the title id "<Name>-title"
	Label        string      // the open viewer's name; Strings.LightboxViewerLabel by default
	Nav          bool        // the prev/next buttons
	Caption      bool        // a figcaption bound to the caption signal
	Download     bool        // an anchor whose href is bound to the src signal
	PrevIcon     render.HTML // decorative; the names come from Strings
	NextIcon     render.HTML
	DownloadIcon render.HTML
	Wiring       LightboxWiring // renders the framework module's attributes instead of the hooks

	ID         string
	ExtraAttrs html.Attrs

	Parts   Parts    // nothing is fillable
	Strings *Strings // nil means the English defaults
}

// LightboxViewer renders the open viewer's body, bound to the widget's alt, src and caption signals.
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
		//gofastr:allow(layerprefix) Wiring renders the framework lightbox module's family on purpose; the hui family is the unwired render
		own["data-fui-lightbox"] = p.Wiring.Viewer
		if p.Wiring.Nav {
			//gofastr:allow(layerprefix) Wiring renders the framework lightbox module's nav key on purpose
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
		//gofastr:allow(layerprefix) Wiring renders the framework lightbox module's zoom target on purpose
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
			//gofastr:allow(layerprefix) Wiring renders the framework lightbox module's nav buttons on purpose
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
		if p.PrevIcon == "" {
			prev = Internal(prev)
		}
		if p.NextIcon == "" {
			next = Internal(next)
		}
		tools = append(tools,
			b.El("button", PartPrev, prev, p.PrevIcon),
			b.El("button", PartNext, next, p.NextIcon))
	}
	if p.Download {
		dl := Mark(Attrs(map[string]string{"aria-label": w.LightboxDownload}), "download")
		dlAttrs := Merge(dl, Bind{Signal: "src", Mode: "attr", Attr: "href"}.attrs())
		if p.DownloadIcon == "" {
			dlAttrs = Internal(dlAttrs)
		}
		tools = append(tools, b.El("a", PartDownload, dlAttrs, p.DownloadIcon))
	}

	kids := []render.HTML{b.El("figure", PartFigure, Internal(nil), figKids...)}
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
					//gofastr:allow(layerprefix) the panic names the family Wiring renders so the caller knows which spelling they got
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
