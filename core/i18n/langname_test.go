package i18n

import "testing"

func TestLanguageNameIsEndonym(t *testing.T) {
	cases := []struct{ tag, want string }{
		{"en", "English"},
		{"es", "Español"},
		{"fr", "Français"},
		{"de", "Deutsch"},
		{"ja", "日本語"},
		{"pt-br", "Português (Brasil)"},
		{"es-419", "Español latinoamericano"},
		{"zz", "zz"},
		{"", ""},
	}
	for _, c := range cases {
		if got := LanguageName(c.tag); got != c.want {
			t.Errorf("LanguageName(%q) = %q, want %q", c.tag, got, c.want)
		}
	}
}

func TestLanguageNameSplitsRegionTwins(t *testing.T) {
	// en-XA is a pseudo region CLDR has no name for: the name must
	// still differ from plain English, or a menu lists two "English".
	if got := LanguageName("en-xa"); got == LanguageName("en") {
		t.Fatalf("LanguageName(en-xa) = %q, same as en", got)
	}
}
