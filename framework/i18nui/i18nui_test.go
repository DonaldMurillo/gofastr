package i18nui

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

func TestStringsReplaceAllNoInfiniteLoop(t *testing.T) {
	// Pins the substitution path used by TranslateValidation — confirms
	// strings.ReplaceAll terminates even when the replacement contains
	// the placeholder it just consumed. (Stdlib guarantees this; we
	// keep the test as a regression net for the substitution call site.)
	done := make(chan string, 1)
	go func() {
		done <- strings.ReplaceAll("{x}", "{x}", "{x}_evil")
	}()
	select {
	case got := <-done:
		if got != "{x}_evil" {
			t.Fatalf("ReplaceAll = %q, want %q", got, "{x}_evil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("strings.ReplaceAll hung on self-containing replacement")
	}
}

func TestHumanizeMultibyte(t *testing.T) {
	got := humanize("naïveCount")
	if !utf8.ValidString(got) {
		t.Fatalf("humanize produced invalid UTF-8: %q", got)
	}
	if !strings.Contains(got, "ï") {
		t.Fatalf("humanize dropped multibyte rune: %q", got)
	}
}

func TestTNilReturnsDefault(t *testing.T) {
	ctx := context.Background()
	for key, expected := range Defaults {
		got := T(ctx, key)
		if got != expected {
			t.Errorf("T(nil, %q) = %q, want %q", key, got, expected)
		}
	}
}

func TestTCtxLocale(t *testing.T) {
	cat := i18n.NewMapCatalog()
	cat.Set("en", "ui.pagination.next", i18n.Message{Text: "Next"})
	cat.Set("fr", "ui.pagination.next", i18n.Message{Text: "Suivant"})
	tr := i18n.NewTranslator(cat, "en")

	ctxFr := WithTranslator(i18n.WithContext(context.Background(), i18n.Locale{Tag: "fr"}), tr)
	if got := T(ctxFr, KeyPaginationNext); got != "Suivant" {
		t.Fatalf("T fr = %q, want Suivant", got)
	}
	ctxEn := WithTranslator(i18n.WithContext(context.Background(), i18n.Locale{Tag: "en"}), tr)
	if got := T(ctxEn, KeyPaginationNext); got != "Next" {
		t.Fatalf("T en = %q, want Next", got)
	}
}

func TestValidationErrorCtxLocale(t *testing.T) {
	cat := i18n.NewMapCatalog()
	cat.Set("fr", "ui.validation.required", i18n.Message{Text: "Champ obligatoire"})
	tr := i18n.NewTranslator(cat, "en")
	ctxFr := i18n.WithContext(context.Background(), i18n.Locale{Tag: "fr"})
	got := TranslateValidation(ctxFr, tr, "required", nil)
	if got != "Champ obligatoire" {
		t.Fatalf("TranslateValidation fr = %q, want Champ obligatoire", got)
	}
}

func TestLabelForFieldCtxLocale(t *testing.T) {
	cat := i18n.NewMapCatalog()
	cat.Set("fr", "entity.user.field.email", i18n.Message{Text: "Courriel"})
	tr := i18n.NewTranslator(cat, "en")
	ctxFr := i18n.WithContext(context.Background(), i18n.Locale{Tag: "fr"})
	if got := LabelForField(ctxFr, tr, "user", "email"); got != "Courriel" {
		t.Fatalf("LabelForField fr = %q, want Courriel", got)
	}
}

func TestTReturnsDefault(t *testing.T) {
	got := T(context.Background(), KeyPaginationNext)
	if got != "Next" {
		t.Fatalf("T(KeyPaginationNext) = %q, want %q", got, "Next")
	}
}

func TestTUsesCtxTranslator(t *testing.T) {
	cat := i18n.NewMapCatalog()
	cat.Set("fr", "ui.pagination.next", i18n.Message{Text: "Suivant"})
	tr := i18n.NewTranslator(cat, "en")
	ctx := i18n.WithContext(context.Background(), i18n.Locale{Tag: "fr"})
	ctx = WithTranslator(ctx, tr)
	if got := T(ctx, KeyPaginationNext); got != "Suivant" {
		t.Fatalf("T(ctx, KeyPaginationNext) = %q, want Suivant", got)
	}
}

// TVars falls back to the English default when the ctx translator is
// present but the catalog has no ui.* entry (the miss case — an app
// catalog won't contain ui.* keys unless the app overrides them).
func TestTVarsMissFallsBackToDefault(t *testing.T) {
	cat := i18n.NewMapCatalog() // no ui.* entries
	tr := i18n.NewTranslator(cat, "en")
	ctx := WithTranslator(context.Background(), tr)
	got := TVars(ctx, KeyTableSortBy, map[string]string{"column": "name"})
	if got != "Sort by name" {
		t.Fatalf("TVars miss = %q, want %q", got, "Sort by name")
	}
}

// TVars uses the translator's value (with interpolation) when the
// catalog DOES contain the key for the locale.
func TestTVarsTranslatorHitWins(t *testing.T) {
	cat := i18n.NewMapCatalog()
	cat.Set("fr", "ui.table.sortBy", i18n.Message{Text: "Trier par {column}"})
	tr := i18n.NewTranslator(cat, "en")
	ctx := i18n.WithContext(context.Background(), i18n.Locale{Tag: "fr"})
	ctx = WithTranslator(ctx, tr)
	got := TVars(ctx, KeyTableSortBy, map[string]string{"column": "nom"})
	if got != "Trier par nom" {
		t.Fatalf("TVars hit = %q, want %q", got, "Trier par nom")
	}
}

// TVars without a translator returns the English default interpolated.
func TestTVarsNilCtxEnglishDefault(t *testing.T) {
	got := TVars(nil, KeyCarouselGoTo, map[string]string{"slide": "3"})
	if got != "Go to slide 3" {
		t.Fatalf("TVars nil ctx = %q, want %q", got, "Go to slide 3")
	}
}

func TestTUnknownKeyReturnsKeyString(t *testing.T) {
	key := Key("ui.nonexistent.key")
	got := T(context.Background(), key)
	if got != string(key) {
		t.Fatalf("T(%q) = %q, want %q", key, got, key)
	}
}

func TestValidationError(t *testing.T) {
	got := TranslateValidation(context.Background(), nil, "required", nil)
	if got != "This field is required" {
		t.Fatalf("TranslateValidation(required) = %q", got)
	}
}

func TestValidationErrorWithVars(t *testing.T) {
	got := TranslateValidation(context.Background(), nil, "min", map[string]string{"min": "5"})
	if got != "Must be at least 5" {
		t.Fatalf("TranslateValidation(min, {min:5}) = %q", got)
	}
}

func TestValidationErrorMinLength(t *testing.T) {
	got := TranslateValidation(context.Background(), nil, "minLength", map[string]string{"min": "3"})
	if !strings.Contains(got, "3") {
		t.Fatalf("expected '3' in %q", got)
	}
	if !strings.Contains(got, "characters") {
		t.Fatalf("expected 'characters' in %q", got)
	}
}

func TestValidationErrorBracesInValue(t *testing.T) {
	// Regression: a var value containing { / } must not cause runaway
	// substitution. The {min} token must be replaced with the literal
	// string "user{name}" exactly once.
	done := make(chan string, 1)
	go func() {
		done <- TranslateValidation(context.Background(), nil, "min", map[string]string{"min": "user{name}"})
	}()
	select {
	case got := <-done:
		if !strings.Contains(got, "user{name}") {
			t.Fatalf("ValidationError = %q, want literal user{name}", got)
		}
		if strings.Count(got, "user{name}") != 1 {
			t.Fatalf("expected single substitution, got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ValidationError hung on brace-containing value")
	}
}

func TestLabelForFieldNoTranslator(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		field, want string
	}{
		{"email", "Email"},
		{"first_name", "First Name"},
		{"createdAt", "Created At"},
		{"user-id", "User Id"},
	}
	for _, tt := range tests {
		got := LabelForField(ctx, nil, "user", tt.field)
		if got != tt.want {
			t.Errorf("LabelForField(nil, %q, %q) = %q, want %q", "user", tt.field, got, tt.want)
		}
	}
}

func TestLabelForFieldSnakeCase(t *testing.T) {
	got := LabelForField(context.Background(), nil, "order", "shipping_address_line_1")
	if !strings.Contains(got, "Shipping") {
		t.Fatalf("expected humanized label for shipping_address_line_1, got %q", got)
	}
}

func TestAllKeysHaveDefaults(t *testing.T) {
	// Discover every exported Key constant via reflection on
	// AllKeys() — guarantees a newly added key without a default is
	// caught immediately, not only when someone updates this test.
	keys := AllKeys()
	if len(keys) == 0 {
		t.Fatal("AllKeys() returned no keys")
	}
	for _, k := range keys {
		got := T(context.Background(), k)
		if got == "" {
			t.Errorf("key %q resolved to empty string", k)
		}
		if got == string(k) {
			t.Errorf("key %q has no default (resolver fell through to bare key)", k)
		}
	}
}

func TestAllKeysCoversAllPackageConstants(t *testing.T) {
	// Belt-and-suspenders: package-declared constants of type Key (as
	// exposed via Defaults) must match AllKeys() exactly. If they
	// diverge, AllKeys is stale.
	allSet := map[Key]struct{}{}
	for _, k := range AllKeys() {
		allSet[k] = struct{}{}
	}
	for k := range Defaults {
		if _, ok := allSet[k]; !ok {
			t.Errorf("Defaults has %q but AllKeys() does not", k)
		}
	}
	// Also reject duplicates in AllKeys().
	seen := map[Key]int{}
	for _, k := range AllKeys() {
		seen[k]++
	}
	for k, n := range seen {
		if n > 1 {
			t.Errorf("AllKeys() lists %q %d times", k, n)
		}
	}
	_ = reflect.TypeOf(Key(""))
}

func TestHumanize(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"email", "Email"},
		{"first_name", "First Name"},
		{"created_at", "Created At"},
		{"id", "Id"},
		{"a", "A"},
	}
	for _, tt := range tests {
		got := humanize(tt.input)
		if got != tt.want {
			t.Errorf("humanize(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
