package cliplugin

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestUserConfigSaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr", "plugins.yml")

	want := UserConfig{
		Version: ConfigVersion,
		Plugins: []RegisteredPlugin{
			{
				Name:           "zebra",
				Command:        []string{"/usr/local/bin/zebra"},
				Summary:        "Zebra helper",
				AllowEnv:       []string{"ZEBRA_*"},
				AllowHooks:     []string{"post:build"},
				AllowOverrides: []string{},
			},
			{
				Name:          "native",
				Command:       []string{"/usr/local/bin/gofastr-native"},
				Summary:       "Mobile CLI",
				AllowEnv:      []string{"ANDROID_*", "JAVA_HOME"},
				AllowEnvFlags: []string{"--keystore-pass-env"},
			},
		},
		TrustedProjects: []TrustedProjectPlugin{
			{
				ProjectDir:     "/tmp/proj",
				Name:           "localtool",
				Command:        []string{"/tmp/proj/tools/localtool"},
				SHA256:         "abcdef0123456789",
				Summary:        "Local helper",
				AllowEnv:       []string{"LOCAL_FLAG"},
				AllowHooks:     []string{"pre:build"},
				AllowOverrides: []string{"deploy"},
			},
		},
	}

	if err := SaveUserConfig(path, want); err != nil {
		t.Fatalf("SaveUserConfig: %v", err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("plugins.yml mode = %04o, want 0600", perm)
		}
	}

	got, err := LoadUserConfig(path)
	if err != nil {
		t.Fatalf("LoadUserConfig: %v", err)
	}
	// FormatUserConfig sorts plugins alphabetically.
	if len(got.Plugins) != 2 || got.Plugins[0].Name != "native" || got.Plugins[1].Name != "zebra" {
		t.Fatalf("plugins not sorted or missing: %+v", got.Plugins)
	}
	if !reflect.DeepEqual(got.TrustedProjects, want.TrustedProjects) {
		t.Fatalf("trusted_projects mismatch:\ngot  %+v\nwant %+v", got.TrustedProjects, want.TrustedProjects)
	}

	// Verify UTF-8 BOM is ignored on parse.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	bomParsed, err := ParseUserConfig(path, append([]byte("\ufeff"), raw...))
	if err != nil {
		t.Fatalf("ParseUserConfig with BOM: %v", err)
	}
	if !reflect.DeepEqual(bomParsed, got) {
		t.Fatalf("BOM changed parsed config")
	}
}

func TestSaveUserConfigRefusesSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "victim.txt")
	if err := os.WriteFile(target, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "plugins.yml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := SaveUserConfig(link, UserConfig{Version: 1}); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink refusal, got: %v", err)
	}
	if _, err := LoadUserConfig(link); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected LoadUserConfig symlink refusal, got: %v", err)
	}
	body, _ := os.ReadFile(target)
	if string(body) != "untouched" {
		t.Fatalf("symlink target was overwritten: %q", body)
	}

	// Refuse group/world-writable registry file and symlinked lockfile.
	badMode := filepath.Join(dir, "world.yml")
	if err := os.WriteFile(badMode, []byte("version: 1\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(badMode, 0o666)
	if _, err := LoadUserConfig(badMode); err == nil || !strings.Contains(err.Error(), "writable") {
		t.Fatalf("SECURITY: world-writable plugins.yml accepted: %v", err)
	}

	lockTarget := filepath.Join(dir, "lock-victim.txt")
	if err := os.WriteFile(lockTarget, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	lockLink := filepath.Join(dir, "safe.yml.lock")
	if err := os.Symlink(lockTarget, lockLink); err != nil {
		t.Fatal(err)
	}
	if err := SaveUserConfig(filepath.Join(dir, "safe.yml"), UserConfig{Version: 1}); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("SECURITY: symlinked lockfile accepted: %v", err)
	}

	// Refuse a non-sticky group/world-writable ancestor directory even when the
	// immediate directory and file are 0700/0600.
	shared := filepath.Join(dir, "shared")
	sub := filepath.Join(shared, "alice")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(shared, 0o777); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(shared, 0o700) })
	nested := filepath.Join(sub, "plugins.yml")
	if err := os.WriteFile(nested, []byte("version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadUserConfig(nested); err == nil || !strings.Contains(err.Error(), "ancestor") {
		t.Fatalf("SECURITY: non-sticky world-writable ancestor accepted on load: %v", err)
	}
	if err := SaveUserConfig(nested, UserConfig{Version: 1}); err == nil || !strings.Contains(err.Error(), "ancestor") {
		t.Fatalf("SECURITY: non-sticky world-writable ancestor accepted on save: %v", err)
	}

	// Refuse a user-owned ancestor symlink whose resolved target sits under a
	// non-sticky world-writable directory (e.g. ~/.config -> /shared/cfg).
	symlinkHome := filepath.Join(dir, "home")
	if err := os.MkdirAll(symlinkHome, 0o700); err != nil {
		t.Fatal(err)
	}
	cfgLink := filepath.Join(symlinkHome, ".config")
	if err := os.Symlink(sub, cfgLink); err != nil {
		t.Fatal(err)
	}
	viaLink := filepath.Join(cfgLink, "plugins.yml")
	if _, err := LoadUserConfig(viaLink); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("SECURITY: direct symlinked config dir accepted on load: %v", err)
	}
	gofastrSub := filepath.Join(sub, "gofastr")
	if err := os.MkdirAll(gofastrSub, 0o700); err != nil {
		t.Fatal(err)
	}
	viaAncestorLink := filepath.Join(cfgLink, "gofastr", "plugins.yml")
	if err := os.WriteFile(filepath.Join(gofastrSub, "plugins.yml"), []byte("version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadUserConfig(viaAncestorLink); err == nil || !strings.Contains(err.Error(), "ancestor") {
		t.Fatalf("SECURITY: symlink target under world-writable ancestor accepted on load: %v", err)
	}
	if err := SaveUserConfig(viaAncestorLink, UserConfig{Version: 1}); err == nil || !strings.Contains(err.Error(), "ancestor") {
		t.Fatalf("SECURITY: symlink target under world-writable ancestor accepted on save: %v", err)
	}

	// Refuse config paths containing ".." segments so symlink + ".." cannot
	// cause filepath.Dir and os.Open to refer to different directories.
	dotDotPath := filepath.Join(symlinkHome, ".config") + "/../plugins.yml"
	if _, err := LoadUserConfig(dotDotPath); err == nil || !strings.Contains(err.Error(), "'..'") {
		t.Fatalf("SECURITY: config path with '..' accepted on load: %v", err)
	}
	if err := SaveUserConfig(dotDotPath, UserConfig{Version: 1}); err == nil || !strings.Contains(err.Error(), "'..'") {
		t.Fatalf("SECURITY: config path with '..' accepted on save: %v", err)
	}
}

func TestUpdateUserConfigSerialized(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plugins.yml")
	initial := UserConfig{
		Version: 1,
		TrustedProjects: []TrustedProjectPlugin{{
			ProjectDir: "/tmp/p",
			Name:       "revokeme",
			Command:    []string{"/tmp/p/tools/revokeme"},
			SHA256:     "abc",
		}},
	}
	if err := SaveUserConfig(path, initial); err != nil {
		t.Fatal(err)
	}

	// Concurrently add a global plugin and revoke project trust; neither
	// operation may clobber the other or resurrect revoked trust.
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		errs <- UpdateUserConfig(path, func(cfg *UserConfig) error {
			cfg.Plugins = append(cfg.Plugins, RegisteredPlugin{Name: "alpha", Command: []string{"/bin/echo"}})
			return nil
		})
	}()
	go func() {
		defer wg.Done()
		errs <- UpdateUserConfig(path, func(cfg *UserConfig) error {
			cfg.TrustedProjects = nil
			return nil
		})
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent UpdateUserConfig failed: %v", err)
		}
	}

	got, err := LoadUserConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Plugins) != 1 || got.Plugins[0].Name != "alpha" {
		t.Fatalf("lost global plugin update: %+v", got.Plugins)
	}
	if len(got.TrustedProjects) != 0 {
		t.Fatalf("SECURITY: concurrent update resurrected revoked trust: %+v", got.TrustedProjects)
	}
}

func TestUpdateUserConfigRejectsOversizedWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plugins.yml")
	initial := UserConfig{
		Version: 1,
		Plugins: []RegisteredPlugin{{Name: "alpha", Command: []string{"/bin/echo"}}},
	}
	if err := SaveUserConfig(path, initial); err != nil {
		t.Fatal(err)
	}
	err := UpdateUserConfig(path, func(cfg *UserConfig) error {
		cfg.Plugins = append(cfg.Plugins, RegisteredPlugin{
			Name:    "huge",
			Command: []string{"/bin/echo"},
			Summary: strings.Repeat("x", maxConfigBytes),
		})
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "byte limit") {
		t.Fatalf("expected oversized config refusal, got: %v", err)
	}
	got, err := LoadUserConfig(path)
	if err != nil {
		t.Fatalf("previous config should remain readable after oversized update: %v", err)
	}
	if len(got.Plugins) != 1 || got.Plugins[0].Name != "alpha" {
		t.Fatalf("previous config mutated: %+v", got.Plugins)
	}
}

func TestParseUserConfigRejectsInvalidAndDuplicate(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want error
	}{
		{"unknown key", "version: 1\nbogus: true\n", nil},
		{"bad version", "version: 2\n", nil},
		{"duplicate plugin", "version: 1\nplugins:\n  - name: a\n    command: [\"/bin/true\"]\n  - name: a\n    command: [\"/bin/true\"]\n", ErrDuplicatePlugin},
		{"canonical duplicate trusted project", "version: 1\ntrusted_projects:\n  - project_dir: \"/work/app\"\n    name: tool\n    command: [\"/work/app/tool\"]\n    sha256: \"abc\"\n  - project_dir: \"/work/./app\"\n    name: tool\n    command: [\"/work/app/tool\"]\n    sha256: \"abc\"\n", ErrDuplicatePlugin},
		{"blocked secret env", "version: 1\nplugins:\n  - name: a\n    command: [\"/bin/true\"]\n    allow_env: [\"GOFASTR_SECRET\"]\n", nil},
		{"protected override", "version: 1\nplugins:\n  - name: a\n    command: [\"/bin/true\"]\n    allow_overrides: [\"plugin\"]\n", ErrProtectedBuiltin},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseUserConfig("plugins.yml", []byte(tc.yaml))
			if err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestDiscoverProjectConfigStopsAtProjectRoot(t *testing.T) {
	outer := t.TempDir()
	if err := os.WriteFile(filepath.Join(outer, "gofastr.settings.yml"), []byte("version: 1\nplugins:\n  - name: evil\n    command: [\"./evil\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	proj := filepath.Join(outer, "myapp")
	sub := filepath.Join(proj, "internal", "pkg")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "go.mod"), []byte("module example.com/myapp\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := DiscoverProjectConfig(sub)
	if err != nil {
		t.Fatalf("DiscoverProjectConfig: %v", err)
	}
	if got.Found {
		t.Fatalf("SECURITY: project plugin discovery escaped module root and found %s", got.Path)
	}

	// Now add settings inside project root and verify discovery from subdir.
	if err := os.WriteFile(filepath.Join(proj, "gofastr.settings.yml"), []byte("version: 1\nplugins:\n  - name: native\n    description: Mobile\n    install: go install example.com/native\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = DiscoverProjectConfig(sub)
	if err != nil {
		t.Fatalf("DiscoverProjectConfig inside project: %v", err)
	}
	if !got.Found || len(got.Plugins) != 1 || got.Plugins[0].Name != "native" {
		t.Fatalf("unexpected discovered project config: %+v", got)
	}
}
