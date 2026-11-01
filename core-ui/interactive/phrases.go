package interactive

import "github.com/DonaldMurillo/gofastr/core/i18n"

// The words this package's components say when the caller sets none.
var (
	sectionsLabel = i18n.Define("interactive.sections", "Sections")
	filterLabel   = i18n.Define("interactive.filter", "Filter")
	noMatches     = i18n.Define("interactive.no_matches", "No matches")
	closeLabel    = i18n.Define("interactive.close", "Close")
	menuLabel     = i18n.Define("interactive.menu", "Menu")
)
