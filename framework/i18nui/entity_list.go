package i18nui

// The entity list's chrome. See entity.go.
const (
	KeyEntityEmpty     Key = "ui.entity.empty"     // "No {entity} yet"
	KeyEntityAdd       Key = "ui.entity.add"       // "Add {entity}"
	KeyEntityEmptyBody Key = "ui.entity.emptyBody" // "They will appear here once created."
	// A list narrowed to nothing: a search, filter or facet that matches
	// no row, or a view that holds none.
	KeyEntityNoMatch       Key = "ui.entity.noMatch"       // "No {entity} match"
	KeyEntityNoMatchBody   Key = "ui.entity.noMatchBody"   // "Try another search, or clear the filters."
	KeyEntityClearSearch   Key = "ui.entity.clearSearch"   // "Clear search and filters"
	KeyEntityViewEmpty     Key = "ui.entity.viewEmpty"     // "No {entity} in this view"
	KeyEntityViewEmptyBody Key = "ui.entity.viewEmptyBody" // "The other views may hold some."
	KeyEntityCount         Key = "ui.entity.count"         // "{count} {entity}"
	KeyEntityCountOne      Key = "ui.entity.countOne"      // "1 {entity}"
	KeyEntitySearch        Key = "ui.entity.search"        // "Search {entity}"
	KeyEntityViewAll       Key = "ui.entity.viewAll"       // "All"
	KeyEntityRowActions    Key = "ui.entity.rowActions"    // "Actions for {title}"
	KeyEntityPageSize      Key = "ui.entity.pageSize"      // "Rows per page"
	KeyEntityViews         Key = "ui.entity.views"         // "Views"
	// KeyEntityFilterInvalidTitle and Body draw the callout when ?filter=
	// text does not parse: the list still renders, without the filter.
	KeyEntityFilterInvalidTitle Key = "ui.entity.filterInvalid"     // "Filter not applied"
	KeyEntityFilterInvalidBody  Key = "ui.entity.filterInvalidBody" // "The filter could not be applied. Check its text and try again."

	// The query box: the filter typed by hand.
	KeyEntityQueryBoxField Key = "ui.entity.queryBoxField" // "Filter expression"
	// The filter rows: the group, each row's three controls and the
	// operators' names.
	KeyEntityInlineEdit     Key = "ui.entity.inlineEdit"     // "Edit {field} of {title}"
	KeyEntityInlineEditHint Key = "ui.entity.inlineEditHint" // "Select a value to edit it in place."
	KeyEntityLayout         Key = "ui.entity.layout"         // "Layout"
	KeyEntityLayoutTable    Key = "ui.entity.layoutTable"    // "Table"
	KeyEntityLayoutCards    Key = "ui.entity.layoutCards"    // "Cards"
	KeyEntityFilterRows     Key = "ui.entity.filterRows"     // "Filter where"
	KeyEntityFilterRowField Key = "ui.entity.filterRowField" // "Field"
	KeyEntityFilterRowOp    Key = "ui.entity.filterRowOp"    // "Operator"
	KeyEntityFilterRowValue Key = "ui.entity.filterRowValue" // "Value"
	KeyEntityFilterOpEq     Key = "ui.entity.filterOpEq"     // "is"
	KeyEntityFilterOpNe     Key = "ui.entity.filterOpNe"     // "is not"
	KeyEntityFilterOpLike   Key = "ui.entity.filterOpLike"   // "contains"
	KeyEntityFilterOpGt     Key = "ui.entity.filterOpGt"     // "more than"
	KeyEntityFilterOpLt     Key = "ui.entity.filterOpLt"     // "less than"
	KeyEntityFilterOpGte    Key = "ui.entity.filterOpGte"    // "at least"
	KeyEntityFilterOpLte    Key = "ui.entity.filterOpLte"    // "at most"
	// The help is one line; the reference under the field labels its
	// rows: an example from the entity's own fields (left out when none
	// fits), the operators, the joining words and the fields.
	KeyEntityQueryBoxHelp      Key = "ui.entity.queryBoxHelp"      // "Compare a field with a value. Quote text."
	KeyEntityQueryBoxExample   Key = "ui.entity.queryBoxExample"   // "Example"
	KeyEntityQueryBoxOperators Key = "ui.entity.queryBoxOperators" // "Operators"
	KeyEntityQueryBoxJoin      Key = "ui.entity.queryBoxJoin"      // "Join"
	KeyEntityQueryBoxFields    Key = "ui.entity.queryBoxFields"    // "Fields"

	// The columns menu.
	KeyEntityColumns Key = "ui.entity.columns" // "Columns"

	// The trash view.
	KeyEntityViewDeleted      Key = "ui.entity.viewDeleted"      // "Deleted"
	KeyEntityDeletedEmpty     Key = "ui.entity.deletedEmpty"     // "No deleted {entity} yet"
	KeyEntityDeletedEmptyBody Key = "ui.entity.deletedEmptyBody" // "Rows you delete will appear here until they are restored or deleted permanently."
	KeyEntityRestore          Key = "ui.entity.restore"          // "Restore"
	KeyEntityPurge            Key = "ui.entity.purge"            // "Delete permanently"
	KeyEntityPurgeTitle       Key = "ui.entity.purgeTitle"       // "Delete this {entity} permanently?"
	KeyEntityPurgeConfirm     Key = "ui.entity.purgeConfirm"     // "It leaves the trash and cannot be restored."
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
	KeyEntitySavedDeleteTitle   Key = "ui.entity.savedDeleteTitle"   // "Delete this saved view?"
	KeyEntitySavedDeleteConfirm Key = "ui.entity.savedDeleteConfirm" // "The view's filters and columns go; the records stay."
	KeyEntitySavedSaved         Key = "ui.entity.savedSaved"         // "View saved."
	KeyEntitySavedDeleted       Key = "ui.entity.savedDeleted"       // "View deleted."
	KeyEntitySavedBadFilter     Key = "ui.entity.savedBadFilter"     // "The filter does not apply to this list."
	KeyEntitySavedBadCols       Key = "ui.entity.savedBadCols"       // "The columns do not apply to this list."
)

var entityListDefaults = map[Key]string{
	KeyEntityEmpty:     "No {entity} yet",
	KeyEntityAdd:       "Add {entity}",
	KeyEntityEmptyBody: "They will appear here once created.",

	KeyEntityNoMatch:       "No {entity} match",
	KeyEntityNoMatchBody:   "Try another search, or clear the filters.",
	KeyEntityClearSearch:   "Clear search and filters",
	KeyEntityViewEmpty:     "No {entity} in this view",
	KeyEntityViewEmptyBody: "The other views may hold some.",
	KeyEntityCount:         "{count} {entity}",
	KeyEntityCountOne:      "1 {entity}",
	KeyEntitySearch:        "Search {entity}",
	KeyEntityViewAll:       "All",
	KeyEntityRowActions:    "Actions for {title}",
	KeyEntityPageSize:      "Rows per page",
	KeyEntityViews:         "Views",

	KeyEntityFilterInvalidTitle: "Filter not applied",
	KeyEntityFilterInvalidBody:  "The filter could not be applied. Check its text and try again.",

	KeyEntityQueryBoxField:     "Filter expression",
	KeyEntityInlineEdit:        "Edit {field} of {title}",
	KeyEntityInlineEditHint:    "Select a value to edit it in place.",
	KeyEntityLayout:            "Layout",
	KeyEntityLayoutTable:       "Table",
	KeyEntityLayoutCards:       "Cards",
	KeyEntityFilterRows:        "Filter where",
	KeyEntityFilterRowField:    "Field",
	KeyEntityFilterRowOp:       "Operator",
	KeyEntityFilterRowValue:    "Value",
	KeyEntityFilterOpEq:        "is",
	KeyEntityFilterOpNe:        "is not",
	KeyEntityFilterOpLike:      "contains",
	KeyEntityFilterOpGt:        "more than",
	KeyEntityFilterOpLt:        "less than",
	KeyEntityFilterOpGte:       "at least",
	KeyEntityFilterOpLte:       "at most",
	KeyEntityQueryBoxHelp:      "Compare a field with a value. Quote text.",
	KeyEntityQueryBoxExample:   "Example",
	KeyEntityQueryBoxOperators: "Operators",
	KeyEntityQueryBoxJoin:      "Join",
	KeyEntityQueryBoxFields:    "Fields",

	KeyEntityColumns: "Columns",

	KeyEntityViewDeleted:      "Deleted",
	KeyEntityDeletedEmpty:     "No deleted {entity} yet",
	KeyEntityDeletedEmptyBody: "Rows you delete will appear here until they are restored or deleted permanently.",
	KeyEntityRestore:          "Restore",
	KeyEntityPurge:            "Delete permanently",
	KeyEntityPurgeTitle:       "Delete this {entity} permanently?",
	KeyEntityPurgeConfirm:     "It leaves the trash and cannot be restored.",
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
	KeyEntitySavedDeleteTitle:   "Delete this saved view?",
	KeyEntitySavedDeleteConfirm: "The view's filters and columns go; the records stay.",
	KeyEntitySavedSaved:         "View saved.",
	KeyEntitySavedDeleted:       "View deleted.",
	KeyEntitySavedBadFilter:     "The filter does not apply to this list.",
	KeyEntitySavedBadCols:       "The columns do not apply to this list.",
}
