package i18n

import (
	"context"
	"net/http/httptest"
	"slices"
	"testing"
)

// A tag fell back only to its language, so zh-Hant-HK skipped a
// registered zh-hant catalog and the reader got the fallback language.
func TestRegionTagFindsScriptCatalog(t *testing.T) {
	cat := NewMapCatalog()
	cat.Set("en", "hi", Message{Text: "hello"})
	cat.Set("zh-hant", "hi", Message{Text: "你好"})
	tr := NewTranslator(cat, "en")
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Accept-Language", "zh-Hant-HK")
	if got := Negotiate(tr, r).Tag; got != "zh-hant" {
		t.Errorf("negotiated %q, want zh-hant", got)
	}
	if got := tr.T(WithContext(context.Background(), Locale{Tag: "zh-hant-hk"}), "hi"); got != "你好" {
		t.Errorf("T under zh-hant-hk = %q, want the zh-hant text", got)
	}
	// zh-Hant's CLDR parent is root: the chain never reaches the
	// Simplified "zh".
	want := []string{"zh-hant-hk", "zh-hant"}
	if got := tagFallbacks("zh-Hant-HK"); !slices.Equal(got, want) {
		t.Errorf("tagFallbacks = %q, want %q", got, want)
	}
}
