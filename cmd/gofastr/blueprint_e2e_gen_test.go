package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// e2eBlueprintYAML is a minimal but valid blueprint (module + screens) that
// triggers e2e_test.go generation. The db.driver/url are parameterised so the
// same fixture exercises both the SQLite (file DSN) and Postgres (provisioned
// DB) branches of the generated end-to-end test.
func e2eBlueprintYAML(t *testing.T, driver, dbURL string) string {
	t.Helper()
	return strings.Join([]string{
		"app:",
		"  name: Demo",
		"  module: example.com/demo",
		"  db:",
		"    driver: " + driver,
		"    url: " + dbURL,
		"entities:",
		"  - name: posts",
		"    crud: true",
		"    fields:",
		"      - name: title",
		"        type: string",
		"        required: true",
		"screens:",
		"  - name: home",
		"    route: /",
		"    title: Home",
		"    body:",
		"      - type: heading",
		"        level: 1",
		"        text: Demo",
		"",
	}, "\n")
}

// loadE2EBlueprint writes the YAML to a temp file and returns the decoded
// blueprint, failing the test on any decode error.
func loadE2EBlueprint(t *testing.T, yml string) Blueprint {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, yml)
	bp, err := loadBlueprint(path)
	if err != nil {
		t.Fatalf("loadBlueprint: %v", err)
	}
	return bp
}

func e2eTestGo(t *testing.T, driver string) string {
	t.Helper()
	url := "file:demo.db"
	if driver == "postgres" {
		url = "postgres://demo:demo@localhost:5432/demo?sslmode=disable"
	}
	bp := loadE2EBlueprint(t, e2eBlueprintYAML(t, driver, url))
	files := mustRenderBlueprintFiles(t, bp)
	byName := filesByName(files)
	got, ok := byName["e2e_test.go"]
	if !ok {
		t.Fatalf("e2e_test.go not generated for driver=%q; files: %v", driver, fileNames(files))
	}
	return got
}

// TestE2ETemplateAppendsExeOnWindows asserts the generated e2e test builds the
// binary with a Windows-aware name so `exec.Command(bin)` resolves on GOOS=windows
// (issue #68: the old template hard-coded "app", which Windows can't exec).
func TestE2ETemplateAppendsExeOnWindows(t *testing.T) {
	got := e2eTestGo(t, "sqlite")
	assertContains(t, got, `if runtime.GOOS == "windows"`)
	assertContains(t, got, `bin += ".exe"`)
	assertContains(t, got, `"runtime"`)
}

// TestE2ETemplateSQLiteKeepsFileDSN asserts a SQLite blueprint still boots the
// child against a disposable file database (unchanged behaviour).
func TestE2ETemplateSQLiteKeepsFileDSN(t *testing.T) {
	got := e2eTestGo(t, "sqlite")
	assertContains(t, got, `DATABASE_URL=file:`)
	if strings.Contains(got, "e2ePostgresDSN") {
		t.Error("sqlite e2e test must not emit the postgres provisioning helper")
	}
	if strings.Contains(got, "DB_DRIVER=postgres") {
		t.Error("sqlite e2e test must not force DB_DRIVER=postgres")
	}
}

// TestE2ETemplatePostgresProvisionsDB asserts a postgres blueprint provisions a
// throwaway database from TEST_POSTGRES_DSN and skips when Postgres is
// unreachable — instead of handing the postgres driver a SQLite file DSN it
// cannot open (issue #68: the old template always used `DATABASE_URL=file:`).
func TestE2ETemplatePostgresProvisionsDB(t *testing.T) {
	got := e2eTestGo(t, "postgres")
	assertContains(t, got, "func e2ePostgresDSN(t *testing.T) string")
	assertContains(t, got, `os.Getenv("TEST_POSTGRES_DSN")`)
	assertContains(t, got, `t.Skip("TEST_POSTGRES_DSN unset; skipping postgres e2e")`)
	assertContains(t, got, `DATABASE_URL="+dbURL, "DB_DRIVER=postgres`)
	assertContains(t, got, `"database/sql"`)
	assertContains(t, got, `"context"`)
	if strings.Contains(got, "DATABASE_URL=file:") {
		t.Error("postgres e2e test must not use a SQLite file DSN")
	}
}

// TestE2ETemplatePostgresCompiles generates a full postgres-driver app and
// type-checks its packages under `go test -short`. -short skips TestE2E, but
// still COMPILES e2e_test.go (including the postgres provisioning helper),
// proving the generated postgres template is valid, linkable Go — the
// regression guard for issue #68's postgres defect.
func TestE2ETemplatePostgresCompiles(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	goVersion, err := repoGoVersion(repoRoot)
	if err != nil {
		t.Fatalf("repoGoVersion: %v", err)
	}
	goMod := "module example.com/demo\n\ngo " + goVersion + "\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => " + repoRoot + "\n"
	writeTestFile(t, filepath.Join(dir, "go.mod"), goMod)
	if err := copyGoSum(repoRoot, dir); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}
	writeTestFile(t, filepath.Join(dir, "gofastr.yml"),
		e2eBlueprintYAML(t, "postgres", "postgres://demo:demo@localhost:5432/demo?sslmode=disable"))
	bp, err := loadBlueprint(filepath.Join(dir, "gofastr.yml"))
	if err != nil {
		t.Fatalf("loadBlueprint: %v", err)
	}
	bp.App.OutputDir = "gen"
	files := mustRenderBlueprintFiles(t, bp)
	for _, file := range files {
		full := filepath.Join(dir, "gen", file.name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(file.content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// -short skips the emitted e2e_test.go (which builds + boots the binary);
	// this proves the generated postgres packages — e2e_test.go included —
	// compile, including the lib/pq-linked driver and the provisioning helper.
	cmd := exec.Command("go", "test", "-short", "-mod=mod", "./gen/entities", "./gen")
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("postgres generated app did not compile: %v\n%s", err, output)
	}
}
