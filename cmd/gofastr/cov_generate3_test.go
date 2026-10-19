package main

import (
	"path/filepath"
	"testing"
)

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
	opts, err := parseGenerateOptions([]string{"--dry-run", "--json", "--no-clean", "--config=c.yml", "--out=o"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.dryRun || !opts.json || opts.clean || opts.configPath != "c.yml" || opts.outputDir != "o" {
		t.Fatalf("opts = %#v", opts)
	}
	if !opts.cleanSet || !opts.outputSet {
		t.Fatal("set flags not tracked")
	}
	opts, err = parseGenerateOptions([]string{"--config", "c.yml", "--dry-run"})
	if err != nil || opts.configPath != "c.yml" || !opts.dryRun {
		t.Fatalf("space form: opts = %#v, err = %v", opts, err)
	}
}

// Plain `gofastr generate` runs the codegen config; an argument it does
// not know is an error, never dropped. A dropped `--from x` used to turn
// into a misleading "Nothing to generate".
func TestGenerateRejectsUnknownArgs(t *testing.T) {
	for _, args := range [][]string{{"--from=f.yml"}, {"--force"}, {"--add"}, {"--config"}, {"--out", "--json"}} {
		if _, err := parseGenerateOptions(args); err == nil {
			t.Errorf("parseGenerateOptions(%q) accepted it", args)
		}
	}
}

func TestGenerateAppArgs(t *testing.T) {
	opts, err := parseAppGenerateArgs(nil)
	if err != nil || opts.spec != "gofastr.yml" {
		t.Fatalf("default spec: opts = %#v, err = %v", opts, err)
	}
	opts, err = parseAppGenerateArgs([]string{"specs/", "--out", "app", "--force", "--dry-run", "--json"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.spec != "specs/" || opts.outputDir != "app" || !opts.outputSet || !opts.force || !opts.dryRun || !opts.json {
		t.Fatalf("opts = %#v", opts)
	}
	for _, args := range [][]string{{"a.yml", "b.yml"}, {"--from=a.yml"}, {"--out"}, {"--out", "--json"}} {
		if _, err := parseAppGenerateArgs(args); err == nil {
			t.Errorf("parseAppGenerateArgs(%q) accepted it", args)
		}
	}
}
