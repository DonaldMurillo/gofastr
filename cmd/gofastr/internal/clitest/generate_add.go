package clitest

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// AddSetup creates a temp dir with go.mod, chdirs into it, and writes a
// app spec yml. Returns (dir, bpPath).
func AddSetup(t *testing.T, yml string) (dir, bpPath string) {
	t.Helper()
	dir = t.TempDir()
	CovT_chdir(t, dir)
	WriteAddGoMod(t, dir, "example.com/addtest")
	bpPath = filepath.Join(dir, "gofastr.yml")
	if err := os.WriteFile(bpPath, []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, bpPath
}

func WriteAddGoMod(t *testing.T, dir, module string) {
	t.Helper()
	repoRoot := RepoRootDir(t)
	goVersion, err := RepoGoVersion(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	content := fmt.Sprintf("module %s\n\ngo %s\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => %s\n", module, goVersion, repoRoot)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CopyGoSum(repoRoot, dir); err != nil {
		t.Fatal(err)
	}
}

// ReadFile reads a file under dir, failing the test if it doesn't exist.
func ReadFile(t *testing.T, dir, rel string) string {
	t.Helper()
	//gofastr:allow(rootread) test helper: dir is the calling test's own generated tree
	data, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

// FileExists reports whether a path exists under dir.
func FileExists(dir, rel string) bool {
	//gofastr:allow(rootread) test helper: dir is the calling test's own generated tree
	_, err := os.Stat(filepath.Join(dir, rel))
	return err == nil
}

// ---- Test 10: --add a screen fragment into an existing project ----

// AddScreenBaseAppSpec is a base project with an entity + a standalone
// screen, so the per-screen layout (seam + screen_about.go) already exists.
func AddScreenBaseAppSpec(module string) string {
	return fmt.Sprintf(`app:
  name: AddScreen
  module: %s
  db:
    driver: sqlite
    url: file:test.db
entities:
  - name: posts
    fields:
      - name: title
        type: string
        required: true
screens:
  - name: about
    route: /about
    title: About
`, module)
}
