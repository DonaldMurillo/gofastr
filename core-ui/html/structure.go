package html

import "github.com/DonaldMurillo/gofastr/core/render"

// DivConfig configures a <div> element. No required fields.
type DivConfig struct {
	Class      string
	ID         string
	Role       string
	AriaLabel  string
	ExtraAttrs Attrs // passthrough for any extra attributes
}

// ArticleConfig configures an <article> element. No required fields.
type ArticleConfig struct {
	Class      string
	ID         string
	ExtraAttrs Attrs
}

// SectionConfig configures a <section> element. Required: Label or LabelledBy (one must be set,
// becomes aria-label/aria-labelledby).
type SectionConfig struct {
	Label      string // required → aria-label
	LabelledBy string // alternative → aria-labelledby
	Class      string
	ID         string
	ExtraAttrs Attrs
}

// MainConfig configures a <main> element.
type MainConfig struct {
	Class      string
	ID         string
	ExtraAttrs Attrs
}

// HeaderConfig configures a <header> element.
type HeaderConfig struct {
	Class      string
	ID         string
	ExtraAttrs Attrs
	Banner     bool // explicit opt-in for role="banner"
}

// FooterConfig configures a <footer> element.
type FooterConfig struct {
	Class       string
	ID          string
	ExtraAttrs  Attrs
	ContentInfo bool // explicit opt-in for role="contentinfo"
}

// NavConfig configures a <nav> element. Required: Label or LabelledBy (one must be set, becomes
// aria-label/aria-labelledby).
type NavConfig struct {
	Label      string // required → aria-label
	LabelledBy string // alternative → aria-labelledby
	Class      string
	ID         string
	ExtraAttrs Attrs
}

// AsideConfig configures an <aside> element. Required: Label or LabelledBy (one must be set).
type AsideConfig struct {
	Label      string // required → aria-label
	LabelledBy string // alternative → aria-labelledby
	Class      string
	ID         string
	ExtraAttrs Attrs
}

// FigureConfig configures a <figure> element. No required fields.
type FigureConfig struct {
	Class      string
	ID         string
	ExtraAttrs Attrs
}

// FigCaptionConfig configures a <figcaption> element. No required fields.
type FigCaptionConfig struct {
	Class      string
	ID         string
	ExtraAttrs Attrs
}

// DetailsConfig configures a <details> element. No required fields.
type DetailsConfig struct {
	Class      string
	ID         string
	ExtraAttrs Attrs
	Open       bool
}

// SummaryConfig configures a <summary> element. No required fields.
type SummaryConfig struct {
	Class      string
	ID         string
	ExtraAttrs Attrs
}

// GroupConfig configures a <div> with an ARIA role. Required: Role.
type GroupConfig struct {
	Role       string // required
	AriaLabel  string
	Class      string
	ID         string
	ExtraAttrs Attrs
}

// Div produces a <div> element.
func Div(cfg DivConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)
	if cfg.Role != "" {
		setAttr(attrs, "role", cfg.Role)
	}
	if cfg.AriaLabel != "" {
		setAttr(attrs, "aria-label", cfg.AriaLabel)
	}
	return render.Tag("div", attrs, children...)
}

// TemplateConfig configures a <template> element. No required fields.
type TemplateConfig struct {
	Class      string
	ID         string
	ExtraAttrs Attrs
}

// Template produces a <template> element: inert, server-rendered content the page's script
// instantiates with template.content.cloneNode(true) instead of building DOM by hand.
func Template(cfg TemplateConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)
	return render.Tag("template", attrs, children...)
}

// Article produces an <article> element representing a self-contained composition in a page.
func Article(cfg ArticleConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)
	return render.Tag("article", attrs, children...)
}

// Section produces a <section> element. Required: Label or LabelledBy.
func Section(cfg SectionConfig, children ...render.HTML) render.HTML {
	return labelledLandmark("section", RoleRegion, "Section", landmark(cfg), children...)
}

type landmark struct {
	Label      string
	LabelledBy string
	Class      string
	ID         string
	ExtraAttrs Attrs
}

func labelledLandmark(tag, role, elem string, cfg landmark, children ...render.HTML) render.HTML {
	if cfg.Label == "" && cfg.LabelledBy == "" {
		panic("html: " + elem + " requires Label or LabelledBy")
	}
	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)
	if cfg.Label != "" {
		setAttr(attrs, "aria-label", cfg.Label)
	}
	if cfg.LabelledBy != "" {
		setAttr(attrs, "aria-labelledby", cfg.LabelledBy)
	}
	setAttr(attrs, "role", role)
	return render.Tag(tag, attrs, children...)
}

// Main produces a <main> element with role="main" and id="main-content" for skip-navigation links.
func Main(cfg MainConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)
	setAttr(attrs, "role", RoleMain)
	if _, ok := attrs["id"]; !ok {
		attrs["id"] = "main-content"
	}
	if _, ok := attrs["tabindex"]; !ok {
		attrs["tabindex"] = "-1"
	}
	return render.Tag("main", attrs, children...)
}

// Header produces a <header> element.
func Header(cfg HeaderConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)
	if cfg.Banner {
		setAttr(attrs, "role", RoleBanner)
	}
	return render.Tag("header", attrs, children...)
}

// Footer produces a <footer> element.
func Footer(cfg FooterConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)
	if cfg.ContentInfo {
		setAttr(attrs, "role", RoleContentinfo)
	}
	return render.Tag("footer", attrs, children...)
}

// Nav produces a <nav> element with role="navigation". Required: Label or LabelledBy.
func Nav(cfg NavConfig, children ...render.HTML) render.HTML {
	return labelledLandmark("nav", RoleNavigation, "Nav", landmark(cfg), children...)
}

// Aside produces an <aside> element with role="complementary". Required: Label or LabelledBy.
func Aside(cfg AsideConfig, children ...render.HTML) render.HTML {
	return labelledLandmark("aside", RoleComplementary, "Aside", landmark(cfg), children...)
}

// Figure produces a <figure> element for self-contained content referenced from the main flow.
func Figure(cfg FigureConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)
	return render.Tag("figure", attrs, children...)
}

// FigCaption produces a <figcaption> element for a figure caption.
func FigCaption(cfg FigCaptionConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)
	return render.Tag("figcaption", attrs, children...)
}

// Details produces a <details> element for a disclosure widget.
func Details(cfg DetailsConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)
	if cfg.Open {
		attrs["open"] = ""
	}
	return render.Tag("details", attrs, children...)
}

// Summary produces a <summary> element for the summary/caption of a details element.
func Summary(cfg SummaryConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)
	return render.Tag("summary", attrs, children...)
}

// Group produces a <div> element with the given ARIA role. Required: Role.
func Group(cfg GroupConfig, children ...render.HTML) render.HTML {
	if cfg.Role == "" {
		panic("html: Group requires Role")
	}
	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)
	setAttr(attrs, "role", cfg.Role)
	if cfg.AriaLabel != "" {
		setAttr(attrs, "aria-label", cfg.AriaLabel)
	}
	return render.Tag("div", attrs, children...)
}
