package i18nui

import "github.com/DonaldMurillo/gofastr/core/i18n"

// The entity list's bulk bar and its answers. See entity.go.
var (
	EntityBulkSelect   = i18n.Define("ui.entity.bulkSelect", "Select {{title}}")
	EntityBulkBar      = i18n.Define("ui.entity.bulkBar", "Bulk actions")
	EntityBulkAction   = i18n.Define("ui.entity.bulkAction", "Action")
	EntityBulkScope    = i18n.Define("ui.entity.bulkScope", "Apply to")
	EntityBulkSelected = i18n.Define("ui.entity.bulkSelected", "Selected rows")
	EntityBulkPage     = i18n.Define("ui.entity.bulkPage", "This page ({{count}})")
	EntityBulkEvery    = i18n.Define("ui.entity.bulkEvery", "Every match ({{count}})")
	EntityBulkApply    = i18n.Define("ui.entity.bulkApply", "Apply")
	EntityBulkTitle    = i18n.Define("ui.entity.bulkTitle", "Apply to the chosen {{entity}}?")
	EntityBulkConfirm  = i18n.Define("ui.entity.bulkConfirm", "The action runs on every row the scope names.")
	EntityBulkDelete   = i18n.Define("ui.entity.bulkDelete", "Delete")
	EntityBulkSet      = i18n.Define("ui.entity.bulkSet", "Set {{field}} to {{value}}")
	EntityBulkMove     = i18n.Define("ui.entity.bulkMove", "Move: {{move}}")
	EntityBulkExport   = i18n.Define("ui.entity.bulkExport", "Export CSV")
	EntityBulkDone     = i18n.Define("ui.entity.bulkDone", "{{done}} done, {{skipped}} skipped, {{failed}} failed")
	EntityBulkDeleted  = i18n.Define("ui.entity.bulkDeleted", "{{count}} {{entity}} deleted")
	EntityBulkRestored = i18n.Define("ui.entity.bulkRestored", "{{count}} {{entity}} restored")
	EntityBulkUpdated  = i18n.Define("ui.entity.bulkUpdated", "{{count}} {{entity}} updated")
	EntityBulkQueued   = i18n.Define("ui.entity.bulkQueued", "{{count}} {{entity}} queued")
)
