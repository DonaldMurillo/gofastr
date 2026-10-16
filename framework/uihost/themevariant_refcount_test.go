package uihost

import (
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// ReleaseThemeVariant must drop only ONE holder, keeping the variant alive while
// any registration remains.
//
// Variant keys are content addresses, so two callers genuinely collide on the
// same key: an embedded surface and the theme editor's live preview rendering
// the same palette, or two surfaces sharing a brand. The refcount is what makes
// one embed's eviction remove only its OWN contribution. Without it, a single
// release deletes the variant another holder is still serving, and that holder's
// ?t= URL degrades to the app theme mid-session.
func TestReleaseThemeVariantKeepsAVariantWhileAHolderRemains(t *testing.T) {
	ds := hostWithTheme(t, style.DefaultTheme())
	th := brandTheme("#ff0000")

	// Two independent holders register the same theme. The key is a content
	// address, so both land on it and the refcount goes to two.
	key1 := ds.RegisterThemeVariant(th)
	key2 := ds.RegisterThemeVariant(th)
	if key1 != key2 {
		t.Fatalf("identical themes must share a key: %q vs %q — the refcount test only holds under collision", key1, key2)
	}

	// One holder releases. The variant must stay because the other still needs it.
	ds.ReleaseThemeVariant(key1)
	if _, ok := ds.themeVariant(key1); !ok {
		t.Fatal("releasing one of two holders deleted the variant — the refcount is ignored and one eviction pulls another holder's theme")
	}
	if got := ds.ThemeVariantCount(); got != 1 {
		t.Errorf("variant count = %d, want 1 after one of two holders released", got)
	}

	// The last holder releasing takes the refcount to zero and removes it.
	ds.ReleaseThemeVariant(key1)
	if _, ok := ds.themeVariant(key1); ok {
		t.Error("variant still resolvable after the last holder released")
	}
}

// A variant's key is the immutable cache address of its app.css, so it
// fingerprints the bytes that URL serves, minified outside dev.
func TestThemeVariantKeyAddressesServedCSS(t *testing.T) {
	ds := hostWithTheme(t, style.DefaultTheme())
	key := ds.RegisterThemeVariant(brandTheme("#00ff00"))
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest("GET", "/__gofastr/app.css?t="+key, nil))
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if got := style.CSSFingerprint(rec.Body.String()); got != key {
		t.Fatalf("variant key %s, but the sheet it serves fingerprints to %s", key, got)
	}
}
