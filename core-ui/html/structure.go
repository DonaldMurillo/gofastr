package html

import "github.com/gofastr/gofastr/core/render"

// DivConfig configures a <div> element. No required fields.
type DivConfig struct {
	Class     string
	ID        string
	Role      string
	AriaLabel string
	Attrs     Attrs // passthrough for any extra attributes
}

// ArticleConfig configures an <article> element. No required fields.
type ArticleConfig struct {
	Class string
	ID    string
	Attrs Attrs
}

// SectionConfig configures a <section> element.
// Required: Label or LabelledBy (one must be set — becomes aria-label/aria-labelledby).
type SectionConfig struct {
	Label      string // required → aria-label
	LabelledBy string // alternative → aria-labelledby
	Class      string
	ID         string
	Attrs      Attrs
}

// MainConfig configures a <main> element.
// Automatically adds role="main" and id="main-content".
type MainConfig struct {
	Class string
	ID    string
	Attrs Attrs
}

// HeaderConfig configures a <header> element.
// Automatically adds role="banner".
type HeaderConfig struct {
	Class string
	ID    string
	Attrs Attrs
}

// FooterConfig configures a <footer> element.
// Automatically adds role="contentinfo".
type FooterConfig struct {
	Class string
	ID    string
	Attrs Attrs
}

// NavConfig configures a <nav> element.
// Required: Label or LabelledBy (one must be set — becomes aria-label/aria-labelledby).
// Automatically adds role="navigation".
type NavConfig struct {
	Label      string // required → aria-label
	LabelledBy string // alternative → aria-labelledby
	Class      string
	ID         string
	Attrs      Attrs
}

// AsideConfig configures an <aside> element.
// Required: Label or LabelledBy (one must be set).
// Automatically adds role="complementary".
type AsideConfig struct {
	Label      string // required → aria-label
	LabelledBy string // alternative → aria-labelledby
	Class      string
	ID         string
	Attrs      Attrs
}

// FigureConfig configures a <figure> element. No required fields.
type FigureConfig struct {
	Class string
	ID    string
	Attrs Attrs
}

// FigCaptionConfig configures a <figcaption> element. No required fields.
type FigCaptionConfig struct {
	Class string
	ID    string
	Attrs Attrs
}

// DetailsConfig configures a <details> element. No required fields.
type DetailsConfig struct {
	Class string
	ID    string
	Attrs Attrs
}

// SummaryConfig configures a <summary> element. No required fields.
type SummaryConfig struct {
	Class string
	ID    string
	Attrs Attrs
}

// GroupConfig configures a <div> with an ARIA role.
// Required: Role.
type GroupConfig struct {
	Role      string // required
	AriaLabel string
	Class     string
	ID        string
	Attrs     Attrs
}

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
		panic("html: Section requires Label or LabelledBy")
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
		panic("html: Nav requires Label or LabelledBy")
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
		panic("html: Aside requires Label or LabelledBy")
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
		panic("html: Group requires Role")
	}
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	setAttr(attrs, "role", cfg.Role)
	if cfg.AriaLabel != "" {
		setAttr(attrs, "aria-label", cfg.AriaLabel)
	}
	return render.Tag("div", attrs, children...)
}
