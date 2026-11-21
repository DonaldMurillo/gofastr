package sitesearch

import (
	_ "embed"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
)

// SearchAttr marks the listbox the browser client fills with results:
// ui.CommandPalette sets it when its Search is on.
const SearchAttr = "data-search"

//go:embed sitesearch.js
var clientJS string

//go:embed sitesearch-static.js
var staticJS string

//go:embed sitesearch-page.js
var pageJS string

func init() { runtime.RegisterStrings(reasonIndexUnavailable) }

var (
	_ = registry.RegisterBehavior("sitesearch", clientJS, registry.Markers("[data-search]"))
	_ = registry.RegisterBehavior("sitesearch-static", staticJS,
		registry.Markers("[data-cui-static]"), registry.LoadIdle(), registry.Requires("sitesearch"))
	_ = registry.RegisterBehavior("sitesearch-page", pageJS,
		registry.Markers("[data-search-page]"), registry.Requires("sitesearch"))
)
