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
	KeyPaginationLabel    Key = "ui.pagination.label"

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
	// DataTable extras
	KeyTableEmptyDesc Key = "ui.table.emptyDescription"
	KeyTableSortBy    Key = "ui.table.sortBy"

	// FilterToolbar
	KeyFilterToolbarLabel Key = "ui.filterToolbar.label"
	KeyFilterApply        Key = "ui.filterToolbar.apply"
	KeyFilterReset        Key = "ui.filterToolbar.reset"
	KeyFilterAll          Key = "ui.filterToolbar.all" // "All {label}"
	KeyFilterAllPlain     Key = "ui.filterToolbar.allPlain"
	KeyFilterSortBy       Key = "ui.filterToolbar.sortBy"
	// FilterChipBar
	KeyFilterClearAll   Key = "ui.filterChipBar.clearAll"
	KeyFilterChipRemove Key = "ui.filterChipBar.removeFilter" // "Remove filter {label}"

	// Search extras
	KeySearchInputPlaceholder Key = "ui.search.inputPlaceholder" // "Search..." (ASCII dots)
	KeySearchLabel            Key = "ui.search.label"
	KeySearchClear            Key = "ui.search.clear"

	// CommandPalette
	KeyCommandPalettePlaceholder Key = "ui.commandPalette.placeholder"
	KeyCommandPaletteOpen        Key = "ui.commandPalette.open"
	KeyCommandPaletteTitle       Key = "ui.commandPalette.title"
	KeyCommandPaletteNavigate    Key = "ui.commandPalette.navigate"
	KeyCommandPaletteSelect      Key = "ui.commandPalette.select"
	KeyCommandPaletteClose       Key = "ui.commandPalette.close"

	// Form extras
	KeyFormErrorsSummary      Key = "ui.form.errorsSummary"
	KeyFormHasErrors          Key = "ui.form.hasErrors"
	KeyFormSave               Key = "ui.form.save"
	KeyValidationSummaryTitle Key = "ui.validationSummary.title"

	// Carousel
	KeyCarouselPrevious   Key = "ui.carousel.previous"
	KeyCarouselNext       Key = "ui.carousel.next"
	KeyCarouselGoTo       Key = "ui.carousel.goTo" // "Go to slide {slide}"
	KeyCarouselPagination Key = "ui.carousel.pagination"

	// Counter
	KeyCounterDecrement Key = "ui.counter.decrement"
	KeyCounterIncrement Key = "ui.counter.increment"
	KeyCounterLabel     Key = "ui.counter.label"

	// NumberInput
	KeyNumberDecrement Key = "ui.number.decrement" // "Decrement {label}"
	KeyNumberIncrement Key = "ui.number.increment" // "Increment {label}"

	// Spinner / common
	KeyLoading Key = "ui.loading"

	// Sparkline
	KeySparklineNoData Key = "ui.sparkline.noData"

	// Auth extras
	KeySignOut Key = "ui.auth.signOut"

	// FileDropzone
	KeyDropzoneDropFiles     Key = "ui.dropzone.promptMultiple"
	KeyDropzoneDropFile      Key = "ui.dropzone.promptSingle"
	KeyDropzoneMaxSize       Key = "ui.dropzone.maxSize"       // "Max {n} MB."
	KeyDropzoneMaxSizeSuffix Key = "ui.dropzone.maxSizeSuffix" // " (max {n} MB)"

	// FileUpload extras
	KeyFileUploadDropSingle Key = "ui.fileUpload.dropzoneSingle" // "Drop a file here, or click to browse"
	KeyFileMaxSize          Key = "ui.fileUpload.maxSize"        // "Max {n} MB"

	// Notification
	KeyNotificationDismiss Key = "ui.notification.dismiss"
	KeyNotificationEmpty   Key = "ui.notification.empty"

	// PollingIndicator
	KeyPollingLive Key = "ui.polling.live"

	// CopyButton
	KeyCopyCopy        Key = "ui.copy.copy"
	KeyCopyCopied      Key = "ui.copy.copied"
	KeyCopyToClipboard Key = "ui.copy.toClipboard"

	// ProgressSteps
	KeyProgressLabel Key = "ui.progress.label"

	// Tag
	KeyTagRemove Key = "ui.tag.remove" // "Remove {label}"

	// StepWizard extras
	KeyStepWizardStep   Key = "ui.stepWizard.step"   // "Step {step}: {heading}"
	KeyStepWizardStepOf Key = "ui.stepWizard.stepOf" // "Step {step} of {total}"

	// Section
	KeySectionLabel Key = "ui.section.label"

	// ThemeToggle
	KeyThemeToggle      Key = "ui.themeToggle.toggle"
	KeyThemeLight       Key = "ui.themeToggle.light"
	KeyThemeDark        Key = "ui.themeToggle.dark"
	KeyThemeAuto        Key = "ui.themeToggle.auto"
	KeyThemeColorScheme Key = "ui.themeToggle.colorScheme"

	// SiteHeader nav
	KeyNavPrimary       Key = "ui.nav.primary"
	KeyNavMobilePrimary Key = "ui.nav.mobilePrimary"
	KeyNavToggle        Key = "ui.nav.toggle"

	// Repeater extras
	KeyRepeaterRemoveItem Key = "ui.repeater.removeItem" // "Remove item {index}"
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
	KeyPaginationLabel:    "Pagination",

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
	KeyTableNoResults: "No results",
	KeyTableLoading:   "Loading…",

	KeyFileUploadDrop:   "Drop files here, or click to browse",
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

	KeyTableEmptyDesc: "Adjust your filters or add new entries.",
	KeyTableSortBy:    "Sort by {column}",

	KeyFilterToolbarLabel: "Filters",
	KeyFilterApply:        "Apply",
	KeyFilterSortBy:       "Sort by",
	KeyFilterClearAll:     "Clear all",
	KeyFilterChipRemove:   "Remove filter {label}",
	KeyFilterReset:        "Reset",
	KeyFilterAll:          "All {label}",
	KeyFilterAllPlain:     "All",

	KeySearchInputPlaceholder: "Search...",
	KeySearchLabel:            "Search",
	KeySearchClear:            "Clear search",

	KeyCommandPalettePlaceholder: "Type a command or search…",
	KeyCommandPaletteOpen:        "Open command palette",
	KeyCommandPaletteTitle:       "Command palette",
	KeyCommandPaletteNavigate:    "Navigate",
	KeyCommandPaletteSelect:      "Select",
	KeyCommandPaletteClose:       "Close",

	KeyFormErrorsSummary:      "Please fix the highlighted fields and try again.",
	KeyFormHasErrors:          "Form has errors",
	KeyFormSave:               "Save",
	KeyValidationSummaryTitle: "Please fix the following errors:",

	KeyCarouselPrevious:   "Previous slide",
	KeyCarouselNext:       "Next slide",
	KeyCarouselGoTo:       "Go to slide {slide}",
	KeyCarouselPagination: "Slide pagination",

	KeyCounterDecrement: "Decrement",
	KeyCounterIncrement: "Increment",
	KeyCounterLabel:     "Counter",

	KeyNumberDecrement: "Decrement {label}",
	KeyNumberIncrement: "Increment {label}",

	KeyLoading: "Loading…",

	KeySparklineNoData: "No trend data",

	KeySignOut: "Sign out",

	KeyDropzoneDropFiles:     "Drop files here or click to browse",
	KeyDropzoneDropFile:      "Drop a file here or click to browse",
	KeyDropzoneMaxSize:       "Max {n} MB.",
	KeyDropzoneMaxSizeSuffix: " (max {n} MB)",

	KeyFileUploadDropSingle: "Drop a file here, or click to browse",
	KeyFileMaxSize:          "Max {n} MB",

	KeyNotificationDismiss: "Dismiss notification",
	KeyNotificationEmpty:   "No new notifications",

	KeyPollingLive: "Live",

	KeyCopyCopy:        "Copy",
	KeyCopyCopied:      "Copied",
	KeyCopyToClipboard: "Copy to clipboard",

	KeyProgressLabel: "Progress",

	KeyTagRemove: "Remove {label}",

	KeyStepWizardStep:   "Step {step}: {heading}",
	KeyStepWizardStepOf: "Step {step} of {total}",

	KeySectionLabel: "Section",

	KeyThemeToggle:      "Toggle color scheme",
	KeyThemeLight:       "Light",
	KeyThemeDark:        "Dark",
	KeyThemeAuto:        "Auto",
	KeyThemeColorScheme: "Color scheme",

	KeyNavPrimary:       "Primary",
	KeyNavMobilePrimary: "Mobile primary",
	KeyNavToggle:        "Toggle navigation",

	KeyRepeaterRemoveItem: "Remove item {index}",
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

// resolve returns the translated message for key: the ctx translator is
// consulted first, but a translator MISS (it returns the bare key, which
// happens when the app catalog has no ui.* entries) falls through to the
// English Defaults entry rather than rendering the raw key to users.
// Without a translator, Defaults is used directly; a key with no default
// falls through to the bare key string (so missing keys surface visibly
// rather than as empty strings).
func resolve(ctx context.Context, key Key) string {
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

// T translates a key. If ctx carries a translator (via WithTranslator),
// it is consulted first; otherwise the English default for the key is
// returned, or the bare key string when no default exists (so missing
// keys surface visibly rather than as empty strings).
//
// Callers without a ctx in scope can pass context.Background() — the
// English defaults still apply.
func T(ctx context.Context, key Key) string {
	return resolve(ctx, key)
}

// TVars translates a key and interpolates {name} placeholders from vars.
// Interpolation applies to whichever string wins the lookup (translator
// hit or English default), so the same {name} tokens work in app
// catalogs and in the built-in defaults. Unknown placeholders are left
// as-is (matching TranslateValidation). A translator miss falls through
// to Defaults — the same miss-fallback as T.
//
// Example: i18nui.TVars(ctx, i18nui.KeyTableSortBy, map[string]string{"column": "name"})
// → "Sort by name" (or the catalog's "ui.table.sortBy" for the locale).
func TVars(ctx context.Context, key Key, vars map[string]string) string {
	s := resolve(ctx, key)
	for k, v := range vars {
		s = strings.ReplaceAll(s, "{"+k+"}", v)
	}
	return s
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
		KeyPaginationLabel,
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
		KeyTableEmptyDesc, KeyTableSortBy,
		KeyFilterToolbarLabel, KeyFilterApply, KeyFilterReset,
		KeyFilterAll, KeyFilterAllPlain, KeyFilterSortBy,
		KeyFilterClearAll, KeyFilterChipRemove,
		KeySearchPlaceholder, KeySearchNoResults,
		KeySearchInputPlaceholder, KeySearchLabel, KeySearchClear,
		KeyCommandPalettePlaceholder, KeyCommandPaletteOpen,
		KeyCommandPaletteTitle, KeyCommandPaletteNavigate,
		KeyCommandPaletteSelect, KeyCommandPaletteClose,
		KeyFormSubmit, KeyFormReset, KeyFormSending,
		KeyFormSuccess, KeyFormError, KeyFormYes, KeyFormNo,
		KeyFormErrorsSummary, KeyFormHasErrors, KeyFormSave,
		KeyValidationSummaryTitle,
		KeyFileUploadDrop, KeyFileUploadBrowse, KeyFileUploadRemove,
		KeyFileUploadDropSingle, KeyFileMaxSize,
		KeyDropzoneDropFiles, KeyDropzoneDropFile,
		KeyDropzoneMaxSize, KeyDropzoneMaxSizeSuffix,
		KeyCarouselPrevious, KeyCarouselNext, KeyCarouselGoTo,
		KeyCarouselPagination,
		KeyCounterDecrement, KeyCounterIncrement, KeyCounterLabel,
		KeyNumberDecrement, KeyNumberIncrement,
		KeyLoading, KeySparklineNoData,
		KeyAuthLogin, KeyAuthLogout, KeyAuthSignup, KeySignOut,
		KeyAuthEmail, KeyAuthPassword, KeyAuthRememberMe,
		KeyNotificationDismiss, KeyNotificationEmpty,
		KeyPollingLive,
		KeyCopyCopy, KeyCopyCopied, KeyCopyToClipboard,
		KeyProgressLabel, KeyTagRemove,
		KeyRepeaterAdd, KeyRepeaterRemove, KeyRepeaterRemoveItem,
		KeyPasswordInputShow, KeyPasswordInputHide,
		KeyLightboxPrev, KeyLightboxNext, KeyLightboxDownload,
		KeyStepWizardBack, KeyStepWizardNext, KeyStepWizardSubmit,
		KeyStepWizardStep, KeyStepWizardStepOf,
		KeySectionLabel,
		KeyThemeToggle, KeyThemeLight, KeyThemeDark,
		KeyThemeAuto, KeyThemeColorScheme,
		KeyNavPrimary, KeyNavMobilePrimary, KeyNavToggle,
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
