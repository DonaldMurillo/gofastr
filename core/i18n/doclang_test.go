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
