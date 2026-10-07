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

	// The query box: the filter typed by hand.
	KeyEntityQueryBoxLabel Key = "ui.entity.queryBox"     // "Filter"
	KeyEntityQueryBoxHelp  Key = "ui.entity.queryBoxHelp" // "Filter by these fields: {fields}"

	// The columns menu.
	KeyEntityColumns     Key = "ui.entity.columns"     // "Columns"
	KeyEntityColumnsUp   Key = "ui.entity.columnsUp"   // "Move {column} up"
	KeyEntityColumnsDown Key = "ui.entity.columnsDown" // "Move {column} down"

	// The trash view.
	KeyEntityViewDeleted      Key = "ui.entity.viewDeleted"      // "Deleted"
	KeyEntityDeletedEmpty     Key = "ui.entity.deletedEmpty"     // "No deleted {entity} yet"
	KeyEntityDeletedEmptyBody Key = "ui.entity.deletedEmptyBody" // "Rows you delete will appear here until they are restored or deleted permanently."
	KeyEntityRestore          Key = "ui.entity.restore"          // "Restore"
	KeyEntityPurge            Key = "ui.entity.purge"            // "Delete permanently"
	KeyEntityPurgeConfirm     Key = "ui.entity.purgeConfirm"     // "Delete this {entity} permanently? This cannot be undone."
	KeyEntityRestored         Key = "ui.entity.restored"         // "{entity} restored"
	KeyEntityPurged           Key = "ui.entity.purged"           // "{entity} deleted permanently"
	KeyEntityRestoreFailed    Key = "ui.entity.restoreFailed"    // "Could not restore."
	KeyEntityPurgeFailed      Key = "ui.entity.purgeFailed"      // "Could not delete permanently."

	// Saved views.
	KeyEntitySavedGoneTitle     Key = "ui.entity.savedGone"          // "This view no longer applies"
	KeyEntitySavedGoneBody      Key = "ui.entity.savedGoneBody"      // "Its filter or columns no longer match this list. Showing every row instead."
	KeyEntitySavedViews         Key = "ui.entity.savedViews"         // "Saved views"
	KeyEntitySavedName          Key = "ui.entity.savedName"          // "View name"
	KeyEntitySavedSave          Key = "ui.entity.savedSave"          // "Save view"
	KeyEntitySavedDelete        Key = "ui.entity.savedDelete"        // "Delete view"
	KeyEntitySavedDeleteConfirm Key = "ui.entity.savedDeleteConfirm" // "Delete this saved view?"
	KeyEntitySavedSaved         Key = "ui.entity.savedSaved"         // "View saved."
	KeyEntitySavedDeleted       Key = "ui.entity.savedDeleted"       // "View deleted."
	KeyEntitySavedBadFilter     Key = "ui.entity.savedBadFilter"     // "The filter does not apply to this list."
	KeyEntitySavedBadCols       Key = "ui.entity.savedBadCols"       // "The columns do not apply to this list."
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

	KeyEntityQueryBoxLabel: "Filter",
	KeyEntityQueryBoxHelp:  "Filter by these fields: {fields}",

	KeyEntityColumns:     "Columns",
	KeyEntityColumnsUp:   "Move {column} up",
	KeyEntityColumnsDown: "Move {column} down",

	KeyEntityViewDeleted:      "Deleted",
	KeyEntityDeletedEmpty:     "No deleted {entity} yet",
	KeyEntityDeletedEmptyBody: "Rows you delete will appear here until they are restored or deleted permanently.",
	KeyEntityRestore:          "Restore",
	KeyEntityPurge:            "Delete permanently",
	KeyEntityPurgeConfirm:     "Delete this {entity} permanently? This cannot be undone.",
	KeyEntityRestored:         "{entity} restored",
	KeyEntityPurged:           "{entity} deleted permanently",
	KeyEntityRestoreFailed:    "Could not restore.",
	KeyEntityPurgeFailed:      "Could not delete permanently.",

	KeyEntitySavedGoneTitle:     "This view no longer applies",
	KeyEntitySavedGoneBody:      "Its filter or columns no longer match this list. Showing every row instead.",
	KeyEntitySavedViews:         "Saved views",
	KeyEntitySavedName:          "View name",
	KeyEntitySavedSave:          "Save view",
	KeyEntitySavedDelete:        "Delete view",
	KeyEntitySavedDeleteConfirm: "Delete this saved view?",
	KeyEntitySavedSaved:         "View saved.",
	KeyEntitySavedDeleted:       "View deleted.",
	KeyEntitySavedBadFilter:     "The filter does not apply to this list.",
	KeyEntitySavedBadCols:       "The columns do not apply to this list.",
}
