package elements

import "github.com/gofastr/gofastr/core/render"

// Config structs for element functions.
// Each element function takes a value struct as its first argument.
// Required fields are enforced at runtime via panic if empty.
// The linter (core-ui/check) catches missing required fields at build time.

// --- Structural elements ---

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

// --- Interactive elements ---

// ButtonConfig configures a <button> element.
// Required: Label (used as both visible text and aria-label).
type ButtonConfig struct {
	Label string // required → text content AND aria-label
	Type  string // defaults to "button"
	Class string
	ID    string
	Attrs Attrs
}

// LinkConfig configures an <a> element.
// Required: Href and Text.
type LinkConfig struct {
	Href  string // required
	Text  string // required (visible text content)
	Class string
	ID    string
	Attrs Attrs
}

// LinkHTMLConfig configures an <a> element with raw HTML content.
// Required: Href and Content.
type LinkHTMLConfig struct {
	Href    string      // required
	Content render.HTML // required (raw HTML content)
	Class   string
	ID      string
	Attrs   Attrs
}

// FormConfig configures a <form> element.
// Required: Method.
type FormConfig struct {
	Method string // required: "GET" or "POST"
	Action string // optional: form action URL
	Class  string
	ID     string
	Attrs  Attrs
}

// InputConfig configures a void <input> element.
// Required: Type and Name.
type InputConfig struct {
	Type  string // required: "text", "email", "password", etc.
	Name  string // required: form field name
	Class string
	ID    string
	Attrs Attrs
}

// LabelConfig configures a <label> element.
// Required: For (the ID of the form control) and Text.
type LabelConfig struct {
	For   string // required: ID of the associated form control
	Text  string // required: label text
	Class string
	ID    string
	Attrs Attrs
}

// SelectConfig configures a <select> element.
// Required: Name and Options.
type SelectConfig struct {
	Name    string         // required: form field name
	Options []SelectOption // required: at least one option
	Class   string
	ID      string
	Attrs   Attrs
}

// TextAreaConfig configures a <textarea> element.
// Required: Name.
type TextAreaConfig struct {
	Name  string // required: form field name
	Class string
	ID    string
	Attrs Attrs
}

// FieldSetConfig configures a <fieldset> element.
// Required: Legend.
type FieldSetConfig struct {
	Legend string // required: becomes <legend> text
	Class  string
	ID     string
	Attrs  Attrs
}

// ButtonGroupConfig configures a <div> with role="group" containing buttons.
// No required fields.
type ButtonGroupConfig struct {
	AriaLabel string
	Class     string
	ID        string
	Attrs     Attrs
}

// --- Text elements ---

// HeadingConfig configures an <h1>–<h6> element.
// Required: Level (1-6).
type HeadingConfig struct {
	Level int // required: 1-6
	Class string
	ID    string
	Attrs Attrs
}

// TextConfig configures a text container element (Paragraph, Span, Strong, Em, etc.).
// No required fields — used for generic text containers.
type TextConfig struct {
	Class string
	ID    string
	Attrs Attrs
}

// AbbrConfig configures an <abbr> element.
// Required: Title.
type AbbrConfig struct {
	Title string // required: full expansion
	Class string
	ID    string
	Attrs Attrs
}

// TimeConfig configures a <time> element.
// Required: Datetime.
type TimeConfig struct {
	Datetime string // required: machine-readable datetime
	Class    string
	ID       string
	Attrs    Attrs
}

// --- Media elements ---

// ImageConfig configures a void <img> element.
// Required: Src and Alt (empty Alt = decorative, gets role="presentation").
type ImageConfig struct {
	Src   string // required
	Alt   string // required (empty = decorative image)
	Class string
	ID    string
	Attrs Attrs
}

// AudioConfig configures an <audio> element. No required fields.
type AudioConfig struct {
	Class string
	ID    string
	Attrs Attrs
}

// VideoConfig configures a <video> element. No required fields.
type VideoConfig struct {
	Class string
	ID    string
	Attrs Attrs
}

// SourceConfig configures a void <source> element.
// Required: Src and Type.
type SourceConfig struct {
	Src   string // required
	Type  string // required: MIME type
	Class string
	ID    string
	Attrs Attrs
}

// --- List elements ---

// ListConfig configures a list element (<ul> or <ol>).
// No required fields.
type ListConfig struct {
	Class string
	ID    string
	Attrs Attrs
}

// ListItemConfig configures an <li> element. No required fields.
type ListItemConfig struct {
	Class string
	ID    string
	Attrs Attrs
}

// --- Table elements ---

// TableConfig configures a <table> element.
// Automatically adds role="table".
type TableConfig struct {
	Class string
	ID    string
	Attrs Attrs
}

// CaptionConfig configures a <caption> element. No required fields.
type CaptionConfig struct {
	Class string
	ID    string
	Attrs Attrs
}

// TableSectionConfig configures thead/tbody/tfoot elements.
type TableSectionConfig struct {
	Class string
	ID    string
	Attrs Attrs
}

// TableRowConfig configures a <tr> element.
type TableRowConfig struct {
	Class string
	ID    string
	Attrs Attrs
}

// THConfig configures a <th> element.
// Scope defaults to "col" (columnheader).
type THConfig struct {
	Scope string // defaults to "col"
	Class string
	ID    string
	Attrs Attrs
}

// TDConfig configures a <td> element.
type TDConfig struct {
	Class string
	ID    string
	Attrs Attrs
}
