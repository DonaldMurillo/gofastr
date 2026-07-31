package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitGeneratedProjectsBuild(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	bin := buildGofastrBin(t)

	cases := []struct {
		name string
		args []string
	}{
		{name: "sqlite_default", args: []string{"init", "sqliteapp", "--module=example.com/sqliteapp"}},
		{name: "no_entity", args: []string{"init", "uiapp", "--module=example.com/uiapp", "--no-entity"}},
		{name: "postgres_compile", args: []string{"init", "pgapp", "--module=example.com/pgapp", "--db=postgres"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			work := t.TempDir()
			cmd := exec.Command(bin, tc.args...)
			cmd.Dir = work
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("gofastr %v: %v\n%s", tc.args, err, out)
			}

			project := filepath.Join(work, tc.args[1])
			prepareGeneratedModule(t, repoRoot, project)

			build := exec.Command("go", "build", ".")
			build.Dir = project
			build.Env = append(os.Environ(),
				"GOCACHE="+filepath.Join(t.TempDir(), "gocache"),
				"GOFLAGS=-mod=mod",
			)
			out, err = build.CombinedOutput()
			if err != nil {
				t.Fatalf("generated project did not build: %v\n%s", err, out)
			}
		})
	}
}

func TestInitGeneratedSQLiteMigrationsRunFromCLI(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	bin := buildGofastrBin(t)
	work := t.TempDir()

	initCmd := exec.Command(bin, "init", "migrateapp", "--module=example.com/migrateapp")
	initCmd.Dir = work
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("gofastr init: %v\n%s", err, out)
	}

	project := filepath.Join(work, "migrateapp")
	prepareGeneratedModule(t, repoRoot, project)

	up := exec.Command(bin, "migrate", "up", "--db-url="+filepath.Join(project, "cli-migrate.db"))
	up.Dir = project
	if out, err := up.CombinedOutput(); err != nil {
		t.Fatalf("gofastr migrate up: %v\n%s", err, out)
	}

	status := exec.Command(bin, "migrate", "status", "--db-url="+filepath.Join(project, "cli-migrate.db"))
	status.Dir = project
	if out, err := status.CombinedOutput(); err != nil {
		t.Fatalf("gofastr migrate status: %v\n%s", err, out)
	}
}

func TestInitCreatesGitRepo(t *testing.T) {
	bin := buildGofastrBin(t)
	work := t.TempDir()

	initCmd := exec.Command(bin, "init", "gitapp", "--module=example.com/gitapp")
	initCmd.Dir = work
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("gofastr init: %v\n%s", err, out)
	}

	project := filepath.Join(work, "gitapp")

	// git init should have created .git/
	gitDir := filepath.Join(project, ".git")
	if fi, err := os.Stat(gitDir); err != nil {
		t.Fatalf(".git directory not found: %v", err)
	} else if !fi.IsDir() {
		t.Fatalf(".git exists but is not a directory")
	}
}

func TestInitWritesCLAUDEmd(t *testing.T) {
	bin := buildGofastrBin(t)
	work := t.TempDir()

	initCmd := exec.Command(bin, "init", "claudeapp", "--module=example.com/claudeapp")
	initCmd.Dir = work
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("gofastr init: %v\n%s", err, out)
	}

	project := filepath.Join(work, "claudeapp")

	// CLAUDE.md should exist and reference key surfaces
	claude := filepath.Join(project, "CLAUDE.md")
	data, err := os.ReadFile(claude)
	if err != nil {
		t.Fatalf("CLAUDE.md not found: %v", err)
	}
	body := string(data)
	for _, substr := range []string{
		"AGENTS.md",
		"DESIGN.md",
		"gofastr-host",
		"gofastr agents sync",
		"gofastr docs",
		"gofastr docs --grep",
		"gofastr docs ui-composition-recipes",
	} {
		if !strings.Contains(body, substr) {
			t.Errorf("CLAUDE.md missing %q", substr)
		}
	}
}

func TestThemeInitGeneratedPackageBuildsFromCLI(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	bin := buildGofastrBin(t)
	project := t.TempDir()
	goVersion, err := repoGoVersion(repoRoot)
	if err != nil {
		t.Fatalf("repoGoVersion: %v", err)
	}
	goMod := "module example.com/themeapp\n\ngo " + goVersion + "\n"
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}

	theme := exec.Command(bin, "theme", "init")
	theme.Dir = project
	if out, err := theme.CombinedOutput(); err != nil {
		t.Fatalf("gofastr theme init: %v\n%s", err, out)
	}
	prepareGeneratedModule(t, repoRoot, project)

	test := exec.Command("go", "test", "-mod=mod", "./theme")
	test.Dir = project
	test.Env = append(os.Environ(), "GOCACHE="+filepath.Join(t.TempDir(), "gocache"))
	if out, err := test.CombinedOutput(); err != nil {
		t.Fatalf("generated theme package did not build: %v\n%s", err, out)
	}
}

func prepareGeneratedModule(t *testing.T, repoRoot, project string) {
	t.Helper()
	if err := copyGoSum(repoRoot, project); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}
	edit := exec.Command("go", "mod", "edit",
		"-require=github.com/DonaldMurillo/gofastr@v0.0.0",
		"-replace=github.com/DonaldMurillo/gofastr="+repoRoot,
	)
	edit.Dir = project
	if out, err := edit.CombinedOutput(); err != nil {
		t.Fatalf("go mod edit: %v\n%s", err, out)
	}
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = project
	tidy.Env = append(os.Environ(), "GOCACHE="+filepath.Join(t.TempDir(), "gocache"))
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy: %v\n%s", err, out)
	}
}

// TestInitEmitsFlatOwnedLayout pins the scaffold-and-own contract from
// framework/ARCHITECTURE.md ("No gen/"): `gofastr init` emits a flat package
// main at the module root (main.go + screens.go), NOT a screens/ subpackage
// or a gen/ quarantine directory. The generated project must build.
func TestInitEmitsFlatOwnedLayout(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	bin := buildGofastrBin(t)
	work := t.TempDir()
	initCmd := exec.Command(bin, "init", "flatapp", "--module=example.com/flatapp")
	initCmd.Dir = work
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("gofastr init: %v\n%s", err, out)
	}
	project := filepath.Join(work, "flatapp")

	// Flat package main at the module root.
	for _, rel := range []string{"main.go", "screens.go", "entities/entities.go"} {
		if _, err := os.Stat(filepath.Join(project, rel)); err != nil {
			t.Errorf("expected flat file %s: %v", rel, err)
		}
	}
	// No screens/ subpackage and no gen/ quarantine dir for owned scaffold.
	for _, rel := range []string{"screens", "screens/home.go", "gen", "gen/.gitkeep"} {
		if _, err := os.Stat(filepath.Join(project, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Errorf("init must not emit %s (scaffold-and-own flat contract): %v", rel, err)
		}
	}
	// screens.go is owned package main: HomeScreen lives beside main.go, no import.
	screens, err := os.ReadFile(filepath.Join(project, "screens.go"))
	if err != nil {
		t.Fatal(err)
	}
	screensSrc := string(screens)
	if !strings.HasPrefix(screensSrc, "// Code generated by gofastr. Owned") {
		t.Errorf("screens.go must carry the owned-scaffold header:\n%s", screensSrc)
	}
	if !strings.Contains(screensSrc, "package main") || strings.Contains(screensSrc, "package screens") {
		t.Errorf("screens.go must be package main (HomeScreen beside main.go):\n%s", screensSrc)
	}
	mainSrc, err := os.ReadFile(filepath.Join(project, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(mainSrc), `"/screens"`) || strings.Contains(string(mainSrc), "screens.HomeScreen") {
		t.Errorf("main.go must not import a screens subpackage; HomeScreen is in package main:\n%s", mainSrc)
	}

	// The generated project builds.
	prepareGeneratedModule(t, repoRoot, project)
	build := exec.Command("go", "build", ".")
	build.Dir = project
	build.Env = append(os.Environ(),
		"GOCACHE="+filepath.Join(t.TempDir(), "gocache"),
		"GOFLAGS=-mod=mod",
	)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("generated flat project did not build: %v\n%s", err, out)
	}
}
