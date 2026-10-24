package i18nui

import "github.com/DonaldMurillo/gofastr/core/i18n"

// The entity list's chrome. See entity.go.
var (
	EntityEmpty     = i18n.Define("ui.entity.empty", "No {{entity}} yet")
	EntityAdd       = i18n.Define("ui.entity.add", "Add {{entity}}")
	EntityEmptyBody = i18n.Define("ui.entity.emptyBody", "They will appear here once created.")
	// A list narrowed to nothing: a search, filter or facet that matches
	// no row, or a view that holds none.
	EntityNoMatch       = i18n.Define("ui.entity.noMatch", "No {{entity}} match")
	EntityNoMatchBody   = i18n.Define("ui.entity.noMatchBody", "Try another search, or clear the filters.")
	EntityClearSearch   = i18n.Define("ui.entity.clearSearch", "Clear search and filters")
	EntityViewEmpty     = i18n.Define("ui.entity.viewEmpty", "No {{entity}} in this view")
	EntityViewEmptyBody = i18n.Define("ui.entity.viewEmptyBody", "The other views may hold some.")
	EntityCount         = i18n.Define("ui.entity.count", "{{count}} {{entity}}")
	EntityCountOne      = i18n.Define("ui.entity.countOne", "1 {{entity}}")
	EntitySearch        = i18n.Define("ui.entity.search", "Search {{entity}}")
	EntityViewAll       = i18n.Define("ui.entity.viewAll", "All")
	EntityRowActions    = i18n.Define("ui.entity.rowActions", "Actions for {{title}}")
	EntityPageSize      = i18n.Define("ui.entity.pageSize", "Rows per page")
	EntityRange         = i18n.Define("ui.entity.range", "{{from}}–{{to}} of {{total}}")
	EntityViews         = i18n.Define("ui.entity.views", "Views")
	// EntityFilterInvalidTitle and Body draw the callout when ?filter=
	// text does not parse: the list still renders, without the filter.
	EntityFilterInvalidTitle = i18n.Define("ui.entity.filterInvalid", "Filter not applied")
	EntityFilterInvalidBody  = i18n.Define("ui.entity.filterInvalidBody", "The filter could not be applied. Check its text and try again.")

	// The query box: the filter typed by hand.
	EntityQueryBoxField = i18n.Define("ui.entity.queryBoxField", "Filter expression")
	// The filter rows: the group, each row's three controls and the
	// operators' names.
	EntityInlineEdit     = i18n.Define("ui.entity.inlineEdit", "Edit {{field}} of {{title}}")
	EntityInlineEditHint = i18n.Define("ui.entity.inlineEditHint", "Select a value to edit it in place.")
	EntityLayout         = i18n.Define("ui.entity.layout", "Layout")
	EntityLayoutTable    = i18n.Define("ui.entity.layoutTable", "Table")
	EntityLayoutCards    = i18n.Define("ui.entity.layoutCards", "Cards")
	EntityFilterRows     = i18n.Define("ui.entity.filterRows", "Filter where")
	EntityFilterRowField = i18n.Define("ui.entity.filterRowField", "Field")
	EntityFilterRowOp    = i18n.Define("ui.entity.filterRowOp", "Operator")
	EntityFilterRowValue = i18n.Define("ui.entity.filterRowValue", "Value")
	EntityFilterOpEq     = i18n.Define("ui.entity.filterOpEq", "is")
	EntityFilterOpNe     = i18n.Define("ui.entity.filterOpNe", "is not")
	EntityFilterOpLike   = i18n.Define("ui.entity.filterOpLike", "contains")
	EntityFilterOpGt     = i18n.Define("ui.entity.filterOpGt", "more than")
	EntityFilterOpLt     = i18n.Define("ui.entity.filterOpLt", "less than")
	EntityFilterOpGte    = i18n.Define("ui.entity.filterOpGte", "at least")
	EntityFilterOpLte    = i18n.Define("ui.entity.filterOpLte", "at most")
	// The help is one line; the reference under the field labels its
	// rows: an example from the entity's own fields (left out when none
	// fits), the operators, the joining words and the fields.
	EntityQueryBoxHelp      = i18n.Define("ui.entity.queryBoxHelp", "Compare a field with a value. Quote text.")
	EntityQueryBoxExample   = i18n.Define("ui.entity.queryBoxExample", "Example")
	EntityQueryBoxOperators = i18n.Define("ui.entity.queryBoxOperators", "Operators")
	EntityQueryBoxJoin      = i18n.Define("ui.entity.queryBoxJoin", "Join")
	EntityQueryBoxFields    = i18n.Define("ui.entity.queryBoxFields", "Fields")

	// The columns menu.
	EntityColumns = i18n.Define("ui.entity.columns", "Columns")

	// The trash view.
	EntityViewDeleted      = i18n.Define("ui.entity.viewDeleted", "Deleted")
	EntityDeletedEmpty     = i18n.Define("ui.entity.deletedEmpty", "No deleted {{entity}} yet")
	EntityDeletedEmptyBody = i18n.Define("ui.entity.deletedEmptyBody", "Rows you delete will appear here until they are restored or deleted permanently.")
	EntityRestore          = i18n.Define("ui.entity.restore", "Restore")
	EntityPurge            = i18n.Define("ui.entity.purge", "Delete permanently")
	EntityPurgeTitle       = i18n.Define("ui.entity.purgeTitle", "Delete this {{entity}} permanently?")
	EntityPurgeConfirm     = i18n.Define("ui.entity.purgeConfirm", "It leaves the trash and cannot be restored.")
	EntityRestored         = i18n.Define("ui.entity.restored", "{{entity}} restored")
	EntityPurged           = i18n.Define("ui.entity.purged", "{{entity}} deleted permanently")
	EntityRestoreFailed    = i18n.Define("ui.entity.restoreFailed", "Could not restore.")
	EntityPurgeFailed      = i18n.Define("ui.entity.purgeFailed", "Could not delete permanently.")

	// Saved views.
	EntitySavedGoneTitle     = i18n.Define("ui.entity.savedGone", "This view no longer applies")
	EntitySavedGoneBody      = i18n.Define("ui.entity.savedGoneBody", "Its filter or columns no longer match this list. Showing every row instead.")
	EntitySavedViews         = i18n.Define("ui.entity.savedViews", "Saved views")
	EntitySavedName          = i18n.Define("ui.entity.savedName", "View name")
	EntitySavedSave          = i18n.Define("ui.entity.savedSave", "Save view")
	EntitySavedDelete        = i18n.Define("ui.entity.savedDelete", "Delete view")
	EntitySavedDeleteTitle   = i18n.Define("ui.entity.savedDeleteTitle", "Delete this saved view?")
	EntitySavedDeleteConfirm = i18n.Define("ui.entity.savedDeleteConfirm", "The view's filters and columns go; the records stay.")
	EntitySavedSaved         = i18n.Define("ui.entity.savedSaved", "View saved.")
	EntitySavedDeleted       = i18n.Define("ui.entity.savedDeleted", "View deleted.")
)
