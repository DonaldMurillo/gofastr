package childenv

import (
	"strings"
	"testing"
)

// Property: the strip loop must apply the shared credential list to the
// live environment it copies. Both eval runners route their agent env
// through this; a name that slips through here reaches an unsandboxed
// agent child in both.
func TestAgentEnvironmentStripsCodexAndCredentials(t *testing.T) {
	t.Setenv("CODEX_HOME", "/attacker/controlled/home")
	t.Setenv("CODEX_SANDBOX_NETWORK_DISABLED", "0")
	t.Setenv("GF_EVAL_CANARY_SECRET", "sentinel")
	t.Setenv("GF_EVAL_CANARY_TOKEN", "sentinel")
	t.Setenv("GF_EVAL_KEEP_ME", "kept")
	for _, entry := range AgentEnvironment(nil, nil) {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "CODEX_") {
			t.Errorf("SECURITY: [env-leak] AgentEnvironment emitted CODEX variable %q", name)
		}
		if LooksCredentialBearing(name) {
			t.Errorf("SECURITY: [env-leak] AgentEnvironment emitted credential-bearing %q", name)
		}
	}
	found := false
	for _, entry := range AgentEnvironment(nil, nil) {
		if strings.HasPrefix(entry, "GF_EVAL_KEEP_ME=") {
			found = true
		}
	}
	if !found {
		t.Error("AgentEnvironment dropped a benign variable")
	}
}

func TestAgentEnvironmentDropsExtrasAndHonorsAllow(t *testing.T) {
	t.Setenv("GF_EVAL_CANARY_PATH", "sentinel")
	t.Setenv("GF_EVAL_CANARY_TOKEN", "sentinel")
	for _, entry := range AgentEnvironment([]string{"gf_eval_canary_path"}, nil) {
		if strings.HasPrefix(entry, "GF_EVAL_CANARY_PATH=") {
			t.Error("AgentEnvironment ignored a dropped name (case-insensitive)")
		}
	}
	allowed := AgentEnvironment(nil, func(name string) bool { return name == "GF_EVAL_CANARY_TOKEN" })
	found := false
	for _, entry := range allowed {
		if strings.HasPrefix(entry, "GF_EVAL_CANARY_TOKEN=") {
			found = true
		}
	}
	if !found {
		t.Error("AgentEnvironment ignored the allow callback for a backend's own credential")
	}
}
