package i18nui

// The entity list's bulk bar and its answers. See entity.go.
const (
	KeyEntityBulkSelect       Key = "ui.entity.bulkSelect"       // "Select {title}"
	KeyEntityBulkBar          Key = "ui.entity.bulkBar"          // "Bulk actions"
	KeyEntityBulkAction       Key = "ui.entity.bulkAction"       // "Action"
	KeyEntityBulkScope        Key = "ui.entity.bulkScope"        // "Apply to"
	KeyEntityBulkSelected     Key = "ui.entity.bulkSelected"     // "Selected rows"
	KeyEntityBulkPage         Key = "ui.entity.bulkPage"         // "This page ({count})"
	KeyEntityBulkEvery        Key = "ui.entity.bulkEvery"        // "Every match ({count})"
	KeyEntityBulkApply        Key = "ui.entity.bulkApply"        // "Apply"
	KeyEntityBulkTitle        Key = "ui.entity.bulkTitle"        // "Apply to the chosen {entity}?"
	KeyEntityBulkConfirm      Key = "ui.entity.bulkConfirm"      // "The action runs on every row the scope names."
	KeyEntityBulkDelete       Key = "ui.entity.bulkDelete"       // "Delete"
	KeyEntityBulkSet          Key = "ui.entity.bulkSet"          // "Set {field} to {value}"
	KeyEntityBulkMove         Key = "ui.entity.bulkMove"         // "Move: {move}"
	KeyEntityBulkExport       Key = "ui.entity.bulkExport"       // "Export CSV"
	KeyEntityBulkDone         Key = "ui.entity.bulkDone"         // "{done} done, {skipped} skipped, {failed} failed"
	KeyEntityBulkDeleted      Key = "ui.entity.bulkDeleted"      // "{count} {entity} deleted"
	KeyEntityBulkRestored     Key = "ui.entity.bulkRestored"     // "{count} {entity} restored"
	KeyEntityBulkUpdated      Key = "ui.entity.bulkUpdated"      // "{count} {entity} updated"
	KeyEntityBulkQueued       Key = "ui.entity.bulkQueued"       // "{count} {entity} queued"
	KeyEntityBulkNone         Key = "ui.entity.bulkNone"         // "Nothing selected that you may change."
	KeyEntityBulkUnknown      Key = "ui.entity.bulkUnknown"      // "That action is not available."
	KeyEntityBulkOverCap      Key = "ui.entity.bulkOverCap"      // "Select at most {cap} records at once."
	KeyEntityBulkNeedsUser    Key = "ui.entity.bulkNeedsUser"    // "Sign in to run an action over more than {cap} records."
	KeyEntityBulkBadFilter    Key = "ui.entity.bulkBadFilter"    // "The filter could not be applied, so every match is not available."
	KeyEntityBulkFailed       Key = "ui.entity.bulkFailed"       // "The action could not be run."
	KeyEntityBulkBadScope     Key = "ui.entity.bulkBadScope"     // "Choose which records to apply it to."
	KeyEntityBulkEveryOverCap Key = "ui.entity.bulkEveryOverCap" // "Every match stops at {cap} records; narrow the list first."
	KeyEntityBulkStale        Key = "ui.entity.bulkStale"        // "The list changed since it was shown. Reload it and try again."
)

var entityBulkDefaults = map[Key]string{
	KeyEntityBulkSelect:       "Select {title}",
	KeyEntityBulkBar:          "Bulk actions",
	KeyEntityBulkAction:       "Action",
	KeyEntityBulkScope:        "Apply to",
	KeyEntityBulkSelected:     "Selected rows",
	KeyEntityBulkPage:         "This page ({count})",
	KeyEntityBulkEvery:        "Every match ({count})",
	KeyEntityBulkApply:        "Apply",
	KeyEntityBulkTitle:        "Apply to the chosen {entity}?",
	KeyEntityBulkConfirm:      "The action runs on every row the scope names.",
	KeyEntityBulkDelete:       "Delete",
	KeyEntityBulkSet:          "Set {field} to {value}",
	KeyEntityBulkMove:         "Move: {move}",
	KeyEntityBulkExport:       "Export CSV",
	KeyEntityBulkDone:         "{done} done, {skipped} skipped, {failed} failed",
	KeyEntityBulkDeleted:      "{count} {entity} deleted",
	KeyEntityBulkRestored:     "{count} {entity} restored",
	KeyEntityBulkUpdated:      "{count} {entity} updated",
	KeyEntityBulkQueued:       "{count} {entity} queued",
	KeyEntityBulkNone:         "Nothing selected that you may change.",
	KeyEntityBulkUnknown:      "That action is not available.",
	KeyEntityBulkOverCap:      "Select at most {cap} records at once.",
	KeyEntityBulkNeedsUser:    "Sign in to run an action over more than {cap} records.",
	KeyEntityBulkBadFilter:    "The filter could not be applied, so every match is not available.",
	KeyEntityBulkFailed:       "The action could not be run.",
	KeyEntityBulkBadScope:     "Choose which records to apply it to.",
	KeyEntityBulkEveryOverCap: "Every match stops at {cap} records; narrow the list first.",
	KeyEntityBulkStale:        "The list changed since it was shown. Reload it and try again.",
}
