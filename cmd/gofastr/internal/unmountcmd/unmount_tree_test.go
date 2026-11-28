package unmountcmd

import (
	"context"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
)

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, "framework", "demo", filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		clitest.WriteTestFile(t, p, body)
	}
	return dir
}

func TestReadUnmountTreeSkipsNestedModules(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"demo.go":          "package demo\n",
		"plugin/go.mod":    "module example.com/plugin\n",
		"plugin/plugin.go": "package plugin\n",
	})
	tree, err := readUnmountTree(dir, project.GofastrModule+"/framework/demo", "v0.91.0")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range tree.Files {
		if strings.HasPrefix(f.Name, "plugin/") {
			t.Fatalf("copied %s from a nested module", f.Name)
		}
	}
}

func TestReadUnmountTreeRefusals(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"no Go files of its own":  {"sub/sub.go": "package sub\n"},
		"already exists upstream": {"demo.go": "package demo\n", appgen.UnmountHookFile: "package demo\n"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := writeTree(t, files)
			if _, err := readUnmountTree(dir, project.GofastrModule+"/framework/demo", "v0.91.0"); err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("readUnmountTree = %v, want %q", err, name)
			}
		})
	}
}

func TestUnmountRefusesATargetInANestedModule(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go toolchain in a temp module")
	}
	dir := writeUnmountModule(t, "package main\n\nfunc main() {}\n")
	clitest.WriteFileAll(t, filepath.Join(dir, "nested", "go.mod"), "module example.com/nested\n\ngo 1.24\n")
	out, code := runUnmountCmd(t, RunUnmount, "core/yaml", "--out=nested/yaml")
	if code != 1 || !strings.Contains(out, "not in this module") {
		t.Fatalf("exit %d, want a refusal of the nested module:\n%s", code, out)
	}
}

func TestPlanImportRewireKeepsGofmtCleanFilesClean(t *testing.T) {
	dir := t.TempDir()
	src := "package app\n\nimport (\n\t\"example.com/app/b\"\n\t\"github.com/DonaldMurillo/gofastr/core/yaml\"\n)\n\nvar _, _ = b.X, yaml.Y\n"
	clitest.WriteFileAll(t, filepath.Join(dir, "app.go"), src)
	plan, err := planImportRewire(dir, "unmounted", project.GofastrModule+"/core/yaml", "example.com/app/a/yaml")
	if err != nil || len(plan) != 1 {
		t.Fatalf("plan = %v, %v", plan, err)
	}
	if formatted, err := format.Source([]byte(plan[0].new)); err != nil || string(formatted) != plan[0].new {
		t.Fatalf("a gofmt-clean file is no longer gofmt-clean after the rewire:\n%s", plan[0].new)
	}
}

func TestGofastrModuleSourceRefusesGofastrItself(t *testing.T) {
	old := goListModule
	t.Cleanup(func() { goListModule = old })
	goListModule = func(context.Context, string) ([]byte, error) {
		return []byte(`{"Path":"` + project.GofastrModule + `","Main":true,"Dir":"/src"}`), nil
	}
	if _, _, err := gofastrModuleSource(context.Background(), t.TempDir()); err == nil || !strings.Contains(err.Error(), "gofastr's own module") {
		t.Fatalf("gofastrModuleSource in gofastr itself = %v", err)
	}
}
