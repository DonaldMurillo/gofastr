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
// Set Banner=true to mark it as the page-wide banner (adds
// role="banner"). Defaults to false because a page may have multiple
// <header> elements (article headers, section headers, page-content
// headers) and only ONE should carry the banner role.
type HeaderConfig struct {
	Class  string
	ID     string
	Attrs  Attrs
	Banner bool // explicit opt-in for role="banner"
}

// FooterConfig configures a <footer> element.
// Set ContentInfo=true to mark it as the page-wide footer (adds
// role="contentinfo"). Defaults to false because a page may have
// multiple <footer> elements (article footers, section footers) and
// only ONE should carry the contentinfo role.
type FooterConfig struct {
	Class       string
	ID          string
	Attrs       Attrs
	ContentInfo bool // explicit opt-in for role="contentinfo"
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
	// Disclosure marks this details element as a dismissible disclosure
	// (mobile hamburger nav, popover, etc.). The runtime will close it
	// automatically on SPA navigation and on Escape. See ARCHITECTURE.md
	// data-fui-disclosure.
	Disclosure bool
	// Open opens the details element on initial render.
	Open bool
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
	// tabindex=-1 makes the element programmatically focusable so the
	// "Skip to main content" link actually moves focus on Safari/iOS,
	// which won't focus a non-tabbable hash target.
	if _, ok := attrs["tabindex"]; !ok {
		attrs["tabindex"] = "-1"
	}
	return render.Tag("main", attrs, children...)
}

// Header produces a <header> element. When cfg.Banner is true it
// also carries role="banner" (use this for the page-wide banner only;
// nested page-content headers should leave Banner=false).
func Header(cfg HeaderConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	if cfg.Banner {
		setAttr(attrs, "role", RoleBanner)
	}
	return render.Tag("header", attrs, children...)
}

// Footer produces a <footer> element. When cfg.ContentInfo is true it
// also carries role="contentinfo" (use this for the page-wide footer
// only; nested article/section footers should leave it false).
func Footer(cfg FooterConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	if cfg.ContentInfo {
		setAttr(attrs, "role", RoleContentinfo)
	}
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
	if cfg.Disclosure {
		attrs["data-fui-disclosure"] = ""
	}
	if cfg.Open {
		attrs["open"] = ""
	}
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
