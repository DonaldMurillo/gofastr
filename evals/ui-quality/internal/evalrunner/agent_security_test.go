package evalrunner

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/evals/internal/childenv"
)

// Agent children run with --approval-mode yolo / bypassPermissions and
// execute their own output; this denylist is the only containment they
// have. This table is deliberately identical to the one in
// evals/backend-adoption/internal/evalrunner/runner_security_test.go:
// one credential-shape list asserted at both surfaces keeps the shared
// list in childenv from drifting without either runner noticing.
func TestCredentialFilterCatchesTokenShapes(t *testing.T) {
	credentialShaped := []string{
		"HF_TOKEN",
		"VERCEL_TOKEN",
		"CLOUDFLARE_API_TOKEN",
		"REPLICATE_API_TOKEN",
		"TWILIO_ACCOUNT_SID",
	}
	for _, name := range credentialShaped {
		if !childenv.LooksCredentialBearing(name) {
			t.Errorf("LooksCredentialBearing(%q) = false; a credential-shaped name would ride into a yolo/bypassPermissions agent tree", name)
		}
	}
	for _, name := range []string{"PATH", "HOME", "LANG"} {
		if childenv.LooksCredentialBearing(name) {
			t.Errorf("LooksCredentialBearing(%q) = true for a benign name", name)
		}
	}
}
