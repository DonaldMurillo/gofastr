package uihost

import (
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

// embedThemeKey must make a duplicate request WAIT for the in-flight owner
// rather than falling through to the empty (app-theme) key.
//
// Two visitors opening the same customer's page at the same moment on a cold
// process both resolve the same brand theme. The first reserves the slot and
// starts the (slow) resolve; the second's lookup misses the in-flight entry,
// its reserve returns dup=true, and it must wait for the owner to record the
// variant key. If the wait is removed the duplicate returns "" and one of the
// two frames renders in the app palette instead of the customer's brand.
//
// This goes through embedThemeKey (not the bare embedThemeState) because that
// is where the dup branch and its waitFor call live. The owner's in-flight
// reservation is planted directly so the duplicate is GUARANTEED to take the
// dup branch — without this, a fast owner would record before the duplicate
// reserved and the test would pass via the top-of-function lookup, never
// exercising the wait.
func TestEmbedThemeKeyWaitsForAnInFlightOwner(t *testing.T) {
	application := app.NewApp("Embed Theme Wait")
	eh, err := fembed.New(fembed.Config{
		Surfaces: []fembed.Surface{{
			Name:    "reports",
			Path:    "/reports",
			Origins: []string{embedTestOrigin},
			Theme:   fembed.ThemeConfig{AllowTokens: []string{"color-primary"}, MaxVariants: 4},
		}},
		BurnStore: fembed.NewMemoryBurnStore(),
	})
	if err != nil {
		t.Fatalf("embed.New: %v", err)
	}
	eh.SetKeys([]byte("nonce-key-nonce-key-nonce-key-32"), []byte("grant-key-grant-key-grant-key-32"))
	ds := New(application, WithEmbed(eh))
	surface, ok := eh.Lookup("reports")
	if !ok {
		t.Fatal("Lookup reports: not found")
	}
	const param = "brand-A"

	// Plant the owner's in-flight reservation directly: the exact state
	// embedThemeKey leaves behind between its own reserve and record while the
	// owner is mid-resolve. This forces the duplicate below onto the dup path.
	if ok, _, _ := ds.embedThemes.reserve("reports", param, 4); !ok {
		t.Fatal("owner reserve refused — cannot plant the in-flight state")
	}
	const ownerKey = "variant-owner"
	go func() {
		time.Sleep(40 * time.Millisecond)
		ds.embedThemes.record("reports", param, ownerKey)
	}()

	// The duplicate goes through the public embedThemeKey. lookup must miss the
	// in-flight (empty-key) entry, reserve returns dup=true, and it must WAIT.
	start := time.Now()
	got := ds.embedThemeKey(surface, param)
	elapsed := time.Since(start)

	if got != ownerKey {
		t.Fatalf("embedThemeKey returned %q, want the owner's key %q — "+
			"the duplicate did not wait for the in-flight owner and rendered under the app theme", got, ownerKey)
	}
	// It must be WOKEN by the owner's record, not rescued by a post-timeout
	// fallback. A path that ignored the wait and re-looked-up the now-recorded
	// key would still return ownerKey but only after the full embedThemeWait.
	if elapsed > time.Second {
		t.Errorf("embedThemeKey waited %v — it timed out instead of being woken when the owner recorded", elapsed)
	}
}
