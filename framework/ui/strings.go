package ui

import (
	"context"
	"reflect"

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
// Where a key already said the sentence (password show/hide,
// pagination Previous/Next, three of the four tone words) it is
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
	// Success/Warning/Danger reuse the toast keys, whose English is
	// the same word; Information does not reuse ui.toast.info because
	// headless says "Information", not "Info", and the headless
	// wording wins.
	"ToneInfo":    i18nui.KeyToneInfo,
	"ToneSuccess": i18nui.KeyToastSuccess,
	"ToneWarning": i18nui.KeyToastWarning,
	"ToneDanger":  i18nui.KeyToastError,

	// Upload's runtime-substituted sentences. The {name}/{n}/{names}
	// tokens are substituted by the runtime when the reader has
	// picked, never by the server, so the bridge passes the templates
	// through unformatted.
	"FileSelected":  i18nui.KeyFileSelected,
	"FilesSelected": i18nui.KeyFilesSelected,

	// ValidationSummary's heading.
	"ThereIsAProblem": i18nui.KeyValidationProblem,
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
// must keep the placeholders the component formats into.
func StringsFor(ctx context.Context) *headless.Strings {
	if ctx == nil {
		return headless.DefaultStrings()
	}
	w := &headless.Strings{}
	v := reflect.ValueOf(w).Elem()
	t := v.Type()
	for i := range t.NumField() {
		key, mapped := stringsKeys[t.Field(i).Name]
		if !mapped {
			// Unreachable in a green build: the reflection gate
			// refuses an unmapped field at test time.
			continue
		}
		f := v.Field(i)
		if !f.CanSet() {
			panic("ui: headless.Strings." + t.Field(i).Name + " is not settable — every field of Strings must be an exported string")
		}
		f.SetString(i18nui.T(ctx, key))
	}
	return w
}
