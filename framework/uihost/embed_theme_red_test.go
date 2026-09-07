//go:build red

package uihost

import (
	"encoding/base64"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

// RED TEST — open finding, 2026-09-06/07 adversarial round 5, phase 2 (family
// enumeration; tier T2).
// Property: the customer-supplied theme parameter is decoded with the
// no-ambiguity rule — token maps carrying a repeated key, or two spellings
// of one token that differ only by case, are refused or degraded to the app
// theme, never silently normalized to the last spelling. The param rides an
// unauthenticated GET (the frame URL's ?theme=, wired at embed.go:716 via
// embedThemeKey), so the decode is the only gate.
// Surfaces: framework/uihost/embed.go::resolveEmbedTheme :859-874 — base64
// decode then plain json.Unmarshal into map[string]string. The strict twin
// is the embed exchange POST :541 (handler.DecodeStrict), whose own comment
// states the family rule; the degrade path for unusable params already
// exists and is documented at :882-885 (unallowed tokens drop to the app's
// value) and in embedThemeKey's "" return (render under the app theme).
// Finding (probed): a theme param decoding to
// {"color-primary":"#111111","color-primary":"#ff0000"} on a surface that
// allows color-primary registers a variant — stdlib's map decode keeps the
// last spelling, so the frame renders with #ff0000 while any first-
// occurrence reader of the param saw #111111. The case-folded spelling
// pair registers the same way.
// Fix direction: decode the token payload through
// handler.UnmarshalStrict (map rule: no repeated key, no two keys folding
// to the same name) and treat a refusal like any other unusable param —
// return ok=false so the frame renders under the app theme.

func TestResolveEmbedThemeRejectsDupKeysRed(t *testing.T) {
	ds := hostWithTheme(t, style.DefaultTheme())
	eh, err := fembed.New(fembed.Config{
		Surfaces: []fembed.Surface{{
			Name:    "reports",
			Screen:  embedTestScreen{"/reports"},
			Origins: []string{embedTestOrigin},
			Theme:   fembed.ThemeConfig{AllowTokens: []string{"color-primary"}},
		}},
		BurnStore: fembed.NewMemoryBurnStore(),
	})
	if err != nil {
		t.Fatalf("setup broken: embed.New: %v", err)
	}
	surface, ok := eh.Lookup("reports")
	if !ok {
		t.Fatal("setup broken: Lookup reports: not found")
	}
	redThemeParam := func(obj string) string {
		return base64.RawURLEncoding.EncodeToString([]byte(obj))
	}

	if key, ok := ds.resolveEmbedTheme(surface, redThemeParam(`{"color-primary":"#111111","color-primary":"#ff0000"}`)); ok {
		t.Errorf("SECURITY: [embed-theme-lenient] duplicate color-primary key registered variant %q — stdlib map decode kept the last spelling (#ff0000) while a first-occurrence reader of the param saw #111111; refuse the ambiguity or degrade to the app theme", key)
	}

	if key, ok := ds.resolveEmbedTheme(surface, redThemeParam(`{"Color-Primary":"#0a0a0a","color-primary":"#ff0000"}`)); ok {
		t.Errorf("SECURITY: [embed-theme-lenient] case-folded color-primary spellings registered variant %q — the two spellings fold to one token name; refuse the ambiguity or degrade to the app theme", key)
	}

	// GREEN-guard: a single-key theme still applies, so the strict decode
	// cannot refuse well-formed branding.
	if key, ok := ds.resolveEmbedTheme(surface, redThemeParam(`{"color-primary":"#0d5c4d"}`)); !ok || key == "" {
		t.Fatalf("setup broken: single-key theme must register a variant (ok=%v key=%q)", ok, key)
	}
}
