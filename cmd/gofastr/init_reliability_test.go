package main

import (
	"os"
	"os/exec"
	"path/filepath"
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
