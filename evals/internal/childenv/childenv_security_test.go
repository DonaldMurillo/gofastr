package childenv

import (
	"strings"
	"testing"
)

// The two eval runners pin the DENY side of this package (their
// *_security_test.go tables of credential shapes); nothing pins the ALLOW
// side, which is the half the unsandboxed candidate server actually
// receives. Allowed is the only thing standing between an agent-built
// binary and the operator's environment, so a name that slips into
// allowedNames ships credentials as surely as a denylist gap.
func TestAllowlistAdmitsOnlyToolVariables(t *testing.T) {
	benign := []string{"PATH", "HOME", "GOCACHE", "GOOS", "LANG", "TMPDIR"}
	for _, name := range benign {
		if !Allowed(name) {
			t.Errorf("Allowed(%q) = false; candidate compilation needs this tool variable", name)
		}
	}
	// Credential shapes the package's own documentation names as the
	// threat (childenv.go: cloud keys, SCM tokens, DATABASE_URL), plus
	// the evaluator's own identity — none of these are tool variables.
	hostile := []string{
		"DATABASE_URL", "AWS_REGION", "GITHUB_TOKEN", "HF_TOKEN",
		"OPENAI_API_KEY", "SSH_AUTH_SOCK", "GOFASTR_SECRET", "CODEX_HOME",
	}
	for _, name := range hostile {
		if Allowed(name) {
			t.Errorf("SECURITY: [allowlist-leak] Allowed(%q) = true; an agent-built candidate would receive it via Allowlisted()", name)
		}
	}
}

// Allowlisted must apply the same verdict to the live environment it
// copies, not just to the predicate in isolation.
func TestAllowlistedFiltersLiveEnvironment(t *testing.T) {
	t.Setenv("GF_EVAL_CANARY_SECRET", "sentinel")
	t.Setenv("GF_EVAL_CANARY_TOKEN", "sentinel")
	for _, entry := range Allowlisted() {
		name, _, _ := strings.Cut(entry, "=")
		if !Allowed(name) {
			t.Fatalf("SECURITY: [allowlist-leak] Allowlisted() emitted %q, which Allowed() rejects", name)
		}
		if strings.Contains(entry, "sentinel") {
			t.Errorf("SECURITY: [allowlist-leak] Allowlisted() emitted a canary credential entry: %q", name)
		}
	}
}
