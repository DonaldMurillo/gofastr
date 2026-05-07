package elements

import "github.com/gofastr/gofastr/core/render"

// Div produces a <div> element.
func Div(attrs Attrs, children ...render.HTML) render.HTML {
	return render.Tag("div", attrs, children...)
}

// Article produces an <article> element representing a self-contained
// composition in a page.
func Article(attrs Attrs, children ...render.HTML) render.HTML {
	return render.Tag("article", attrs, children...)
}

// Section produces a <section> element. If attrs contains an "aria-label"
// or "aria-labelledby" key, the role="region" attribute is added automatically.
func Section(attrs Attrs, children ...render.HTML) render.HTML {
	if attrs != nil {
		if _, ok := attrs["aria-label"]; ok {
			attrs["role"] = RoleRegion
		} else if _, ok := attrs["aria-labelledby"]; ok {
			attrs["role"] = RoleRegion
		}
	}
	return render.Tag("section", attrs, children...)
}

// Main produces a <main> element with role="main" and id="main-content"
// for skip-navigation links. If attrs already provides an id, it is preserved.
func Main(attrs Attrs, children ...render.HTML) render.HTML {
	if attrs == nil {
		attrs = make(Attrs, 2)
	}
	if _, ok := attrs["role"]; !ok {
		attrs["role"] = RoleMain
	}
	if _, ok := attrs["id"]; !ok {
		attrs["id"] = "main-content"
	}
	return render.Tag("main", attrs, children...)
}

// Header produces a <header> element. It adds role="banner" to signal
// a top-level banner landmark.
func Header(attrs Attrs, children ...render.HTML) render.HTML {
	if attrs == nil {
		attrs = make(Attrs, 1)
	}
	if _, ok := attrs["role"]; !ok {
		attrs["role"] = RoleBanner
	}
	return render.Tag("header", attrs, children...)
}

// Footer produces a <footer> element. It adds role="contentinfo" to signal
// a top-level contentinfo landmark.
func Footer(attrs Attrs, children ...render.HTML) render.HTML {
	if attrs == nil {
		attrs = make(Attrs, 1)
	}
	if _, ok := attrs["role"]; !ok {
		attrs["role"] = RoleContentinfo
	}
	return render.Tag("footer", attrs, children...)
}

// Nav produces a <nav> element with role="navigation".
// Callers should provide an aria-label via attrs.
func Nav(attrs Attrs, children ...render.HTML) render.HTML {
	if attrs == nil {
		attrs = make(Attrs, 1)
	}
	if _, ok := attrs["role"]; !ok {
		attrs["role"] = RoleNavigation
	}
	return render.Tag("nav", attrs, children...)
}

// Aside produces an <aside> element with role="complementary".
func Aside(attrs Attrs, children ...render.HTML) render.HTML {
	if attrs == nil {
		attrs = make(Attrs, 1)
	}
	if _, ok := attrs["role"]; !ok {
		attrs["role"] = RoleComplementary
	}
	return render.Tag("aside", attrs, children...)
}

// Figure produces a <figure> element for self-contained content
// referenced from the main flow.
func Figure(attrs Attrs, children ...render.HTML) render.HTML {
	return render.Tag("figure", attrs, children...)
}

// FigCaption produces a <figcaption> element for a figure caption.
func FigCaption(attrs Attrs, children ...render.HTML) render.HTML {
	return render.Tag("figcaption", attrs, children...)
}

// Details produces a <details> element for a disclosure widget.
func Details(attrs Attrs, children ...render.HTML) render.HTML {
	return render.Tag("details", attrs, children...)
}

// Summary produces a <summary> element for the summary/caption
// of a details element.
func Summary(attrs Attrs, children ...render.HTML) render.HTML {
	return render.Tag("summary", attrs, children...)
}
