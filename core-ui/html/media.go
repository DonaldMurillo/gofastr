package html

import (
	"strings"

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
	// sinks that must keep rejecting data:, <script src>, <link href>,
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

// IFrameConfig configures an <iframe> element.
// Required: Src and Title. An iframe with no title is an unnamed region
// to a screen reader.
type IFrameConfig struct {
	Src   string // required
	Title string // required
	// Sandbox is the sandbox attribute's token list ("allow-popups
	// allow-forms"). The attribute is always rendered: an empty Sandbox
	// applies every restriction (no scripts, no forms, a unique origin),
	// the right default for content the page did not write. Unsandboxed
	// drops the attribute, for a frame of the app's own trusted pages.
	Sandbox     string
	Unsandboxed bool
	Class       string
	ID          string
	// ExtraAttrs forwards additional attributes. src, srcdoc, sandbox and
	// title belong to the fields above and are dropped in every case
	// spelling, so ExtraAttrs cannot lift the sandbox or swap the URL.
	ExtraAttrs Attrs
}

// IFrame produces an <iframe> element, sandboxed unless the config opts
// out. Src passes the Resource URL policy; a refused URL omits src.
func IFrame(cfg IFrameConfig) render.HTML {
	if cfg.Src == "" {
		panic("html: IFrame requires Src")
	}
	if cfg.Title == "" {
		panic("html: IFrame requires Title")
	}
	attrs := make(Attrs, len(cfg.ExtraAttrs)+3)
	for k, v := range cfg.ExtraAttrs {
		switch strings.ToLower(k) {
		case "src", "srcdoc", "sandbox", "title":
			continue
		}
		attrs[k] = v
	}
	attrs = buildAttrs(attrs, cfg.ID, cfg.Class)
	attrs = setURLAttr(attrs, "src", cfg.Src, urlsafe.Resource)
	attrs = setAttr(attrs, "title", cfg.Title)
	if !cfg.Unsandboxed {
		attrs = setAttr(attrs, "sandbox", cfg.Sandbox)
	}
	return render.Tag("iframe", attrs)
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
