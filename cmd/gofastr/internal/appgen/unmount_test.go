package appgen

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
	"github.com/DonaldMurillo/gofastr/codegen"
	"github.com/DonaldMurillo/gofastr/core/settings"
)

// A sibling whose name starts with the tree's is not in the tree.
func TestRewriteImportPrefixKeepsSiblings(t *testing.T) {
	src := "package x\n\nimport (\n\t\"" + project.GofastrModule + "/battery/auth\"\n\t\"" + project.GofastrModule + "/battery/auth/oidc\"\n\t\"" + project.GofastrModule + "/battery/authz\"\n)\n\n// \"" + project.GofastrModule + "/battery/auth\" in a comment\n"
	got, changed, err := RewriteImportPrefix(src, project.GofastrModule+"/battery/auth", "example.com/app/auth")
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	for _, want := range []string{`"example.com/app/auth"`, `"example.com/app/auth/oidc"`, `"` + project.GofastrModule + `/battery/authz"`, `// "` + project.GofastrModule + `/battery/auth" in a comment`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in:\n%s", want, got)
		}
	}
}

func TestCheckUnmountOverlap(t *testing.T) {
	entries := []settings.Unmounted{{Source: project.GofastrModule + "/battery/auth", Path: "unmounted/battery/auth"}}
	for _, tc := range []struct {
		source, path string
		rewire       bool
		want         string
	}{
		{project.GofastrModule + "/battery/auth", "x", true, "already unmounted"},
		{project.GofastrModule + "/battery/auth/oidc", "x", true, "part of the unmounted copy"},
		{project.GofastrModule + "/battery", "x", true, "remount it first"},
		{project.GofastrModule + "/core/yaml", "unmounted/battery/auth/yaml", true, "overlaps"},
		{project.GofastrModule + "/battery/authz", "unmounted/battery/authz", true, ""},
		// generate package: a second copy of a source at its own path.
		{project.GofastrModule + "/battery/auth", "beta/auth", false, ""},
		{project.GofastrModule + "/battery/auth", "unmounted/battery/auth/x", false, "overlaps"},
	} {
		err := CheckUnmountOverlap(settings.Unmounted{Source: tc.source, Path: tc.path}, entries, tc.rewire)
		if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
			t.Errorf("%s at %s: err = %v, want %q", tc.source, tc.path, err, tc.want)
		}
	}
}

// A failed write takes back the directories it created inside the
// target too, not only the target itself.
func TestPackageCopyRollbackRemovesSubdirs(t *testing.T) {
	dir := t.TempDir()
	clitest.WriteTestModule(t, dir)
	clitest.CovT_chdir(t, dir)
	packageWriteFiles = func(files *codegen.FileSet, opts codegen.WriteOptions) error {
		if err := codegen.WriteFiles(files, opts); err != nil {
			return err
		}
		return errors.New("disk full")
	}
	t.Cleanup(func() { packageWriteFiles = codegen.WriteFiles })
	wd, _ := os.Getwd()
	tgt, err := ResolvePackageTarget("demo", "unmounted/demo", wd)
	if err != nil {
		t.Fatal(err)
	}
	files := []GeneratedFile{{Name: "demo.go", Content: "package demo\n"}, {Name: "a/b/c.go", Content: "package b\n"}, {Name: "a/d.go", Content: "package a\n"}}
	if _, err := WritePackageCopy(tgt, files); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "unmounted")); !os.IsNotExist(err) {
		entries, _ := os.ReadDir(filepath.Join(dir, "unmounted", "demo"))
		t.Errorf("the failed copy left %v behind: %v", entries, err)
	}
}

// The undo a successful write returns, used when a later step fails,
// removes the parents the write created and keeps a target that existed
// empty before.
func TestPackageCopyUndo(t *testing.T) {
	dir := t.TempDir()
	clitest.WriteTestModule(t, dir)
	clitest.CovT_chdir(t, dir)
	wd, _ := os.Getwd()
	files := []GeneratedFile{{Name: "demo.go", Content: "package demo\n"}, {Name: "a/d.go", Content: "package a\n"}}

	tgt, err := ResolvePackageTarget("demo", "unmounted/battery/demo", wd)
	if err != nil {
		t.Fatal(err)
	}
	undo, err := WritePackageCopy(tgt, files)
	if err != nil {
		t.Fatal(err)
	}
	undo()
	if _, err := os.Stat(filepath.Join(dir, "unmounted")); !os.IsNotExist(err) {
		t.Errorf("undo left the parents it created: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(dir, "mine"), 0o755); err != nil {
		t.Fatal(err)
	}
	tgt, err = ResolvePackageTarget("demo", "mine", wd)
	if err != nil {
		t.Fatal(err)
	}
	if undo, err = WritePackageCopy(tgt, files); err != nil {
		t.Fatal(err)
	}
	undo()
	if entries, err := os.ReadDir(filepath.Join(dir, "mine")); err != nil || len(entries) != 0 {
		t.Errorf("undo removed the target that existed, or left files: %v %v", entries, err)
	}
}
