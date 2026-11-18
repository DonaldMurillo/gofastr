// Package i18nui holds the phrases framework UI surfaces show, each an
// [i18n.Phrase] a catalog entry under its key translates, and the Entity*
// helpers that resolve the strings an entity's Display config names.
package i18nui

import (
	"context"
	"strings"
	"unicode"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/internal/inflect"
)

var (
	PaginationPrevious = i18n.Define("ui.pagination.previous", "Previous")
	PaginationNext     = i18n.Define("ui.pagination.next", "Next")
	PaginationPage     = i18n.Define("ui.pagination.page", "Page")
	PaginationOf       = i18n.Define("ui.pagination.of", "of")
	PaginationShowing  = i18n.Define("ui.pagination.showing", "Showing")
	PaginationResults  = i18n.Define("ui.pagination.results", "results")
	PaginationLabel    = i18n.Define("ui.pagination.label", "Pagination")

	EmptyStateTitle = i18n.Define("ui.empty.title", "Nothing here yet")
	EmptyStateDesc  = i18n.Define("ui.empty.description", "No items to display.")

	DialogConfirm = i18n.Define("ui.dialog.confirm", "Confirm")
	DialogCancel  = i18n.Define("ui.dialog.cancel", "Cancel")
	DialogClose   = i18n.Define("ui.dialog.close", "Close")
	DialogSave    = i18n.Define("ui.dialog.save", "Save")
	DialogDelete  = i18n.Define("ui.dialog.delete", "Delete")
	// DialogConfirmTitle titles the confirm dialog a
	// data-cui-confirm control opens when it names no title.
	DialogConfirmTitle = i18n.Define("ui.dialog.confirmTitle", "Are you sure?")

	ToastSuccess = i18n.Define("ui.toast.success", "Success")
	ToastError   = i18n.Define("ui.toast.error", "Error")
	ToastWarning = i18n.Define("ui.toast.warning", "Warning")
	ToastInfo    = i18n.Define("ui.toast.info", "Info")

	BannerDismiss = i18n.Define("ui.banner.dismiss", "Dismiss")

	TableSortAsc   = i18n.Define("ui.table.sortAscending", "Sort ascending")
	TableSortDesc  = i18n.Define("ui.table.sortDescending", "Sort descending")
	TableNoSort    = i18n.Define("ui.table.noSort", "Remove sort")
	TableFilter    = i18n.Define("ui.table.filter", "Filter")
	TableNoResults = i18n.Define("ui.table.noResults", "No results")
	TableLoading   = i18n.Define("ui.table.loading", "Loading…")

	FileUploadDrop   = i18n.Define("ui.fileUpload.dropzone", "Drop files here, or click to browse")
	FileUploadBrowse = i18n.Define("ui.fileUpload.browse", "Browse")
	FileUploadRemove = i18n.Define("ui.fileUpload.remove", "Remove")

	FormSubmit  = i18n.Define("ui.form.submit", "Submit")
	FormReset   = i18n.Define("ui.form.reset", "Reset")
	FormSending = i18n.Define("ui.form.sending", "Sending…")
	FormSuccess = i18n.Define("ui.form.success", "Saved successfully")
	FormError   = i18n.Define("ui.form.error", "An error occurred")
	FormYes     = i18n.Define("ui.form.yes", "Yes")
	FormNo      = i18n.Define("ui.form.no", "No")

	SearchPlaceholder = i18n.Define("ui.search.placeholder", "Search…")
	SearchNoResults   = i18n.Define("ui.search.noResults", "No results")

	AuthLogin      = i18n.Define("ui.auth.login", "Log in")
	AuthLogout     = i18n.Define("ui.auth.logout", "Log out")
	AuthSignup     = i18n.Define("ui.auth.signup", "Sign up")
	AuthEmail      = i18n.Define("ui.auth.email", "Email")
	AuthPassword   = i18n.Define("ui.auth.password", "Password") // nosecret: UI label, not a credential
	AuthRememberMe = i18n.Define("ui.auth.rememberMe", "Remember me")
	// AuthOneTimeCode labels a ui.OneTimeCode field.
	AuthOneTimeCode = i18n.Define("ui.auth.oneTimeCode", "Authentication code")
	// AuthForgotPassword is the sign-in form's link to the password reset.
	AuthForgotPassword = i18n.Define("ui.auth.forgotPassword", "Forgot your password?") // nosecret: UI label, not a credential

	RepeaterAdd    = i18n.Define("ui.repeater.add", "Add item")
	RepeaterRemove = i18n.Define("ui.repeater.remove", "Remove")

	PasswordInputShow = i18n.Define("ui.passwordInput.show", "Show password")

	LightboxLabel    = i18n.Define("ui.lightbox.label", "Image viewer")
	LightboxPrev     = i18n.Define("ui.lightbox.previous", "Previous image")
	LightboxNext     = i18n.Define("ui.lightbox.next", "Next image")
	LightboxDownload = i18n.Define("ui.lightbox.download", "Download image")

	StepWizardBack   = i18n.Define("ui.stepWizard.back", "Back")
	StepWizardNext   = i18n.Define("ui.stepWizard.next", "Continue")
	StepWizardSubmit = i18n.Define("ui.stepWizard.submit", "Submit")

	TableEmptyDesc = i18n.Define("ui.table.emptyDescription", "Adjust your filters or add new entries.")
	TableSortBy    = i18n.Define("ui.table.sortBy", "Sort by {{column}}")
	TableSelectAll = i18n.Define("ui.table.selectAll", "Select all rows")

	FilterToolbarLabel  = i18n.Define("ui.filterToolbar.label", "Filters")
	FilterApply         = i18n.Define("ui.filterToolbar.apply", "Apply")
	FilterReset         = i18n.Define("ui.filterToolbar.reset", "Reset")
	ShortcutSheetTitle  = i18n.Define("ui.shortcutSheet.title", "Keyboard shortcuts")
	TextAreaInvalidJSON = i18n.Define("ui.textArea.invalidJSON", "Enter valid JSON")
	InlineEditSave      = i18n.Define("ui.inlineEdit.save", "Save")
	InlineEditSaved     = i18n.Define("ui.inlineEdit.saved", "Saved")
	SelectionCount      = i18n.Define("ui.selection.count", "{{n}} selected")
	SelectionClear      = i18n.Define("ui.selection.clear", "Clear selection")
	SelectionCopy       = i18n.Define("ui.selection.copy", "Copy CSV")
	SelectionCopied     = i18n.Define("ui.selection.copied", "Copied {{n}} rows as CSV")
	SelectionCopyFailed = i18n.Define("ui.selection.copyFailed", "The rows could not be copied.")

	ColumnShow     = i18n.Define("ui.columnPicker.show", "Show {{column}}")
	ColumnHide     = i18n.Define("ui.columnPicker.hide", "Hide {{column}}")
	ColumnMoveUp   = i18n.Define("ui.columnPicker.moveUp", "Move {{column}} earlier")
	ColumnMoveDown = i18n.Define("ui.columnPicker.moveDown", "Move {{column}} later")
	FilterAll      = i18n.Define("ui.filterToolbar.all", "All {{label}}")
	FilterAllPlain = i18n.Define("ui.filterToolbar.allPlain", "All")
	FilterSortBy   = i18n.Define("ui.filterToolbar.sortBy", "Sort by")

	FilterClearAll   = i18n.Define("ui.filterChipBar.clearAll", "Clear all")
	FilterChipRemove = i18n.Define("ui.filterChipBar.removeFilter", "Remove filter {{label}}")

	SearchInputPlaceholder = i18n.Define("ui.search.inputPlaceholder", "Search...")
	SearchLabel            = i18n.Define("ui.search.label", "Search")
	SearchClear            = i18n.Define("ui.search.clear", "Clear search")

	SearchResultsCount = i18n.Define("ui.searchResults.count", "{{n}} results for “{{q}}”")
	SearchResultsOne   = i18n.Define("ui.searchResults.one", "1 result for “{{q}}”")
	SearchResultsNone  = i18n.Define("ui.searchResults.none", "No pages match “{{q}}”")
	SearchResultsLabel = i18n.Define("ui.searchResults.label", "Search results")
	SearchResultsMore  = i18n.Define("ui.searchResults.more", "More results")
	SearchResultsAll   = i18n.Define("ui.searchResults.all", "See all results for “{{q}}”")

	CommandPalettePlaceholder = i18n.Define("ui.commandPalette.placeholder", "Type a command or search…")
	CommandPaletteOpen        = i18n.Define("ui.commandPalette.open", "Open command palette")
	CommandPaletteTitle       = i18n.Define("ui.commandPalette.title", "Command palette")
	CommandPaletteNavigate    = i18n.Define("ui.commandPalette.navigate", "Navigate")
	CommandPaletteSelect      = i18n.Define("ui.commandPalette.select", "Select")
	CommandPaletteClose       = i18n.Define("ui.commandPalette.close", "Close")

	DiffOld = i18n.Define("ui.diff.old", "Old")
	DiffNew = i18n.Define("ui.diff.new", "New")

	FormErrorsSummary      = i18n.Define("ui.form.errorsSummary", "Please fix the highlighted fields and try again.")
	FormHasErrors          = i18n.Define("ui.form.hasErrors", "Form has errors")
	FormSave               = i18n.Define("ui.form.save", "Save")
	ValidationSummaryTitle = i18n.Define("ui.validationSummary.title", "Please fix the following errors:")

	CarouselPrevious   = i18n.Define("ui.carousel.previous", "Previous slide")
	CarouselNext       = i18n.Define("ui.carousel.next", "Next slide")
	CarouselGoTo       = i18n.Define("ui.carousel.goTo", "Go to slide {{slide}}")
	CarouselPagination = i18n.Define("ui.carousel.pagination", "Slide pagination")

	CounterDecrement = i18n.Define("ui.counter.decrement", "Decrement")
	CounterIncrement = i18n.Define("ui.counter.increment", "Increment")
	CounterLabel     = i18n.Define("ui.counter.label", "Counter")

	NumberDecrement = i18n.Define("ui.number.decrement", "Decrement {{label}}")
	NumberIncrement = i18n.Define("ui.number.increment", "Increment {{label}}")

	Loading = i18n.Define("ui.loading", "Loading…")

	SparklineNoData = i18n.Define("ui.sparkline.noData", "No trend data")

	ChartNoData        = i18n.Define("ui.chart.noData", "No data yet")
	ChartNotEnoughData = i18n.Define("ui.chart.notEnoughData", "Not enough data yet")
	ChartValue         = i18n.Define("ui.chart.value", "{{label}}: {{value}}")
	GaugeNoData        = i18n.Define("ui.gauge.noData", "{{label}}: no data yet")
	ScatterPoint       = i18n.Define("ui.scatter.point", "{{series}}: {{label}} ({{x}}, {{y}})")
	ScatterPointBare   = i18n.Define("ui.scatter.pointBare", "{{series}}: ({{x}}, {{y}})")
	HeatmapCell        = i18n.Define("ui.heatmap.cell", "{{row}}, {{col}}: {{value}}")
	HeatmapNoData      = i18n.Define("ui.heatmap.noData", "no data")
	HeatmapLess        = i18n.Define("ui.heatmap.less", "Less")
	HeatmapMore        = i18n.Define("ui.heatmap.more", "More")

	WaterfallStep     = i18n.Define("ui.waterfall.step", "Step")
	WaterfallStart    = i18n.Define("ui.waterfall.start", "Starts at")
	WaterfallTook     = i18n.Define("ui.waterfall.took", "Took")
	WaterfallTimeline = i18n.Define("ui.waterfall.timeline", "Timeline")
	WaterfallEmpty    = i18n.Define("ui.waterfall.empty", "No steps recorded")
	DurationMicros    = i18n.Define("ui.duration.micros", "{{n}} µs")
	DurationMillis    = i18n.Define("ui.duration.millis", "{{n}} ms")
	DurationSeconds   = i18n.Define("ui.duration.seconds", "{{n}} s")

	SignOut = i18n.Define("ui.auth.signOut", "Sign out")

	DropzoneDropFiles     = i18n.Define("ui.dropzone.promptMultiple", "Drop files here or click to browse")
	DropzoneDropFile      = i18n.Define("ui.dropzone.promptSingle", "Drop a file here or click to browse")
	DropzoneMaxSize       = i18n.Define("ui.dropzone.maxSize", "Max {{n}} MB.")
	DropzoneMaxSizeSuffix = i18n.Define("ui.dropzone.maxSizeSuffix", " (max {{n}} MB)")

	FileUploadDropSingle = i18n.Define("ui.fileUpload.dropzoneSingle", "Drop a file here, or click to browse")
	FileMaxSize          = i18n.Define("ui.fileUpload.maxSize", "Max {{n}} MB")

	SkipToMain = i18n.Define("ui.skipLink.toMain", "Skip to main content")

	NotificationDismiss = i18n.Define("ui.notification.dismiss", "Dismiss notification")
	NotificationEmpty   = i18n.Define("ui.notification.empty", "No new notifications")

	PollingLive = i18n.Define("ui.polling.live", "Live")

	CopyCopy        = i18n.Define("ui.copy.copy", "Copy")
	CopyCopied      = i18n.Define("ui.copy.copied", "Copied")
	CopyFailed      = i18n.Define("ui.copy.failed", "Could not copy to the clipboard")
	CopyToClipboard = i18n.Define("ui.copy.toClipboard", "Copy to clipboard")
	CopyLink        = i18n.Define("ui.copy.link", "Copy link")
	DrawerOpenPage  = i18n.Define("ui.drawer.open_page", "Open as page")
	DrawerOpenPanel = i18n.Define("ui.drawer.open_panel", "Open in panel")
	DrawerPrev      = i18n.Define("ui.drawer.prev", "Previous record")
	DrawerNext      = i18n.Define("ui.drawer.next", "Next record")

	AgoNow     = i18n.Define("ui.ago.now", "just now")
	AgoMinutes = i18n.Define("ui.ago.minutes", "{{n}}m ago")
	AgoHours   = i18n.Define("ui.ago.hours", "{{n}}h ago")
	AgoDays    = i18n.Define("ui.ago.days", "{{n}}d ago")

	ChangeFrom = i18n.Define("ui.change.from", "from")
	ChangeTo   = i18n.Define("ui.change.to", "to")

	ProgressLabel = i18n.Define("ui.progress.label", "Progress")

	StepRailLabel = i18n.Define("ui.stepRail.label", "Page steps")

	AvatarStatusOnline  = i18n.Define("ui.avatar.statusOnline", "Online")
	AvatarStatusAway    = i18n.Define("ui.avatar.statusAway", "Away")
	AvatarStatusBusy    = i18n.Define("ui.avatar.statusBusy", "Busy")
	AvatarStatusOffline = i18n.Define("ui.avatar.statusOffline", "Offline")

	AvatarGroupLabel    = i18n.Define("ui.avatarGroup.label", "Avatars")
	AvatarGroupMore     = i18n.Define("ui.avatarGroup.more", "{{count}} more")
	ContentRowSidebar   = i18n.Define("ui.contentRow.sidebar", "Sidebar")
	ContentRowToolbar   = i18n.Define("ui.contentRow.toolbar", "Toolbar")
	ContentRowAside     = i18n.Define("ui.contentRow.aside", "Context")
	FilterChipBarLabel  = i18n.Define("ui.filterChipBar.label", "Active filters")
	GalleryLabel        = i18n.Define("ui.gallery.label", "Image gallery")
	ListDetailBack      = i18n.Define("ui.listDetail.back", "Back")
	SidebarOpenDrawer   = i18n.Define("ui.sidebar.openDrawer", "Open navigation")
	SidebarCloseDrawer  = i18n.Define("ui.sidebar.closeDrawer", "Close navigation")
	SidebarDrawerLabel  = i18n.Define("ui.sidebar.drawer", "Navigation")
	NetworkRetryTitle   = i18n.Define("ui.networkRetry.title", "Connection lost")
	NetworkRetryText    = i18n.Define("ui.networkRetry.text", "We're having trouble reaching the server. Your changes are paused until the connection comes back.")
	NetworkRetryNowText = i18n.Define("ui.networkRetry.retry", "Retry now")

	TagRemove = i18n.Define("ui.tag.remove", "Remove {{label}}")

	StepWizardStep   = i18n.Define("ui.stepWizard.step", "Step {{step}}: {{heading}}")
	StepWizardStepOf = i18n.Define("ui.stepWizard.stepOf", "Step {{step}} of {{total}}")

	Section = i18n.Define("ui.section.label", "Section")

	ThemeToggle      = i18n.Define("ui.themeToggle.toggle", "Toggle color scheme")
	ThemeLight       = i18n.Define("ui.themeToggle.light", "Light")
	ThemeDark        = i18n.Define("ui.themeToggle.dark", "Dark")
	ThemeAuto        = i18n.Define("ui.themeToggle.auto", "Auto")
	ThemeColorScheme = i18n.Define("ui.themeToggle.colorScheme", "Color scheme")

	LanguageMenu = i18n.Define("ui.languageMenu.label", "Language")

	ThemePicker  = i18n.Define("ui.themePicker.label", "Theme")
	ThemeDefault = i18n.Define("ui.themePicker.default", "Default")

	NavPrimary       = i18n.Define("ui.nav.primary", "Primary")
	NavMobilePrimary = i18n.Define("ui.nav.mobilePrimary", "Mobile primary")
	NavToggle        = i18n.Define("ui.nav.toggle", "Toggle navigation")

	NavMoreArticles = i18n.Define("ui.nav.moreArticles", "More articles")

	RepeaterRemoveItem = i18n.Define("ui.repeater.removeItem", "Remove item {{index}}")

	DismissTitled           = i18n.Define("ui.dismiss.titled", "Dismiss: {{title}}")
	TagRemoveLabelled       = i18n.Define("ui.tag.removeLabelled", "Remove {{label}}")
	ActionFailed            = i18n.Define("ui.action.failed", "Could not save. Try again.")
	ColorPick               = i18n.Define("ui.color.pick", "Pick {{name}}")
	PasswordRevealShow      = i18n.Define("ui.passwordInput.revealShow", "Show")
	ToneInfo                = i18n.Define("ui.tone.info", "Information")
	ToneSuccess             = i18n.Define("ui.tone.success", "Success")
	ToneWarning             = i18n.Define("ui.tone.warning", "Warning")
	ToneDanger              = i18n.Define("ui.tone.danger", "Error")
	FileSelected            = i18n.Define("ui.fileUpload.fileSelected", "{{name}} selected.")
	FilesSelected           = i18n.Define("ui.fileUpload.filesSelected", "{{n}} files selected: {{names}}.")
	ValidationProblem       = i18n.Define("ui.validationSummary.problem", "There is a problem")
	TableSortedBy           = i18n.Define("ui.table.sortedBy", "Sorted by {{column}}, {{direction}}")
	TableDirAscending       = i18n.Define("ui.table.dirAscending", "ascending")
	TableDirDescending      = i18n.Define("ui.table.dirDescending", "descending")
	HuiBackToTop            = i18n.Define("ui.backToTop.label", "Back to top")
	HuiCopyStatus           = i18n.Define("ui.copy.status", "Copied {{name}}")
	HuiNumberDecrement      = i18n.Define("ui.number.decrementLabelled", "Decrement {{label}}")
	HuiNumberIncrement      = i18n.Define("ui.number.incrementLabelled", "Increment {{label}}")
	HuiRangeLow             = i18n.Define("ui.range.low", "Minimum {{label}}")
	HuiRangeHigh            = i18n.Define("ui.range.high", "Maximum {{label}}")
	HuiRangeValue           = i18n.Define("ui.range.value", "{{low}} to {{high}}")
	HuiRatingChoice         = i18n.Define("ui.rating.choice", "{{n}} out of {{max}}")
	HuiTagInputAdd          = i18n.Define("ui.tagInput.add", "Add {{label}}")
	HuiTagInputAdded        = i18n.Define("ui.tagInput.added", "{{name}} added")
	HuiTagInputRemoved      = i18n.Define("ui.tagInput.removed", "{{name}} removed")
	HuiRepeaterRemove       = i18n.Define("ui.repeater.removeItemIndex", "Remove item {{n}}")
	HuiNotificationCount    = i18n.Define("ui.notification.countUnread", "{{n}} unread notifications")
	HuiStepOf               = i18n.Define("ui.stepWizard.stepOfFormat", "Step {{step}} of {{total}}")
	HuiStepName             = i18n.Define("ui.stepWizard.stepNameFormat", "Step {{step}}: {{heading}}")
	HuiTableOfContentsLabel = i18n.Define("ui.toc.label", "On this page")
	HuiCarouselSlide        = i18n.Define("ui.carousel.slide", "Slide {{n}} of {{total}}")
	HuiCarouselGoToSlide    = i18n.Define("ui.carousel.goToSlide", "Go to slide {{n}}")
	HuiCarouselRole         = i18n.Define("ui.carousel.role", "carousel")
	HuiCarouselSlideRole    = i18n.Define("ui.carousel.slideRole", "slide")
	HuiPaneSecondary        = i18n.Define("ui.paneHost.secondary", "Secondary")
	HuiPaneTertiary         = i18n.Define("ui.paneHost.tertiary", "Tertiary")

	HuiJSONObject                = i18n.Define("ui.json.object", "Object")
	HuiJSONArray                 = i18n.Define("ui.json.array", "Array")
	HuiJSONNull                  = i18n.Define("ui.json.null", "null")
	HuiJSONTrue                  = i18n.Define("ui.json.true", "true")
	HuiJSONFalse                 = i18n.Define("ui.json.false", "false")
	HuiJSONEmptyObj              = i18n.Define("ui.json.emptyObj", "{}")
	HuiJSONEmptyArr              = i18n.Define("ui.json.emptyArr", "[]")
	HuiJSONTruncated             = i18n.Define("ui.json.truncated", "…")
	HuiComboboxLoading           = i18n.Define("ui.combobox.loading", "Loading…")
	HuiComboboxNoResults         = i18n.Define("ui.combobox.noResults", "No matches")
	HuiComboboxResultCount       = i18n.Define("ui.combobox.resultCount", "{{n}} results")
	HuiComboboxResultsLabel      = i18n.Define("ui.combobox.resultsLabel", "results")
	HuiSidebarCollapse           = i18n.Define("ui.sidebar.collapse", "Collapse navigation")
	HuiSidebarExpand             = i18n.Define("ui.sidebar.expand", "Expand navigation")
	HuiSidebarCollapseText       = i18n.Define("ui.sidebar.collapseText", "Collapse")
	HuiBreadcrumbsLabel          = i18n.Define("ui.breadcrumbs.label", "Breadcrumb")
	HuiSortableItemRole          = i18n.Define("ui.sortable.itemRole", "sortable item")
	HuiSortableDragLabel         = i18n.Define("ui.sortable.dragLabel", "Drag {{label}}")
	HuiSortableGrabbed           = i18n.Define("ui.sortable.grabbed", "Grabbed {{label}}. Arrow keys to move, Space to drop.")
	HuiSortablePosition          = i18n.Define("ui.sortable.position", "Position {{position}} in {{list}}.")
	HuiSortableMoved             = i18n.Define("ui.sortable.moved", "Moved to {{list}}, position {{position}}.")
	HuiSortableSaved             = i18n.Define("ui.sortable.saved", "Order saved.")
	HuiSortableReverted          = i18n.Define("ui.sortable.reverted", "Save failed. Reverted.")
	HuiSortableCancelled         = i18n.Define("ui.sortable.cancelled", "Cancelled.")
	HuiSortableConflictReverted  = i18n.Define("ui.sortable.conflictReverted", "Conflict. Reverted.")
	HuiSortableConflictRefreshed = i18n.Define("ui.sortable.conflictRefreshed", "Conflict. List refreshed from server.")
	HuiMultiSelectPlaceholder    = i18n.Define("ui.multiselect.placeholder", "Choose…")
)

var (
	HuiDatePickerPrevMonth = i18n.Define("ui.datepicker.prevMonth", "Previous month")
	HuiDatePickerNextMonth = i18n.Define("ui.datepicker.nextMonth", "Next month")
	HuiDatePickerPrevYear  = i18n.Define("ui.datepicker.prevYear", "Previous year")
	HuiDatePickerNextYear  = i18n.Define("ui.datepicker.nextYear", "Next year")
	HuiDatePickerStart     = i18n.Define("ui.datepicker.start", "Start date")
	HuiDatePickerEnd       = i18n.Define("ui.datepicker.end", "End date")
	HuiDatePickerChosen    = i18n.Define("ui.datepicker.chosen", "{{date}} selected.")
	HuiDatePickerChooseEnd = i18n.Define("ui.datepicker.chooseEnd", "{{date}} selected. Choose the end date.")
	HuiDatePickerRange     = i18n.Define("ui.datepicker.range", "{{start}} to {{end}} selected.")
	HuiDatePickerFailed    = i18n.Define("ui.datepicker.monthFailed", "The month could not be loaded.")
)

var (
	CodeBlockSource     = i18n.Define("ui.codeBlock.source", "source code")
	CodeBlockLangSource = i18n.Define("ui.codeBlock.langSource", "{{language}} source")
	CodeBlockLines      = i18n.Define("ui.codeBlock.lines", "{{n}} lines")
	CodeBlockCopy       = i18n.Define("ui.codeBlock.copy", "copy")
	CodeBlockCopied     = i18n.Define("ui.codeBlock.copied", "copied")
	CodeBlockCopyOf     = i18n.Define("ui.codeBlock.copyOf", "copy {{what}}")
)

var (
	PricingRecommended = i18n.Define("ui.pricing.recommended", "Recommended")
	PricingChoose      = i18n.Define("ui.pricing.choose", "Choose {{plan}}")
)

var (
	ShortcutLabel = i18n.Define("ui.shortcut.label", "Shortcut: {{keys}}")
	KeyCtrl       = i18n.Define("ui.key.ctrl", "Ctrl")
	KeyMod        = i18n.Define("ui.key.mod", "Mod")
	KeyShift      = i18n.Define("ui.key.shift", "Shift")
	KeyAlt        = i18n.Define("ui.key.alt", "Alt")
	KeyEnter      = i18n.Define("ui.key.enter", "Enter")
	KeyEsc        = i18n.Define("ui.key.esc", "Esc")
	KeyTab        = i18n.Define("ui.key.tab", "Tab")
	KeySpace      = i18n.Define("ui.key.space", "Space")
	KeyUp         = i18n.Define("ui.key.up", "Up")
	KeyDown       = i18n.Define("ui.key.down", "Down")
	KeyLeft       = i18n.Define("ui.key.left", "Left")
	KeyRight      = i18n.Define("ui.key.right", "Right")
)

// HTML is [i18n.Phrase.Text] for a line that carries markup: the
// translated text is escaped and each {{name}} becomes vars[name] as
// given. The caller escapes what it builds each value from.
func HTML(ctx context.Context, p i18n.Phrase, vars map[string]render.HTML) render.HTML {
	s := p.Text(ctx)
	var b strings.Builder
	for {
		open := strings.Index(s, "{{")
		if open < 0 {
			break
		}
		end := strings.Index(s[open:], "}}")
		if end < 0 {
			break
		}
		v, ok := vars[strings.TrimSpace(s[open+2:open+end])]
		if !ok {
			b.WriteString(string(render.Text(s[:open+2])))
			s = s[open+2:]
			continue
		}
		b.WriteString(string(render.Text(s[:open])))
		b.WriteString(string(v))
		s = s[open+end+2:]
	}
	b.WriteString(string(render.Text(s)))
	return render.HTML(b.String())
}

// EntitySingular returns entity.<entity>.singular, else display, else the
// entity name singularized and title-cased. A nil tr reads the ctx's.
func EntitySingular(ctx context.Context, tr *i18n.Translator, entityName, display string) string {
	return displayLabel(ctx, tr, "entity."+entityName+".singular", display, inflect.Singular(entityName))
}

// EntityPlural returns entity.<entity>.plural, else display, else the
// entity name title-cased.
func EntityPlural(ctx context.Context, tr *i18n.Translator, entityName, display string) string {
	return displayLabel(ctx, tr, "entity."+entityName+".plural", display, entityName)
}

// EntityNoun returns the entity's name for use inside a sentence: the
// plural or singular catalog entry as written, else display or the derived
// name with each word capitalized only at its start lowercased.
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
	return fallback(ctx, tr, strings.Join(words, " "))
}

// EntityDescription returns entity.<entity>.description, else display,
// else "".
func EntityDescription(ctx context.Context, tr *i18n.Translator, entityName, display string) string {
	return displayLabel(ctx, tr, "entity."+entityName+".description", display, "")
}

// FieldLabel returns entity.<entity>.fields.<field>.label, else display,
// else the humanized field name.
func FieldLabel(ctx context.Context, tr *i18n.Translator, entityName, fieldName, display string) string {
	key := "entity." + entityName + ".fields." + fieldName + ".label"
	if got, ok := catalogString(ctx, tr, key); ok {
		return got
	}
	if display != "" {
		return fallback(ctx, tr, display)
	}
	return fallback(ctx, tr, humanize(fieldName))
}

// RelationLabel is FieldLabel for a Relation field, whose humanized
// fallback drops a trailing "_id", "Id" or "ID".
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

// FieldHelp returns entity.<entity>.fields.<field>.help, else display,
// else "".
func FieldHelp(ctx context.Context, tr *i18n.Translator, entityName, fieldName, display string) string {
	return displayLabel(ctx, tr, "entity."+entityName+".fields."+fieldName+".help", display, "")
}

// FieldValueLabel returns entity.<entity>.fields.<field>.values.<value>,
// else the value title-cased ("past_due" is "Past due").
func FieldValueLabel(ctx context.Context, tr *i18n.Translator, entityName, fieldName, value string) string {
	return displayLabel(ctx, tr, "entity."+entityName+".fields."+fieldName+".values."+value, "", value)
}

// ViewLabel returns entity.<entity>.views.<key>, else display, else the
// key title-cased.
func ViewLabel(ctx context.Context, tr *i18n.Translator, entityName, key, display string) string {
	return displayLabel(ctx, tr, "entity."+entityName+".views."+key, display, key)
}

// TransitionLabel returns entity.<entity>.transitions.<key>, else
// display, else the key title-cased.
func TransitionLabel(ctx context.Context, tr *i18n.Translator, entityName, key, display string) string {
	return displayLabel(ctx, tr, "entity."+entityName+".transitions."+key, display, key)
}

// SectionLabel returns entity.<entity>.sections.<key>, else display, else
// the key title-cased.
func SectionLabel(ctx context.Context, tr *i18n.Translator, entityName, key, display string) string {
	return displayLabel(ctx, tr, "entity."+entityName+".sections."+key, display, key)
}

// NavGroupLabel returns nav.groups.<key>, else display, else the key
// title-cased.
func NavGroupLabel(ctx context.Context, tr *i18n.Translator, key, display string) string {
	return displayLabel(ctx, tr, "nav.groups."+key, display, key)
}

func displayLabel(ctx context.Context, tr *i18n.Translator, key, display, slug string) string {
	if got, ok := catalogString(ctx, tr, key); ok {
		return got
	}
	if display != "" {
		return fallback(ctx, tr, display)
	}
	return fallback(ctx, tr, titleCase(slug))
}

func catalogString(ctx context.Context, tr *i18n.Translator, key string) (string, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if tr == nil {
		tr = i18n.TranslatorFrom(ctx)
	}
	if tr == nil {
		return "", false
	}
	if val := tr.T(ctx, key); val != "" && val != key {
		return val, true
	}
	return "", false
}

func titleCase(slug string) string {
	if slug == "" {
		return ""
	}
	s := strings.NewReplacer("_", " ", "-", " ").Replace(slug)
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

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
