package evalrunner

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/evals/internal/childenv"
)

// The codex child compiles and executes its own output under the
// developer's environment; childenv.LooksCredentialBearing is the only
// containment that child has. This table is deliberately identical to
// the one in evals/ui-quality/internal/evalrunner/agent_security_test.go:
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
			t.Errorf("LooksCredentialBearing(%q) = false; a credential-shaped name would ride into the codex child", name)
		}
	}
	for _, name := range []string{"PATH", "HOME", "LANG"} {
		if childenv.LooksCredentialBearing(name) {
			t.Errorf("LooksCredentialBearing(%q) = true for a benign name", name)
		}
	}
}

// Property: the codex child must run under exactly the harness's chosen
// Codex identity, never one inherited from the operator's desktop.
//
// codexEnvironment strips every CODEX_-prefixed variable and then appends
// the single CODEX_HOME chosen by normalizeConfig. A parent CODEX_* pair
// (Desktop task identity, policy overrides, or a second CODEX_HOME) would
// otherwise decide which session store and policy the eval's builder
// agent launches under — the wrong half of the blind protocol.
func TestCodexChildGetsSingleCodexHome(t *testing.T) {
	t.Setenv("CODEX_HOME", "/attacker/controlled/home")
	t.Setenv("CODEX_SANDBOX_NETWORK_DISABLED", "0")
	home := "/eval/chosen/codex-home"
	env := codexEnvironment(home, "/eval/tools", "/eval/workspace")
	codexHome := 0
	for _, entry := range env {
		name, value, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "CODEX_") {
			if name != "CODEX_HOME" {
				t.Errorf("SECURITY: [env-leak] codex child inherited %q from the operator environment", name)
			}
			codexHome++
			if value != home {
				t.Errorf("SECURITY: [env-leak] CODEX_HOME = %q, want the harness-chosen %q", value, home)
			}
		}
	}
	if codexHome != 1 {
		t.Errorf("SECURITY: [env-leak] codex child sees %d CODEX_HOME entries, want exactly the harness-chosen one", codexHome)
	}
}
