package evalrunner

import "testing"

// Agent children run with --approval-mode yolo / bypassPermissions and
// execute their own output; this denylist is the only containment they
// have. This table is deliberately identical to the one in
// evals/backend-adoption/internal/evalrunner/runner_security_test.go — the
// other hand-copied denylist. One credential-shape list asserted at both
// surfaces keeps the two copies from drifting apart silently.
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
			t.Errorf("looksCredentialBearing(%q) = false; a credential-shaped name would ride into a yolo/bypassPermissions agent tree", name)
		}
	}
	for _, name := range []string{"PATH", "HOME", "LANG"} {
		if looksCredentialBearing(name) {
			t.Errorf("looksCredentialBearing(%q) = true for a benign name", name)
		}
	}
}
