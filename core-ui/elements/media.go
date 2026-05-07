package elements

import "github.com/gofastr/gofastr/core/render"

// Image produces a void <img> element. The alt parameter is always required;
// pass an empty string for decorative images (role="presentation" is added
// automatically in that case).
func Image(src, alt string, attrs Attrs) render.HTML {
	if attrs == nil {
		attrs = make(Attrs, 2)
	}
	attrs["src"] = src
	attrs["alt"] = alt
	if alt == "" {
		if _, ok := attrs["role"]; !ok {
			attrs["role"] = "presentation"
		}
	}
	return render.VoidTag("img", attrs)
}

// Audio produces an <audio> element for sound content.
func Audio(attrs Attrs, children ...render.HTML) render.HTML {
	return render.Tag("audio", attrs, children...)
}

// Video produces a <video> element for video content.
func Video(attrs Attrs, children ...render.HTML) render.HTML {
	return render.Tag("video", attrs, children...)
}

// Source produces a void <source> element for use inside <audio> or <video>.
func Source(src, mediaType string, attrs Attrs) render.HTML {
	if attrs == nil {
		attrs = make(Attrs, 2)
	}
	attrs["src"] = src
	attrs["type"] = mediaType
	return render.VoidTag("source", attrs)
}

// HR produces a void <hr> element (thematic break).
func HR(attrs Attrs) render.HTML {
	return render.VoidTag("hr", attrs)
}

// BR produces a void <br> element (line break).
func BR(attrs Attrs) render.HTML {
	return render.VoidTag("br", attrs)
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
	return render.VoidTag("link", Attrs{
		"rel":  "stylesheet",
		"href": href,
	})
}

// Script produces a <script> element with the given src.
func Script(src string) render.HTML {
	return render.Tag("script", Attrs{"src": src})
}
