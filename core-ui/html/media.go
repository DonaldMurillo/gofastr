package html

import (
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ImageConfig configures a void <img> element.
// Required: Src and Alt (empty Alt = decorative, gets role="presentation").
type ImageConfig struct {
	Src        string // required
	Alt        string // required (empty = decorative image)
	Class      string
	ID         string
	ExtraAttrs Attrs
}

// AudioConfig configures an <audio> element. No required fields.
type AudioConfig struct {
	Class      string
	ID         string
	ExtraAttrs Attrs
}

// VideoConfig configures a <video> element. No required fields.
type VideoConfig struct {
	Class      string
	ID         string
	ExtraAttrs Attrs
}

// SourceConfig configures a void <source> element.
// Required: Src and Type.
type SourceConfig struct {
	Src        string // required
	Type       string // required: MIME type
	Class      string
	ID         string
	ExtraAttrs Attrs
}

// Image produces a void <img> element.
// Required: Src and Alt. Empty Alt marks the image as decorative
// (role="presentation" is added automatically).
func Image(cfg ImageConfig) render.HTML {
	if cfg.Src == "" {
		panic("html: Image requires Src")
	}
	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)
	// ImageSource rather than Resource: an inline raster data: URI is a
	// legitimate <img src> (LQIP placeholders, generated icons), while the
	// sinks that must keep rejecting data: — <script src>, <link href> —
	// stay on Resource.
	attrs = setURLAttr(attrs, "src", cfg.Src, urlsafe.ImageSource)
	attrs = setAttr(attrs, "alt", cfg.Alt)
	if cfg.Alt == "" {
		if _, ok := attrs["role"]; !ok {
			setAttr(attrs, "role", "presentation")
		}
	}
	return render.VoidTag("img", attrs)
}

// Audio produces an <audio> element for sound content.
func Audio(cfg AudioConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)
	return render.Tag("audio", attrs, children...)
}

// Video produces a <video> element for video content.
func Video(cfg VideoConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)
	return render.Tag("video", attrs, children...)
}

// Source produces a void <source> element for use inside <audio> or <video>.
// Required: Src and Type.
func Source(cfg SourceConfig) render.HTML {
	if cfg.Src == "" {
		panic("html: Source requires Src")
	}
	if cfg.Type == "" {
		panic("html: Source requires Type")
	}
	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)
	attrs = setURLAttr(attrs, "src", cfg.Src, urlsafe.Resource)
	attrs = setAttr(attrs, "type", cfg.Type)
	return render.VoidTag("source", attrs)
}

// HR produces a void <hr> element (thematic break).
func HR(cfg TextConfig) render.HTML {
	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)
	return render.VoidTag("hr", attrs)
}

// BR produces a void <br> element (line break).
func BR() render.HTML {
	return render.VoidTag("br", nil)
}

// Meta produces a void <meta> element with name and content attributes.
func Meta(name, content string) render.HTML {
	return render.VoidTag("meta", Attrs{
		"name":    name,
		"content": content,
	})
}

// StyleSheet produces a <link> element with rel="stylesheet".
func StyleSheet(href string) render.HTML {
	return render.VoidTag("link", setURLAttr(Attrs{"rel": "stylesheet"}, "href", href, urlsafe.Resource))
}

// Script produces a <script> element with the given src.
func Script(src string) render.HTML {
	return render.Tag("script", setURLAttr(nil, "src", src, urlsafe.Resource))
}
