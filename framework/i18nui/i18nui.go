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
	"maps"
	"slices"
	"strings"
	"unicode"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/internal/inflect"
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
	// KeyDialogConfirmTitle titles the confirm dialog a
	// data-cui-confirm control opens when it names no title.
	KeyDialogConfirmTitle Key = "ui.dialog.confirmTitle"

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
	KeyLightboxLabel    Key = "ui.lightbox.label"
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
	KeyTableSelectAll Key = "ui.table.selectAll"

	// FilterToolbar
	KeyFilterToolbarLabel  Key = "ui.filterToolbar.label"
	KeyFilterApply         Key = "ui.filterToolbar.apply"
	KeyFilterReset         Key = "ui.filterToolbar.reset"
	KeyShortcutSheetTitle  Key = "ui.shortcutSheet.title"  // "Keyboard shortcuts"
	KeyTextAreaInvalidJSON Key = "ui.textArea.invalidJSON" // "Enter valid JSON"
	KeyInlineEditSave      Key = "ui.inlineEdit.save"      // "Save"
	KeyInlineEditSaved     Key = "ui.inlineEdit.saved"     // "Saved"
	KeySelectionCount      Key = "ui.selection.count"      // "{n} selected"
	KeySelectionClear      Key = "ui.selection.clear"      // "Clear selection"
	KeySelectionCopy       Key = "ui.selection.copy"       // "Copy CSV"
	KeySelectionCopied     Key = "ui.selection.copied"     // "Copied {n} rows as CSV"
	KeySelectionCopyFailed Key = "ui.selection.copyFailed" // "The rows could not be copied."
	// ColumnPicker's link names.
	KeyColumnShow     Key = "ui.columnPicker.show"     // "Show {column}"
	KeyColumnHide     Key = "ui.columnPicker.hide"     // "Hide {column}"
	KeyColumnMoveUp   Key = "ui.columnPicker.moveUp"   // "Move {column} earlier"
	KeyColumnMoveDown Key = "ui.columnPicker.moveDown" // "Move {column} later"
	KeyFilterAll      Key = "ui.filterToolbar.all"     // "All {label}"
	KeyFilterAllPlain Key = "ui.filterToolbar.allPlain"
	KeyFilterSortBy   Key = "ui.filterToolbar.sortBy"
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
	KeyCopyLink        Key = "ui.copy.link"
	KeyDrawerOpenPage  Key = "ui.drawer.open_page"
	KeyDrawerOpenPanel Key = "ui.drawer.open_panel"
	KeyDrawerPrev      Key = "ui.drawer.prev"
	KeyDrawerNext      Key = "ui.drawer.next"

	// Ago: how long before now, the way an activity feed says it.
	KeyAgoNow     Key = "ui.ago.now"     // "just now"
	KeyAgoMinutes Key = "ui.ago.minutes" // "{n}m ago"
	KeyAgoHours   Key = "ui.ago.hours"   // "{n}h ago"
	KeyAgoDays    Key = "ui.ago.days"    // "{n}d ago"

	// ChangeList: the visually hidden words before an edit's old and
	// new value.
	KeyChangeFrom Key = "ui.change.from" // "from"
	KeyChangeTo   Key = "ui.change.to"   // "to"

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

	// ThemePicker
	KeyThemePicker  Key = "ui.themePicker.label"
	KeyThemeDefault Key = "ui.themePicker.default"

	// Site navigation (an app's own header package)
	KeyNavPrimary       Key = "ui.nav.primary"
	KeyNavMobilePrimary Key = "ui.nav.mobilePrimary"
	KeyNavToggle        Key = "ui.nav.toggle"

	// Repeater extras
	KeyRepeaterRemoveItem Key = "ui.repeater.removeItem" // "Remove item {index}"

	// Headless component strings. framework/ui's StringsFor bridge
	// resolves every field of headless.Strings from these, so a
	// component rebuilt on headless says its words in the reader's
	// language. Placeholder conventions follow headless, not this
	// package's {name} style: %s where headless formats at render
	// (fmt.Sprintf at the site that owns the value), {name} tokens
	// where the runtime substitutes. A catalog translation keeps the
	// placeholders and may reorder the words around them.
	KeyDismissTitled           Key = "ui.dismiss.titled"            // "Dismiss: %s"
	KeyTagRemoveLabelled       Key = "ui.tag.removeLabelled"        // "Remove %s"
	KeyActionFailed            Key = "ui.action.failed"             // "Could not save. Try again."
	KeyColorPick               Key = "ui.color.pick"                // "Pick %s"
	KeyPasswordRevealShow      Key = "ui.passwordInput.revealShow"  // "Show"
	KeyPasswordRevealHide      Key = "ui.passwordInput.revealHide"  // "Hide"
	KeyToneInfo                Key = "ui.tone.info"                 // "Information"
	KeyToneSuccess             Key = "ui.tone.success"              // "Success"
	KeyToneWarning             Key = "ui.tone.warning"              // "Warning"
	KeyToneDanger              Key = "ui.tone.danger"               // "Error"
	KeyFileSelected            Key = "ui.fileUpload.fileSelected"   // "{name} selected."
	KeyFilesSelected           Key = "ui.fileUpload.filesSelected"  // "{n} files selected: {names}."
	KeyValidationProblem       Key = "ui.validationSummary.problem" // "There is a problem"
	KeyTableSortedBy           Key = "ui.table.sortedBy"            // "Sorted by {column}, {direction}"
	KeyTableDirAscending       Key = "ui.table.dirAscending"        // "ascending"
	KeyTableDirDescending      Key = "ui.table.dirDescending"       // "descending"
	KeyHuiBackToTop            Key = "ui.backToTop.label"           // "Back to top"
	KeyHuiCopyStatus           Key = "ui.copy.status"               // "Copied {name}"
	KeyHuiNumberDecrement      Key = "ui.number.decrementLabelled"  // "Decrement %s"
	KeyHuiNumberIncrement      Key = "ui.number.incrementLabelled"  // "Increment %s"
	KeyHuiRangeLow             Key = "ui.range.low"                 // "Minimum %s"
	KeyHuiRangeHigh            Key = "ui.range.high"                // "Maximum %s"
	KeyHuiRangeValue           Key = "ui.range.value"               // "%s to %s"
	KeyHuiRatingChoice         Key = "ui.rating.choice"             // "%d out of %d"
	KeyHuiTagInputAdd          Key = "ui.tagInput.add"              // "Add %s"
	KeyHuiTagInputAdded        Key = "ui.tagInput.added"            // "{name} added"
	KeyHuiTagInputRemoved      Key = "ui.tagInput.removed"          // "{name} removed"
	KeyHuiRepeaterRemove       Key = "ui.repeater.removeItemIndex"  // "Remove item %d"
	KeyHuiNotificationCount    Key = "ui.notification.countUnread"  // "%d unread notifications"
	KeyHuiStepOf               Key = "ui.stepWizard.stepOfFormat"   // "Step %d of %d"
	KeyHuiStepName             Key = "ui.stepWizard.stepNameFormat" // "Step %d: %s"
	KeyHuiTableOfContentsLabel Key = "ui.toc.label"
	KeyHuiCarouselSlide        Key = "ui.carousel.slide"     // "Slide {n} of {total}"
	KeyHuiCarouselGoToSlide    Key = "ui.carousel.goToSlide" // "Go to slide {n}"

	KeyHuiJSONObject                Key = "ui.json.object"                // "Object"
	KeyHuiJSONArray                 Key = "ui.json.array"                 // "Array"
	KeyHuiJSONNull                  Key = "ui.json.null"                  // "null"
	KeyHuiJSONTrue                  Key = "ui.json.true"                  // "true"
	KeyHuiJSONFalse                 Key = "ui.json.false"                 // "false"
	KeyHuiJSONEmptyObj              Key = "ui.json.emptyObj"              // "{}"
	KeyHuiJSONEmptyArr              Key = "ui.json.emptyArr"              // "[]"
	KeyHuiJSONTruncated             Key = "ui.json.truncated"             // "…"
	KeyHuiComboboxLoading           Key = "ui.combobox.loading"           // "Loading…"
	KeyHuiComboboxNoResults         Key = "ui.combobox.noResults"         // "No matches"
	KeyHuiComboboxResultCount       Key = "ui.combobox.resultCount"       // "{n} results"
	KeyHuiComboboxResultsLabel      Key = "ui.combobox.resultsLabel"      // "results"                 // "On this page"
	KeyHuiSidebarCollapse           Key = "ui.sidebar.collapse"           // "Collapse navigation"
	KeyHuiSidebarExpand             Key = "ui.sidebar.expand"             // "Expand navigation"
	KeyHuiSidebarCollapseText       Key = "ui.sidebar.collapseText"       // "Collapse"
	KeyHuiBreadcrumbsLabel          Key = "ui.breadcrumbs.label"          // "Breadcrumb"
	KeyHuiSortableItemRole          Key = "ui.sortable.itemRole"          // "sortable item"
	KeyHuiSortableDragLabel         Key = "ui.sortable.dragLabel"         // "Drag %s"
	KeyHuiSortableGrabbed           Key = "ui.sortable.grabbed"           // "Grabbed {label}. …"
	KeyHuiSortablePosition          Key = "ui.sortable.position"          // "Position {position} in {list}."
	KeyHuiSortableMoved             Key = "ui.sortable.moved"             // "Moved to {list}, position {position}."
	KeyHuiSortableSaved             Key = "ui.sortable.saved"             // "Order saved."
	KeyHuiSortableReverted          Key = "ui.sortable.reverted"          // "Save failed. Reverted."
	KeyHuiSortableCancelled         Key = "ui.sortable.cancelled"         // "Cancelled."
	KeyHuiSortableConflictReverted  Key = "ui.sortable.conflictReverted"  // "Conflict. Reverted."
	KeyHuiSortableConflictRefreshed Key = "ui.sortable.conflictRefreshed" // "Conflict. List refreshed from server."
	KeyHuiMultiSelectPlaceholder    Key = "ui.multiselect.placeholder"    // "Choose…"
	KeyHuiMultiSelectRemoveLabel    Key = "ui.multiselect.removeLabel"    // "Remove {label}"
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

	KeyDialogConfirm:      "Confirm",
	KeyDialogCancel:       "Cancel",
	KeyDialogClose:        "Close",
	KeyDialogSave:         "Save",
	KeyDialogDelete:       "Delete",
	KeyDialogConfirmTitle: "Are you sure?",

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
	KeyLightboxLabel:     "Image viewer",
	KeyLightboxPrev:      "Previous image",
	KeyLightboxNext:      "Next image",
	KeyLightboxDownload:  "Download image",

	KeyStepWizardBack:   "Back",
	KeyStepWizardNext:   "Continue",
	KeyStepWizardSubmit: "Submit",

	KeyTableEmptyDesc: "Adjust your filters or add new entries.",
	KeyTableSortBy:    "Sort by {column}",
	KeyTableSelectAll: "Select all rows",

	KeyFilterToolbarLabel:  "Filters",
	KeyFilterApply:         "Apply",
	KeyFilterSortBy:        "Sort by",
	KeyFilterClearAll:      "Clear all",
	KeyFilterChipRemove:    "Remove filter {label}",
	KeyFilterReset:         "Reset",
	KeyShortcutSheetTitle:  "Keyboard shortcuts",
	KeyTextAreaInvalidJSON: "Enter valid JSON",
	KeyInlineEditSave:      "Save",
	KeyInlineEditSaved:     "Saved",
	KeySelectionCount:      "{n} selected",
	KeySelectionClear:      "Clear selection",
	KeySelectionCopy:       "Copy CSV",
	KeySelectionCopied:     "Copied {n} rows as CSV",
	KeySelectionCopyFailed: "The rows could not be copied.",
	KeyColumnShow:          "Show {column}",
	KeyColumnHide:          "Hide {column}",
	KeyColumnMoveUp:        "Move {column} earlier",
	KeyColumnMoveDown:      "Move {column} later",
	KeyFilterAll:           "All {label}",
	KeyFilterAllPlain:      "All",

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
	KeyCopyLink:        "Copy link",
	KeyDrawerOpenPage:  "Open as page",
	KeyDrawerOpenPanel: "Open in panel",
	KeyDrawerPrev:      "Previous record",
	KeyDrawerNext:      "Next record",

	KeyAgoNow:     "just now",
	KeyAgoMinutes: "{n}m ago",
	KeyAgoHours:   "{n}h ago",
	KeyAgoDays:    "{n}d ago",

	KeyChangeFrom: "from",
	KeyChangeTo:   "to",

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

	KeyThemePicker:  "Theme",
	KeyThemeDefault: "Default",

	KeyNavPrimary:       "Primary",
	KeyNavMobilePrimary: "Mobile primary",
	KeyNavToggle:        "Toggle navigation",

	KeyRepeaterRemoveItem: "Remove item {index}",

	// Headless component strings (see the const block above; the
	// English matches headless's own defaults byte for byte — the
	// bridge's no-translator output must be the words the goldens
	// pin, and framework/ui's bridge test holds the two together).
	KeyDismissTitled:                "Dismiss: %s",
	KeyTagRemoveLabelled:            "Remove %s",
	KeyActionFailed:                 "Could not save. Try again.",
	KeyColorPick:                    "Pick %s",
	KeyPasswordRevealShow:           "Show",
	KeyPasswordRevealHide:           "Hide",
	KeyToneInfo:                     "Information",
	KeyToneSuccess:                  "Success",
	KeyToneWarning:                  "Warning",
	KeyToneDanger:                   "Error",
	KeyTableSortedBy:                "Sorted by {column}, {direction}",
	KeyTableDirAscending:            "ascending",
	KeyTableDirDescending:           "descending",
	KeyFileSelected:                 "{name} selected.",
	KeyFilesSelected:                "{n} files selected: {names}.",
	KeyValidationProblem:            "There is a problem",
	KeyHuiBackToTop:                 "Back to top",
	KeyHuiCopyStatus:                "Copied {name}",
	KeyHuiNumberDecrement:           "Decrement %s",
	KeyHuiNumberIncrement:           "Increment %s",
	KeyHuiRangeLow:                  "Minimum %s",
	KeyHuiRangeHigh:                 "Maximum %s",
	KeyHuiRangeValue:                "%s to %s",
	KeyHuiRatingChoice:              "%d out of %d",
	KeyHuiTagInputAdd:               "Add %s",
	KeyHuiTagInputAdded:             "{name} added",
	KeyHuiTagInputRemoved:           "{name} removed",
	KeyHuiRepeaterRemove:            "Remove item %d",
	KeyHuiNotificationCount:         "%d unread notifications",
	KeyHuiStepOf:                    "Step %d of %d",
	KeyHuiStepName:                  "Step %d: %s",
	KeyHuiTableOfContentsLabel:      "On this page",
	KeyHuiCarouselSlide:             "Slide {n} of {total}",
	KeyHuiJSONObject:                "Object",
	KeyHuiJSONArray:                 "Array",
	KeyHuiJSONNull:                  "null",
	KeyHuiJSONTrue:                  "true",
	KeyHuiJSONFalse:                 "false",
	KeyHuiJSONEmptyObj:              "{}",
	KeyHuiJSONEmptyArr:              "[]",
	KeyHuiJSONTruncated:             "…",
	KeyHuiCarouselGoToSlide:         "Go to slide {n}",
	KeyHuiComboboxLoading:           "Loading…",
	KeyHuiComboboxNoResults:         "No matches",
	KeyHuiComboboxResultCount:       "{n} results",
	KeyHuiComboboxResultsLabel:      "results",
	KeyHuiSidebarCollapse:           "Collapse navigation",
	KeyHuiSidebarExpand:             "Expand navigation",
	KeyHuiSidebarCollapseText:       "Collapse",
	KeyHuiBreadcrumbsLabel:          "Breadcrumb",
	KeyHuiSortableItemRole:          "sortable item",
	KeyHuiSortableDragLabel:         "Drag %s",
	KeyHuiSortableGrabbed:           "Grabbed {label}. Arrow keys to move, Space to drop.",
	KeyHuiSortablePosition:          "Position {position} in {list}.",
	KeyHuiSortableMoved:             "Moved to {list}, position {position}.",
	KeyHuiSortableSaved:             "Order saved.",
	KeyHuiSortableReverted:          "Save failed. Reverted.",
	KeyHuiSortableCancelled:         "Cancelled.",
	KeyHuiSortableConflictReverted:  "Conflict. Reverted.",
	KeyHuiSortableConflictRefreshed: "Conflict. List refreshed from server.",
	KeyHuiMultiSelectPlaceholder:    "Choose…",
	KeyHuiMultiSelectRemoveLabel:    "Remove {label}",
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
// Callers without a ctx in scope can pass context.Background(), the
// English defaults still apply.
func T(ctx context.Context, key Key) string {
	return resolve(ctx, key)
}

// TVars translates a key and interpolates {name} placeholders from vars.
// Interpolation applies to whichever string wins the lookup (translator
// hit or English default), so the same {name} tokens work in app
// catalogs and in the built-in defaults. Unknown placeholders are left
// as-is (matching TranslateValidation). A translator miss falls through
// to Defaults, the same miss-fallback as T.
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

// TVarsHTML is TVars for a line that carries markup: an activity line
// with a bold actor and the record as a link. The translated text is
// escaped and each {name} becomes vars[name] as given, in one pass, so
// a value holding "{verb}" stays as written. A placeholder vars does not
// name is left as text. The caller escapes what it builds each value
// from.
func TVarsHTML(ctx context.Context, key Key, vars map[string]render.HTML) render.HTML {
	s := resolve(ctx, key)
	var b strings.Builder
	for {
		open := strings.IndexByte(s, '{')
		if open < 0 {
			break
		}
		end := strings.IndexByte(s[open:], '}')
		if end < 0 {
			break
		}
		v, ok := vars[s[open+1:open+end]]
		if !ok {
			b.WriteString(string(render.Text(s[:open+1])))
			s = s[open+1:]
			continue
		}
		b.WriteString(string(render.Text(s[:open])))
		b.WriteString(string(v))
		s = s[open+end+1:]
	}
	b.WriteString(string(render.Text(s)))
	return render.HTML(b.String())
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

// Entity display label helpers resolve the strings an entity's Display
// config names: its singular and plural names, its description, a field's
// label, help text and enum values, a view, transition or form-section key,
// and a nav group. Every one resolves the same way:
//
//  1. the catalog key for the locale (the ctx translator is consulted when
//     the tr argument is nil, the same fallback T uses),
//  2. the Display value the caller passes, when it is set,
//  3. a fallback derived from the key itself.
//
// Nothing in Display is itself a translation: the key, title-cased, is the
// English fallback, and a catalog entry under the key wins when it exists.
// The last-resort fallback for prose with no natural derived text (a
// description, a help line) is the empty string.

// EntitySingular returns the entity's singular name for headings, buttons
// and breadcrumbs: entity.<entity>.singular, else the Display singular,
// else the entity name singularized and title-cased. The singularization
// matters: entity names are usually plurals ("invoices"), and a heading
// that read "New Invoices" for ONE record is the bug the derived
// fallback exists to avoid. Words the shared singularizer leaves alone
// (irregulars like "people") come back title-cased, same as before.
func EntitySingular(ctx context.Context, tr *i18n.Translator, entityName, display string) string {
	return displayLabel(ctx, tr, "entity."+entityName+".singular", display, inflect.Singular(entityName))
}

// EntityPlural returns the entity's plural name for nav and list headings:
// entity.<entity>.plural, else the Display plural, else the entity name
// title-cased.
func EntityPlural(ctx context.Context, tr *i18n.Translator, entityName, display string) string {
	return displayLabel(ctx, tr, "entity."+entityName+".plural", display, entityName)
}

// EntityNoun returns the entity's name for use inside a sentence (the
// "11 customers" count): the plural or singular catalog entry as written,
// since the translator owns its casing, else the Display name or the
// derived name with each word that is capitalized only at its start
// lowercased. A word with any other capital keeps it ("API keys").
func EntityNoun(ctx context.Context, tr *i18n.Translator, entityName, display string, plural bool) string {
	key, slug := "entity."+entityName+".singular", inflect.Singular(entityName)
	if plural {
		key, slug = "entity."+entityName+".plural", entityName
	}
	if got, ok := catalogString(ctx, tr, key); ok {
		return got
	}
	name := display
	if name == "" {
		name = titleCase(slug)
	}
	words := strings.Fields(name)
	for i, w := range words {
		runes := []rune(w)
		plain := true
		for _, r := range runes[1:] {
			if unicode.IsUpper(r) {
				plain = false
				break
			}
		}
		if plain {
			runes[0] = unicode.ToLower(runes[0])
			words[i] = string(runes)
		}
	}
	return strings.Join(words, " ")
}

// EntityDescription returns the one-line description shown under a list
// heading: entity.<entity>.description, else the Display description, else
// the empty string (prose with no derived fallback).
func EntityDescription(ctx context.Context, tr *i18n.Translator, entityName, display string) string {
	return displayLabel(ctx, tr, "entity."+entityName+".description", display, "")
}

// FieldLabel returns a field's label: entity.<entity>.fields.<field>.label,
// else the Display label (pass "" when there is none), else the humanized
// field name.
func FieldLabel(ctx context.Context, tr *i18n.Translator, entityName, fieldName, display string) string {
	key := "entity." + entityName + ".fields." + fieldName + ".label"
	if got, ok := catalogString(ctx, tr, key); ok {
		return got
	}
	if display != "" {
		return display
	}
	return humanize(fieldName)
}

// RelationLabel is FieldLabel for a Relation field. Its humanized
// fallback drops a trailing "_id", "Id" or "ID": the field names the
// record it points at, so customer_id labels as "Customer". The catalog
// key keeps the real field name.
func RelationLabel(ctx context.Context, tr *i18n.Translator, entityName, fieldName, display string) string {
	if display == "" {
		display = humanize(trimIDSuffix(fieldName))
	}
	return FieldLabel(ctx, tr, entityName, fieldName, display)
}

func trimIDSuffix(name string) string {
	for _, suf := range []string{"_id", "Id", "ID"} {
		if len(name) > len(suf) && strings.HasSuffix(name, suf) {
			return strings.TrimSuffix(name, suf)
		}
	}
	return name
}

// FieldHelp returns the help line under a field's input:
// entity.<entity>.fields.<field>.help, else the Display help, else the
// empty string.
func FieldHelp(ctx context.Context, tr *i18n.Translator, entityName, fieldName, display string) string {
	return displayLabel(ctx, tr, "entity."+entityName+".fields."+fieldName+".help", display, "")
}

// FieldValueLabel returns the label for one value of an Enum field:
// entity.<entity>.fields.<field>.values.<value>, else the value
// title-cased ("past_due" -> "Past due").
func FieldValueLabel(ctx context.Context, tr *i18n.Translator, entityName, fieldName, value string) string {
	return displayLabel(ctx, tr, "entity."+entityName+".fields."+fieldName+".values."+value, "", value)
}

// ViewLabel returns a list view's tab label: entity.<entity>.views.<key>,
// else the view's Display label, else the key title-cased.
func ViewLabel(ctx context.Context, tr *i18n.Translator, entityName, key, display string) string {
	return displayLabel(ctx, tr, "entity."+entityName+".views."+key, display, key)
}

// TransitionLabel returns a state move's button label:
// entity.<entity>.transitions.<key>, else the transition's Display label,
// else the key title-cased ("mark_paid" -> "Mark paid").
func TransitionLabel(ctx context.Context, tr *i18n.Translator, entityName, key, display string) string {
	return displayLabel(ctx, tr, "entity."+entityName+".transitions."+key, display, key)
}

// SectionLabel returns a form section's heading:
// entity.<entity>.sections.<key>, else the passed label, else the key
// title-cased. A form section declares Help (prose under the heading), not
// a label of its own, so callers pass the empty string for now.
func SectionLabel(ctx context.Context, tr *i18n.Translator, entityName, key, display string) string {
	return displayLabel(ctx, tr, "entity."+entityName+".sections."+key, display, key)
}

// NavGroupLabel returns a sidebar group's heading: nav.groups.<key>, else
// the group's Display label, else the key title-cased ("billing" ->
// "Billing").
func NavGroupLabel(ctx context.Context, tr *i18n.Translator, key, display string) string {
	return displayLabel(ctx, tr, "nav.groups."+key, display, key)
}

// displayLabel resolves one Display string: catalog first, then the Display
// value, then slug title-cased. An empty slug means prose with no derived
// fallback (a description, a help line), which resolves to "".
func displayLabel(ctx context.Context, tr *i18n.Translator, key, display, slug string) string {
	if got, ok := catalogString(ctx, tr, key); ok {
		return got
	}
	if display != "" {
		return display
	}
	return titleCase(slug)
}

// catalogString returns the catalog text for key and true when the catalog
// holds it. A miss returns the bare key from Translator.T, which is
// indistinguishable from a catalog that maps the key to itself; that shape
// is refused: it reads as a miss.
func catalogString(ctx context.Context, tr *i18n.Translator, key string) (string, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if tr == nil {
		tr = translatorFromContext(ctx)
	}
	if tr == nil {
		return "", false
	}
	if val := tr.T(ctx, key); val != "" && val != key {
		return val, true
	}
	return "", false
}

// titleCase turns a lowercase slug into its English fallback label:
// underscores and hyphens become spaces and the first letter is
// capitalized ("past_due" -> "Past due"). Unlike humanize, later words
// stay lowercase, which reads as a sentence: the label of a key, not a
// title of a column.
func titleCase(slug string) string {
	if slug == "" {
		return ""
	}
	s := strings.NewReplacer("_", " ", "-", " ").Replace(slug)
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

// AllKeys returns every translation Key constant declared by this
// package. Used by completeness tests to assert that adding a new Key
// without a matching Defaults entry is a build-breaking error.
//
// The per-entity families (entity.<entity>.* and nav.groups.<key>) are NOT
// here: their keys name app data, so no constant or Defaults entry can hold
// them. The Entity* label helpers build them, and the package test walks
// every helper against a catalog to pin each key's shape.
//
// Keep this in sync with the const block above. The
// TestAllKeysCoversAllPackageConstants test cross-checks against the
// Defaults map so stale entries here are caught at test time.
func AllKeys() []Key {
	keys := []Key{
		KeyPaginationPrevious, KeyPaginationNext, KeyPaginationPage,
		KeyPaginationOf, KeyPaginationShowing, KeyPaginationResults,
		KeyPaginationLabel,
		KeyValidationRequired, KeyValidationEmail, KeyValidationMin,
		KeyValidationMax, KeyValidationMinLen, KeyValidationMaxLen,
		KeyValidationPattern, KeyValidationUnique,
		KeyEmptyStateTitle, KeyEmptyStateDesc,
		KeyDialogConfirm, KeyDialogCancel, KeyDialogClose,
		KeyDialogSave, KeyDialogDelete, KeyDialogConfirmTitle,
		KeyToastSuccess, KeyToastError, KeyToastWarning, KeyToastInfo,
		KeyBannerDismiss,
		KeyTableSortAsc, KeyTableSortDesc, KeyTableNoSort,
		KeyTableFilter, KeyTableNoResults, KeyTableLoading,
		KeyTableEmptyDesc, KeyTableSortBy, KeyTableSelectAll,
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
		KeyCopyCopy, KeyCopyCopied, KeyCopyToClipboard, KeyCopyLink,
		KeyDrawerOpenPage, KeyDrawerOpenPanel, KeyDrawerPrev, KeyDrawerNext,
		KeyAgoNow, KeyAgoMinutes, KeyAgoHours, KeyAgoDays,
		KeyChangeFrom, KeyChangeTo,
		KeyProgressLabel, KeyTagRemove,
		KeyRepeaterAdd, KeyRepeaterRemove, KeyRepeaterRemoveItem,
		KeyPasswordInputShow, KeyPasswordInputHide,
		KeyLightboxLabel, KeyLightboxPrev, KeyLightboxNext, KeyLightboxDownload,
		KeyStepWizardBack, KeyStepWizardNext, KeyStepWizardSubmit,
		KeyStepWizardStep, KeyStepWizardStepOf,
		KeySectionLabel,
		KeyThemeToggle, KeyThemeLight, KeyThemeDark,
		KeyThemeAuto, KeyThemeColorScheme,
		KeyThemePicker, KeyThemeDefault,
		KeyShortcutSheetTitle, KeyTextAreaInvalidJSON,
		KeyInlineEditSave, KeyInlineEditSaved,
		KeySelectionCount, KeySelectionClear,
		KeySelectionCopy, KeySelectionCopied, KeySelectionCopyFailed,
		KeyColumnShow, KeyColumnHide,
		KeyColumnMoveUp, KeyColumnMoveDown,
		KeyNavPrimary, KeyNavMobilePrimary, KeyNavToggle,
		KeyDismissTitled, KeyTagRemoveLabelled, KeyActionFailed,
		KeyColorPick, KeyPasswordRevealShow, KeyPasswordRevealHide,
		KeyToneInfo, KeyToneSuccess, KeyToneWarning, KeyToneDanger,
		KeyFileSelected, KeyFilesSelected, KeyValidationProblem,
		KeyTableSortedBy, KeyTableDirAscending, KeyTableDirDescending,
		KeyHuiBackToTop, KeyHuiCopyStatus,
		KeyHuiNumberDecrement, KeyHuiNumberIncrement,
		KeyHuiRangeLow, KeyHuiRangeHigh, KeyHuiRangeValue,
		KeyHuiRatingChoice,
		KeyHuiTagInputAdd, KeyHuiTagInputAdded, KeyHuiTagInputRemoved,
		KeyHuiRepeaterRemove, KeyHuiNotificationCount,
		KeyHuiStepOf, KeyHuiStepName,
		KeyHuiTableOfContentsLabel,
		KeyHuiCarouselSlide, KeyHuiCarouselGoToSlide,
		KeyHuiJSONObject, KeyHuiJSONArray, KeyHuiJSONNull,
		KeyHuiJSONTrue, KeyHuiJSONFalse, KeyHuiJSONEmptyObj,
		KeyHuiJSONEmptyArr, KeyHuiJSONTruncated,
		KeyHuiComboboxLoading, KeyHuiComboboxNoResults,
		KeyHuiComboboxResultCount, KeyHuiComboboxResultsLabel,
		KeyHuiSidebarCollapse, KeyHuiSidebarExpand, KeyHuiSidebarCollapseText, KeyHuiBreadcrumbsLabel, KeyHuiSortableItemRole,
		KeyHuiSortableDragLabel, KeyHuiSortableGrabbed, KeyHuiSortablePosition, KeyHuiSortableMoved,
		KeyHuiSortableSaved, KeyHuiSortableReverted, KeyHuiSortableCancelled, KeyHuiSortableConflictReverted,
		KeyHuiSortableConflictRefreshed, KeyHuiMultiSelectPlaceholder,
		KeyHuiMultiSelectRemoveLabel,
	}
	// The entity screens keep their keys in one block per area.
	for _, block := range entityKeyBlocks {
		keys = append(keys, slices.Sorted(maps.Keys(block))...)
	}
	return keys
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
