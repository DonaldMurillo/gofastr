package i18nui

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestHumanizeMultibyte(t *testing.T) {
	got := humanize("naïveCount")
	if !utf8.ValidString(got) {
		t.Fatalf("humanize produced invalid UTF-8: %q", got)
	}
	if !strings.Contains(got, "ï") {
		t.Fatalf("humanize dropped multibyte rune: %q", got)
	}
}

// Every ui.* phrase writes its placeholders as {{name}}: one spelling a
// translator learns once, which the catalog check and the runtime fill
// both read. A printf verb or a single-brace token is the old split.
func TestUIPhrasesUseNamedPlaceholders(t *testing.T) {
	verb := regexp.MustCompile(`%[-+# 0-9.]*[a-zA-Z]`)
	single := regexp.MustCompile(`(^|[^{])\{[A-Za-z_][A-Za-z0-9_]*\}($|[^}])`)
	n := 0
	for key, en := range i18n.Phrases() {
		if !strings.HasPrefix(key, "ui.") {
			continue
		}
		n++
		if verb.MatchString(en) {
			t.Errorf("%s = %q carries a printf verb; write {{name}}", key, en)
		}
		if single.MatchString(en) {
			t.Errorf("%s = %q carries a {name} token; write {{name}}", key, en)
		}
	}
	if n < 500 {
		t.Fatalf("only %d ui.* phrases defined; the package's vars did not register", n)
	}
}

func TestTCtxLocale(t *testing.T) {
	cat := i18n.NewMapCatalog()
	cat.Set("en", "ui.pagination.next", i18n.Message{Text: "Next"})
	cat.Set("fr", "ui.pagination.next", i18n.Message{Text: "Suivant"})
	tr := i18n.NewTranslator(cat, "en")

	ctxFr := i18n.WithTranslator(i18n.WithContext(context.Background(), i18n.Locale{Tag: "fr"}), tr)
	if got := PaginationNext.Text(ctxFr); got != "Suivant" {
		t.Fatalf("T fr = %q, want Suivant", got)
	}
	ctxEn := i18n.WithTranslator(i18n.WithContext(context.Background(), i18n.Locale{Tag: "en"}), tr)
	if got := PaginationNext.Text(ctxEn); got != "Next" {
		t.Fatalf("T en = %q, want Next", got)
	}
}

func TestFieldLabelCtxLocale(t *testing.T) {
	cat := i18n.NewMapCatalog()
	cat.Set("fr", "entity.user.fields.email.label", i18n.Message{Text: "Courriel"})
	tr := i18n.NewTranslator(cat, "en")
	ctxFr := i18n.WithContext(context.Background(), i18n.Locale{Tag: "fr"})
	if got := FieldLabel(ctxFr, tr, "user", "email", ""); got != "Courriel" {
		t.Fatalf("FieldLabel fr = %q, want Courriel", got)
	}
}

func TestTReturnsDefault(t *testing.T) {
	got := PaginationNext.Text(context.Background())
	if got != "Next" {
		t.Fatalf("T(PaginationNext) = %q, want %q", got, "Next")
	}
}

func TestTUsesCtxTranslator(t *testing.T) {
	cat := i18n.NewMapCatalog()
	cat.Set("fr", "ui.pagination.next", i18n.Message{Text: "Suivant"})
	tr := i18n.NewTranslator(cat, "en")
	ctx := i18n.WithContext(context.Background(), i18n.Locale{Tag: "fr"})
	ctx = i18n.WithTranslator(ctx, tr)
	if got := PaginationNext.Text(ctx); got != "Suivant" {
		t.Fatalf("PaginationNext.Text(ctx) = %q, want Suivant", got)
	}
}

// Text falls back to the English default when the ctx translator is
// present but the catalog has no ui.* entry (the miss case, an app
// catalog won't contain ui.* keys unless the app overrides them).
func TestTextMissFallsBackToDefault(t *testing.T) {
	cat := i18n.NewMapCatalog() // no ui.* entries
	tr := i18n.NewTranslator(cat, "en")
	ctx := i18n.WithTranslator(context.Background(), tr)
	got := TableSortBy.Text(ctx, map[string]any{"column": "name"})
	if got != "Sort by name" {
		t.Fatalf("Text miss = %q, want %q", got, "Sort by name")
	}
}

// Text uses the translator's value (with interpolation) when the
// catalog DOES contain the key for the locale.
func TestTextTranslatorHitWins(t *testing.T) {
	cat := i18n.NewMapCatalog()
	cat.Set("fr", "ui.table.sortBy", i18n.Message{Text: "Trier par {{column}}"})
	tr := i18n.NewTranslator(cat, "en")
	ctx := i18n.WithContext(context.Background(), i18n.Locale{Tag: "fr"})
	ctx = i18n.WithTranslator(ctx, tr)
	got := TableSortBy.Text(ctx, map[string]any{"column": "nom"})
	if got != "Trier par nom" {
		t.Fatalf("Text hit = %q, want %q", got, "Trier par nom")
	}
}

// Text without a translator returns the English default interpolated.
func TestTextNilCtxEnglishDefault(t *testing.T) {
	got := CarouselGoTo.Text(nil, map[string]any{"slide": "3"})
	if got != "Go to slide 3" {
		t.Fatalf("Text nil ctx = %q, want %q", got, "Go to slide 3")
	}
}

func TestFieldLabelNoTranslator(t *testing.T) {
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
		got := FieldLabel(ctx, nil, "user", tt.field, "")
		if got != tt.want {
			t.Errorf("FieldLabel(nil, %q, %q) = %q, want %q", "user", tt.field, got, tt.want)
		}
	}
}

func TestFieldLabelSnakeCase(t *testing.T) {
	got := FieldLabel(context.Background(), nil, "order", "shipping_address_line_1", "")
	if !strings.Contains(got, "Shipping") {
		t.Fatalf("expected humanized label for shipping_address_line_1, got %q", got)
	}
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

// HTML escapes the translated text, drops markup values into their
// placeholders in one pass (a value holding "{{verb}}" stays as written),
// and leaves an unknown placeholder as text.
func TestHTMLEscapesTextNotValues(t *testing.T) {
	cat := i18n.NewMapCatalog()
	cat.Set("fr", AdminActivityLine.Key(), i18n.Message{Text: "<i>{{actor}}</i> a {{verb}} {{record}} {{nope}} {{x {{verb}} <u>"})
	ctx := i18n.WithTranslator(i18n.WithContext(context.Background(), i18n.Locale{Tag: "fr"}), i18n.NewTranslator(cat, "en"))
	got := HTML(ctx, AdminActivityLine, map[string]render.HTML{
		"actor": "<b>{{verb}}</b>", "verb": render.Text("créé & co"), "record": `<a href="/r/1">INV-1</a>`,
	})
	want := `&lt;i&gt;<b>{{verb}}</b>&lt;/i&gt; a créé &amp; co <a href="/r/1">INV-1</a> {{nope}} {{x créé &amp; co &lt;u&gt;`
	if string(got) != want {
		t.Fatalf("HTML = %q\nwant        %q", got, want)
	}
	if got := HTML(nil, AdminActivityLine, map[string]render.HTML{"actor": "a", "verb": "v", "record": "r"}); got != "a v r" {
		t.Fatalf("English default = %q", got)
	}
}
