package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateProjectWithOverrideFlags(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	customEnt := filepath.Join(dir, "custom")
	if err := os.MkdirAll(customEnt, 0o755); err != nil {
		t.Fatal(err)
	}
	decl := `{"name":"posts","table":"posts","fields":[{"name":"title","type":"string"}]}`
	if err := os.WriteFile(filepath.Join(customEnt, "posts.json"), []byte(decl), 0o644); err != nil {
		t.Fatal(err)
	}
	// --entities + --out + --no-clean exercise applyGenerateOverrides +
	// isEntitySourceOverrideTarget.
	covT_capStdout(t, func() {
		generateProject([]string{"--entities=custom", "--out=.gen", "--no-clean"})
	})
	if _, err := os.Stat(filepath.Join(dir, ".gen", "register.go")); err != nil {
		t.Fatalf("override-dir generate failed: %v", err)
	}
}

func TestGenerateProjectDryRunJSONDiscoverError(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	// --config points at a missing file → discoverGenerateConfig errors;
	// dry-run + json prints the error JSON and exits 1.
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() {
			generateProject([]string{"--config=" + filepath.Join(dir, "missing.yml"), "--dry-run", "--json"})
		})
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestParseGenerateOptionsFlags(t *testing.T) {
	opts := parseGenerateOptions([]string{"--dry-run", "--json", "--no-clean", "--config=c.yml", "--entities=e", "--from=f.yml", "--out=o"})
	if !opts.dryRun || !opts.json || opts.clean || opts.configPath != "c.yml" || opts.entitiesDir != "e" || opts.from != "f.yml" || opts.outputDir != "o" {
		t.Fatalf("opts = %#v", opts)
	}
	if !opts.cleanSet || !opts.entitiesSet || !opts.outputSet {
		t.Fatal("set flags not tracked")
	}
}

func TestRunBuildWithGeneration(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module bgen\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// An entities dir triggers the codegen step inside runBuild.
	entDir := filepath.Join(dir, "entities")
	if err := os.MkdirAll(entDir, 0o755); err != nil {
		t.Fatal(err)
	}
	decl := `{"name":"posts","table":"posts","fields":[{"name":"title","type":"string"}]}`
	if err := os.WriteFile(filepath.Join(entDir, "posts.json"), []byte(decl), 0o644); err != nil {
		t.Fatal(err)
	}
	// The generated gen/entities package imports the framework, which
	// this throwaway module doesn't require — so `go vet ./...` inside
	// runBuild fails and the function exits. We only need the codegen-
	// detection + vet branches exercised, so capture the exit.
	out := covT_capStdout(t, func() {
		_ = covT_capExit(t, func() { runBuild([]string{"-o=" + filepath.Join(dir, "bin", "srv")}) })
	})
	if !strings.Contains(out, "Generating") {
		t.Fatalf("expected codegen step: %s", out)
	}
}
