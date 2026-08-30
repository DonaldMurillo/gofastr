package evalrunner

import "testing"

// The codex child compiles and executes its own output under the developer's
// environment; looksCredentialBearing is the only containment that child
// has. This table is deliberately identical to the one in
// evals/ui-quality/internal/evalrunner/agent_security_test.go — the other
// hand-copied denylist. One credential-shape list asserted at both surfaces
// keeps the two copies from drifting apart silently (they already have:
// NUGET_/TWILIO_ prefixes exist only in the twin).
func TestCredentialFilterCatchesTokenShapes(t *testing.T) {
	credentialShaped := []string{
		"HF_TOKEN",
		"VERCEL_TOKEN",
		"CLOUDFLARE_API_TOKEN",
		"REPLICATE_API_TOKEN",
		"TWILIO_ACCOUNT_SID",
	}
	for _, name := range credentialShaped {
		if !looksCredentialBearing(name) {
			t.Errorf("looksCredentialBearing(%q) = false; a credential-shaped name would ride into the codex child", name)
		}
	}
	for _, name := range []string{"PATH", "HOME", "LANG"} {
		if looksCredentialBearing(name) {
			t.Errorf("looksCredentialBearing(%q) = true for a benign name", name)
		}
	}
}
