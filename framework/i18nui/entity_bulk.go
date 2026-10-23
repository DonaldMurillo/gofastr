package i18nui

// The entity list's bulk bar and its answers. See entity.go.
const (
	KeyEntityBulkSelect   Key = "ui.entity.bulkSelect"   // "Select {title}"
	KeyEntityBulkBar      Key = "ui.entity.bulkBar"      // "Bulk actions"
	KeyEntityBulkAction   Key = "ui.entity.bulkAction"   // "Action"
	KeyEntityBulkScope    Key = "ui.entity.bulkScope"    // "Apply to"
	KeyEntityBulkSelected Key = "ui.entity.bulkSelected" // "Selected rows"
	KeyEntityBulkPage     Key = "ui.entity.bulkPage"     // "This page ({count})"
	KeyEntityBulkEvery    Key = "ui.entity.bulkEvery"    // "Every match ({count})"
	KeyEntityBulkApply    Key = "ui.entity.bulkApply"    // "Apply"
	KeyEntityBulkTitle    Key = "ui.entity.bulkTitle"    // "Apply to the chosen {entity}?"
	KeyEntityBulkConfirm  Key = "ui.entity.bulkConfirm"  // "The action runs on every row the scope names."
	KeyEntityBulkDelete   Key = "ui.entity.bulkDelete"   // "Delete"
	KeyEntityBulkSet      Key = "ui.entity.bulkSet"      // "Set {field} to {value}"
	KeyEntityBulkMove     Key = "ui.entity.bulkMove"     // "Move: {move}"
	KeyEntityBulkExport   Key = "ui.entity.bulkExport"   // "Export CSV"
	KeyEntityBulkDone     Key = "ui.entity.bulkDone"     // "{done} done, {skipped} skipped, {failed} failed"
	KeyEntityBulkDeleted  Key = "ui.entity.bulkDeleted"  // "{count} {entity} deleted"
	KeyEntityBulkRestored Key = "ui.entity.bulkRestored" // "{count} {entity} restored"
	KeyEntityBulkUpdated  Key = "ui.entity.bulkUpdated"  // "{count} {entity} updated"
	KeyEntityBulkQueued   Key = "ui.entity.bulkQueued"   // "{count} {entity} queued"
)

var entityBulkDefaults = map[Key]string{
	KeyEntityBulkSelect:   "Select {title}",
	KeyEntityBulkBar:      "Bulk actions",
	KeyEntityBulkAction:   "Action",
	KeyEntityBulkScope:    "Apply to",
	KeyEntityBulkSelected: "Selected rows",
	KeyEntityBulkPage:     "This page ({count})",
	KeyEntityBulkEvery:    "Every match ({count})",
	KeyEntityBulkApply:    "Apply",
	KeyEntityBulkTitle:    "Apply to the chosen {entity}?",
	KeyEntityBulkConfirm:  "The action runs on every row the scope names.",
	KeyEntityBulkDelete:   "Delete",
	KeyEntityBulkSet:      "Set {field} to {value}",
	KeyEntityBulkMove:     "Move: {move}",
	KeyEntityBulkExport:   "Export CSV",
	KeyEntityBulkDone:     "{done} done, {skipped} skipped, {failed} failed",
	KeyEntityBulkDeleted:  "{count} {entity} deleted",
	KeyEntityBulkRestored: "{count} {entity} restored",
	KeyEntityBulkUpdated:  "{count} {entity} updated",
	KeyEntityBulkQueued:   "{count} {entity} queued",
}
