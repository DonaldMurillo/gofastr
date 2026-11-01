package i18n

import (
	"context"
	"testing"
)

func TestDocLang(t *testing.T) {
	cat := NewMapCatalog()
	cat.Set("fr", "k", Message{Text: "v"})
	cat.Set("ar", "k", Message{Text: "v"})
	tr := NewTranslator(cat, "fr")
	noFallback := NewTranslator(cat, "")
	at := func(tr *Translator, tag string) context.Context {
		ctx := WithContext(context.Background(), Locale{Tag: tag})
		if tr != nil {
			ctx = WithTranslator(ctx, tr)
		}
		return ctx
	}
	for _, c := range []struct {
		name string
		ctx  context.Context
		want string
	}{
		{"catalog locale", at(tr, "ar"), "ar"},
		{"region of a catalog locale", at(tr, "fr-ca"), "fr-ca"},
		// X-Locale wins outright, so a tag the catalog lacks can reach
		// ctx; the text then comes from the fallback.
		{"unknown tag reads the fallback", at(tr, "zz"), "fr"},
		{"unknown tag without a fallback is English", at(noFallback, "zz"), "en"},
		{"no translator is English", at(nil, "ar"), "en"},
		{"no locale", context.Background(), "en"},
	} {
		if got := DocLang(c.ctx); got != c.want {
			t.Errorf("%s: DocLang = %q; want %q", c.name, got, c.want)
		}
	}
}

// CatalogLocale answers a catalog locale or the fallback, never the tag
// the request carried, so per-locale caches stay bounded by the catalog.
func TestCatalogLocale(t *testing.T) {
	cat := NewMapCatalog()
	cat.Set("es", "k", Message{Text: "v"})
	cat.Set("fr-ca", "k", Message{Text: "v"})
	tr := NewTranslator(cat, "en")
	at := func(tag string) context.Context {
		return WithTranslator(WithContext(context.Background(), Locale{Tag: tag}), tr)
	}
	for tag, want := range map[string]string{
		"es": "es", "es-mx": "es", "fr-ca": "fr-ca", "zz-attacker": "en", "": "en",
	} {
		if got := CatalogLocale(at(tag)); got != want {
			t.Errorf("CatalogLocale(%q) = %q; want %q", tag, got, want)
		}
	}
}

func TestEnvLocale(t *testing.T) {
	cat := NewMapCatalog()
	cat.Set("fr", "k", Message{Text: "v"})
	cat.Set("de", "k", Message{Text: "v"})
	tr := NewTranslator(cat, "en")
	for _, c := range []struct {
		name                           string
		language, lcAll, lcMessages, l string
		want                           string
	}{
		{"LANG with region and charset", "", "", "", "fr_CA.UTF-8", "fr"},
		{"LC_ALL beats LANG", "", "de_DE.UTF-8", "", "fr_FR.UTF-8", "de"},
		{"LC_MESSAGES beats LANG", "", "", "de_AT@euro", "fr_FR", "de"},
		{"LANGUAGE list, first catalog hit", "pt:de:fr", "", "", "fr_FR", "de"},
		{"C locale is no preference", "", "C", "", "", "en"},
		{"POSIX locale is no preference", "", "", "", "POSIX", "en"},
		{"garbage falls back", "", "", "", "../../etc", "en"},
		{"unset falls back", "", "", "", "", "en"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("LANGUAGE", c.language)
			t.Setenv("LC_ALL", c.lcAll)
			t.Setenv("LC_MESSAGES", c.lcMessages)
			t.Setenv("LANG", c.l)
			if got := EnvLocale(tr).Tag; got != c.want {
				t.Errorf("EnvLocale = %q; want %q", got, c.want)
			}
		})
	}
	t.Run("no translator takes the first tag", func(t *testing.T) {
		t.Setenv("LANGUAGE", "")
		t.Setenv("LC_ALL", "")
		t.Setenv("LC_MESSAGES", "")
		t.Setenv("LANG", "pt_BR.UTF-8")
		if got := EnvLocale(nil).Tag; got != "pt-br" {
			t.Errorf("EnvLocale(nil) = %q; want pt-br", got)
		}
	})
}

// posixTag cut at "@", so sr_RS.UTF-8@latin read as Cyrillic sr-rs and a
// desktop app showed Cyrillic menus to a Latin-script reader.
func TestEnvLocaleScriptModifier(t *testing.T) {
	cat := NewMapCatalog()
	cat.Set("sr", "k", Message{Text: "v"})
	cat.Set("sr-latn", "k", Message{Text: "v"})
	cat.Set("uz-cyrl", "k", Message{Text: "v"})
	tr := NewTranslator(cat, "en")
	for _, c := range []struct{ lang, want string }{
		{"sr_RS.UTF-8@latin", "sr-latn"},
		{"sr_RS@latin", "sr-latn"},
		{"uz_UZ.UTF-8@cyrillic", "uz-cyrl"},
		{"sr_RS.UTF-8", "sr"},
		{"sr_RS@euro", "sr"},
	} {
		t.Setenv("LANGUAGE", "")
		t.Setenv("LC_ALL", "")
		t.Setenv("LC_MESSAGES", "")
		t.Setenv("LANG", c.lang)
		if got := EnvLocale(tr).Tag; got != c.want {
			t.Errorf("LANG=%s: EnvLocale = %q; want %q", c.lang, got, c.want)
		}
	}
	if got := posixTag("sr_RS.UTF-8@latin"); got != "sr-latn-rs" {
		t.Errorf("posixTag = %q; want sr-latn-rs", got)
	}
}
