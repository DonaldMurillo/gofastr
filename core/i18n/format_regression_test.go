package i18n

import (
	"testing"
)

func TestLocaleRootPathWithFragment(t *testing.T) {
	p := PathLocales{Default: "en", Prefixed: []string{"fr"}}
	if got := p.Path("fr", "/#about"); got != "/fr#about" {
		t.Fatalf("localized fragment URL = %q", got)
	}
}

func TestUnknownCurrencyOmitsAffixSpacing(t *testing.T) {
	ctx := inLocale("fr")
	if got := Currency(ctx, 12, "invalid"); got != "12,00" {
		t.Fatalf("unknown currency = %q", got)
	}
}
