package i18n

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func pathSet() PathLocales {
	return PathLocales{Default: "en", Prefixed: []string{"es", "fr-ca"}}
}

func TestSplitLocalePrefix(t *testing.T) {
	cases := []struct{ path, tag, rest string }{
		{"/fr-ca/pricing", "fr-ca", "/pricing"},
		{"/es", "es", "/"},
		{"/es/", "es", "/"},
		{"/es/a/b", "es", "/a/b"},
		{"/en/pricing", "en", "/pricing"},
		{"/esx/pricing", "", "/esx/pricing"},
		{"/ES/pricing", "", "/ES/pricing"},
		{"/fr/pricing", "", "/fr/pricing"},
		{"/", "", "/"},
		{"", "", ""},
	}
	for _, c := range cases {
		tag, rest := pathSet().Split(c.path)
		if tag != c.tag || rest != c.rest {
			t.Errorf("Split(%q) = %q, %q; want %q, %q", c.path, tag, rest, c.tag, c.rest)
		}
	}
}

func FuzzSplitLocalePrefix(f *testing.F) {
	for _, s := range []string{"/es/a", "/es", "/en/", "/", "", "//es", "/es//x", "/fr-ca?x"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, path string) {
		set := pathSet()
		tag, rest := set.Split(path)
		if tag == "" {
			if rest != path {
				t.Fatalf("no tag but rest %q != %q", rest, path)
			}
			return
		}
		if tag != set.Default && !slices.Contains(set.Prefixed, tag) {
			t.Fatalf("tag %q is not in the set", tag)
		}
		if !strings.HasPrefix(rest, "/") {
			t.Fatalf("rest %q does not start with /", rest)
		}
		if got := "/" + tag + strings.TrimSuffix(rest, "/"); rest != "/" && got != strings.TrimSuffix(path, "/") {
			t.Fatalf("Split(%q) lost bytes: %q + %q", path, tag, rest)
		}
	})
}

func TestNewPathLocales(t *testing.T) {
	c := NewMapCatalog()
	c.Set("en", "k", Message{Text: "x"})
	c.Set("fr", "k", Message{Text: "x"})
	c.Set("es", "k", Message{Text: "x"})
	got, err := NewPathLocales(NewTranslator(c, "en"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Default != "en" || !slices.Equal(got.Prefixed, []string{"es", "fr"}) {
		t.Fatalf("got %+v", got)
	}
	if _, err := NewPathLocales(NewTranslator(c, "")); err == nil {
		t.Fatal("no fallback: want an error")
	}
	if _, err := NewPathLocales(nil); err == nil {
		t.Fatal("nil translator: want an error")
	}
}

func TestPathLocaleRejectsOddTags(t *testing.T) {
	c := NewMapCatalog()
	c.Set("en", "k", Message{Text: "x"})
	c.Set("fr_FR", "k", Message{Text: "x"})
	if _, err := NewPathLocales(NewTranslator(c, "en")); err == nil {
		t.Fatal("a tag that cannot be a path segment: want an error")
	}
}

func TestPrefixWinsOverCookie(t *testing.T) {
	c := NewMapCatalog()
	c.Set("en", "k", Message{Text: "x"})
	c.Set("es", "k", Message{Text: "x"})
	c.Set("de", "k", Message{Text: "x"})
	tr := NewTranslator(c, "en")
	r := httptest.NewRequest("GET", "/x", nil)
	r.Header.Set("Accept-Language", "de")
	r.Header.Set("X-Locale", "de")
	r = r.WithContext(WithPathLocale(r.Context(), pathSet(), "es"))
	got := Negotiate(tr, r, WithLocaleResolver(func(*http.Request) (string, bool) { return "de", true }))
	if got.Tag != "es" {
		t.Fatalf("Negotiate = %q, want the path's es", got.Tag)
	}
}

func TestPathPrefixAndLocalePath(t *testing.T) {
	ctx := context.Background()
	if PathPrefix(ctx) != "" || LocalePath(ctx, "es", "/a") != "/a" {
		t.Fatal("no path locale: want no prefix")
	}
	es := WithPathLocale(ctx, pathSet(), "es")
	if got := PathPrefix(es); got != "/es" {
		t.Fatalf("PathPrefix = %q", got)
	}
	en := WithPathLocale(ctx, pathSet(), "en")
	if got := PathPrefix(en); got != "" {
		t.Fatalf("default PathPrefix = %q, want none", got)
	}
	for _, c := range []struct{ tag, path, want string }{
		{"es", "/a", "/es/a"},
		{"es", "/", "/es"},
		{"en", "/a", "/a"},
		{"de", "/a", "/a"},
		{"fr-ca", "/a?q=1", "/fr-ca/a?q=1"},
	} {
		if got := LocalePath(es, c.tag, c.path); got != c.want {
			t.Errorf("LocalePath(%q, %q) = %q, want %q", c.tag, c.path, got, c.want)
		}
	}
}
