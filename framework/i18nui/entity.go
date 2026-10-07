package i18nui

import "maps"

// The entity screens' chrome (framework/entityui): headings, buttons,
// notices and toasts. The record data and the entity, field, view and
// transition names come from the entity.* keys the Entity* helpers read.
//
// Each screen area keeps its keys in its own file (entity_list.go,
// entity_record.go) with its own Defaults block, merged here, so the
// areas grow without touching one another.
const (
	KeyEntityAccessDenied   Key = "ui.entity.accessDenied"   // "You do not have permission to view {entity}."
	KeyEntitySlotFailed     Key = "ui.entity.slotFailed"     // "Couldn't load this section"
	KeyEntitySlotFailedBody Key = "ui.entity.slotFailedBody" // "See server logs."
	KeyEntityLoadFailed     Key = "ui.entity.loadFailed"     // "Couldn't load {entity}"
	KeyEntityNotFound       Key = "ui.entity.notFound"       // "Not found"
	KeyEntityNotFoundBody   Key = "ui.entity.notFoundBody"   // "This {entity} does not exist."
	KeyEntityNew            Key = "ui.entity.new"            // "New {entity}"
	KeyEntityView           Key = "ui.entity.view"           // "View"
	KeyEntityBack           Key = "ui.entity.back"           // "Back"
	KeyEntityCancel         Key = "ui.entity.cancel"         // "Cancel"
	KeyEntityDelete         Key = "ui.entity.delete"         // "Delete"
	KeyEntityDeleteConfirm  Key = "ui.entity.deleteConfirm"  // "Delete this {entity}? This cannot be undone."
	KeyEntityDeleteFailed   Key = "ui.entity.deleteFailed"   // "Could not delete this {entity}."
	KeyEntityDeleted        Key = "ui.entity.deleted"        // "{entity} deleted"
	KeyEntityDuplicate      Key = "ui.entity.duplicate"      // "Duplicate"
	KeyEntityCopyLink       Key = "ui.entity.copyLink"       // "Copy link"
	KeyEntitySelect         Key = "ui.entity.select"         // "— Select —"
	KeyEntityYes            Key = "ui.entity.yes"            // "Yes"
	KeyEntityNo             Key = "ui.entity.no"             // "No"
)

var entityDefaults = map[Key]string{
	KeyEntityAccessDenied:   "You do not have permission to view {entity}.",
	KeyEntitySlotFailed:     "Couldn't load this section",
	KeyEntitySlotFailedBody: "See server logs.",
	KeyEntityLoadFailed:     "Couldn't load {entity}",
	KeyEntityNotFound:       "Not found",
	KeyEntityNotFoundBody:   "This {entity} does not exist.",
	KeyEntityNew:            "New {entity}",
	KeyEntityView:           "View",
	KeyEntityBack:           "Back",
	KeyEntityCancel:         "Cancel",
	KeyEntityDelete:         "Delete",
	KeyEntityDeleteConfirm:  "Delete this {entity}? This cannot be undone.",
	KeyEntityDeleteFailed:   "Could not delete this {entity}.",
	KeyEntityDeleted:        "{entity} deleted",
	KeyEntityDuplicate:      "Duplicate",
	KeyEntityCopyLink:       "Copy link",
	KeyEntitySelect:         "— Select —",
	KeyEntityYes:            "Yes",
	KeyEntityNo:             "No",
}

// entityKeyBlocks are the entity screens' and the admin's Defaults blocks,
// one per area. AllKeys lists every key they hold; init merges them into
// Defaults.
var entityKeyBlocks = []map[Key]string{entityDefaults, entityListDefaults, entityRecordDefaults, entityBulkDefaults, adminDefaults}

func init() {
	for _, block := range entityKeyBlocks {
		maps.Copy(Defaults, block)
	}
}
