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
	KeyTableSortAsc   Key = "ui.table.sortAscending"
	KeyTableSortDesc  Key = "ui.table.sortDescending"
	KeyTableNoSort    Key = "ui.table.noSort"
	KeyTableFilter    Key = "ui.table.filter"
	KeyTableNoResults Key = "ui.table.noResults"
	KeyTableLoading   Key = "ui.table.loading"

	// File upload
	KeyFileUploadDrop   Key = "ui.fileUpload.dropzone"
	KeyFileUploadBrowse Key = "ui.fileUpload.browse"
	KeyFileUploadRemove Key = "ui.fileUpload.remove"

	// Form
	KeyFormSubmit  Key = "ui.form.submit"
	KeyFormReset   Key = "ui.form.reset"
	KeyFormSending Key = "ui.form.sending"
	KeyFormSuccess Key = "ui.form.success"
	KeyFormError   Key = "ui.form.error"
	KeyFormYes     Key = "ui.form.yes"
	KeyFormNo      Key = "ui.form.no"

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

	// Repeater
	KeyRepeaterAdd    Key = "ui.repeater.add"
	KeyRepeaterRemove Key = "ui.repeater.remove"

	// PasswordInput
	KeyPasswordInputShow Key = "ui.passwordInput.show"
	KeyPasswordInputHide Key = "ui.passwordInput.hide"

	// Lightbox
	KeyLightboxPrev     Key = "ui.lightbox.previous"
	KeyLightboxNext     Key = "ui.lightbox.next"
	KeyLightboxDownload Key = "ui.lightbox.download"

	// StepWizard
	KeyStepWizardBack   Key = "ui.stepWizard.back"
	KeyStepWizardNext   Key = "ui.stepWizard.next"
	KeyStepWizardSubmit Key = "ui.stepWizard.submit"
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
	KeyAuthPassword:   "Password", // nosecret: UI label, not a credential
	KeyAuthRememberMe: "Remember me",

	KeyRepeaterAdd:    "Add item",
	KeyRepeaterRemove: "Remove",

	KeyPasswordInputShow: "Show password",
	KeyPasswordInputHide: "Hide password",

	KeyLightboxPrev:     "Previous image",
	KeyLightboxNext:     "Next image",
	KeyLightboxDownload: "Download image",

	KeyStepWizardBack:   "Back",
	KeyStepWizardNext:   "Continue",
	KeyStepWizardSubmit: "Submit",
}

// translatorKey is the unexported context key used by WithTranslator
// and translatorFromContext. Type-uniqueness prevents collisions with
// other packages stashing values on the same ctx.
type translatorKey struct{}

// WithTranslator attaches a translator to ctx so callers downstream
// can resolve translations via T(ctx, key) without threading a
// translator argument through every signature.
func WithTranslator(ctx context.Context, tr *i18n.Translator) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, translatorKey{}, tr)
}

// translatorFromContext returns the translator attached via
// WithTranslator, or nil if none.
func translatorFromContext(ctx context.Context) *i18n.Translator {
	if ctx == nil {
		return nil
	}
	v, _ := ctx.Value(translatorKey{}).(*i18n.Translator)
	return v
}

// T translates a key. If ctx carries a translator (via WithTranslator),
// it is consulted first; otherwise the English default for the key is
// returned, or the bare key string when no default exists (so missing
// keys surface visibly rather than as empty strings).
//
// Callers without a ctx in scope can pass context.Background() — the
// English defaults still apply.
func T(ctx context.Context, key Key) string {
	if ctx == nil {
		ctx = context.Background()
	}
	tr := translatorFromContext(ctx)
	if tr != nil {
		if val := tr.T(ctx, string(key)); val != "" && val != string(key) {
			return val
		}
	}
	if def, ok := Defaults[key]; ok {
		return def
	}
	return string(key)
}

// TranslateValidation returns a translated validation error message
// for the given validator type, with optional template variables. ctx
// carries the per-request locale; tr may be nil to use English
// defaults.
//
// Renamed from ValidationError so the symbol no longer collides with
// core/handler.ValidationError when a file imports both packages.
func TranslateValidation(ctx context.Context, tr *i18n.Translator, validator string, vars map[string]string) string {
	if ctx == nil {
		ctx = context.Background()
	}
	if tr != nil {
		ctx = WithTranslator(ctx, tr)
	}
	key := Key("ui.validation." + validator)
	msg := T(ctx, key)
	for k, v := range vars {
		msg = strings.ReplaceAll(msg, "{"+k+"}", v)
	}
	return msg
}

// LabelForField returns the display label for an entity field. If a
// translation key "entity.<entity>.field.<field>" exists in the
// translator's catalog for the ctx locale, it's used. Otherwise the
// field name is humanized.
func LabelForField(ctx context.Context, tr *i18n.Translator, entityName, fieldName string) string {
	if ctx == nil {
		ctx = context.Background()
	}
	key := "entity." + entityName + ".field." + fieldName
	if tr != nil {
		if val := tr.T(ctx, key); val != "" && val != key {
			return val
		}
	}
	return humanize(fieldName)
}

// AllKeys returns every translation Key constant declared by this
// package. Used by completeness tests to assert that adding a new Key
// without a matching Defaults entry is a build-breaking error.
//
// Keep this in sync with the const block above. The
// TestAllKeysCoversAllPackageConstants test cross-checks against the
// Defaults map so stale entries here are caught at test time.
func AllKeys() []Key {
	return []Key{
		KeyPaginationPrevious, KeyPaginationNext, KeyPaginationPage,
		KeyPaginationOf, KeyPaginationShowing, KeyPaginationResults,
		KeyValidationRequired, KeyValidationEmail, KeyValidationMin,
		KeyValidationMax, KeyValidationMinLen, KeyValidationMaxLen,
		KeyValidationPattern, KeyValidationUnique,
		KeyEmptyStateTitle, KeyEmptyStateDesc,
		KeyDialogConfirm, KeyDialogCancel, KeyDialogClose,
		KeyDialogSave, KeyDialogDelete,
		KeyToastSuccess, KeyToastError, KeyToastWarning, KeyToastInfo,
		KeyBannerDismiss,
		KeyTableSortAsc, KeyTableSortDesc, KeyTableNoSort,
		KeyTableFilter, KeyTableNoResults, KeyTableLoading,
		KeyFileUploadDrop, KeyFileUploadBrowse, KeyFileUploadRemove,
		KeyFormSubmit, KeyFormReset, KeyFormSending,
		KeyFormSuccess, KeyFormError, KeyFormYes, KeyFormNo,
		KeySearchPlaceholder, KeySearchNoResults,
		KeyAuthLogin, KeyAuthLogout, KeyAuthSignup,
		KeyAuthEmail, KeyAuthPassword, KeyAuthRememberMe,
		KeyRepeaterAdd, KeyRepeaterRemove,
		KeyPasswordInputShow, KeyPasswordInputHide,
		KeyLightboxPrev, KeyLightboxNext, KeyLightboxDownload,
		KeyStepWizardBack, KeyStepWizardNext, KeyStepWizardSubmit,
	}
}

// humanize converts snake_case or camelCase to "Title Case".
func humanize(s string) string {
	runes := []rune(s)
	var out []rune
	for i, ch := range runes {
		if ch == '_' || ch == '-' {
			out = append(out, ' ')
			continue
		}
		if i > 0 && unicode.IsUpper(ch) && ((i+1 < len(runes) && unicode.IsLower(runes[i+1])) || unicode.IsLower(runes[i-1])) {
			out = append(out, ' ')
		}
		if i == 0 || len(out) == 0 || out[len(out)-1] == ' ' {
			out = append(out, unicode.ToUpper(ch))
		} else {
			out = append(out, unicode.ToLower(ch))
		}
	}
	return string(out)
}
