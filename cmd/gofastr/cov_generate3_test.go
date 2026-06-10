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
	opts := parseGenerateOptions([]string{"--dry-run", "--json", "--no-clean", "--config=c.yml", "--from=f.yml", "--out=o"})
	if !opts.dryRun || !opts.json || opts.clean || opts.configPath != "c.yml" || opts.from != "f.yml" || opts.outputDir != "o" {
		t.Fatalf("opts = %#v", opts)
	}
	if !opts.cleanSet || !opts.outputSet {
		t.Fatal("set flags not tracked")
	}
}

// The space form `--from f.yml` must work too — silently ignoring it
// yields a misleading "Nothing to generate".
func TestFromFlagSpaceForm(t *testing.T) {
	opts := parseGenerateOptions([]string{"--from", "f.yml", "--dry-run"})
	if opts.from != "f.yml" {
		t.Fatalf("--from f.yml not parsed: %#v", opts)
	}
	if !opts.dryRun {
		t.Fatalf("flag after space-form value lost: %#v", opts)
	}
	// A trailing bare --from has no value to consume.
	if opts := parseGenerateOptions([]string{"--from"}); opts.from != "" {
		t.Fatalf("bare --from must stay empty: %#v", opts)
	}
	// The next flag is not a value for --from.
	if opts := parseGenerateOptions([]string{"--from", "--json"}); opts.from != "" || !opts.json {
		t.Fatalf("--from must not eat the next flag: %#v", opts)
	}
}
