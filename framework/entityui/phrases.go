package entityui

import "github.com/DonaldMurillo/gofastr/core/i18n"

// The refusals this package writes, under their catalog keys.
var (
	reasonAccessDenied      = i18n.Define("error.access_denied", "access denied")
	reasonDeleteFailed      = i18n.Define("entityui.delete_failed", "delete failed")
	reasonInvalidReturnPath = i18n.Define("entityui.invalid_return_path", "invalid return path")
	reasonMissingReturnPath = i18n.Define("entityui.missing_return_path", "missing return path")
	reasonNotDeleted        = i18n.Define("entityui.not_deleted", "not deleted")
	reasonSaveFailed        = i18n.Define("entityui.save_failed", "save failed")
	reasonTooManySavedViews = i18n.Define("entityui.too_many_saved_views", "too many saved views")
	reasonViewNameRequired  = i18n.Define("entityui.view_name_required", "a saved view needs a name")
	reasonViewNameTaken     = i18n.Define("entityui.view_name_taken", "a saved view with that name exists")
	reasonViewSignIn        = i18n.Define("entityui.view_sign_in", "sign in to save views")
	reasonViewTooLong       = i18n.Define("entityui.view_too_long", "saved view name, filter or columns too long")
	reasonWriteFailed       = i18n.Define("entityui.write_failed", "write failed")

	reasonBulkNone         = i18n.Define("entityui.bulk_none", "Nothing selected that you may change.")
	reasonBulkUnknown      = i18n.Define("entityui.bulk_unknown", "That action is not available.")
	reasonBulkOverCap      = i18n.Define("entityui.bulk_over_cap", "Select at most {{cap}} records at once.")
	reasonBulkNeedsUser    = i18n.Define("entityui.bulk_needs_user", "Sign in to run an action over more than {{cap}} records.")
	reasonBulkBadFilter    = i18n.Define("entityui.bulk_bad_filter", "The filter could not be applied, so every match is not available.")
	reasonBulkFailed       = i18n.Define("entityui.bulk_failed", "The action could not be run.")
	reasonBulkBadScope     = i18n.Define("entityui.bulk_bad_scope", "Choose which records to apply it to.")
	reasonBulkEveryOverCap = i18n.Define("entityui.bulk_every_over_cap", "Every match stops at {{cap}} records; narrow the list first.")
	reasonBulkStale        = i18n.Define("entityui.bulk_stale", "The list changed since it was shown. Reload it and try again.")
	reasonSavedBadFilter   = i18n.Define("entityui.saved_bad_filter", "The filter does not apply to this list.")
	reasonSavedBadCols     = i18n.Define("entityui.saved_bad_cols", "The columns do not apply to this list.")
	reasonOverrideFailed   = i18n.Define("entityui.override_failed", "Could not override the status.")
	reasonOverrideBlank    = i18n.Define("entityui.override_blank", "A reason is required.")
	reasonOverrideLong     = i18n.Define("entityui.override_long", "The reason is longer than 500 characters.")
	reasonOverrideStateBad = i18n.Define("entityui.override_state_bad", "That status is not one this entity declares.")
	reasonOverrideNoAudit  = i18n.Define("entityui.override_no_audit", "A status override needs an audit log on this entity.")
	reasonOverrideDenied   = i18n.Define("entityui.override_denied", "You may not override the status of this {{entity}}.")
)
