package uihost

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

func brandTheme(hex string) style.Theme {
	t := style.DefaultTheme()
	t.Colors.Primary = style.Color{Name: "primary", Value: hex}
	return t
}

func hostWithTheme(t *testing.T, th style.Theme) *UIHost {
	t.Helper()
	a := app.NewApp("variant-test").WithTheme(th)
	return New(a)
}

func getAppCSS(t *testing.T, ds *UIHost, query string) *httptest.ResponseRecorder {
	t.Helper()
	url := "/__gofastr/app.css"
	if query != "" {
		url += "?" + query
	}
	rec := httptest.NewRecorder()
	ds.handleAppCSS(rec, httptest.NewRequest(http.MethodGet, url, nil))
	return rec
}

// The default URL is untouched by the variant mechanism: same bytes, same
// no-cache posture as before variants existed.
func TestAppCSS_DefaultURLUnchanged(t *testing.T) {
	ds := hostWithTheme(t, brandTheme("#4F46E5"))
	rec := getAppCSS(t, ds, "")

	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}
	if !strings.Contains(rec.Body.String(), "--color-primary: #4F46E5") {
		t.Error("default app.css should declare the app theme's primary token")
	}
}

// A registered variant is served under its own hash, immutably.
func TestAppCSS_RegisteredVariantIsServedImmutable(t *testing.T) {
	ds := hostWithTheme(t, brandTheme("#4F46E5"))
	hash := ds.RegisterThemeVariant(brandTheme("#0D9488"))

	rec := getAppCSS(t, ds, "t="+hash)
	body := rec.Body.String()

	// Assert on the TOKEN DECLARATION, not on raw substrings: a var()
	// fallback legitimately mentions a hex that is not the effective value,
	// so substring matching would both false-positive and miss real leaks.
	if !strings.Contains(body, "--color-primary: #0D9488") {
		t.Error("variant response should declare the VARIANT's primary token")
	}
	if strings.Contains(body, "--color-primary: #4F46E5") {
		t.Error("variant response declared the app theme's primary token — themes leaked")
	}
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Errorf("Cache-Control = %q, want immutable for a content-addressed variant", got)
	}
}

// An unknown hash must not be cacheable under that key: registering the real
// variant later would otherwise find the app theme's bytes already cached
// against its hash.
func TestAppCSS_UnknownVariantFallsBackAndIsNotImmutable(t *testing.T) {
	ds := hostWithTheme(t, brandTheme("#4F46E5"))

	rec := getAppCSS(t, ds, "t=deadbeefcafe")
	if !strings.Contains(rec.Body.String(), "--color-primary: #4F46E5") {
		t.Error("unknown variant should fall back to the app theme")
	}
	if got := rec.Header().Get("Cache-Control"); strings.Contains(got, "immutable") {
		t.Errorf("Cache-Control = %q — an unknown hash must never be cached immutably", got)
	}
}

// The request names a hash; it can never describe a theme. This is what makes
// CSS injection through a theme value unrepresentable rather than filtered.
func TestAppCSS_RequestCannotInjectCSS(t *testing.T) {
	ds := hostWithTheme(t, brandTheme("#4F46E5"))

	for _, probe := range []string{
		"t=red;--x:}body{display:none}",
		"t=" + style.ThemeHash(brandTheme("#123456")), // valid shape, never registered
		"t=../../etc/passwd",
		"t=%3Cscript%3E",
	} {
		rec := getAppCSS(t, ds, probe)
		body := rec.Body.String()
		if strings.Contains(body, "display:none") || strings.Contains(body, "<script") {
			t.Errorf("probe %q injected content into app.css", probe)
		}
		if !strings.Contains(body, "--color-primary: #4F46E5") {
			t.Errorf("probe %q should have fallen back to the app theme", probe)
		}
	}
}

// Registration is idempotent by content, so a host re-registering an
// equivalent theme cannot grow the variant space. This is the bound that stops
// a caller minting unlimited cache entries.
func TestAppCSS_VariantRegistrationIsIdempotentByContent(t *testing.T) {
	ds := hostWithTheme(t, style.DefaultTheme())

	h1 := ds.RegisterThemeVariant(brandTheme("#0D9488"))
	h2 := ds.RegisterThemeVariant(brandTheme("#0D9488"))
	if h1 != h2 {
		t.Fatalf("same theme registered twice gave %q and %q", h1, h2)
	}

	named := brandTheme("#0D9488")
	named.Name = "renamed-but-identical-css"
	if h3 := ds.RegisterThemeVariant(named); h3 != h1 {
		t.Errorf("theme differing only in Name got hash %q, want %q — Name changes no pixel", h3, h1)
	}
	if n := ds.ThemeVariantCount(); n != 1 {
		t.Errorf("variant count = %d, want 1", n)
	}

	if h := ds.RegisterThemeVariant(brandTheme("#B45309")); h == h1 {
		t.Error("a genuinely different theme must get a different hash")
	}
	if n := ds.ThemeVariantCount(); n != 2 {
		t.Errorf("variant count = %d, want 2", n)
	}
}

// Sol review #12: the immutable URL must address the WHOLE stylesheet. Keying
// on the palette alone means a release that changes customCSS but keeps the
// palette reuses the URL, and browsers hold the stale bytes for a year.
func TestAppCSS_VariantKeyCoversCustomCSSNotJustPalette(t *testing.T) {
	th := brandTheme("#0D9488")

	plain := New(app.NewApp("k1").WithTheme(style.DefaultTheme()))
	withCustom := New(app.NewApp("k2").WithTheme(style.DefaultTheme()),
		WithCustomCSS(".host-rule{color:red}"))

	k1 := plain.RegisterThemeVariant(th)
	k2 := withCustom.RegisterThemeVariant(th)

	if k1 == k2 {
		t.Errorf("same palette but different custom CSS produced the same key %q — "+
			"an immutable URL would serve one host's stylesheet for the other", k1)
	}
	if !strings.Contains(getAppCSS(t, withCustom, "t="+k2).Body.String(), ".host-rule") {
		t.Error("variant response dropped the host's custom CSS")
	}
}

// Sol review #13: Theme carries maps, so registering by value shares the
// caller's backing store. A later caller-side write must not change what we
// serve under an already-issued key.
func TestAppCSS_VariantIsIsolatedFromCallerMutation(t *testing.T) {
	ds := hostWithTheme(t, style.DefaultTheme())

	th := brandTheme("#0D9488")
	th.DarkColors = map[string]string{"primary": "#5EEAD4"}

	key := ds.RegisterThemeVariant(th)
	before := getAppCSS(t, ds, "t="+key).Body.String()

	th.DarkColors["primary"] = "#FF00FF" // caller mutates AFTER registering

	if after := getAppCSS(t, ds, "t="+key).Body.String(); after != before {
		t.Error("caller mutation changed the bytes served under an already-issued key")
	}
	if strings.Contains(getAppCSS(t, ds, "t="+key).Body.String(), "#FF00FF") {
		t.Error("post-registration mutation leaked into the served variant")
	}
}
