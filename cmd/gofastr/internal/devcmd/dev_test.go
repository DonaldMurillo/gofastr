package devcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResolveDevIsolationRemapsAddrAndChildEnv(t *testing.T) {
	for _, key := range []string{
		"GOFASTR_ISOLATION",
		"GOFASTR_ISOLATION_APPLIED",
		"GOFASTR_ISOLATION_ID",
		"GOFASTR_ISOLATION_PORT_8080",
	} {
		t.Setenv(key, "")
	}
	dir := t.TempDir()
	writeDevFile(t, filepath.Join(dir, ".git"), "gitdir: "+filepath.Join(t.TempDir(), ".git", "worktrees", "feature")+"\n")
	writeDevFile(t, filepath.Join(dir, "gofastr.settings.yml"), `
isolation:
  enabled: true
  port:
    offset: 1000
    range: 1
    scan: 0
`)

	rt, addr, err := resolveDevIsolation(dir, "localhost:8080")
	if err != nil {
		t.Fatalf("resolveDevIsolation: %v", err)
	}
	if !rt.Active() {
		t.Fatal("expected dev isolation to be active")
	}
	if addr != "localhost:9080" {
		t.Fatalf("addr = %q, want localhost:9080", addr)
	}
	env := devEnvMap(rt.Env([]string{"PORT=localhost:8080"}))
	if env["PORT"] != "localhost:9080" {
		t.Fatalf("child PORT = %q, want localhost:9080", env["PORT"])
	}
	if env["GOFASTR_ISOLATION_APPLIED"] != "1" || env["GOFASTR_ISOLATION_PORT_8080"] != "9080" {
		t.Fatalf("child env missing isolation markers: %#v", env)
	}
}

// TestParseDevFlagsAcceptsEqualsAndSpaceForms pins that dev's hand-rolled
// flag parser accepts BOTH the documented `--flag=value` form and the
// `--flag value` space form for every flag it knows. Before the fix, only
// the space form was parsed and `--addr=localhost:9090` (the form the help
// text shows) was silently ignored.
func TestParseDevFlagsAcceptsEqualsAndSpaceForms(t *testing.T) {
	cases := []struct {
		name   string
		args   []string
		addr   string
		dir    string
		pkg    string
		noA11y bool
	}{
		{"addr equals", []string{"--addr=localhost:9090"}, "localhost:9090", ".", ".", false},
		{"addr space", []string{"--addr", "localhost:9090"}, "localhost:9090", ".", ".", false},
		{"p equals", []string{"-p=9090"}, "localhost:9090", ".", ".", false},
		{"p space", []string{"-p", "9090"}, "localhost:9090", ".", ".", false},
		{"dir equals", []string{"--dir=./app"}, "localhost:8080", "./app", ".", false},
		{"dir space", []string{"--dir", "./app"}, "localhost:8080", "./app", ".", false},
		{"pkg equals", []string{"--pkg=./cmd/srv"}, "localhost:8080", ".", "./cmd/srv", false},
		{"pkg space", []string{"--pkg", "./cmd/srv"}, "localhost:8080", ".", "./cmd/srv", false},
		{"no-a11y", []string{"--no-a11y"}, "localhost:8080", ".", ".", true},
		{"combined", []string{"--addr=:9000", "--dir=app", "--pkg=./cmd/srv", "--no-a11y"}, ":9000", "app", "./cmd/srv", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			addr, dir, pkg, noA11y, err := parseDevFlags(tc.args)
			if err != nil {
				t.Fatalf("parseDevFlags(%v): %v", tc.args, err)
			}
			if addr != tc.addr || dir != tc.dir || pkg != tc.pkg || noA11y != tc.noA11y {
				t.Fatalf("parseDevFlags(%v) = (addr=%q dir=%q pkg=%q noA11y=%v), want (addr=%q dir=%q pkg=%q noA11y=%v)",
					tc.args, addr, dir, pkg, noA11y, tc.addr, tc.dir, tc.pkg, tc.noA11y)
			}
		})
	}
}

// A flag missing its value took the next flag as one (`--addr --no-a11y`
// set the address to "--no-a11y" and kept the a11y gate on), and an
// unknown flag kept every default.
func TestParseDevFlagsRefusesTypos(t *testing.T) {
	for _, args := range [][]string{
		{"--addr", "--no-a11y"},
		{"--dir", "-p", "9000"},
		{"--adr=:9000"},
		{"--no-a11y=false"},
		{"--addr=:9000", "-p", "9001"},
	} {
		if _, _, _, _, err := parseDevFlags(args); err == nil {
			t.Errorf("parseDevFlags(%q) accepted", args)
		}
	}
}

func writeDevFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// buildDevChildEnv must inject GOFASTR_DEV=1 so it wins on BOTH macOS
// (last-occurrence semantics) and Linux glibc (first-occurrence). A
// parent shell setting GOFASTR_DEV=0 must not survive into the child.
func TestBuildDevChildEnvOverridesParentDisable(t *testing.T) {
	parent := []string{
		"PATH=/usr/bin",
		"GOFASTR_DEV=0", // user tries to disable
		"HOME=/tmp",
	}
	out := BuildDevChildEnv(parent, "localhost:8080")

	// First occurrence of GOFASTR_DEV must be "1" (Linux first-wins).
	first := ""
	for _, kv := range out {
		if strings.HasPrefix(kv, "GOFASTR_DEV=") {
			first = strings.TrimPrefix(kv, "GOFASTR_DEV=")
			break
		}
	}
	if first != "1" {
		t.Fatalf("first GOFASTR_DEV entry = %q, want 1; got env:\n%v", first, out)
	}

	// No leftover GOFASTR_DEV=0 anywhere. Duplicates confuse audit
	// tools and depend on platform semantics.
	count := 0
	for _, kv := range out {
		if strings.HasPrefix(kv, "GOFASTR_DEV=") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one GOFASTR_DEV entry, got %d:\n%v", count, out)
	}

	// Other env vars must survive untouched.
	got := devEnvMap(out)
	if got["PATH"] != "/usr/bin" || got["HOME"] != "/tmp" {
		t.Fatalf("non-target env clobbered: %#v", got)
	}
}

func devEnvMap(env []string) map[string]string {
	out := map[string]string{}
	for _, pair := range env {
		for i := 0; i < len(pair); i++ {
			if pair[i] == '=' {
				out[pair[:i]] = pair[i+1:]
				break
			}
		}
	}
	return out
}

// buildDevChildEnv must inject PORT=<addr> so the generated scaffold's
// getEnv("PORT", …) binds the address `gofastr dev --addr` resolved,
// instead of silently falling back to the committed .env default (the
// scaffold never parses argv, so --addr alone reaches nothing). A PORT
// already exported in the parent shell must be overridden; dedup to a
// single entry so the override is platform-independent (Linux getenv is
// first-wins, macOS last-wins).
func TestBuildDevChildEnvInjectsResolvedPort(t *testing.T) {
	parent := []string{
		"PATH=/usr/bin",
		"PORT=localhost:8080", // stale shell export / inherited .env
		"HOME=/tmp",
	}
	out := BuildDevChildEnv(parent, "localhost:9000")

	got := devEnvMap(out)
	if got["PORT"] != "localhost:9000" {
		t.Fatalf("PORT = %q, want localhost:9000 (resolved --addr must win over parent/.env):\n%v", got["PORT"], out)
	}
	count := 0
	for _, kv := range out {
		if strings.HasPrefix(kv, "PORT=") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one PORT entry, got %d:\n%v", count, out)
	}
	// The GOFASTR_DEV concern must survive alongside the new PORT concern.
	if got["GOFASTR_DEV"] != "1" {
		t.Fatalf("GOFASTR_DEV=1 lost when PORT added: %#v", got)
	}
	if got["PATH"] != "/usr/bin" || got["HOME"] != "/tmp" {
		t.Fatalf("non-target env clobbered: %#v", got)
	}
}

// With no resolved addr, buildDevChildEnv must leave PORT alone. It must
// not fabricate PORT= and clobber an operator's own export.
func TestBuildDevChildEnvNoAddrLeavesPortUntouched(t *testing.T) {
	parent := []string{"PORT=localhost:8080", "HOME=/tmp"}
	out := BuildDevChildEnv(parent, "")
	got := devEnvMap(out)
	if got["PORT"] != "localhost:8080" {
		t.Fatalf("empty addr must not rewrite PORT; got %q", got["PORT"])
	}
}

// ─── scanModTimes + changed ──────────────────────────────────────────

func TestScanModTimesPicksUpGoFiles(t *testing.T) {
	dir := t.TempDir()
	writeDevFile(t, filepath.Join(dir, "main.go"), "package main")
	result := ScanModTimes(dir)
	if len(result) != 1 {
		t.Fatalf("scanModTimes found %d files, want 1: %+v", len(result), result)
	}
}

func TestScanModTimesPicksUpJSFiles(t *testing.T) {
	dir := t.TempDir()
	writeDevFile(t, filepath.Join(dir, "runtime.js"), "// JS")
	result := ScanModTimes(dir)
	if len(result) != 1 {
		t.Fatalf("scanModTimes found %d files, want 1 (.js should be watched): %+v", len(result), result)
	}
}

func TestScanModTimesPicksUpCSSFiles(t *testing.T) {
	dir := t.TempDir()
	writeDevFile(t, filepath.Join(dir, "theme.css"), "/* CSS */")
	result := ScanModTimes(dir)
	if len(result) != 1 {
		t.Fatalf("scanModTimes found %d files, want 1 (.css should be watched): %+v", len(result), result)
	}
}

func TestScanModTimesPicksUpHTMLFiles(t *testing.T) {
	dir := t.TempDir()
	writeDevFile(t, filepath.Join(dir, "index.html"), "<html></html>")
	result := ScanModTimes(dir)
	if len(result) != 1 {
		t.Fatalf("scanModTimes found %d files, want 1 (.html should be watched): %+v", len(result), result)
	}
}

func TestScanModTimesPicksUpMDFiles(t *testing.T) {
	dir := t.TempDir()
	writeDevFile(t, filepath.Join(dir, "llm.md"), "# doc")
	result := ScanModTimes(dir)
	if len(result) != 1 {
		t.Fatalf("scanModTimes found %d files, want 1 (.md is embedded like .js/.css/.html): %+v", len(result), result)
	}
}

func TestScanModTimesIgnoresVendorGitNodeModules(t *testing.T) {
	dir := t.TempDir()
	writeDevFile(t, filepath.Join(dir, "main.go"), "package main")
	writeDevFile(t, filepath.Join(dir, "vendor", "v.go"), "package v")
	writeDevFile(t, filepath.Join(dir, ".git", "g.go"), "package g")
	writeDevFile(t, filepath.Join(dir, "node_modules", "n.go"), "package n")
	result := ScanModTimes(dir)
	if len(result) != 1 {
		t.Fatalf("scanModTimes found %d files, want 1 (vendor/.git/node_modules ignored): %+v", len(result), result)
	}
}

func TestChangedDetectsNewFile(t *testing.T) {
	dir := t.TempDir()
	prev := ScanModTimes(dir)
	writeDevFile(t, filepath.Join(dir, "main.go"), "package main")
	curr := ScanModTimes(dir)
	if !Changed(prev, curr) {
		t.Fatal("changed() = false, want true after adding a file")
	}
}

func TestChangedDetectsModifiedFile(t *testing.T) {
	dir := t.TempDir()
	writeDevFile(t, filepath.Join(dir, "main.go"), "package main")
	prev := ScanModTimes(dir)
	// Modify the file. Use os.Chtimes to force a different mtime,
	// avoiding flakiness on filesystems with coarse mtime granularity.
	writeDevFile(t, filepath.Join(dir, "main.go"), "package main // modified")
	future := time.Now().Add(time.Second)
	os.Chtimes(filepath.Join(dir, "main.go"), future, future)
	curr := ScanModTimes(dir)
	if !Changed(prev, curr) {
		t.Fatal("changed() = false, want true after modifying a file")
	}
}

func TestChangedDetectsDeletedFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "main.go")
	writeDevFile(t, f, "package main")
	prev := ScanModTimes(dir)
	os.Remove(f)
	curr := ScanModTimes(dir)
	if !Changed(prev, curr) {
		t.Fatal("changed() = false, want true after deleting a file")
	}
}
