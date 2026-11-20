package print

import (
	"net/http"
	"regexp"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
)

// Document declares one named, route-addressable print document.
type Document struct {
	// Name is a stable identifier.
	Name string

	// Path is the route relative to Config.PathPrefix.
	Path string

	// Title is the document <title> (and the default PDF filename stem when
	// Name is unsuitable).
	Title string

	// TitleFunc, when non-nil, overrides Title per request, e.g. "Invoice
	// #1042".
	TitleFunc func(r *http.Request) string

	// Build produces the document body for one request.
	Build func(r *http.Request) (component.Component, error)

	// Page overrides Config.DefaultPage; nil inherits it.
	Page *PageConfig

	// Access gates the document before Build; nil inherits Config.DefaultAccess.
	Access AccessPolicy

	// AutoPrint, when true, opens the browser print dialog on load.
	AutoPrint bool

	// Stylesheet is trusted CSS appended after the print base.
	Stylesheet string
}

// PageSize is a named physical page size used for @page { size: … }.
type PageSize string

// The supported page sizes.
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
type Margins struct {
	Top    string
	Right  string
	Bottom string
	Left   string
}

// PageConfig is the physical page setup.
type PageConfig struct {
	Size         PageSize
	Orientation  Orientation
	Margin       Margins
	CustomWidth  string // Custom only
	CustomHeight string // Custom only
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

var safeLength = regexp.MustCompile(`^(?:[0-9]+(?:\.[0-9]+)?(?:mm|cm|in|px|pt)|auto)$`)

func safeCSSLengthOr(v, fallback string) string {
	if safeLength.MatchString(v) {
		return v
	}
	return fallback
}
