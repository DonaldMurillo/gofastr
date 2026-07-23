package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunInitFullProject(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	covT_capStdout(t, func() { runInit([]string{"myapp"}) })
	for _, rel := range []string{
		"myapp/main.go", "myapp/screens/home.go", "myapp/DESIGN.md",
		"myapp/entities/entities.go", "myapp/.env", "myapp/.gitignore",
		"myapp/AGENTS.md", "myapp/CLAUDE.md", "myapp/go.mod",
	} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("missing %s: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "myapp", "screens", "styles.go")); !os.IsNotExist(err) {
		t.Fatalf("generated app must not contain screens/styles.go: %v", err)
	}
	main, err := os.ReadFile(filepath.Join(dir, "myapp", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`uitheme "github.com/DonaldMurillo/gofastr/framework/ui/theme"`,
		`app.NewApp("myapp").WithTheme(uitheme.Default())`,
	} {
		if !strings.Contains(string(main), want) {
			t.Errorf("generated main.go missing adaptive framework theme %q\n%s", want, main)
		}
	}
}

func TestRunInitPinsFrameworkToCLIRelease(t *testing.T) {
	oldVersion := version
	version = "0.39.1"
	t.Cleanup(func() { version = oldVersion })

	dir := t.TempDir()
	covT_chdir(t, dir)
	out := covT_capStdout(t, func() { runInit([]string{"pinned"}) })
	mod, err := os.ReadFile(filepath.Join(dir, "pinned", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mod), "github.com/DonaldMurillo/gofastr v0.39.1") {
		t.Fatalf("scaffold did not pin the CLI release:\n%s", mod)
	}
	if !strings.Contains(out, "matching this CLI") {
		t.Fatalf("scaffold did not explain the pin:\n%s", out)
	}
}

func TestNormalizeFrameworkVersion(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		{"0.39.1", "v0.39.1"},
		{"v0.39.1", "v0.39.1"},
		{"dev", ""},
		{"(devel)", ""},
		{"not-a-version", ""},
	} {
		if got := normalizeFrameworkVersion(tc.in); got != tc.want {
			t.Errorf("normalizeFrameworkVersion(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRunInitNoEntityPostgres(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	covT_capStdout(t, func() { runInit([]string{"pgapp", "--no-entity", "--db=postgres", "--module=example.com/pgapp"}) })
	env, err := os.ReadFile(filepath.Join(dir, "pgapp", ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(env), "DATABASE_URL") {
		t.Fatal("no-entity .env should omit DATABASE_URL")
	}
	if _, err := os.Stat(filepath.Join(dir, "pgapp", "entities")); !os.IsNotExist(err) {
		t.Fatal("entities dir should not exist with --no-entity")
	}
}

func TestRunInitNoNameExits(t *testing.T) {
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runInit(nil) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestRunInitBadNameExits(t *testing.T) {
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runInit([]string{"Bad Name!"}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestRunInitExistingDirExits(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	if err := os.Mkdir(filepath.Join(dir, "dup"), 0o755); err != nil {
		t.Fatal(err)
	}
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runInit([]string{"dup"}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestRunInitEmptyModuleExits(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runInit([]string{"app", "--module="}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestValidateProjectName(t *testing.T) {
	good := []string{".", "myapp", "my-blog-app", "a1_b2"}
	for _, n := range good {
		if err := validateProjectName(n); err != nil {
			t.Errorf("validateProjectName(%q) = %v, want nil", n, err)
		}
	}
	bad := []string{"", "Bad", "with space", "-lead", "_lead", "name!"}
	for _, n := range bad {
		if err := validateProjectName(n); err == nil {
			t.Errorf("validateProjectName(%q) = nil, want error", n)
		}
	}
}

func TestClaudeMDContentAndWrite(t *testing.T) {
	if body := string(claudeMDContent()); !strings.Contains(body, "GoFastr host project") || !strings.Contains(body, "DESIGN.md") {
		t.Fatal("claudeMDContent")
	}
	dir := t.TempDir()
	if err := writeCLAUDEmd(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); err != nil {
		t.Fatalf("CLAUDE.md not written: %v", err)
	}
}

func TestDesignMDContentCapturesCompositionFailureModes(t *testing.T) {
	body := string(designMDContent())
	for _, want := range []string{
		"Chosen recipe and named framework primitives",
		"repeated across Banner/summary/header",
		"actions stay natural-width",
		"Opening summary is one or two sentences",
		"SiteHeader.MobileBrand",
		"next decision/action appears in the first useful viewport",
		"No narrow desktop detail column leaves an accidental empty half-canvas",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("DESIGN.md template missing %q", want)
		}
	}
}

func TestRunReinitFreshAndIdempotent(t *testing.T) {
	dir := t.TempDir()
	// Fresh: no AGENTS.md / CLAUDE.md yet → both created.
	covT_capStdout(t, func() { runReinit(dir, false) })
	for _, f := range []string{"AGENTS.md", "CLAUDE.md", "DESIGN.md"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("reinit did not create %s: %v", f, err)
		}
	}
	// Second run: files exist & unchanged → refresh path.
	covT_capStdout(t, func() { runReinit(dir, false) })
}

func TestRunReinitPreservesDesignDirection(t *testing.T) {
	dir := t.TempDir()
	covT_capStdout(t, func() { runReinit(dir, false) })
	designPath := filepath.Join(dir, "DESIGN.md")
	const custom = "# Product-specific direction\n\nKeep this exact composition decision.\n"
	if err := os.WriteFile(designPath, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	covT_capStdout(t, func() { runReinit(dir, true) })
	got, err := os.ReadFile(designPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != custom {
		t.Fatalf("reinit must preserve app-owned DESIGN.md; got:\n%s", got)
	}
}

func TestRunReinitModifiedClaude(t *testing.T) {
	dir := t.TempDir()
	covT_capStdout(t, func() { runReinit(dir, false) })
	// User-modify CLAUDE.md.
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# my own edits\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Without force → preserved (warn path).
	covT_capStdout(t, func() { runReinit(dir, false) })
	body, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if !strings.Contains(string(body), "my own edits") {
		t.Fatal("modified CLAUDE.md should be preserved without --force")
	}
	// With force → overwritten.
	covT_capStdout(t, func() { runReinit(dir, true) })
	body2, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if strings.Contains(string(body2), "my own edits") {
		t.Fatal("--force should overwrite CLAUDE.md")
	}
}

func TestRunInitReinitFlag(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	target := filepath.Join(dir, ".")
	_ = target
	// init . in-place with --reinit refreshes onboarding only.
	covT_capStdout(t, func() { runInit([]string{".", "--reinit"}) })
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Fatalf("reinit via runInit did not create AGENTS.md: %v", err)
	}
}
