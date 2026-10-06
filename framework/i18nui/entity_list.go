package i18nui

// The entity list's chrome. See entity.go.
const (
	KeyEntityEmpty      Key = "ui.entity.empty"      // "No {entity} yet"
	KeyEntityEmptyBody  Key = "ui.entity.emptyBody"  // "They will appear here once created."
	KeyEntityCount      Key = "ui.entity.count"      // "{count} {entity}"
	KeyEntityCountOne   Key = "ui.entity.countOne"   // "1 {entity}"
	KeyEntitySearch     Key = "ui.entity.search"     // "Search {entity}"
	KeyEntityViewAll    Key = "ui.entity.viewAll"    // "All"
	KeyEntityRowActions Key = "ui.entity.rowActions" // "Actions for {title}"
	KeyEntityPageSize   Key = "ui.entity.pageSize"   // "Rows per page"
	KeyEntityViews      Key = "ui.entity.views"      // "Views"
	// KeyEntityFilterInvalidTitle and Body draw the callout when ?filter=
	// text does not parse: the list still renders, without the filter.
	KeyEntityFilterInvalidTitle Key = "ui.entity.filterInvalid"     // "Filter not applied"
	KeyEntityFilterInvalidBody  Key = "ui.entity.filterInvalidBody" // "The filter could not be applied. Check its text and try again."
)

var entityListDefaults = map[Key]string{
	KeyEntityEmpty:      "No {entity} yet",
	KeyEntityEmptyBody:  "They will appear here once created.",
	KeyEntityCount:      "{count} {entity}",
	KeyEntityCountOne:   "1 {entity}",
	KeyEntitySearch:     "Search {entity}",
	KeyEntityViewAll:    "All",
	KeyEntityRowActions: "Actions for {title}",
	KeyEntityPageSize:   "Rows per page",
	KeyEntityViews:      "Views",

	KeyEntityFilterInvalidTitle: "Filter not applied",
	KeyEntityFilterInvalidBody:  "The filter could not be applied. Check its text and try again.",
}
