package style

import (
	"strings"
	"testing"
)

func TestFontSlug(t *testing.T) {
	cases := map[string]string{
		"Bricolage Grotesque": "bricolage-grotesque",
		"IBM Plex Mono":       "ibm-plex-mono",
		"Inter":               "inter",
		"  Space  Grotesk  ":  "space-grotesk",
		"Noto Sans JP":        "noto-sans-jp",
		"Source Sans 3":       "source-sans-3",
		"":                    "",
	}
	for family, want := range cases {
		if got := FontSlug(family); got != want {
			t.Errorf("FontSlug(%q) = %q, want %q", family, got, want)
		}
	}
}

func TestFontFaceCSSDefaults(t *testing.T) {
	css := FontFaceCSS("", WebFont{Family: "Bricolage Grotesque"})
	for _, want := range []string{
		"font-family: 'Bricolage Grotesque'",
		"font-style: normal",
		"font-weight: 400 700",
		"font-display: swap",
		"url('/fonts/bricolage-grotesque.woff2') format('woff2')",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("rendered CSS missing %q:\n%s", want, css)
		}
	}
}

func TestFontFaceCSSOverrides(t *testing.T) {
	css := FontFaceCSS("/static/f/", WebFont{
		Family: "Inter", File: "inter-var", Weight: "300", Style: "italic", Display: "block",
	})
	for _, want := range []string{
		"font-style: italic", "font-weight: 300", "font-display: block",
		"url('/static/f/inter-var.woff2')",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("rendered CSS missing %q:\n%s", want, css)
		}
	}
	if strings.Contains(css, "//") {
		t.Errorf("trailing slash on dir produced a doubled separator:\n%s", css)
	}
}

func TestFontFaceCSSDeduplicatesAndSkipsBlanks(t *testing.T) {
	css := FontFaceCSS("",
		WebFont{Family: "Inter"},
		WebFont{Family: ""},
		WebFont{Family: "Inter", Weight: "900"}, // first declaration wins
		WebFont{Family: "Mono"},
	)
	if n := strings.Count(css, "@font-face"); n != 2 {
		t.Fatalf("emitted %d rules, want 2:\n%s", n, css)
	}
	if strings.Contains(css, "900") {
		t.Errorf("a repeated family overrode the first declaration:\n%s", css)
	}
}

func TestFontFaceCSSEmptyInput(t *testing.T) {
	if got := FontFaceCSS(""); got != "" {
		t.Errorf("no fonts should render nothing, got %q", got)
	}
}

func TestFontFamilies(t *testing.T) {
	fonts := []WebFont{{Family: "Zeta"}, {Family: ""}, {Family: "Alpha"}, {Family: "Zeta"}}
	got := FontFamilies(fonts...)
	if len(got) != 2 || got[0] != "Zeta" || got[1] != "Alpha" {
		t.Errorf("FontFamilies = %v, want declaration order [Zeta Alpha]", got)
	}
	sorted := SortedFontFamilies(fonts...)
	if sorted[0] != "Alpha" || sorted[1] != "Zeta" {
		t.Errorf("SortedFontFamilies = %v", sorted)
	}
}
