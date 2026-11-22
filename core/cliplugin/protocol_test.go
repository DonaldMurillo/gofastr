package cliplugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestManifestValidate(t *testing.T) {
	valid := Manifest{
		ProtocolVersion: ProtocolVersion,
		Name:            "native",
		Version:         "v0.2.0",
		Summary:         "Build and run mobile apps",
		MinCLIVersion:   "v0.98.0",
		MaxCLIVersion:   "v1.9.0",
		Subcommands: []CommandInfo{
			{Name: "doctor", Summary: "Check mobile toolchains"},
			{Name: "build", Summary: "Build iOS/Android artifacts"},
		},
		EnvPassthrough: []string{"ANDROID_*", "JAVA_HOME", "GOFASTR_BUNDLETOOL"},
		EnvFromFlags:   []string{"--keystore-pass-env", "--key-pass-env"},
		Hooks:          []HookSpec{{Phase: HookPre, Command: "build", Summary: "pre-build check"}},
		Overrides:      []string{"deploy"},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}

	cases := []struct {
		name    string
		mutate  func(*Manifest)
		wantErr error
	}{
		{"wrong protocol", func(m *Manifest) { m.ProtocolVersion = 99 }, ErrProtocolMismatch},
		{"zero protocol", func(m *Manifest) { m.ProtocolVersion = 0 }, ErrProtocolMismatch},
		{"bad name upper", func(m *Manifest) { m.Name = "Native" }, ErrInvalidManifest},
		{"bad name slash", func(m *Manifest) { m.Name = "../evil" }, ErrInvalidManifest},
		{"empty version", func(m *Manifest) { m.Version = "   " }, ErrInvalidManifest},
		{"multiline version", func(m *Manifest) { m.Version = "v1\n2" }, ErrInvalidManifest},
		{"empty summary", func(m *Manifest) { m.Summary = "" }, ErrInvalidManifest},
		{"multiline summary", func(m *Manifest) { m.Summary = "a\nb" }, ErrInvalidManifest},
		{"bad min semver", func(m *Manifest) { m.MinCLIVersion = "not-semver" }, ErrInvalidManifest},
		{"bad max semver", func(m *Manifest) { m.MaxCLIVersion = "1.0" }, ErrInvalidManifest},
		{"min gt max", func(m *Manifest) { m.MinCLIVersion = "v2.0.0"; m.MaxCLIVersion = "v1.0.0" }, ErrInvalidManifest},
		{"dup subcommand", func(m *Manifest) { m.Subcommands = append(m.Subcommands, CommandInfo{Name: "doctor"}) }, ErrInvalidManifest},
		{"blocked secret env", func(m *Manifest) { m.EnvPassthrough = []string{"GOFASTR_SECRET"} }, ErrInvalidManifest},
		{"broad gofastr wildcard", func(m *Manifest) { m.EnvPassthrough = []string{"GOFASTR_*"} }, ErrInvalidManifest},
		{"star only wildcard", func(m *Manifest) { m.EnvPassthrough = []string{"*"} }, ErrInvalidManifest},
		{"bad env flag", func(m *Manifest) { m.EnvFromFlags = []string{"-k"} }, ErrInvalidManifest},
		{"bad hook phase", func(m *Manifest) { m.Hooks = []HookSpec{{Phase: "during", Command: "build"}} }, ErrInvalidManifest},
		{"protected hook", func(m *Manifest) { m.Hooks = []HookSpec{{Phase: HookPre, Command: "plugin"}} }, ErrProtectedBuiltin},
		{"protected override help", func(m *Manifest) { m.Overrides = []string{"help"} }, ErrProtectedBuiltin},
		{"protected override version", func(m *Manifest) { m.Overrides = []string{"version"} }, ErrProtectedBuiltin},
		{"protected override plugin", func(m *Manifest) { m.Overrides = []string{"plugin"} }, ErrProtectedBuiltin},
		{"dup override", func(m *Manifest) { m.Overrides = []string{"deploy", "deploy"} }, ErrInvalidManifest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := valid
			m.Subcommands = append([]CommandInfo(nil), valid.Subcommands...)
			m.EnvPassthrough = append([]string(nil), valid.EnvPassthrough...)
			m.EnvFromFlags = append([]string(nil), valid.EnvFromFlags...)
			m.Hooks = append([]HookSpec(nil), valid.Hooks...)
			m.Overrides = append([]string(nil), valid.Overrides...)
			tc.mutate(&m)
			err := m.Validate()
			if err == nil {
				t.Fatalf("expected error %v, got nil", tc.wantErr)
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got error %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestManifestCheckCLIVersion(t *testing.T) {
	m := Manifest{
		ProtocolVersion: ProtocolVersion,
		Name:            "native",
		Version:         "v0.2.0",
		Summary:         "Native tools",
		MinCLIVersion:   "v0.98.0",
		MaxCLIVersion:   "v1.5.0",
	}
	for _, okV := range []string{"dev", "", "v0.98.0", "0.99.0", "v1.5.0"} {
		if err := m.CheckCLIVersion(okV); err != nil {
			t.Errorf("CheckCLIVersion(%q) unexpected error: %v", okV, err)
		}
	}
	for _, badV := range []string{"v0.97.9", "v1.5.1", "v2.0.0"} {
		if err := m.CheckCLIVersion(badV); !errors.Is(err, ErrIncompatibleVersion) {
			t.Errorf("CheckCLIVersion(%q) = %v, want ErrIncompatibleVersion", badV, err)
		}
	}
}

func TestMaybeServeManifest(t *testing.T) {
	m := Manifest{Name: "native", Version: "v0.1.0", Summary: "Mobile CLI"}
	var buf bytes.Buffer

	handled, err := MaybeServeManifest([]string{"doctor"}, &buf, m)
	if handled || err != nil || buf.Len() != 0 {
		t.Fatalf("standalone args should not trigger manifest: handled=%v err=%v buf=%q", handled, err, buf.String())
	}

	handled, err = MaybeServeManifest([]string{HandshakeFlag}, &buf, m)
	if !handled || err != nil {
		t.Fatalf("manifest handshake failed: handled=%v err=%v", handled, err)
	}
	var decoded Manifest
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode emitted manifest: %v", err)
	}
	if decoded.ProtocolVersion != ProtocolVersion || decoded.Name != "native" {
		t.Fatalf("unexpected decoded manifest: %+v", decoded)
	}

	// Fixed prefix args with GOFASTR_PLUGIN_MANIFEST=1 must also trigger.
	t.Setenv(EnvManifestRequest, "1")
	buf.Reset()
	handled, err = MaybeServeManifest([]string{"plugin-mode", HandshakeFlag}, &buf, m)
	if !handled || err != nil || buf.Len() == 0 {
		t.Fatalf("prefixed manifest handshake failed: handled=%v err=%v buf=%q", handled, err, buf.String())
	}
}

func TestBuildProbeEnvExcludesCredentials(t *testing.T) {
	parent := []string{
		"PATH=/usr/bin:/bin",
		"HOME=/home/dev",
		"GOPROXY=https://user:secretpass@proxy.example.com",
		"GOFASTR_SECRET=secret",
		"DATABASE_URL=postgres://secret",
	}
	env := BuildProbeEnv(parent)
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "secretpass") || strings.Contains(joined, "GOPROXY=") {
		t.Fatalf("SECURITY: probe env inherited GOPROXY credentials:\n%s", joined)
	}
	if strings.Contains(joined, "GOFASTR_SECRET") || strings.Contains(joined, "DATABASE_URL") {
		t.Fatalf("SECURITY: probe env inherited blocked secrets:\n%s", joined)
	}
}

func TestBuildExecEnvFiltering(t *testing.T) {
	parent := []string{
		"PATH=/usr/bin:/bin",
		"HOME=/home/dev",
		"TERM=xterm-256color",
		"ANDROID_HOME=/sdk/android",
		"ANDROID_NDK_HOME=/sdk/ndk",
		"JAVA_HOME=/jdk",
		"GOFASTR_BUNDLETOOL=/tools/bundletool.jar",
		"MY_KS_PASS=top-secret-keystore-pass",
		"GOFASTR_SECRET=do-not-leak-session-key",
		"DATABASE_URL=postgres://secret@db/prod",
		"GOFASTR_HARNESS_PASSPHRASE=vault-pass",
		"AWS_SECRET_ACCESS_KEY=aws-secret",
		"GITHUB_TOKEN=ghp_secret",
		"OPENAI_API_KEY=sk-secret",
		"RANDOM_UNRELATED_VAR=nope",
		"GOFASTR_HOOK_PHASE=spoofed",
	}

	env := BuildExecEnv(EnvOptions{
		Environ:      parent,
		AllowEnv:     []string{"ANDROID_*", "JAVA_HOME", "GOFASTR_BUNDLETOOL"},
		AllowEnvFlag: []string{"--keystore-pass-env", "--key-pass-env"},
		Args: []string{
			"build",
			"--keystore-pass-env", "MY_KS_PASS",
			"--key-pass-env=GOFASTR_SECRET", // must be blocked!
			"--key-pass-env",                // missing value immediately before -- must NOT step over --!
			"--",
			"--keystore-pass-env", "RANDOM_UNRELATED_VAR", // after -- must be ignored!
		},
		ProtocolVars: map[string]string{
			EnvProtocolVersion: "1",
			EnvPluginName:      "native",
		},
	})

	envMap := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		envMap[k] = v
	}

	mustHave := map[string]string{
		"PATH":               "/usr/bin:/bin",
		"HOME":               "/home/dev",
		"TERM":               "xterm-256color",
		"ANDROID_HOME":       "/sdk/android",
		"ANDROID_NDK_HOME":   "/sdk/ndk",
		"JAVA_HOME":          "/jdk",
		"GOFASTR_BUNDLETOOL": "/tools/bundletool.jar",
		"MY_KS_PASS":         "top-secret-keystore-pass",
		EnvProtocolVersion:   "1",
		EnvPluginName:        "native",
	}
	for k, want := range mustHave {
		if got := envMap[k]; got != want {
			t.Errorf("env[%s] = %q, want %q", k, got, want)
		}
	}

	mustNotHave := []string{
		"GOFASTR_SECRET",
		"DATABASE_URL",
		"GOFASTR_HARNESS_PASSPHRASE",
		"AWS_SECRET_ACCESS_KEY",
		"GITHUB_TOKEN",
		"OPENAI_API_KEY",
		"RANDOM_UNRELATED_VAR",
		"GOFASTR_HOOK_PHASE",
	}
	for _, k := range mustNotHave {
		if v, ok := envMap[k]; ok {
			t.Errorf("SECURITY: [env-leak] child environment unexpectedly contained %s=%q", k, v)
		}
	}
}

func TestInvocationFromEnv(t *testing.T) {
	t.Setenv(EnvProtocolVersion, "")
	t.Setenv(EnvHookPhase, "")
	t.Setenv(EnvOverrideCommand, "")
	if got := InvocationFromEnv(); got.Mode != ModeStandalone {
		t.Fatalf("mode = %q, want standalone", got.Mode)
	}

	t.Setenv(EnvProtocolVersion, "1")
	t.Setenv(EnvPluginName, "native")
	if got := InvocationFromEnv(); got.Mode != ModeNamespace || got.PluginName != "native" {
		t.Fatalf("unexpected namespace invocation: %+v", got)
	}

	t.Setenv(EnvHookPhase, "pre")
	t.Setenv(EnvHookCommand, "build")
	if got := InvocationFromEnv(); got.Mode != ModeHook || got.HookPhase != HookPre || got.TargetCommand != "build" {
		t.Fatalf("unexpected hook invocation: %+v", got)
	}

	t.Setenv(EnvOverrideCommand, "deploy")
	if got := InvocationFromEnv(); got.Mode != ModeOverride || got.TargetCommand != "deploy" {
		t.Fatalf("unexpected override invocation: %+v", got)
	}
}
