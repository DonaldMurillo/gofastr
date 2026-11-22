package cliplugin

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func makeDummyExecutable(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func testBuiltins() map[string]string {
	return map[string]string{
		"build":    "build",
		"dev":      "dev",
		"deploy":   "deploy",
		"generate": "generate",
		"gen":      "generate",
		"g":        "generate",
		"migrate":  "migrate",
		"m":        "migrate",
		"plugin":   "plugin",
		"help":     "help",
		"version":  "version",
	}
}

func TestResolveRejectsBuiltinShadowing(t *testing.T) {
	exe := makeDummyExecutable(t, filepath.Join(t.TempDir(), "fakebin"), "#!/bin/sh\n")
	for _, shadow := range []string{"build", "m", "gen", "g", "plugin", "help", "version"} {
		resolved := Resolve(ResolveOptions{
			UserConfig: UserConfig{
				Version: 1,
				Plugins: []RegisteredPlugin{{Name: shadow, Command: []string{exe}}},
			},
			Builtins: testBuiltins(),
		})
		rp, ok := FindResolved(resolved, shadow)
		if !ok {
			t.Fatalf("missing resolved entry for %q", shadow)
		}
		if rp.Status != StatusConflict || !errors.Is(rp.Err, ErrBuiltinCollision) {
			t.Fatalf("shadow %q: status=%s err=%v, want conflict/ErrBuiltinCollision", shadow, rp.Status, rp.Err)
		}
	}
}

func TestResolveProjectTrustAndTamperDetection(t *testing.T) {
	proj := t.TempDir()
	bin := makeDummyExecutable(t, filepath.Join(proj, "tools", "custom"), "#!/bin/sh\necho v1\n")
	realBin, err := filepath.EvalSymlinks(bin)
	if err != nil {
		t.Fatal(err)
	}
	sha, err := HashFile(realBin)
	if err != nil {
		t.Fatal(err)
	}

	projCfg := ProjectConfig{
		Path:       filepath.Join(proj, "gofastr.settings.yml"),
		ProjectDir: proj,
		Found:      true,
		Plugins: []ProjectPlugin{{
			Name:      "custom",
			Command:   []string{"./tools/custom"},
			AllowEnv:  []string{"CUSTOM_TOKEN"},
			Hooks:     []string{"pre:build"},
			Overrides: []string{"deploy"},
		}},
	}

	// 1. Untrusted by default — never executable silently.
	untrusted := Resolve(ResolveOptions{
		UserConfig:    UserConfig{Version: 1},
		ProjectConfig: projCfg,
		Builtins:      testBuiltins(),
	})
	rp, _ := FindResolved(untrusted, "custom")
	if rp.Status != StatusUntrusted || !errors.Is(rp.Err, ErrUntrustedPlugin) {
		t.Fatalf("SECURITY: untrusted project plugin status=%s err=%v, want untrusted/ErrUntrustedPlugin", rp.Status, rp.Err)
	}

	// 2. Trusted with exact command, SHA-256, and capabilities -> ready.
	trustRec := TrustedProjectPlugin{
		ProjectDir:     CanonicalProjectDir(proj),
		Name:           "custom",
		Command:        []string{realBin},
		SHA256:         sha,
		AllowEnv:       []string{"CUSTOM_TOKEN"},
		AllowHooks:     []string{"pre:build"},
		AllowOverrides: []string{"deploy"},
	}
	trusted := Resolve(ResolveOptions{
		UserConfig:    UserConfig{Version: 1, TrustedProjects: []TrustedProjectPlugin{trustRec}},
		ProjectConfig: projCfg,
		Builtins:      testBuiltins(),
	})
	rp, _ = FindResolved(trusted, "custom")
	if rp.Status != StatusReady {
		t.Fatalf("trusted plugin status=%s err=%v, want ready", rp.Status, rp.Err)
	}

	// 3. Repo requests a new unapproved env var -> trust invalidated!
	escalatedProj := projCfg
	escalatedProj.Plugins = []ProjectPlugin{{
		Name:      "custom",
		Command:   []string{"./tools/custom"},
		AllowEnv:  []string{"CUSTOM_TOKEN", "EXTRA_VAR"},
		Hooks:     []string{"pre:build"},
		Overrides: []string{"deploy"},
	}}
	escalated := Resolve(ResolveOptions{
		UserConfig:    UserConfig{Version: 1, TrustedProjects: []TrustedProjectPlugin{trustRec}},
		ProjectConfig: escalatedProj,
		Builtins:      testBuiltins(),
	})
	rp, _ = FindResolved(escalated, "custom")
	if rp.Status != StatusUntrusted || !errors.Is(rp.Err, ErrTrustMismatch) {
		t.Fatalf("SECURITY: capability escalation allowed: status=%s err=%v", rp.Status, rp.Err)
	}

	// 4. Executable modified on disk (e.g. git pull replaced ./tools/custom) -> trust invalidated!
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho tampered\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	tampered := Resolve(ResolveOptions{
		UserConfig:    UserConfig{Version: 1, TrustedProjects: []TrustedProjectPlugin{trustRec}},
		ProjectConfig: projCfg,
		Builtins:      testBuiltins(),
	})
	rp, _ = FindResolved(tampered, "custom")
	if rp.Status != StatusUntrusted || !errors.Is(rp.Err, ErrTrustMismatch) {
		t.Fatalf("SECURITY: tampered project binary accepted: status=%s err=%v", rp.Status, rp.Err)
	}
}

func TestResolveRejectsProjectPathEscapeAndSymlinkEscape(t *testing.T) {
	outer := t.TempDir()
	outsideBin := makeDummyExecutable(t, filepath.Join(outer, "outside"), "#!/bin/sh\n")
	proj := filepath.Join(outer, "app")
	if err := os.MkdirAll(filepath.Join(proj, "tools"), 0o700); err != nil {
		t.Fatal(err)
	}

	// Relative traversal "../outside".
	res := Resolve(ResolveOptions{
		UserConfig: UserConfig{Version: 1},
		ProjectConfig: ProjectConfig{
			ProjectDir: proj,
			Found:      true,
			Plugins:    []ProjectPlugin{{Name: "escape", Command: []string{"../outside"}}},
		},
		Builtins: testBuiltins(),
	})
	rp, _ := FindResolved(res, "escape")
	if rp.Status != StatusInvalid || !errors.Is(rp.Err, ErrInvalidPath) {
		t.Fatalf("SECURITY: relative path escape allowed: status=%s err=%v", rp.Status, rp.Err)
	}

	// Symlink inside project pointing outside project.
	if runtime.GOOS != "windows" {
		link := filepath.Join(proj, "tools", "sym")
		if err := os.Symlink(outsideBin, link); err != nil {
			t.Fatal(err)
		}
		res = Resolve(ResolveOptions{
			UserConfig: UserConfig{Version: 1},
			ProjectConfig: ProjectConfig{
				ProjectDir: proj,
				Found:      true,
				Plugins:    []ProjectPlugin{{Name: "sym", Command: []string{"./tools/sym"}}},
			},
			Builtins: testBuiltins(),
		})
		rp, _ = FindResolved(res, "sym")
		if rp.Status != StatusInvalid || !errors.Is(rp.Err, ErrInvalidPath) {
			t.Fatalf("SECURITY: symlink escape allowed: status=%s err=%v", rp.Status, rp.Err)
		}
	}
}

func TestResolveRejectsProjectLaunchers(t *testing.T) {
	proj := t.TempDir()
	bin := makeDummyExecutable(t, filepath.Join(proj, "tools", "plugin.sh"), "#!/bin/sh\necho ok\n")
	_ = bin

	cases := []struct {
		name string
		cmd  []string
	}{
		{"shell launcher", []string{"sh", "./tools/plugin.sh"}},
		{"go tool launcher", []string{"go", "tool", "gofastr-native"}},
		{"direct binary with extra arg", []string{"./tools/plugin.sh", "--flag"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := Resolve(ResolveOptions{
				UserConfig: UserConfig{Version: 1},
				ProjectConfig: ProjectConfig{
					ProjectDir: proj,
					Found:      true,
					Plugins:    []ProjectPlugin{{Name: "launcher", Command: tc.cmd}},
				},
				Builtins: testBuiltins(),
			})
			rp, _ := FindResolved(res, "launcher")
			if rp.Status != StatusInvalid || !errors.Is(rp.Err, ErrInvalidPath) {
				t.Fatalf("SECURITY: project launcher %q allowed: status=%s err=%v", tc.cmd, rp.Status, rp.Err)
			}
		})
	}
}

func TestResolveGlobalAndProjectConflict(t *testing.T) {
	proj := t.TempDir()
	globalBin := makeDummyExecutable(t, filepath.Join(t.TempDir(), "gbin"), "#!/bin/sh\n")
	otherBin := makeDummyExecutable(t, filepath.Join(t.TempDir(), "obin"), "#!/bin/sh\n")
	projBin := makeDummyExecutable(t, filepath.Join(proj, "tools", "local"), "#!/bin/sh\n")

	// Global hook/override must survive when an untrusted repo declares a
	// conflicting project command of the same name, while namespace lookup
	// surfaces the conflict.
	res := Resolve(ResolveOptions{
		UserConfig: UserConfig{
			Version: 1,
			Plugins: []RegisteredPlugin{{
				Name:           "guard",
				Command:        []string{globalBin},
				AllowHooks:     []string{"pre:deploy"},
				AllowOverrides: []string{"build"},
			}},
		},
		ProjectConfig: ProjectConfig{
			ProjectDir: proj,
			Found:      true,
			Plugins:    []ProjectPlugin{{Name: "guard", Command: []string{projBin}}},
		},
		Builtins: testBuiltins(),
	})
	rp, _ := FindResolved(res, "guard")
	if rp.Status != StatusConflict || !errors.Is(rp.Err, ErrDuplicatePlugin) {
		t.Fatalf("expected duplicate plugin conflict on namespace lookup, got status=%s err=%v", rp.Status, rp.Err)
	}
	if hooks := ResolveHooks(res, HookPre, "deploy"); len(hooks) != 1 || hooks[0].Name != "guard" {
		t.Fatalf("SECURITY: untrusted repo disabled global pre:deploy hook: %+v", hooks)
	}
	if ov, err := ResolveOverride(res, "build"); err != nil || ov == nil || ov.Name != "guard" {
		t.Fatalf("SECURITY: untrusted repo disabled global build override: ov=%+v err=%v", ov, err)
	}

	// Two global plugins claiming override of "deploy" -> ErrDuplicateOverride.
	res = Resolve(ResolveOptions{
		UserConfig: UserConfig{
			Version: 1,
			Plugins: []RegisteredPlugin{
				{Name: "alpha", Command: []string{globalBin}, AllowOverrides: []string{"deploy"}},
				{Name: "beta", Command: []string{otherBin}, AllowOverrides: []string{"deploy"}},
			},
		},
		Builtins: testBuiltins(),
	})
	if _, err := ResolveOverride(res, "deploy"); !errors.Is(err, ErrDuplicateOverride) {
		t.Fatalf("expected ErrDuplicateOverride, got %v", err)
	}
}

func TestResolveInvalidPaths(t *testing.T) {
	dir := t.TempDir()
	nonExec := filepath.Join(dir, "notexec")
	if err := os.WriteFile(nonExec, []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		cmd  []string
	}{
		{"missing file", []string{filepath.Join(dir, "nope")}},
		{"directory", []string{dir}},
		{"nul byte", []string{"/bin/echo\x00evil"}},
	}
	if runtime.GOOS != "windows" {
		cases = append(cases, struct {
			name string
			cmd  []string
		}{"non-executable", []string{nonExec}})
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := ResolveExecutable(tc.cmd, dir, false); !errors.Is(err, ErrInvalidPath) {
				t.Fatalf("ResolveExecutable(%q) = %v, want ErrInvalidPath", tc.cmd, err)
			}
		})
	}
}
