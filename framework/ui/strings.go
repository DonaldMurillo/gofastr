package ui

import (
	"context"
	"reflect"
	"slices"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// ─── The Strings bridge ────────────────────────────────────────────
//
// headless.Strings is the typed table of every string a headless
// component says; framework/i18nui is where the framework's
// translated strings live. This file is the one place the two meet:
// a field-to-key table, and StringsFor, which resolves each field
// through the request's translator. Components rebuilt on headless
// (the form family, PR B) take `Strings: ui.StringsFor(r.Context())`
// and say their words in the reader's language with no per-component
// wiring — the same deal the Ctx field gives the config-struct
// components.

// stringsKeys maps each headless.Strings field name to the i18nui key
// that says it. Written once; the reflection gate in strings_test.go
// walks it against the struct in both directions, so a field added to
// headless.Strings fails the build here until the bridge maps it, and
// a map entry naming a field the struct does not carry (a typo) fails
// the same test rather than silently translating nothing.
//
// Where an existing consumer already says the same word for the
// same job (password show/hide, pagination Previous/Next) its key is
// reused; the rest were added with headless's own English as their
// defaults, and the bridge test pins those defaults byte-for-byte
// against headless.DefaultStrings, so the no-translator page is the
// page the goldens pin.
var stringsKeys = map[string]i18n.Phrase{
	// Said by more than one component: an Alert's and a
	// SystemBanner's dismiss control, a Tag's remove control.
	"DismissTitled":  i18nui.DismissTitled,
	"RemoveLabelled": i18nui.TagRemoveLabelled,

	// OptimisticAction and ToggleAction's failed-mutation sentence.
	"ActionFailed": i18nui.ActionFailed,

	// Color's swatch, which carries no visible label of its own.
	"PickColor": i18nui.ColorPick,

	// Password: the reveal button's one name, then its one visible
	// word. It is a toggle button, so neither changes with its state.
	"ShowPassword": i18nui.PasswordInputShow,
	"RevealShow":   i18nui.PasswordRevealShow,

	// Pagination's disabled ends.
	"Previous": i18nui.PaginationPrevious,
	"Next":     i18nui.PaginationNext,

	// The tone word said before a SystemBanner's or Alert's title.
	// Their own keys, not ui.toast.*: the toast keys name a surface
	// a host may have translated with toast wording, and nothing in
	// the framework renders them yet, so the first renderer of a key
	// should be the surface its name promises.
	"ToneInfo":    i18nui.ToneInfo,
	"ToneSuccess": i18nui.ToneSuccess,
	"ToneWarning": i18nui.ToneWarning,
	"ToneDanger":  i18nui.ToneDanger,

	// Upload's runtime-substituted sentences. The {{name}}/{{n}}/{{names}}
	// placeholders are filled by the runtime when the reader has
	// picked, never by the server, so the bridge passes the templates
	// through unformatted.
	"FileSelected":  i18nui.FileSelected,
	"FilesSelected": i18nui.FilesSelected,

	// ValidationSummary's heading.
	"ThereIsAProblem": i18nui.ValidationProblem,

	// The lightbox viewer's accessible name and its three controls.
	// Prev/Next/Download reuse the keys the config-struct Lightbox
	// already resolved through Ctx; the label is new with the headless
	// viewer and its English is the default the config carried.
	"LightboxViewerLabel": i18nui.LightboxLabel,
	"LightboxPrevious":    i18nui.LightboxPrev,
	"LightboxNext":        i18nui.LightboxNext,
	"LightboxDownload":    i18nui.LightboxDownload,

	// Table's sort control on a column with no visible header, and
	// the sentence a changed table announces. The sort-control key is
	// the one ui.DataTable already resolved for the same purpose; the
	// announcement's three keys are new with the headless behaviour
	// and their English is the component's own defaults.
	"TableSortBy":         i18nui.TableSortBy,
	"TableSelectAll":      i18nui.TableSelectAll,
	"SelectionCount":      i18nui.SelectionCount,
	"SelectionClear":      i18nui.SelectionClear,
	"SelectionCopy":       i18nui.SelectionCopy,
	"SelectionCopied":     i18nui.SelectionCopied,
	"SelectionCopyFailed": i18nui.SelectionCopyFailed,
	"TableSortedBy":       i18nui.TableSortedBy,
	"SortAscending":       i18nui.TableDirAscending,
	"SortDescending":      i18nui.TableDirDescending,

	// The stateful-control family's words. The counter, copy and
	// scheme words reuse the keys their config-struct components
	// already resolved through Ctx (their English is the same word
	// for the same job); the rest are new with the headless
	// primitives and their English is the component's own default.
	"CounterLabel":     i18nui.CounterLabel,
	"CounterDecrement": i18nui.CounterDecrement,
	"CounterIncrement": i18nui.CounterIncrement,
	"BackToTop":        i18nui.HuiBackToTop,
	"NumberDecrement":  i18nui.HuiNumberDecrement,
	"NumberIncrement":  i18nui.HuiNumberIncrement,
	"RangeLow":         i18nui.HuiRangeLow,
	"RangeHigh":        i18nui.HuiRangeHigh,
	"RangeValue":       i18nui.HuiRangeValue,
	"RatingChoice":     i18nui.HuiRatingChoice,
	"TagInputAdd":      i18nui.HuiTagInputAdd,
	"TagInputAdded":    i18nui.HuiTagInputAdded,
	"TagInputRemoved":  i18nui.HuiTagInputRemoved,
	"RepeaterAdd":      i18nui.RepeaterAdd,
	"RepeaterRemove":   i18nui.HuiRepeaterRemove,

	// NotificationBell's spoken count.
	"NotificationCount": i18nui.HuiNotificationCount,

	// StepWizard's three controls, the rail's name and one dot's.
	// The three control words reuse the keys the config-struct wizard
	// already resolved through Ctx; the two sentence shapes are new
	// with the primitive, which fills them at render.
	"StepBack":   i18nui.StepWizardBack,
	"StepNext":   i18nui.StepWizardNext,
	"StepSubmit": i18nui.StepWizardSubmit,
	"StepOf":     i18nui.HuiStepOf,
	"StepName":   i18nui.HuiStepName,

	// TableOfContents' landmark name. New with the primitive; its
	// English is the word the config-struct TOC hardcoded.
	"TableOfContentsLabel": i18nui.HuiTableOfContentsLabel,

	// Combobox's status sentences and listbox noun. New with the
	// primitive; the config-struct pattern hardcoded the first three
	// in English.
	"CarouselSlide": i18nui.HuiCarouselSlide,
	"CarouselGoTo":  i18nui.HuiCarouselGoToSlide,
	// The carousel's roledescriptions and PaneHost's default region
	// names: words a screen reader speaks that the primitives once
	// wrote in English past Strings.
	"CarouselRole":      i18nui.HuiCarouselRole,
	"CarouselSlideRole": i18nui.HuiCarouselSlideRole,
	"PaneSecondary":     i18nui.HuiPaneSecondary,
	"PaneTertiary":      i18nui.HuiPaneTertiary,
	// The phone drawer's hamburger when its DrawerLabel is empty: the
	// phrase ui.Sidebar already names its own trigger with.
	"SidebarOpenDrawer": i18nui.SidebarOpenDrawer,

	"ComboboxLoading":      i18nui.HuiComboboxLoading,
	"ComboboxNoResults":    i18nui.HuiComboboxNoResults,
	"ComboboxResultCount":  i18nui.HuiComboboxResultCount,
	"ComboboxResultsLabel": i18nui.HuiComboboxResultsLabel,

	"JSONObject":      i18nui.HuiJSONObject,
	"JSONArray":       i18nui.HuiJSONArray,
	"JSONNull":        i18nui.HuiJSONNull,
	"JSONTrue":        i18nui.HuiJSONTrue,
	"JSONFalse":       i18nui.HuiJSONFalse,
	"JSONEmptyObject": i18nui.HuiJSONEmptyObj,
	"JSONEmptyArray":  i18nui.HuiJSONEmptyArr,

	"JSONTruncated": i18nui.HuiJSONTruncated,

	// Breadcrumbs' landmark name. New with the primitive; its English
	// is the word the pattern hardcoded.
	"BreadcrumbsLabel": i18nui.HuiBreadcrumbsLabel,

	// SortableList's row names and the module's announcements. New
	// with the primitive; the {{label}}/{{list}}/{{position}} placeholders are
	// written in by the module when the move has happened.
	"SortableItemRole":          i18nui.HuiSortableItemRole,
	"SortableDragLabel":         i18nui.HuiSortableDragLabel,
	"SortableGrabbed":           i18nui.HuiSortableGrabbed,
	"SortablePosition":          i18nui.HuiSortablePosition,
	"SortableMoved":             i18nui.HuiSortableMoved,
	"SortableSaved":             i18nui.HuiSortableSaved,
	"SortableReverted":          i18nui.HuiSortableReverted,
	"SortableCancelled":         i18nui.HuiSortableCancelled,
	"SortableConflictReverted":  i18nui.HuiSortableConflictReverted,
	"SortableConflictRefreshed": i18nui.HuiSortableConflictRefreshed,

	// MultiSelect's empty-state word. Its chips' remove controls say
	// RemoveLabelled, filled by the module when it builds the chip.
	"MultiSelectPlaceholder": i18nui.HuiMultiSelectPlaceholder,

	// DatePicker's nav names, a range's input labels and the module's
	// pick announcements, {{date}}/{{start}}/{{end}} filled at say-time.
	"DatePickerPrevMonth": i18nui.HuiDatePickerPrevMonth,
	"DatePickerNextMonth": i18nui.HuiDatePickerNextMonth,
	"DatePickerPrevYear":  i18nui.HuiDatePickerPrevYear,
	"DatePickerNextYear":  i18nui.HuiDatePickerNextYear,
	"DatePickerStart":     i18nui.HuiDatePickerStart,
	"DatePickerEnd":       i18nui.HuiDatePickerEnd,
	"DatePickerChosen":    i18nui.HuiDatePickerChosen,
	"DatePickerChooseEnd": i18nui.HuiDatePickerChooseEnd,
	"DatePickerRange":     i18nui.HuiDatePickerRange,

	"DatePickerMonthFailed": i18nui.HuiDatePickerFailed,
}

// StringsFor resolves a headless.Strings from the request's context:
// every field from its key, through the translator WithI18n put on
// the context. No translator on the ctx yields the English defaults
// for every field, and a translator that misses a key yields English
// for that field — i18nui's own miss semantics, which is what makes a
// partial app catalog safe. A nil ctx returns headless.DefaultStrings().
//
// Fields with a value to put in keep their {{name}} placeholders: the
// strings travel unfilled and headless or the runtime fills them, so a
// translation may move a placeholder but must keep the set. A
// translation naming a different set is refused and the field keeps
// its English, because the alternative is a literal "{{n}}" or a lost
// count inside an accessible name, where nobody sighted would see it.
// i18n.CheckCatalog reports the same drift in a test, where it is
// cheap to fix.
//
// The result also carries ctx for numbers (headless.Strings.Number), so
// a counter or a step number reads the way the reader writes numbers.
func StringsFor(ctx context.Context) *headless.Strings {
	w := headless.DefaultStrings()
	if ctx == nil {
		return w
	}
	w = w.ForRequest(ctx)
	v := reflect.ValueOf(w).Elem()
	t := v.Type()
	for i := range t.NumField() {
		if !t.Field(i).IsExported() {
			continue
		}
		name := t.Field(i).Name
		key, mapped := stringsKeys[name]
		if !mapped {
			// Unreachable in a green build: the reflection gate
			// refuses an unmapped field at test time.
			continue
		}
		f := v.Field(i)
		if !f.CanSet() {
			panic("ui: headless.Strings." + name + " is not settable — every field of Strings must be an exported string")
		}
		if s := key.Text(ctx); slices.Equal(i18n.Placeholders(f.String()), i18n.Placeholders(s)) {
			f.SetString(s)
		}
	}
	return w
}
