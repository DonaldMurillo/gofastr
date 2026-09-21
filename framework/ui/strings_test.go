package ui

import (
	"context"
	"reflect"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// The Strings bridge gates. stringsKeys is the one table from
// headless.Strings field to i18nui key; these tests hold the three
// contracts the table exists for:
//
//  1. Coverage (the reflection gate): every exported string field of
//     headless.Strings is mapped, every entry names a real field, and
//     the two sets are the same size — a map cannot hold a duplicate,
//     so no field is mapped twice. A field added to headless fails
//     here until the bridge maps it.
//  2. English is the English headless already says: with no
//     translator (and for nil ctx), every field equals
//     headless.DefaultStrings(), so the no-translator page is the
//     page the headless goldens pin.
//  3. Translation flows and misses fall back: with a translator every
//     field resolves through the catalog, a partial catalog yields a
//     mix (i18nui's own miss semantics, unchanged), and format
//     placeholders travel unformatted.

// stringsCtx builds a ctx with a French translator. entries become
// catalog messages for the "fr" locale; keys not listed miss, which is
// the partial-catalog case.
func stringsCtx(entries map[i18nui.Key]string) context.Context {
	cat := i18n.NewMapCatalog()
	for k, v := range entries {
		cat.Set("fr", string(k), i18n.Message{Text: v})
	}
	tr := i18n.NewTranslator(cat, "en")
	ctx := i18n.WithContext(context.Background(), i18n.Locale{Tag: "fr"})
	return i18nui.WithTranslator(ctx, tr)
}

// everyHeadlessStringField walks headless.Strings and calls fn for
// each exported string field with its name and current value.
func everyHeadlessStringField(t *testing.T, w *headless.Strings, fn func(name, val string)) {
	t.Helper()
	v := reflect.ValueOf(w).Elem()
	typ := v.Type()
	for i := range typ.NumField() {
		f := typ.Field(i)
		if f.PkgPath != "" || f.Type.Kind() != reflect.String {
			t.Fatalf("headless.Strings.%s is not an exported string field — the bridge's walk and this test both assume the struct is flat", f.Name)
		}
		fn(f.Name, v.Field(i).String())
	}
}

// TestStringsForMapsEveryField is the reflection gate: the table and
// the struct are the same set of names.
func TestStringsForMapsEveryField(t *testing.T) {
	typ := reflect.TypeFor[headless.Strings]()
	fields := map[string]bool{}
	for i := range typ.NumField() {
		f := typ.Field(i)
		if f.PkgPath != "" || f.Type.Kind() != reflect.String {
			t.Fatalf("headless.Strings.%s is not an exported string field", f.Name)
		}
		fields[f.Name] = true
	}
	for name := range stringsKeys {
		if !fields[name] {
			t.Errorf("stringsKeys maps %q, which headless.Strings does not carry (a typo silently translates nothing)", name)
		}
	}
	for name := range fields {
		if _, ok := stringsKeys[name]; !ok {
			t.Errorf("headless.Strings.%s is not mapped by the bridge — add it to stringsKeys or the field stays English on every translated page", name)
		}
	}
	if len(stringsKeys) != len(fields) {
		t.Errorf("stringsKeys has %d entries for %d fields; with no name mismatch above this means a duplicate slipped in", len(stringsKeys), len(fields))
	}
}

// TestStringsForNoTranslatorIsHeadlessEnglish: without a translator
// every field is the English default headless itself ships, field by
// field — i18nui.Defaults and headless's defaultStrings cannot drift
// apart without this failing.
func TestStringsForNoTranslatorIsHeadlessEnglish(t *testing.T) {
	want := headless.DefaultStrings()
	got := StringsFor(context.Background())
	if got == want {
		t.Fatal("StringsFor returned headless.DefaultStrings()'s own pointer — it must build its own value")
	}
	everyHeadlessStringField(t, got, func(name, val string) {
		w := reflect.ValueOf(want).Elem().FieldByName(name).String()
		if val != w {
			t.Errorf("%s = %q, want headless default %q (i18nui default drifted from headless English)", name, val, w)
		}
	})
}

// TestStringsForNilCtxIsHeadlessEnglish: nil ctx is the nil-prop
// case, the English defaults.
func TestStringsForNilCtxIsHeadlessEnglish(t *testing.T) {
	want := headless.DefaultStrings()
	got := StringsFor(nil)
	everyHeadlessStringField(t, got, func(name, val string) {
		w := reflect.ValueOf(want).Elem().FieldByName(name).String()
		if val != w {
			t.Errorf("%s = %q, want headless default %q", name, val, w)
		}
	})
}

// TestStringsForTranslatesEveryField: a catalog entry for every
// mapped key comes through on every field, proving the table is not
// just listed but filled. The probe values carry the field's
// placeholders where it has them, so the pass-through (no
// interpolation, no Sprintf) is asserted on the same fields.
func TestStringsForTranslatesEveryField(t *testing.T) {
	entries := map[i18nui.Key]string{}
	for _, key := range i18nui.AllKeys() {
		entries[key] = "fr·" + string(key)
	}
	// Placeholders on the format fields: the translation keeps the
	// tokens headless formats into, and the bridge must hand them
	// through untouched (a probe without them would be refused, which
	// TestStringsForRefusesPlaceholderDrift covers on its own).
	entries[i18nui.KeyDismissTitled] = "fr·Fermer : %s"
	entries[i18nui.KeyTagRemoveLabelled] = "fr·Retirer %s"
	entries[i18nui.KeyColorPick] = "fr·Choisir %s"
	entries[i18nui.KeyFileSelected] = "fr·{name} choisi."
	entries[i18nui.KeyFilesSelected] = "fr·{n} fichiers : {names}."
	entries[i18nui.KeyTableSortBy] = "fr·Trier par {column}"

	got := StringsFor(stringsCtx(entries))
	everyHeadlessStringField(t, got, func(name, val string) {
		key := stringsKeys[name]
		want := entries[key]
		if val != want {
			t.Errorf("%s = %q, want %q", name, val, want)
		}
	})
}

// TestStringsForPartialCatalogKeepsEnglishForMisses: an app catalog
// with a handful of ui.* overrides yields a mix — hits translated,
// misses English — which is what makes shipping a partial catalog
// safe (headless's own Resolve semantics, unchanged by the bridge).
func TestStringsForPartialCatalogKeepsEnglishForMisses(t *testing.T) {
	got := StringsFor(stringsCtx(map[i18nui.Key]string{
		i18nui.KeyPaginationNext: "Suivant",
		i18nui.KeyToneInfo:       "Renseignements",
	}))
	for name, want := range map[string]string{
		"Next":            "Suivant",            // hit
		"Previous":        "Previous",           // miss: English
		"ToneInfo":        "Renseignements",     // hit
		"ToneDanger":      "Error",              // miss: English
		"ShowPassword":    "Show password",      // miss: English
		"ThereIsAProblem": "There is a problem", // miss: English
	} {
		if val := reflect.ValueOf(got).Elem().FieldByName(name).String(); val != want {
			t.Errorf("%s = %q, want %q", name, val, want)
		}
	}
}

// TestStringsForDefaultsArePairwiseDistinct makes the English-parity
// oracle sufficient for identity: TestStringsForNoTranslatorIsHeadlessEnglish
// catches two fields mapped to each other's keys only while their
// English differs, so two fields sharing a default would let a swap
// through every gate. This holds the precondition.
func TestStringsForDefaultsArePairwiseDistinct(t *testing.T) {
	seen := map[string]string{}
	everyHeadlessStringField(t, headless.DefaultStrings(), func(name, val string) {
		if other, dup := seen[val]; dup {
			t.Errorf("headless.Strings.%s and .%s share the default %q; the bridge's identity oracle needs distinct English, give one of them its own words or pin their keys by name", name, other, val)
		}
		seen[val] = name
	})
}

// TestPlaceholdersMatch holds the two halves of the placeholder rule
// directly, because no pair of English defaults carries two % verbs
// and the swap case cannot be expressed through the catalog.
func TestPlaceholdersMatch(t *testing.T) {
	cases := []struct {
		name, def, translated string
		want                  bool
	}{
		{"identical", "Dismiss: %s", "Fermer : %s", true},
		{"verb dropped", "Dismiss: %s", "Fermer", false},
		{"verb added", "Pick %s", "Choisir %s parmi %s", false},
		{"verb respelled as a token", "Remove %s", "Retirer {label}", false},
		// fmt applies verbs positionally, so their order is contract.
		{"verbs swapped", "%s of %d", "%d de %s", false},
		{"verbs in order", "%s of %d", "%s sur %d", true},
		// The runtime replaces these by name, so their order is the
		// translator's business.
		{"named tokens reordered", "{n} files selected: {names}.", "{names} : {n} fichiers.", true},
		{"named token dropped", "{n} files selected: {names}.", "{n} fichiers.", false},
		{"named token added", "{name} selected.", "{name} {n} choisis.", false},
		{"named token repeated", "{name} selected.", "{name} et {name}.", false},
		{"no placeholder either side", "Could not save.", "Échec.", true},
		{"escaped percent is not a verb", "100%% sure", "sûr à 100%%", true},
		// A percent sign in prose is not a placeholder. French sets a
		// space before it, and the sentence often ends right after —
		// neither is a verb, because no verb letter follows.
		{"percent ending a sentence", "Could not save.", "Échec à 100 %.", true},
		{"percent before a space", "Could not save.", "100 % sûr", true},
		{"percent with a width but no verb", "Could not save.", "remise de 50 %", true},
		// A field whose English carries a verb still holds its
		// translation to that verb.
		{"format field still needs its verb", "Dismiss: %s", "Fermer à 100 %.", false},
		// The discriminating case for the grammar: the verb is there
		// AND the sentence carries a prose percent. Reading the two
		// bytes after a % would see a second verb and refuse a correct
		// translation.
		{"verb kept, prose percent alongside", "Pick %s", "Choisir %s (100 %) environ", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := placeholdersMatch(c.def, c.translated); got != c.want {
				t.Errorf("placeholdersMatch(%q, %q) = %v, want %v", c.def, c.translated, got, c.want)
			}
		})
	}
}

// TestStringsForRefusesPlaceholderDrift: a translation that loses,
// gains or respells a placeholder is refused and the field keeps its
// English, because headless formats these with fmt and the result
// lands in accessible names.
func TestStringsForRefusesPlaceholderDrift(t *testing.T) {
	got := StringsFor(stringsCtx(map[i18nui.Key]string{
		i18nui.KeyDismissTitled:     "Fermer",              // dropped %s
		i18nui.KeyTagRemoveLabelled: "Retirer {label}",     // %s written as a token
		i18nui.KeyColorPick:         "Choisir %s parmi %s", // one added
		i18nui.KeyFilesSelected:     "{n} fichiers.",       // a named token dropped
		i18nui.KeyFileSelected:      "{name} choisi.",      // kept: accepted
		i18nui.KeyActionFailed:      "Échec. 100%% sûr.",   // no placeholder either side: accepted
	}))
	want := map[string]string{
		"DismissTitled":  "Dismiss: %s",
		"RemoveLabelled": "Remove %s",
		"PickColor":      "Pick %s",
		"FilesSelected":  "{n} files selected: {names}.",
		"FileSelected":   "{name} choisi.",
		"ActionFailed":   "Échec. 100%% sûr.",
	}
	for name, w := range want {
		if val := reflect.ValueOf(got).Elem().FieldByName(name).String(); val != w {
			t.Errorf("%s = %q, want %q", name, val, w)
		}
	}
}

// TestStringsForAcceptsProseWithAPercentSign: ActionFailed carries no
// verb, so it is not a format string and nothing calls Sprintf on it.
// A translator writing a percent sign in that sentence is writing
// prose, and the bridge must let it through.
func TestStringsForAcceptsProseWithAPercentSign(t *testing.T) {
	const prose = "Échec de l'enregistrement à 100 %. Réessayez."
	got := StringsFor(stringsCtx(map[i18nui.Key]string{
		i18nui.KeyActionFailed: prose,
	}))
	if got.ActionFailed != prose {
		t.Errorf("ActionFailed = %q, want the translation %q — a percent in prose is not a placeholder", got.ActionFailed, prose)
	}
	if drift := CheckStrings(stringsCtx(map[i18nui.Key]string{
		i18nui.KeyActionFailed: prose,
	})); len(drift) != 0 {
		t.Errorf("CheckStrings reported %+v for prose with a percent sign", drift)
	}
}

// TestStringsForAcceptsReorderedNamedTokens: the runtime substitutes
// {name} tokens by name, so a translation whose grammar puts them in
// another order is a correct translation and must reach the page.
func TestStringsForAcceptsReorderedNamedTokens(t *testing.T) {
	const reordered = "{names} : {n} fichiers choisis."
	got := StringsFor(stringsCtx(map[i18nui.Key]string{
		i18nui.KeyFilesSelected: reordered,
	}))
	if got.FilesSelected != reordered {
		t.Errorf("FilesSelected = %q, want the reordered translation %q", got.FilesSelected, reordered)
	}
}

// TestCheckStringsNamesEveryRefusal: the fallback is deliberate, but
// invisible on its own — a catalog that drops a placeholder renders
// one English sentence among the translated ones on every request and
// nothing says so. CheckStrings is how a host finds that in a test
// instead of in production.
func TestCheckStringsNamesEveryRefusal(t *testing.T) {
	ctx := stringsCtx(map[i18nui.Key]string{
		i18nui.KeyDismissTitled:  "Fermer",         // drops the %s
		i18nui.KeyFilesSelected:  "{n} fichiers.",  // drops {names}
		i18nui.KeyPaginationNext: "Suivant",        // fine: no placeholder
		i18nui.KeyFileSelected:   "{name} choisi.", // fine: keeps it
	})
	got := CheckStrings(ctx)
	if len(got) != 2 {
		t.Fatalf("CheckStrings returned %d refusal(s), want 2: %+v", len(got), got)
	}
	byField := map[string]StringsRefusal{}
	for _, r := range got {
		byField[r.Field] = r
	}
	d, ok := byField["DismissTitled"]
	if !ok {
		t.Fatalf("DismissTitled not reported: %+v", got)
	}
	if d.Key != i18nui.KeyDismissTitled || d.English != "Dismiss: %s" || d.Translated != "Fermer" {
		t.Errorf("DismissTitled refusal = %+v, want the key, the English default and the offered translation", d)
	}
	if _, ok := byField["FilesSelected"]; !ok {
		t.Errorf("FilesSelected not reported: %+v", got)
	}
	// What the page renders is unchanged: the refused fields stay
	// English and the accepted ones are translated.
	w := StringsFor(ctx)
	if w.DismissTitled != "Dismiss: %s" || w.Next != "Suivant" {
		t.Errorf("StringsFor disagreed with CheckStrings: DismissTitled=%q Next=%q", w.DismissTitled, w.Next)
	}
}

// TestCheckStringsCleanCatalogIsEmpty: a catalog that keeps every
// placeholder, and a ctx with no translator at all, have nothing to
// report — so a host can fail its test on any non-empty result.
func TestCheckStringsCleanCatalogIsEmpty(t *testing.T) {
	entries := map[i18nui.Key]string{}
	for _, key := range i18nui.AllKeys() {
		entries[key] = "fr·" + string(key)
	}
	entries[i18nui.KeyDismissTitled] = "fr·Fermer : %s"
	entries[i18nui.KeyTagRemoveLabelled] = "fr·Retirer %s"
	entries[i18nui.KeyColorPick] = "fr·Choisir %s"
	entries[i18nui.KeyFileSelected] = "fr·{name} choisi."
	entries[i18nui.KeyFilesSelected] = "fr·{names} : {n}."
	entries[i18nui.KeyTableSortBy] = "fr·Trier par {column}"
	if got := CheckStrings(stringsCtx(entries)); len(got) != 0 {
		t.Errorf("a catalog that keeps every placeholder reported %d refusal(s): %+v", len(got), got)
	}
	if got := CheckStrings(context.Background()); len(got) != 0 {
		t.Errorf("no translator reported %d refusal(s): %+v", len(got), got)
	}
	if got := CheckStrings(nil); got != nil {
		t.Errorf("nil ctx returned %+v, want nil", got)
	}
}
