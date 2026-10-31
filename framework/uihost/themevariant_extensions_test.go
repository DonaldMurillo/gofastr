package uihost

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

type variantGlowTokens struct{ Glow style.Color }

// Extensions is a slice of token-set pointers, so it is copied at
// registration like the maps: a host that builds the slice by hand and
// writes a token after the key was issued must not change the bytes
// served under it.
func TestAppCSS_VariantExtensionsIsolatedFromCallerMutation(t *testing.T) {
	ds := hostWithTheme(t, style.DefaultTheme())

	glow := &variantGlowTokens{Glow: style.Color{Name: "variant-glow", Value: "#0D9488"}}
	th := brandTheme("#0D9488")
	th.Extensions = []any{glow}

	key := ds.RegisterThemeVariant(th)
	before := getAppCSS(t, ds, "t="+key).Body.String()
	if !strings.Contains(before, "--color-variant-glow") {
		t.Fatalf("precondition: the extension token must reach the served CSS:\n%s", before)
	}

	glow.Glow.Value = "#FF00FF" // caller mutates AFTER registering

	if after := getAppCSS(t, ds, "t="+key).Body.String(); after != before {
		t.Error("caller mutation of Extensions changed the bytes served under an already-issued key")
	}
}
