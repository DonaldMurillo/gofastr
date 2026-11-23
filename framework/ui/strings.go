package ui

import (
	"context"
	"reflect"
	"slices"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

var stringsKeys = map[string]i18n.Phrase{
	"DismissTitled":  i18nui.DismissTitled,
	"RemoveLabelled": i18nui.TagRemoveLabelled,

	"ActionFailed": i18nui.ActionFailed,

	"PickColor": i18nui.ColorPick,

	"ShowPassword": i18nui.PasswordInputShow,
	"RevealShow":   i18nui.PasswordRevealShow,

	"Previous": i18nui.PaginationPrevious,
	"Next":     i18nui.PaginationNext,

	"ToneInfo":    i18nui.ToneInfo,
	"ToneSuccess": i18nui.ToneSuccess,
	"ToneWarning": i18nui.ToneWarning,
	"ToneDanger":  i18nui.ToneDanger,

	"FileSelected":  i18nui.FileSelected,
	"FilesSelected": i18nui.FilesSelected,

	"ThereIsAProblem": i18nui.ValidationProblem,

	"LightboxViewerLabel": i18nui.LightboxLabel,
	"LightboxPrevious":    i18nui.LightboxPrev,
	"LightboxNext":        i18nui.LightboxNext,
	"LightboxDownload":    i18nui.LightboxDownload,

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

	"NotificationCount": i18nui.HuiNotificationCount,

	"StepBack":   i18nui.StepWizardBack,
	"StepNext":   i18nui.StepWizardNext,
	"StepSubmit": i18nui.StepWizardSubmit,
	"StepOf":     i18nui.HuiStepOf,
	"StepName":   i18nui.HuiStepName,

	"TableOfContentsLabel": i18nui.HuiTableOfContentsLabel,

	"CarouselSlide":     i18nui.HuiCarouselSlide,
	"CarouselGoTo":      i18nui.HuiCarouselGoToSlide,
	"CarouselRole":      i18nui.HuiCarouselRole,
	"CarouselSlideRole": i18nui.HuiCarouselSlideRole,
	"CarouselPause":     i18nui.HuiCarouselPause,
	"PaneSecondary":     i18nui.HuiPaneSecondary,
	"PaneTertiary":      i18nui.HuiPaneTertiary,
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

	"BreadcrumbsLabel": i18nui.HuiBreadcrumbsLabel,

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

	"MultiSelectPlaceholder": i18nui.HuiMultiSelectPlaceholder,

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

// StringsFor returns headless.Strings in ctx's language, field by field with
// English for a missing key or a translation whose placeholders differ.
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
