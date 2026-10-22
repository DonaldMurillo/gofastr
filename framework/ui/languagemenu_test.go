package ui

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

var testPathLocales = i18n.PathLocales{Default: "en", Prefixed: []string{"es", "fr"}}

func languageMenuCtx(tag, target string) context.Context {
	ctx := i18n.WithPathLocale(context.Background(), testPathLocales, tag)
	return app.WithRequest(ctx, httptest.NewRequest("GET", target, nil))
}

func TestLanguageMenuLinksThisPage(t *testing.T) {
	out := string(LanguageMenu(LanguageMenuConfig{Ctx: languageMenuCtx("es", "/pricing?plan=pro")}))
	for _, want := range []string{
		`href="/pricing?plan=pro"`,
		`href="/es/pricing?plan=pro"`,
		`href="/fr/pricing?plan=pro"`,
		`hreflang="en"`, `hreflang="es"`, `hreflang="fr"`,
		`rel="alternate"`,
		">English<", ">Español<", ">Français<",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("menu lacks %s:\n%s", want, out)
		}
	}
}

func TestLanguageMenuChecksCurrent(t *testing.T) {
	out := string(LanguageMenu(LanguageMenuConfig{Ctx: languageMenuCtx("es", "/about")}))
	at := strings.Index(out, `href="/es/about"`)
	row := out[strings.LastIndex(out[:at], "<a "):]
	row = row[:strings.Index(row, ">")]
	if !strings.Contains(row, `aria-checked="true"`) {
		t.Fatalf("Spanish row is not checked: %s", row)
	}
	if strings.Count(out, `aria-checked="true"`) != 1 {
		t.Fatalf("want one checked row:\n%s", out)
	}
}

func TestLanguageMenuTriggerNamesBoth(t *testing.T) {
	out := string(LanguageMenu(LanguageMenuConfig{Ctx: languageMenuCtx("fr", "/")}))
	summary := out[strings.Index(out, "<summary"):strings.Index(out, "</summary>")]
	// The accessible name holds the visible name (WCAG 2.5.3), and the
	// visible name is marked with its own language.
	for _, want := range []string{">Language<", `lang="fr"`, ">Français<"} {
		if !strings.Contains(summary, want) {
			t.Errorf("trigger lacks %s:\n%s", want, summary)
		}
	}
	if !strings.Contains(out, `href="/fr"`) {
		t.Errorf("root page in French is not /fr:\n%s", out)
	}
}

func TestLanguageMenuStripsUnroutedPrefix(t *testing.T) {
	// A request the router has not stripped yet still names the page.
	out := string(LanguageMenu(LanguageMenuConfig{Ctx: languageMenuCtx("es", "/es/about")}))
	if !strings.Contains(out, `href="/fr/about"`) || strings.Contains(out, "/es/es/") {
		t.Fatalf("prefix kept:\n%s", out)
	}
}

func TestLanguageMenuNamesOverride(t *testing.T) {
	out := string(LanguageMenu(LanguageMenuConfig{
		Ctx:   languageMenuCtx("en", "/"),
		Names: map[string]string{"es": "Castellano"},
	}))
	if !strings.Contains(out, ">Castellano<") || strings.Contains(out, ">Español<") {
		t.Fatalf("Names not applied:\n%s", out)
	}
}

func TestLanguageMenuEmptyWithoutPrefixes(t *testing.T) {
	ctx := app.WithRequest(context.Background(), httptest.NewRequest("GET", "/", nil))
	if out := LanguageMenu(LanguageMenuConfig{Ctx: ctx}); out != "" {
		t.Fatalf("want nothing without path locales, got %s", out)
	}
	if out := LanguageMenu(LanguageMenuConfig{}); out != "" {
		t.Fatalf("want nothing with no ctx, got %s", out)
	}
}
