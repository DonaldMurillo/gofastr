package ui

import (
	"context"
	"reflect"
	"slices"
	"strings"

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
var stringsKeys = map[string]i18nui.Key{
	// Said by more than one component: an Alert's and a
	// SystemBanner's dismiss control, a Tag's remove control.
	"DismissTitled":  i18nui.KeyDismissTitled,
	"RemoveLabelled": i18nui.KeyTagRemoveLabelled,

	// OptimisticAction and ToggleAction's failed-mutation sentence.
	"ActionFailed": i18nui.KeyActionFailed,

	// Color's swatch, which carries no visible label of its own.
	"PickColor": i18nui.KeyColorPick,

	// Password: the reveal button's two names, then its two visible
	// words. The names reuse the keys PasswordInput already resolved
	// through Ctx; the one-word texts are their own pair.
	"ShowPassword": i18nui.KeyPasswordInputShow,
	"HidePassword": i18nui.KeyPasswordInputHide,
	"RevealShow":   i18nui.KeyPasswordRevealShow,
	"RevealHide":   i18nui.KeyPasswordRevealHide,

	// Pagination's disabled ends.
	"Previous": i18nui.KeyPaginationPrevious,
	"Next":     i18nui.KeyPaginationNext,

	// The tone word said before a SystemBanner's or Alert's title.
	// Their own keys, not ui.toast.*: the toast keys name a surface
	// a host may have translated with toast wording, and nothing in
	// the framework renders them yet, so the first renderer of a key
	// should be the surface its name promises.
	"ToneInfo":    i18nui.KeyToneInfo,
	"ToneSuccess": i18nui.KeyToneSuccess,
	"ToneWarning": i18nui.KeyToneWarning,
	"ToneDanger":  i18nui.KeyToneDanger,

	// Upload's runtime-substituted sentences. The {name}/{n}/{names}
	// tokens are substituted by the runtime when the reader has
	// picked, never by the server, so the bridge passes the templates
	// through unformatted.
	"FileSelected":  i18nui.KeyFileSelected,
	"FilesSelected": i18nui.KeyFilesSelected,

	// ValidationSummary's heading.
	"ThereIsAProblem": i18nui.KeyValidationProblem,

	// The lightbox viewer's accessible name and its three controls.
	// Prev/Next/Download reuse the keys the config-struct Lightbox
	// already resolved through Ctx; the label is new with the headless
	// viewer and its English is the default the config carried.
	"LightboxViewerLabel": i18nui.KeyLightboxLabel,
	"LightboxPrevious":    i18nui.KeyLightboxPrev,
	"LightboxNext":        i18nui.KeyLightboxNext,
	"LightboxDownload":    i18nui.KeyLightboxDownload,

	// Table's sort control on a column with no visible header, and
	// the sentence a changed table announces. The sort-control key is
	// the one ui.DataTable already resolved for the same purpose; the
	// announcement's three keys are new with the headless behaviour
	// and their English is the component's own defaults.
	"TableSortBy":    i18nui.KeyTableSortBy,
	"TableSortedBy":  i18nui.KeyTableSortedBy,
	"SortAscending":  i18nui.KeyTableDirAscending,
	"SortDescending": i18nui.KeyTableDirDescending,

	// The stateful-control family's words. The counter, copy and
	// scheme words reuse the keys their config-struct components
	// already resolved through Ctx (their English is the same word
	// for the same job); the rest are new with the headless
	// primitives and their English is the component's own default.
	"CounterLabel":     i18nui.KeyCounterLabel,
	"CounterDecrement": i18nui.KeyCounterDecrement,
	"CounterIncrement": i18nui.KeyCounterIncrement,
	"BackToTop":        i18nui.KeyHuiBackToTop,
	"NumberDecrement":  i18nui.KeyHuiNumberDecrement,
	"NumberIncrement":  i18nui.KeyHuiNumberIncrement,
	"RangeLow":         i18nui.KeyHuiRangeLow,
	"RangeHigh":        i18nui.KeyHuiRangeHigh,
	"RangeValue":       i18nui.KeyHuiRangeValue,
	"RatingChoice":     i18nui.KeyHuiRatingChoice,
	"TagInputAdd":      i18nui.KeyHuiTagInputAdd,
	"TagInputAdded":    i18nui.KeyHuiTagInputAdded,
	"TagInputRemoved":  i18nui.KeyHuiTagInputRemoved,
	"RepeaterAdd":      i18nui.KeyRepeaterAdd,
	"RepeaterRemove":   i18nui.KeyHuiRepeaterRemove,

	// NotificationBell's spoken count.
	"NotificationCount": i18nui.KeyHuiNotificationCount,

	// StepWizard's three controls, the rail's name and one dot's.
	// The three control words reuse the keys the config-struct wizard
	// already resolved through Ctx; the two sentence shapes are new
	// with the primitive, whose formats are %d-shaped (headless
	// formats at render) rather than the {step}/{total} tokens the
	// config-struct keys carry.
	"StepBack":   i18nui.KeyStepWizardBack,
	"StepNext":   i18nui.KeyStepWizardNext,
	"StepSubmit": i18nui.KeyStepWizardSubmit,
	"StepOf":     i18nui.KeyHuiStepOf,
	"StepName":   i18nui.KeyHuiStepName,
}

// StringsFor resolves a headless.Strings from the request's context:
// every field from its key, through the translator WithI18n put on
// the context. No translator on the ctx yields the English defaults
// for every field, and a translator that misses a key yields English
// for that field — i18nui's own miss semantics, which is what makes a
// partial app catalog safe. A nil ctx returns headless.DefaultStrings().
//
// Format fields keep their %s verbs and runtime-substituted fields
// their {name} tokens: strings travel unformatted and headless
// applies them at render, so a translation may reorder the words but
// must keep the placeholders the component formats into. That rule
// is enforced here, not only documented: a translation whose
// placeholders differ from the English default's (one dropped, one
// added, a %s written as {name}) is refused and the field keeps its
// English, because the alternative is fmt's "%!s(MISSING)" inside an
// accessible name, where nobody sighted would see it. Reordering is a
// translator's right where the substitution is by name; see
// placeholdersMatch for the split.
func StringsFor(ctx context.Context) *headless.Strings {
	w := headless.DefaultStrings()
	if ctx == nil {
		return w
	}
	eachBridgedField(w, func(_ string, f reflect.Value, key i18nui.Key) {
		if s := i18nui.T(ctx, key); placeholdersMatch(f.String(), s) {
			f.SetString(s)
		}
	})
	return w
}

// StringsRefusal is one translation StringsFor will not use, and the
// English it renders instead.
type StringsRefusal struct {
	// Field is the headless.Strings field, Key the catalog key it
	// reads.
	Field string
	Key   i18nui.Key
	// English is what the page will say; Translated is what the
	// catalog offered and the bridge refused.
	English    string
	Translated string
}

// CheckStrings reports every bridged key whose translation on this
// context would be refused for placeholder drift.
//
// It exists because the refusal is otherwise invisible: a catalog that
// drops a %s renders one English sentence among the translated ones,
// on every request, with nothing to notice it by. The fallback is
// deliberate — English beats fmt's "%!s(MISSING)" inside an accessible
// name — but a translator cannot fix what nobody can see. Call this
// once per locale you ship, in a test or at boot, and fail on a
// non-empty result: the drift is a catalog bug, and this is where it
// is cheap to find.
//
// A nil ctx, or one with no translator, has nothing to refuse and
// returns nil. The order follows headless.Strings' field order, so
// the result is stable to print and to diff.
func CheckStrings(ctx context.Context) []StringsRefusal {
	if ctx == nil {
		return nil
	}
	var out []StringsRefusal
	eachBridgedField(headless.DefaultStrings(), func(name string, f reflect.Value, key i18nui.Key) {
		def := f.String()
		if s := i18nui.T(ctx, key); !placeholdersMatch(def, s) {
			out = append(out, StringsRefusal{Field: name, Key: key, English: def, Translated: s})
		}
	})
	return out
}

// eachBridgedField walks the settable string fields of w that the
// bridge maps, in field order. Both the fill and the check go through
// it so neither can walk a different set than the other.
func eachBridgedField(w *headless.Strings, fn func(name string, f reflect.Value, key i18nui.Key)) {
	v := reflect.ValueOf(w).Elem()
	t := v.Type()
	for i := range t.NumField() {
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
		fn(name, f, key)
	}
}

// placeholdersMatch reports whether translated carries exactly the
// placeholders of def. The two kinds are held to different rules,
// because the two substitutions are different:
//
//   - The % verbs are applied by fmt, positionally, so their ORDER is
//     part of the contract: a translation that swaps two verbs swaps
//     the values. An escaped %% is not a verb at all, and neither is a
//     percent sign in prose ("Échec à 100 %."), which is why the scan
//     follows fmt's own grammar instead of taking the two bytes after
//     a %. A field whose English carries no verb is not a format
//     string, so its translation is not held to a verb contract at
//     all: nothing calls Sprintf on it, and a translator writing
//     "100 % sûr" is writing prose, not a placeholder.
//   - The {name} tokens are replaced by name in the runtime
//     (behavior.js replaces "{n}" and "{names}" literally), so a
//     translation may put them in whatever order its grammar wants.
//     Only which tokens appear, and how many times, has to match —
//     refusing a reordered pair would refuse a correct translation.
func placeholdersMatch(def, translated string) bool {
	defVerbs, defNames := placeholdersIn(def)
	trVerbs, trNames := placeholdersIn(translated)
	if len(defVerbs) > 0 && !slices.Equal(defVerbs, trVerbs) {
		return false
	}
	slices.Sort(defNames)
	slices.Sort(trNames)
	return slices.Equal(defNames, trNames)
}

// verbEnd reports where the fmt verb starting at s[i] ends, following
// fmt's grammar: the percent, any flags, a width, an optional
// precision, then the verb letter that makes it a verb. Without that
// letter there is no verb — "100 %." ends the sentence, it does not
// format anything — and an escaped %% is handled by the caller.
func verbEnd(s string, i int) (int, bool) {
	j := i + 1
	for j < len(s) && strings.IndexByte("+-# 0", s[j]) >= 0 {
		j++
	}
	for j < len(s) && s[j] >= '0' && s[j] <= '9' {
		j++
	}
	if j < len(s) && s[j] == '.' {
		j++
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
	}
	if j < len(s) && (s[j] >= 'a' && s[j] <= 'z' || s[j] >= 'A' && s[j] <= 'Z') {
		return j + 1, true
	}
	return 0, false
}

// placeholdersIn splits a string's placeholders into the % verbs, in
// the order fmt will apply them, and the {name} tokens, whose order
// does not bind. It is the same walk framework/headless uses to hold
// its probe words to its defaults.
func placeholdersIn(s string) (verbs, names []string) {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '%':
			if end, ok := verbEnd(s, i); ok {
				verbs = append(verbs, s[i:end])
				i = end - 1
			} else if i+1 < len(s) && s[i+1] == '%' {
				i++ // an escaped %%, and not a verb
			}
		case '{':
			if end := strings.IndexByte(s[i:], '}'); end > 0 {
				names = append(names, s[i:i+end+1])
				i += end
			}
		}
	}
	return verbs, names
}
