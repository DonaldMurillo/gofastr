package plugincmd

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/cliplugin"
)

func TestComputeApprovedGrantsEnforcesOptInAndManifestBounds(t *testing.T) {
	builtins := map[string]string{
		"build":    "build",
		"deploy":   "deploy",
		"validate": "validate",
		"plugin":   "plugin",
		"help":     "help",
		"version":  "version",
	}
	manifest := cliplugin.Manifest{
		ProtocolVersion: cliplugin.ProtocolVersion,
		Name:            "native",
		Version:         "v0.1.0",
		Summary:         "Native CLI",
		EnvPassthrough:  []string{"ANDROID_*", "JAVA_HOME"},
		EnvFromFlags:    []string{"--keystore-pass-env"},
		Hooks:           []cliplugin.HookSpec{{Phase: cliplugin.HookPre, Command: "build"}},
		Overrides:       []string{"deploy"},
	}

	// Default add approves manifest env vars/flags, but NEVER hooks or overrides.
	env, envFlags, hooks, overrides, err := computeApprovedGrants(manifest, grantFlags{}, builtins)
	if err != nil {
		t.Fatalf("computeApprovedGrants default: %v", err)
	}
	if len(env) != 2 || len(envFlags) != 1 || len(hooks) != 0 || len(overrides) != 0 {
		t.Fatalf("unexpected default grants: env=%v flags=%v hooks=%v overrides=%v", env, envFlags, hooks, overrides)
	}

	// --no-manifest-env suppresses manifest env passthrough.
	env, envFlags, _, _, err = computeApprovedGrants(manifest, grantFlags{noManifestEnv: true}, builtins)
	if err != nil || len(env) != 0 || len(envFlags) != 0 {
		t.Fatalf("--no-manifest-env failed: env=%v flags=%v err=%v", env, envFlags, err)
	}

	// Requesting a hook NOT declared in manifest is refused.
	if _, _, _, _, err := computeApprovedGrants(manifest, grantFlags{allowHooks: []string{"post:build"}}, builtins); err == nil {
		t.Fatal("expected refusal for undeclared hook")
	}

	// Requesting an override NOT declared in manifest is refused.
	if _, _, _, _, err := computeApprovedGrants(manifest, grantFlags{allowOverride: []string{"build"}}, builtins); err == nil {
		t.Fatal("expected refusal for undeclared override")
	}

	// Protected built-in override is refused.
	if _, _, _, _, err := computeApprovedGrants(manifest, grantFlags{allowOverride: []string{"plugin"}}, builtins); err == nil {
		t.Fatal("expected refusal for protected override")
	}

	// Explicit opt-in for declared hook + override succeeds.
	_, _, hooks, overrides, err = computeApprovedGrants(manifest, grantFlags{
		allowHooks:    []string{"pre:build"},
		allowOverride: []string{"deploy"},
	}, builtins)
	if err != nil || len(hooks) != 1 || hooks[0] != "pre:build" || len(overrides) != 1 || overrides[0] != "deploy" {
		t.Fatalf("explicit opt-in failed: hooks=%v overrides=%v err=%v", hooks, overrides, err)
	}
}

func TestParseGrantFlagsAndPositionals(t *testing.T) {
	pos, g, err := parseGrantFlagsAndPositionals([]string{
		"native", "/usr/local/bin/gofastr-native",
		"--allow-env", "ANDROID_*",
		"--allow-env=JAVA_HOME",
		"--allow-env-flag", "--keystore-pass-env",
		"--allow-hook", "pre:build",
		"--allow-override=deploy",
		"--no-manifest-env",
	})
	if err != nil {
		t.Fatalf("parseGrantFlagsAndPositionals: %v", err)
	}
	if len(pos) != 2 || pos[0] != "native" || pos[1] != "/usr/local/bin/gofastr-native" {
		t.Fatalf("unexpected positionals: %v", pos)
	}
	if !g.noManifestEnv || len(g.allowEnv) != 2 || len(g.allowEnvFlags) != 1 || len(g.allowHooks) != 1 || len(g.allowOverride) != 1 {
		t.Fatalf("unexpected grants: %+v", g)
	}

	// Unknown flag refused.
	if _, _, err := parseGrantFlagsAndPositionals([]string{"native", "--bogus"}); err == nil {
		t.Fatal("expected unknown flag error")
	}
	// Missing value refused.
	if _, _, err := parseGrantFlagsAndPositionals([]string{"native", "--allow-env", "--no-manifest-env"}); err == nil {
		t.Fatal("expected missing value error")
	}
}

func TestKnownCompanionHintNative(t *testing.T) {
	h, ok := LookupCompanionHint("native")
	if !ok || h.Install == "" || h.Register == "" {
		t.Fatalf("missing native companion hint: %+v ok=%v", h, ok)
	}
	if _, ok := LookupCompanionHint("definitely-unknown"); ok {
		t.Fatal("unexpected hint for unknown command")
	}
}
