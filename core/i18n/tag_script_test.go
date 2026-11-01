package i18n

import (
	"context"
	"net/http/httptest"
	"testing"
)

// zh-TW only dropped subtags from the right (zh-tw, zh), so a reader in
// Taiwan got the Simplified "zh" catalog, or English when the catalogs
// were named zh-Hans and zh-Hant. CLDR reads zh-TW, zh-HK and zh-MO as
// Traditional and zh-CN, zh-SG as Simplified.
func TestChineseRegionsPickScript(t *testing.T) {
	cases := []struct {
		catalogs []string
		accept   string
		want     string
	}{
		{[]string{"en", "zh", "zh-hant"}, "zh-TW,zh;q=0.9,en;q=0.7", "zh-hant"},
		{[]string{"en", "zh-hans", "zh-hant"}, "zh-TW,en;q=0.7", "zh-hant"},
		{[]string{"en", "zh-hans", "zh-hant"}, "zh-HK", "zh-hant"},
		{[]string{"en", "zh-hans", "zh-hant"}, "zh-MO", "zh-hant"},
		{[]string{"en", "zh-hans", "zh-hant"}, "zh-CN", "zh-hans"},
		{[]string{"en", "zh-hans", "zh-hant"}, "zh-SG", "zh-hans"},
		{[]string{"en", "zh-hans", "zh-hant"}, "zh", "zh-hans"},
		{[]string{"en", "sr", "sr-latn"}, "sr-ME", "sr-latn"},
		{[]string{"en", "sr-cyrl", "sr-latn"}, "sr-RS", "sr-cyrl"},
		// Traditional never falls back to a Simplified catalog.
		{[]string{"en", "zh"}, "zh-TW", "en"},
		{[]string{"en", "zh-hans"}, "zh-Hant-HK", "en"},
		{[]string{"en", "sr"}, "sr-Latn", "en"},
	}
	for _, c := range cases {
		cat := NewMapCatalog()
		for _, l := range c.catalogs {
			cat.Set(l, "hi", Message{Text: l})
		}
		tr := NewTranslator(cat, "en")
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Accept-Language", c.accept)
		if got := Negotiate(tr, r).Tag; got != c.want {
			t.Errorf("%v %q: negotiated %q, want %q", c.catalogs, c.accept, got, c.want)
		}
	}
}

// Translator.T walks the same chain: text for a zh-TW reader comes from
// zh-Hant, and a Simplified "zh" entry is never used for it.
func TestTraditionalTextSkipsSimplified(t *testing.T) {
	cat := NewMapCatalog()
	cat.Set("en", "hi", Message{Text: "hello"})
	cat.Set("zh", "hi", Message{Text: "简体"})
	tr := NewTranslator(cat, "en")
	ctx := WithContext(context.Background(), Locale{Tag: "zh-tw"})
	if got := tr.T(ctx, "hi"); got != "hello" {
		t.Errorf("T under zh-tw with only zh = %q, want the en text", got)
	}
	cat.Set("zh-hant", "hi", Message{Text: "繁體"})
	if got := tr.T(ctx, "hi"); got != "繁體" {
		t.Errorf("T under zh-tw = %q, want the zh-hant text", got)
	}
}
