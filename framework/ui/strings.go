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
// accessible name, where nobody sighted would see it.
func StringsFor(ctx context.Context) *headless.Strings {
	if ctx == nil {
		return headless.DefaultStrings()
	}
	w := headless.DefaultStrings()
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
		if s := i18nui.T(ctx, key); placeholdersMatch(f.String(), s) {
			f.SetString(s)
		}
	}
	return w
}

// placeholdersMatch reports whether translated carries exactly the
// placeholders of def, in order: the % verbs (an escaped %% is not
// one) and the {name} tokens. Order matters because fmt applies
// positional arguments; a reordered pair would swap the values.
func placeholdersMatch(def, translated string) bool {
	return slices.Equal(placeholdersIn(def), placeholdersIn(translated))
}

// placeholdersIn lists a string's placeholders in order, the same walk
// framework/headless uses to hold its probe words to its defaults.
func placeholdersIn(s string) []string {
	var out []string
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '%':
			if i+1 < len(s) {
				if s[i+1] == '%' {
					i++
					continue
				}
				out = append(out, s[i:i+2])
				i++
			}
		case '{':
			if end := strings.IndexByte(s[i:], '}'); end > 0 {
				out = append(out, s[i:i+end+1])
				i += end
			}
		}
	}
	return out
}
