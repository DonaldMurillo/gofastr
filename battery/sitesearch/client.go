package sitesearch

import (
	_ "embed"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
)

// SearchAttr marks the listbox the browser client fills with results:
// ui.CommandPalette sets it when its Search is on.
const SearchAttr = "data-search"

//go:embed sitesearch.js
var clientJS string

//go:embed sitesearch-static.js
var staticJS string

// The browser client loads when a marked listbox appears (the palette's
// chrome arrives when it first opens). The static ranker is a module of
// its own, so a live page never downloads it: it loads at idle on an
// exported page (<html data-cui-static>), or when the client asks for
// it on the first query. Both ship in every static export. Markers are
// string literals so the runtime's marker gate can read them.
var (
	_ = registry.RegisterBehavior("sitesearch", clientJS, registry.Markers("[data-search]"))
	_ = registry.RegisterBehavior("sitesearch-static", staticJS,
		registry.Markers("[data-cui-static]"), registry.LoadIdle(), registry.Requires("sitesearch"))
)
