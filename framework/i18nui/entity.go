package i18nui

import "github.com/DonaldMurillo/gofastr/core/i18n"

// The entity screens' chrome (framework/entityui): headings, buttons,
// notices and toasts. The record data and the entity, field, view and
// transition names come from the entity.* keys the Entity* helpers read.
//
// Each screen area keeps its phrases in its own file (entity_list.go,
// entity_record.go, entity_bulk.go, admin.go), so the areas grow without
// touching one another.
var (
	EntityAccessDenied   = i18n.Define("ui.entity.accessDenied", "You do not have permission to view {{entity}}.")
	EntitySlotFailed     = i18n.Define("ui.entity.slotFailed", "Couldn't load this section")
	EntitySlotFailedBody = i18n.Define("ui.entity.slotFailedBody", "See server logs.")
	EntityLoadFailed     = i18n.Define("ui.entity.loadFailed", "Couldn't load {{entity}}")
	EntityNotFound       = i18n.Define("ui.entity.notFound", "Not found")
	EntityNotFoundBody   = i18n.Define("ui.entity.notFoundBody", "This {{entity}} does not exist.")
	EntityNew            = i18n.Define("ui.entity.new", "New {{entity}}")
	EntityView           = i18n.Define("ui.entity.view", "View")
	EntityCancel         = i18n.Define("ui.entity.cancel", "Cancel")
	EntityDelete         = i18n.Define("ui.entity.delete", "Delete")
	EntityDeleteTitle    = i18n.Define("ui.entity.deleteTitle", "Delete this {{entity}}?")
	EntityDeleteConfirm  = i18n.Define("ui.entity.deleteConfirm", "This cannot be undone.")
	EntityDeleteSoft     = i18n.Define("ui.entity.deleteSoft", "It can be restored afterwards.")
	EntityUndo           = i18n.Define("ui.entity.undo", "Undo")
	EntityDeleteFailed   = i18n.Define("ui.entity.deleteFailed", "Could not delete this {{entity}}.")
	EntityDeleted        = i18n.Define("ui.entity.deleted", "{{entity}} deleted")
	EntityDuplicate      = i18n.Define("ui.entity.duplicate", "Duplicate")
	EntityCopyLink       = i18n.Define("ui.entity.copyLink", "Copy link")
	EntityOpen           = i18n.Define("ui.entity.open", "Open {{entity}}")
	EntitySelect         = i18n.Define("ui.entity.select", "— Select —")
	EntityYes            = i18n.Define("ui.entity.yes", "Yes")
	EntityNo             = i18n.Define("ui.entity.no", "No")
	EntityCurrency       = i18n.Define("ui.entity.currency", "$")
)
