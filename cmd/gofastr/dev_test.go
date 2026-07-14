package main

import (
	"os"
	"os/exec"
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
	writeDevFile(t, filepath.Join(dir, "gofastr.yml"), `
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

// Smoke test: a fresh `gofastr init` lands the AI-agent files
// alongside the app scaffold. Runs the real binary as a subprocess
// (runInit uses os.Exit so it can't be called in-process) so the test
// catches the silent-drift class where init.go forgets to call
// writeAgentDetailFiles or writeHostSkill after a refactor.
func TestInitDropsAIAgentFiles(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	// Build the binary inside the gofastr module (where go.mod lives),
	// then invoke it from the tempdir — `go run` from outside any
	// module fails to resolve cmd/gofastr's imports.
	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "gofastr")
	build := exec.Command("go", "build", "-o", binPath, "./cmd/gofastr")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build gofastr: %v\n%s", err, out)
	}

	work := t.TempDir()
	cmd := exec.Command(binPath, "init", "smoke", "--no-entity")
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gofastr init: %v\n%s", err, out)
	}

	for _, rel := range []string{
		"smoke/AGENTS.md",
		"smoke/DESIGN.md",
		"smoke/agents/framework.md",
		"smoke/agents/battery-admin.md",
		"smoke/agents/battery-log.md",
		"smoke/agents/battery-print.md",
		"smoke/.claude/skills/gofastr-host/SKILL.md",
	} {
		path := filepath.Join(work, rel)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected file missing after `gofastr init`: %s — init.go likely dropped a writer call\nstdout/stderr:\n%s", rel, out)
		}
	}

	// AGENTS.md must be the thin TOC, not the old 500-line inline form.
	body, _ := os.ReadFile(filepath.Join(work, "smoke", "AGENTS.md"))
	if !strings.Contains(string(body), "| Section | Use this when | Details |") {
		t.Error("init wrote AGENTS.md without the TOC header — regression to old inline shape")
	}

	// A detail file must carry the AUTO-GENERATED sentinel — proves
	// writeAgentDetailFiles ran with the right header.
	detail, _ := os.ReadFile(filepath.Join(work, "smoke", "agents", "framework.md"))
	if !strings.HasPrefix(string(detail), "<!-- AUTO-GENERATED") {
		t.Error("agents/framework.md missing AUTO-GENERATED header — header writer regressed")
	}
}

func TestInitGeneratedMainUsesIsolationHelpers(t *testing.T) {
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	if err := os.Mkdir("demo", 0o755); err != nil {
		t.Fatal(err)
	}

	writeMainGo("demo", "example.com/demo", false, "sqlite", "file:demo.db")
	data, err := os.ReadFile(filepath.Join("demo", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{
		`"github.com/DonaldMurillo/gofastr/framework/isolation"`,
		`runtimeIsolation, err := isolation.Resolve(".")`,
		`runtimeIsolation.Database("sqlite3", getEnv("DATABASE_URL", "file:demo.db"))`,
		`runtimeIsolation.Addr(getEnv("PORT", "localhost:8080"))`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("generated main.go missing %q:\n%s", want, got)
		}
	}
}

func TestInitGeneratedUIIsFrameworkComposed(t *testing.T) {
	dir := t.TempDir()
	project := filepath.Join(dir, "demo")
	if err := os.MkdirAll(filepath.Join(project, "screens"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeMainGo(project, "example.com/demo", true, "sqlite", "")
	writeHomeScreen(project, true)

	mainBody, err := os.ReadFile(filepath.Join(project, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	homeBody, err := os.ReadFile(filepath.Join(project, "screens", "home.go"))
	if err != nil {
		t.Fatal(err)
	}
	combined := string(mainBody) + "\n" + string(homeBody)
	for _, forbidden := range []string{
		"WithCustomCSS", "CreateStyleSheet", "style.NewStyleSheet", "render.Tag(",
	} {
		if strings.Contains(combined, forbidden) {
			t.Errorf("generated UI must not contain app-owned styling or structural markup %q", forbidden)
		}
	}
	for _, required := range []string{"ui.Container", "ui.Stack", "ui.PageHeader", "ui.Section"} {
		if !strings.Contains(string(homeBody), required) {
			t.Errorf("generated home screen missing framework primitive %q", required)
		}
	}
}

// The generated gofastr.yml must not advertise strategy values that the
// isolation resolver doesn't consult — a reader who edits "strategy: path"
// to "strategy: foo" expects to see behavior change, and nothing happens.
// Drop the decorative field rather than mislead.
func TestInitIsolationConfigOmitsDecorativeStrategy(t *testing.T) {
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	if err := os.Mkdir("demo", 0o755); err != nil {
		t.Fatal(err)
	}

	writeIsolationConfig("demo", "sqlite")
	data, err := os.ReadFile(filepath.Join("demo", "gofastr.yml"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, bad := range []string{"strategy: path", "strategy: offset"} {
		if strings.Contains(got, bad) {
			t.Errorf("generated yml advertises unrecognized %q:\n%s", bad, got)
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
	out := buildDevChildEnv(parent)

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

	// No leftover GOFASTR_DEV=0 anywhere — duplicates confuse audit
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

// ─── scanModTimes + changed ──────────────────────────────────────────

func TestScanModTimesPicksUpGoFiles(t *testing.T) {
	dir := t.TempDir()
	writeDevFile(t, filepath.Join(dir, "main.go"), "package main")
	result := scanModTimes(dir)
	if len(result) != 1 {
		t.Fatalf("scanModTimes found %d files, want 1: %+v", len(result), result)
	}
}

func TestScanModTimesPicksUpJSFiles(t *testing.T) {
	dir := t.TempDir()
	writeDevFile(t, filepath.Join(dir, "runtime.js"), "// JS")
	result := scanModTimes(dir)
	if len(result) != 1 {
		t.Fatalf("scanModTimes found %d files, want 1 (.js should be watched): %+v", len(result), result)
	}
}

func TestScanModTimesPicksUpCSSFiles(t *testing.T) {
	dir := t.TempDir()
	writeDevFile(t, filepath.Join(dir, "theme.css"), "/* CSS */")
	result := scanModTimes(dir)
	if len(result) != 1 {
		t.Fatalf("scanModTimes found %d files, want 1 (.css should be watched): %+v", len(result), result)
	}
}

func TestScanModTimesPicksUpHTMLFiles(t *testing.T) {
	dir := t.TempDir()
	writeDevFile(t, filepath.Join(dir, "index.html"), "<html></html>")
	result := scanModTimes(dir)
	if len(result) != 1 {
		t.Fatalf("scanModTimes found %d files, want 1 (.html should be watched): %+v", len(result), result)
	}
}

func TestScanModTimesIgnoresVendorGitNodeModules(t *testing.T) {
	dir := t.TempDir()
	writeDevFile(t, filepath.Join(dir, "main.go"), "package main")
	writeDevFile(t, filepath.Join(dir, "vendor", "v.go"), "package v")
	writeDevFile(t, filepath.Join(dir, ".git", "g.go"), "package g")
	writeDevFile(t, filepath.Join(dir, "node_modules", "n.go"), "package n")
	result := scanModTimes(dir)
	if len(result) != 1 {
		t.Fatalf("scanModTimes found %d files, want 1 (vendor/.git/node_modules ignored): %+v", len(result), result)
	}
}

func TestChangedDetectsNewFile(t *testing.T) {
	dir := t.TempDir()
	prev := scanModTimes(dir)
	writeDevFile(t, filepath.Join(dir, "main.go"), "package main")
	curr := scanModTimes(dir)
	if !changed(prev, curr) {
		t.Fatal("changed() = false, want true after adding a file")
	}
}

func TestChangedDetectsModifiedFile(t *testing.T) {
	dir := t.TempDir()
	writeDevFile(t, filepath.Join(dir, "main.go"), "package main")
	prev := scanModTimes(dir)
	// Modify the file. Use os.Chtimes to force a different mtime,
	// avoiding flakiness on filesystems with coarse mtime granularity.
	writeDevFile(t, filepath.Join(dir, "main.go"), "package main // modified")
	future := time.Now().Add(time.Second)
	os.Chtimes(filepath.Join(dir, "main.go"), future, future)
	curr := scanModTimes(dir)
	if !changed(prev, curr) {
		t.Fatal("changed() = false, want true after modifying a file")
	}
}

func TestChangedDetectsDeletedFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "main.go")
	writeDevFile(t, f, "package main")
	prev := scanModTimes(dir)
	os.Remove(f)
	curr := scanModTimes(dir)
	if !changed(prev, curr) {
		t.Fatal("changed() = false, want true after deleting a file")
	}
}
