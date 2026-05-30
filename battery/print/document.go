package print

import (
	"net/http"
	"regexp"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
)

// Document declares one named, route-addressable print document — the
// print-battery equivalent of a screen/route. Each document mounts a
// chrome-free, print-friendly HTML route (and, when a PDFRenderer is
// configured, a sibling PDF route).
type Document struct {
	// Name is a stable identifier. It is the registry key, the default
	// PDF filename stem, and what shows up in logs. Required, unique
	// per battery.
	Name string

	// Path is the route relative to Config.PathPrefix. It supports the
	// router's Go-1.22 {param} syntax, e.g. "/invoice/{id}". Required,
	// must begin with "/".
	Path string

	// Title is the document <title> (and the default PDF filename stem
	// when Name is unsuitable). For per-request titles set TitleFunc.
	Title string

	// TitleFunc, when non-nil, overrides Title per request — e.g.
	// "Invoice #1042". The returned string is HTML-escaped by the shell.
	TitleFunc func(r *http.Request) string

	// Build produces the document body for one request. This is the
	// host's hook: it reads route params via router.Param(r, …), closes
	// over the host's own services/DB to load data, and returns the body
	// component. The component is rendered with component.SafeRenderCtx
	// (panic-safe, RenderCtx-aware).
	//
	// Return ErrNotFound to render a clean 404; any other error renders a
	// clean 500 status page — never a stack trace. Required.
	Build func(r *http.Request) (component.Component, error)

	// Page overrides the battery default PageConfig for this document
	// (an invoice may be A4 portrait while a receipt is an 80mm roll).
	// nil = inherit Config.DefaultPage.
	Page *PageConfig

	// Access gates the document, evaluated BEFORE Build so an
	// unauthorized caller never triggers a data load. nil = inherit
	// Config.DefaultAccess (which itself defaults to RequireAuth).
	Access AccessPolicy

	// AutoPrint, when true, opens the browser print dialog on load. It
	// is implemented as a CSP-safe external script (see autoPrintPath),
	// not an inline <script>. Ignored on the PDF path.
	AutoPrint bool

	// Stylesheet is optional document-specific CSS appended after the
	// generated @page rules and the print base. It is trusted host
	// input (not user input) and injected verbatim into a <style>.
	Stylesheet string
}

// PageSize is a named physical page size used for @page { size: … }.
type PageSize string

// The supported page sizes. A4/Letter/Legal map directly to the CSS
// @page size keyword; Custom uses CustomWidth/CustomHeight instead.
const (
	A4     PageSize = "A4"
	Letter PageSize = "Letter"
	Legal  PageSize = "Legal"
	Custom PageSize = "Custom"
)

// Orientation is the page orientation for named sizes.
type Orientation string

// The supported orientations.
const (
	Portrait  Orientation = "portrait"
	Landscape Orientation = "landscape"
)

// Margins holds the four page margins as CSS lengths (e.g. "12mm").
// An empty side inherits the battery default ("12mm").
type Margins struct {
	Top    string
	Right  string
	Bottom string
	Left   string
}

// PageConfig is the physical page setup. It is turned into @page CSS for
// the HTML shell and into paper/margin flags for the PDF renderer.
type PageConfig struct {
	Size         PageSize
	Orientation  Orientation
	Margin       Margins
	CustomWidth  string // only when Size == Custom, e.g. "80mm"
	CustomHeight string // only when Size == Custom, e.g. "auto"
}

// Ptr returns a pointer to a copy of p, for use in Document.Page.
func (p PageConfig) Ptr() *PageConfig { return &p }

// MM builds uniform Margins of n millimetres on every side.
func MM(n int) Margins {
	v := strconv.Itoa(n) + "mm"
	return Margins{Top: v, Right: v, Bottom: v, Left: v}
}

// A4Portrait is a convenience constructor for the most common page setup.
func A4Portrait(m Margins) PageConfig {
	return PageConfig{Size: A4, Orientation: Portrait, Margin: m}
}

// LetterPortrait is a convenience constructor for US Letter portrait.
func LetterPortrait(m Margins) PageConfig {
	return PageConfig{Size: Letter, Orientation: Portrait, Margin: m}
}

// safeLength allows only simple, non-injectable CSS lengths: a number
// with a unit (mm/cm/in/px/pt) or the keyword "auto". Anything else
// (semicolons, braces, url(), etc.) is rejected so a PageConfig can
// never smuggle arbitrary CSS into the shell's @page block.
var safeLength = regexp.MustCompile(`^(?:[0-9]+(?:\.[0-9]+)?(?:mm|cm|in|px|pt)|auto)$`)

// safeCSSLengthOr returns v when it is a safe CSS length, else fallback.
func safeCSSLengthOr(v, fallback string) string {
	if safeLength.MatchString(v) {
		return v
	}
	return fallback
}
