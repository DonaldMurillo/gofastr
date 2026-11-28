package clitest

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// AddSetup chdirs into a new module holding yml as gofastr.yml.
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

// ReadFile returns dir/rel, failing the test when it cannot be read.
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

// AddScreenBaseAppSpec is an app spec with an entity and one standalone screen.
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
