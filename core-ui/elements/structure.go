package elements

import "github.com/gofastr/gofastr/core/render"

// Div produces a <div> element.
func Div(cfg DivConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	if cfg.Role != "" {
		setAttr(attrs, "role", cfg.Role)
	}
	if cfg.AriaLabel != "" {
		setAttr(attrs, "aria-label", cfg.AriaLabel)
	}
	return render.Tag("div", attrs, children...)
}

// Article produces an <article> element representing a self-contained
// composition in a page.
func Article(cfg ArticleConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	return render.Tag("article", attrs, children...)
}

// Section produces a <section> element.
// Required: Label or LabelledBy. Automatically adds role="region" and the
// corresponding aria attribute.
func Section(cfg SectionConfig, children ...render.HTML) render.HTML {
	if cfg.Label == "" && cfg.LabelledBy == "" {
		panic("elements: Section requires Label or LabelledBy")
	}
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	if cfg.Label != "" {
		setAttr(attrs, "aria-label", cfg.Label)
	}
	if cfg.LabelledBy != "" {
		setAttr(attrs, "aria-labelledby", cfg.LabelledBy)
	}
	setAttr(attrs, "role", RoleRegion)
	return render.Tag("section", attrs, children...)
}

// Main produces a <main> element with role="main" and id="main-content"
// for skip-navigation links. If cfg.ID is set, it overrides the default id.
func Main(cfg MainConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	setAttr(attrs, "role", RoleMain)
	if _, ok := attrs["id"]; !ok {
		attrs["id"] = "main-content"
	}
	return render.Tag("main", attrs, children...)
}

// Header produces a <header> element. It adds role="banner" to signal
// a top-level banner landmark.
func Header(cfg HeaderConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	setAttr(attrs, "role", RoleBanner)
	return render.Tag("header", attrs, children...)
}

// Footer produces a <footer> element. It adds role="contentinfo" to signal
// a top-level contentinfo landmark.
func Footer(cfg FooterConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	setAttr(attrs, "role", RoleContentinfo)
	return render.Tag("footer", attrs, children...)
}

// Nav produces a <nav> element with role="navigation".
// Required: Label or LabelledBy.
func Nav(cfg NavConfig, children ...render.HTML) render.HTML {
	if cfg.Label == "" && cfg.LabelledBy == "" {
		panic("elements: Nav requires Label or LabelledBy")
	}
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	if cfg.Label != "" {
		setAttr(attrs, "aria-label", cfg.Label)
	}
	if cfg.LabelledBy != "" {
		setAttr(attrs, "aria-labelledby", cfg.LabelledBy)
	}
	setAttr(attrs, "role", RoleNavigation)
	return render.Tag("nav", attrs, children...)
}

// Aside produces an <aside> element with role="complementary".
// Required: Label or LabelledBy.
func Aside(cfg AsideConfig, children ...render.HTML) render.HTML {
	if cfg.Label == "" && cfg.LabelledBy == "" {
		panic("elements: Aside requires Label or LabelledBy")
	}
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	if cfg.Label != "" {
		setAttr(attrs, "aria-label", cfg.Label)
	}
	if cfg.LabelledBy != "" {
		setAttr(attrs, "aria-labelledby", cfg.LabelledBy)
	}
	setAttr(attrs, "role", RoleComplementary)
	return render.Tag("aside", attrs, children...)
}

// Figure produces a <figure> element for self-contained content
// referenced from the main flow.
func Figure(cfg FigureConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	return render.Tag("figure", attrs, children...)
}

// FigCaption produces a <figcaption> element for a figure caption.
func FigCaption(cfg FigCaptionConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	return render.Tag("figcaption", attrs, children...)
}

// Details produces a <details> element for a disclosure widget.
func Details(cfg DetailsConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	return render.Tag("details", attrs, children...)
}

// Summary produces a <summary> element for the summary/caption
// of a details element.
func Summary(cfg SummaryConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	return render.Tag("summary", attrs, children...)
}

// Group produces a <div> element with the given ARIA role.
// Required: Role.
func Group(cfg GroupConfig, children ...render.HTML) render.HTML {
	if cfg.Role == "" {
		panic("elements: Group requires Role")
	}
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	setAttr(attrs, "role", cfg.Role)
	if cfg.AriaLabel != "" {
		setAttr(attrs, "aria-label", cfg.AriaLabel)
	}
	return render.Tag("div", attrs, children...)
}
