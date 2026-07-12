package i18n

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// ----- map catalog + translation basics ------------------------------------

func TestTranslator_LooksUpInLocaleThenFallback(t *testing.T) {
	c := NewMapCatalog()
	c.Set("en", "welcome", Message{Text: "Hello, {{name}}!"})
	c.Set("fr", "welcome", Message{Text: "Bonjour, {{name}} !"})
	tr := NewTranslator(c, "en")

	en := WithContext(context.Background(), Locale{Tag: "en"})
	fr := WithContext(context.Background(), Locale{Tag: "fr"})

	if got := tr.T(en, "welcome", map[string]any{"name": "Alice"}); got != "Hello, Alice!" {
		t.Fatalf("en: got %q", got)
	}
	if got := tr.T(fr, "welcome", map[string]any{"name": "Alice"}); got != "Bonjour, Alice !" {
		t.Fatalf("fr: got %q", got)
	}

	// Missing locale falls back to en.
	de := WithContext(context.Background(), Locale{Tag: "de"})
	if got := tr.T(de, "welcome", map[string]any{"name": "Alice"}); got != "Hello, Alice!" {
		t.Fatalf("de fallback: got %q", got)
	}
}

func TestTranslator_LanguageBaseFallback(t *testing.T) {
	// fr-CA missing → fr should still match.
	c := NewMapCatalog()
	c.Set("fr", "hi", Message{Text: "Salut"})
	tr := NewTranslator(c, "en")

	ctx := WithContext(context.Background(), Locale{Tag: "fr-CA"})
	if got := tr.T(ctx, "hi"); got != "Salut" {
		t.Fatalf("fr-CA→fr fallback: got %q", got)
	}
}

func TestTranslator_MissingKeyReturnsKey(t *testing.T) {
	tr := NewTranslator(NewMapCatalog(), "en")
	if got := tr.T(context.Background(), "no.such.key"); got != "no.such.key" {
		t.Fatalf("missing key: got %q", got)
	}
}

// ----- placeholder interpolation -------------------------------------------

func TestInterpolate_LeavesUnknownPlaceholdersIntact(t *testing.T) {
	got := interpolate("Hello, {{name}}! You are {{role}}.", map[string]any{"name": "Bob"})
	if !strings.Contains(got, "Bob") {
		t.Fatalf("known placeholder unfilled: %q", got)
	}
	if !strings.Contains(got, "{{role}}") {
		t.Fatalf("unknown placeholder removed silently: %q", got)
	}
}

func TestInterpolate_PreservesUnbalancedBraces(t *testing.T) {
	got := interpolate("Look at this {{unclosed", map[string]any{"x": 1})
	if got != "Look at this {{unclosed" {
		t.Fatalf("unbalanced braces mangled: %q", got)
	}
}

// ----- plural handling -----------------------------------------------------

func TestTranslator_PluralEnglish(t *testing.T) {
	c := NewMapCatalog()
	c.Set("en", "cart.items", Message{Plural: map[string]string{
		"one":   "1 item in cart",
		"other": "{{count}} items in cart",
	}})
	tr := NewTranslator(c, "en")
	ctx := WithContext(context.Background(), Locale{Tag: "en"})

	for _, tc := range []struct {
		n    int
		want string
	}{
		{0, "0 items in cart"},
		{1, "1 item in cart"},
		{2, "2 items in cart"},
		{99, "99 items in cart"},
	} {
		got := tr.T(ctx, "cart.items", map[string]any{"count": tc.n})
		if got != tc.want {
			t.Errorf("n=%d: got %q want %q", tc.n, got, tc.want)
		}
	}
}

func TestTranslator_PluralCustomRule(t *testing.T) {
	c := NewMapCatalog()
	c.Set("ru", "items", Message{Plural: map[string]string{
		"one":   "{{count}} штука",
		"few":   "{{count}} штуки",
		"many":  "{{count}} штук",
		"other": "{{count}} штук",
	}})
	tr := NewTranslator(c, "ru")
	// Very small Russian plural rule for the test (real one is longer).
	tr.RegisterPluralRule("ru", func(n int) string {
		mod10 := n % 10
		mod100 := n % 100
		switch {
		case mod10 == 1 && mod100 != 11:
			return "one"
		case mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14):
			return "few"
		default:
			return "many"
		}
	})
	ctx := WithContext(context.Background(), Locale{Tag: "ru"})

	for _, tc := range []struct {
		n    int
		want string
	}{
		{1, "1 штука"},
		{2, "2 штуки"},
		{5, "5 штук"},
		{21, "21 штука"},
		{22, "22 штуки"},
		{25, "25 штук"},
	} {
		got := tr.T(ctx, "items", map[string]any{"count": tc.n})
		if got != tc.want {
			t.Errorf("n=%d: got %q want %q", tc.n, got, tc.want)
		}
	}
}

func TestTranslator_PluralNoCountUsesOther(t *testing.T) {
	c := NewMapCatalog()
	c.Set("en", "x", Message{Plural: map[string]string{
		"one":   "one",
		"other": "other",
	}})
	tr := NewTranslator(c, "en")
	ctx := WithContext(context.Background(), Locale{Tag: "en"})
	if got := tr.T(ctx, "x"); got != "other" {
		t.Fatalf("no count: got %q", got)
	}
}

// ----- accept-language negotiation -----------------------------------------

func TestNegotiate_PicksHighestQMatchingLocale(t *testing.T) {
	c := NewMapCatalog()
	c.Set("en", "k", Message{Text: "en"})
	c.Set("fr", "k", Message{Text: "fr"})
	c.Set("de", "k", Message{Text: "de"})
	tr := NewTranslator(c, "en")

	cases := []struct {
		header string
		want   string
	}{
		{"fr;q=0.9, de;q=1.0, en;q=0.5", "de"},
		{"fr-CA, en;q=0.5", "fr"},
		{"es", "en"}, // none match → fallback
		{"", "en"},
	}
	for _, tc := range cases {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if tc.header != "" {
			r.Header.Set("Accept-Language", tc.header)
		}
		got := Negotiate(tr, r)
		if got.Tag != tc.want {
			t.Errorf("header=%q: got %q want %q", tc.header, got.Tag, tc.want)
		}
	}
}

func TestNegotiate_XLocaleHeaderWins(t *testing.T) {
	tr := NewTranslator(NewMapCatalog(), "en")
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Language", "fr;q=1.0")
	r.Header.Set("X-Locale", "ja")
	if got := Negotiate(tr, r); got.Tag != "ja" {
		t.Fatalf("X-Locale should win: got %q", got.Tag)
	}
}

// ----- middleware -----------------------------------------------------------

func TestMiddleware_AttachesLocaleToContext(t *testing.T) {
	c := NewMapCatalog()
	c.Set("en", "k", Message{Text: "en"})
	c.Set("fr", "k", Message{Text: "fr"})
	tr := NewTranslator(c, "en")

	var seen string
	h := Middleware(tr)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = tr.T(r.Context(), "k")
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Language", "fr;q=1.0,en;q=0.5")
	h.ServeHTTP(httptest.NewRecorder(), r)
	if seen != "fr" {
		t.Fatalf("middleware didn't attach fr; T returned %q", seen)
	}
}

// ----- locale resolver -----------------------------------------------------

func TestNegotiate_ResolverCookieWinsOverHeaders(t *testing.T) {
	c := NewMapCatalog()
	c.Set("en", "k", Message{Text: "en"})
	c.Set("fr", "k", Message{Text: "fr"})
	c.Set("de", "k", Message{Text: "de"})
	tr := NewTranslator(c, "en")

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Language", "de")
	r.Header.Set("X-Locale", "en")
	r.AddCookie(&http.Cookie{Name: "locale", Value: "fr"})

	got := Negotiate(tr, r, WithLocaleResolver(CookieLocale("locale")))
	if got.Tag != "fr" {
		t.Fatalf("cookie resolver should win: got %q want fr", got.Tag)
	}
}

func TestNegotiate_ResolverUnknownValueFallsThrough(t *testing.T) {
	c := NewMapCatalog()
	c.Set("en", "k", Message{Text: "en"})
	c.Set("de", "k", Message{Text: "de"})
	tr := NewTranslator(c, "en")

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Language", "de")
	r.AddCookie(&http.Cookie{Name: "locale", Value: "klingon"}) // not in catalog

	got := Negotiate(tr, r, WithLocaleResolver(CookieLocale("locale")))
	if got.Tag != "de" {
		t.Fatalf("unknown cookie should fall through to Accept-Language: got %q want de", got.Tag)
	}
}

func TestNegotiate_ResolverOversizedValueRejected(t *testing.T) {
	c := NewMapCatalog()
	c.Set("en", "k", Message{Text: "en"})
	c.Set("fr", "k", Message{Text: "fr"})
	tr := NewTranslator(c, "en")

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: "locale", Value: strings.Repeat("a", 40)})

	got := Negotiate(tr, r, WithLocaleResolver(CookieLocale("locale")))
	if got.Tag != "en" {
		t.Fatalf("oversized cookie should be rejected → fallback: got %q want en", got.Tag)
	}
}

func TestNegotiate_ResolverGarbageCharsRejected(t *testing.T) {
	c := NewMapCatalog()
	c.Set("en", "k", Message{Text: "en"})
	c.Set("fr", "k", Message{Text: "fr"})
	tr := NewTranslator(c, "en")

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: "locale", Value: "<script>"})

	got := Negotiate(tr, r, WithLocaleResolver(CookieLocale("locale")))
	if got.Tag != "en" {
		t.Fatalf("garbage cookie should be rejected → fallback: got %q want en", got.Tag)
	}
}

func TestNegotiate_ResolverFalseFallsThrough(t *testing.T) {
	c := NewMapCatalog()
	c.Set("en", "k", Message{Text: "en"})
	c.Set("de", "k", Message{Text: "de"})
	tr := NewTranslator(c, "en")

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Language", "de")
	// missing cookie → CookieLocale returns ok=false
	got := Negotiate(tr, r, WithLocaleResolver(CookieLocale("locale")))
	if got.Tag != "de" {
		t.Fatalf("resolver false should fall through: got %q want de", got.Tag)
	}
}

func TestNegotiate_ResolverRegionFallbackMatches(t *testing.T) {
	c := NewMapCatalog()
	c.Set("en", "k", Message{Text: "en"})
	c.Set("fr", "k", Message{Text: "fr"})
	tr := NewTranslator(c, "en")

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: "locale", Value: "fr-CA"}) // matches "fr" via tag fallback

	got := Negotiate(tr, r, WithLocaleResolver(CookieLocale("locale")))
	if got.Tag != "fr" {
		t.Fatalf("fr-CA cookie should match fr: got %q want fr", got.Tag)
	}
}

func TestMiddleware_PassesResolverOption(t *testing.T) {
	c := NewMapCatalog()
	c.Set("en", "k", Message{Text: "en"})
	c.Set("fr", "k", Message{Text: "fr"})
	tr := NewTranslator(c, "en")

	var seen string
	h := Middleware(tr, WithLocaleResolver(CookieLocale("locale")))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen = tr.T(r.Context(), "k")
		}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Language", "en")
	r.AddCookie(&http.Cookie{Name: "locale", Value: "fr"})
	h.ServeHTTP(httptest.NewRecorder(), r)
	if seen != "fr" {
		t.Fatalf("middleware resolver: got %q want fr", seen)
	}
}

// ----- JSON catalog loader --------------------------------------------------

func TestLoadJSONCatalog_FlattensNestedKeysAndDetectsPluralBuckets(t *testing.T) {
	fsys := fstest.MapFS{
		"en.json": &fstest.MapFile{
			Data: []byte(`{
				"welcome": "Hello, {{name}}!",
				"cart": {
					"items": { "one": "1 item", "other": "{{count}} items" },
					"empty": "Your cart is empty"
				}
			}`),
		},
		"fr.json": &fstest.MapFile{
			Data: []byte(`{"welcome": "Bonjour, {{name}} !"}`),
		},
	}
	cat, err := LoadJSONCatalog(fsys, ".")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if m, ok := cat.Get("en", "welcome"); !ok || m.Text != "Hello, {{name}}!" {
		t.Fatalf("welcome: got %+v ok=%v", m, ok)
	}
	if m, ok := cat.Get("en", "cart.empty"); !ok || m.Text != "Your cart is empty" {
		t.Fatalf("cart.empty: got %+v ok=%v", m, ok)
	}
	if m, ok := cat.Get("en", "cart.items"); !ok || m.Plural["one"] != "1 item" {
		t.Fatalf("cart.items plural: got %+v ok=%v", m, ok)
	}
	if m, ok := cat.Get("fr", "welcome"); !ok || m.Text != "Bonjour, {{name}} !" {
		t.Fatalf("fr welcome: got %+v ok=%v", m, ok)
	}
}

// ----- package-level default -----------------------------------------------

func TestPackageT_UsesDefault(t *testing.T) {
	c := NewMapCatalog()
	c.Set("en", "hello", Message{Text: "hi"})
	prev := Default()
	SetDefault(NewTranslator(c, "en"))
	defer SetDefault(prev)

	ctx := WithContext(context.Background(), Locale{Tag: "en"})
	if got := T(ctx, "hello"); got != "hi" {
		t.Fatalf("package T: got %q", got)
	}
}

func TestPackageT_NoDefaultReturnsKey(t *testing.T) {
	prev := Default()
	SetDefault(nil)
	defer SetDefault(prev)
	if got := T(context.Background(), "no.default"); got != "no.default" {
		t.Fatalf("got %q", got)
	}
}
