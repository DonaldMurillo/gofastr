// Package i18nui provides translated default strings for framework UI
// surfaces. When a translator is configured (via App.WithI18n), these
// defaults are resolved through the translator. Without a translator,
// English fallbacks are returned.
//
// This addresses the i18n surface coverage gap: entity field labels,
// validator error messages, and framework/ui defaults (Pagination,
// ValidationSummary, EmptyState, Banner, Toast) currently emit
// hardcoded English.
package i18nui

import (
	"context"
	"strings"
	"unicode"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// Key is a translation key for framework UI surfaces.
type Key string

const (
	// Pagination
	KeyPaginationPrevious Key = "ui.pagination.previous"
	KeyPaginationNext     Key = "ui.pagination.next"
	KeyPaginationPage     Key = "ui.pagination.page"
	KeyPaginationOf       Key = "ui.pagination.of"
	KeyPaginationShowing  Key = "ui.pagination.showing"
	KeyPaginationResults  Key = "ui.pagination.results"

	// Validation
	KeyValidationRequired Key = "ui.validation.required"
	KeyValidationEmail    Key = "ui.validation.email"
	KeyValidationMin      Key = "ui.validation.min"
	KeyValidationMax      Key = "ui.validation.max"
	KeyValidationMinLen   Key = "ui.validation.minLength"
	KeyValidationMaxLen   Key = "ui.validation.maxLength"
	KeyValidationPattern  Key = "ui.validation.pattern"
	KeyValidationUnique   Key = "ui.validation.unique"

	// Empty state
	KeyEmptyStateTitle Key = "ui.empty.title"
	KeyEmptyStateDesc  Key = "ui.empty.description"

	// Dialog / modal
	KeyDialogConfirm Key = "ui.dialog.confirm"
	KeyDialogCancel  Key = "ui.dialog.cancel"
	KeyDialogClose   Key = "ui.dialog.close"
	KeyDialogSave    Key = "ui.dialog.save"
	KeyDialogDelete  Key = "ui.dialog.delete"

	// Toast
	KeyToastSuccess Key = "ui.toast.success"
	KeyToastError   Key = "ui.toast.error"
	KeyToastWarning Key = "ui.toast.warning"
	KeyToastInfo    Key = "ui.toast.info"

	// Banner
	KeyBannerDismiss Key = "ui.banner.dismiss"

	// DataTable
	KeyTableSortAsc    Key = "ui.table.sortAscending"
	KeyTableSortDesc   Key = "ui.table.sortDescending"
	KeyTableNoSort     Key = "ui.table.noSort"
	KeyTableFilter     Key = "ui.table.filter"
	KeyTableNoResults  Key = "ui.table.noResults"
	KeyTableLoading    Key = "ui.table.loading"

	// File upload
	KeyFileUploadDrop     Key = "ui.fileUpload.dropzone"
	KeyFileUploadBrowse   Key = "ui.fileUpload.browse"
	KeyFileUploadRemove   Key = "ui.fileUpload.remove"

	// Form
	KeyFormSubmit    Key = "ui.form.submit"
	KeyFormReset     Key = "ui.form.reset"
	KeyFormSending   Key = "ui.form.sending"
	KeyFormSuccess   Key = "ui.form.success"
	KeyFormError     Key = "ui.form.error"
	KeyFormYes       Key = "ui.form.yes"
	KeyFormNo        Key = "ui.form.no"

	// Search
	KeySearchPlaceholder Key = "ui.search.placeholder"
	KeySearchNoResults   Key = "ui.search.noResults"

	// Auth
	KeyAuthLogin      Key = "ui.auth.login"
	KeyAuthLogout     Key = "ui.auth.logout"
	KeyAuthSignup     Key = "ui.auth.signup"
	KeyAuthEmail      Key = "ui.auth.email"
	KeyAuthPassword   Key = "ui.auth.password"
	KeyAuthRememberMe Key = "ui.auth.rememberMe"
)

// Defaults are the English fallback strings. Apps that provide their
// own translations should cover all of these keys.
var Defaults = map[Key]string{
	KeyPaginationPrevious: "Previous",
	KeyPaginationNext:     "Next",
	KeyPaginationPage:     "Page",
	KeyPaginationOf:       "of",
	KeyPaginationShowing:  "Showing",
	KeyPaginationResults:  "results",

	KeyValidationRequired: "This field is required",
	KeyValidationEmail:    "Enter a valid email address",
	KeyValidationMin:      "Must be at least {min}",
	KeyValidationMax:      "Must be at most {max}",
	KeyValidationMinLen:   "Must be at least {min} characters",
	KeyValidationMaxLen:   "Must be at most {max} characters",
	KeyValidationPattern:  "Invalid format",
	KeyValidationUnique:   "This value is already taken",

	KeyEmptyStateTitle: "Nothing here yet",
	KeyEmptyStateDesc:  "No items to display.",

	KeyDialogConfirm: "Confirm",
	KeyDialogCancel:  "Cancel",
	KeyDialogClose:   "Close",
	KeyDialogSave:    "Save",
	KeyDialogDelete:  "Delete",

	KeyToastSuccess: "Success",
	KeyToastError:   "Error",
	KeyToastWarning: "Warning",
	KeyToastInfo:    "Info",

	KeyBannerDismiss: "Dismiss",

	KeyTableSortAsc:   "Sort ascending",
	KeyTableSortDesc:  "Sort descending",
	KeyTableNoSort:    "Remove sort",
	KeyTableFilter:    "Filter",
	KeyTableNoResults: "No results found",
	KeyTableLoading:   "Loading…",

	KeyFileUploadDrop:   "Drop files here",
	KeyFileUploadBrowse: "Browse",
	KeyFileUploadRemove: "Remove",

	KeyFormSubmit:  "Submit",
	KeyFormReset:   "Reset",
	KeyFormSending: "Sending…",
	KeyFormSuccess: "Saved successfully",
	KeyFormError:   "An error occurred",
	KeyFormYes:     "Yes",
	KeyFormNo:      "No",

	KeySearchPlaceholder: "Search…",
	KeySearchNoResults:   "No results",

	KeyAuthLogin:      "Log in",
	KeyAuthLogout:     "Log out",
	KeyAuthSignup:     "Sign up",
	KeyAuthEmail:      "Email",
	KeyAuthPassword:   "Password",
	KeyAuthRememberMe: "Remember me",
}

// T translates a key using the default translator (if configured),
// falling back to the English default.
func T(key Key) string {
	return TWith(nil, key)
}

// TWith translates a key using the given translator. Falls back to
// the English default if the translator is nil or the key is not found.
func TWith(tr *i18n.Translator, key Key) string {
	if tr != nil {
		if val := tr.T(context.Background(), string(key)); val != "" {
			return val
		}
	}
	if def, ok := Defaults[key]; ok {
		return def
	}
	return string(key)
}

// ValidationError returns a translated validation error message for
// the given validator type, with optional template variables.
func ValidationError(validator string, vars map[string]string) string {
	key := Key("ui.validation." + validator)
	msg := T(key)
	for k, v := range vars {
		msg = replaceAll(msg, "{"+k+"}", v)
	}
	return msg
}

func replaceAll(s, old, new string) string {
	for strings.Contains(s, old) {
		s = strings.Replace(s, old, new, 1)
	}
	return s
}

// LabelForField returns the display label for an entity field.
// If a translation key "entity.<entity>.field.<field>" exists, it's used.
// Otherwise the field name is humanized.
func LabelForField(tr *i18n.Translator, entityName, fieldName string) string {
	key := "entity." + entityName + ".field." + fieldName
	if tr != nil {
		if val := tr.T(context.Background(), key); val != "" {
			return val
		}
	}
	return humanize(fieldName)
}

// humanize converts snake_case or camelCase to "Title Case".
func humanize(s string) string {
	var result []byte
	for i, ch := range s {
		if ch == '_' || ch == '-' {
			result = append(result, ' ')
			continue
		}
		if i > 0 && isUpper(byte(ch)) && (i+1 < len(s) && isLower(s[i+1]) || i > 0 && isLower(s[i-1])) {
			result = append(result, ' ')
		}
		if i == 0 || len(result) == 0 || result[len(result)-1] == ' ' {
			result = append(result, byte(unicode.ToUpper(ch)))
		} else {
			result = append(result, byte(unicode.ToLower(ch)))
		}
	}
	return string(result)
}

func isUpper(b byte) bool { return b >= 'A' && b <= 'Z' }
func isLower(b byte) bool { return b >= 'a' && b <= 'z' }
