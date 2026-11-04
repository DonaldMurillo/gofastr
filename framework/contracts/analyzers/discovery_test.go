package analyzers_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

func TestRunRejectsZeroSourceFiles(t *testing.T) {
	for _, fixture := range []struct {
		name  string
		files map[string]string
	}{
		{name: "empty"},
		{name: "config-only", files: map[string]string{
			"go.mod":               "module example.com/app\n\ngo 1.26\n",
			"gofastr.settings.yml": "contracts:\n  strict: true\n",
		}},
		{name: "excluded-only", files: map[string]string{
			"dist/source.go":     "package app\n",
			".hidden/source.go":  "package app\n",
			"_scratch/source.go": "package app\n",
		}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			root := t.TempDir()
			seedFixture(t, root, fixture.files)
			pass, err := contracts.NewPass(root, nil)
			if err != nil {
				t.Fatalf("empty passes must remain inspectable: %v", err)
			}
			if len(pass.Files())+len(pass.StyleFiles())+len(pass.ScriptFiles()) != 0 {
				t.Fatal("fixture unexpectedly contains analysable files")
			}
			report, err := contracts.Run(pass, contracts.RunOptions{Analyzers: []string{"rendering"}})
			if err == nil || !strings.Contains(err.Error(), "no analysable source files") {
				t.Fatalf("Run = %v, %v; want an incomplete-discovery error", report, err)
			}
		})
	}
}

func TestRunReportsUnreadableSources(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires Unix permissions and a non-root user")
	}
	for _, readable := range []bool{true, false} {
		t.Run(map[bool]string{true: "readable-sibling", false: "all-unreadable"}[readable], func(t *testing.T) {
			root := t.TempDir()
			files := map[string]string{"unreadable.go": "package app\n"}
			if readable {
				files["readable.go"] = "package app\n"
			}
			seedFixture(t, root, files)
			path := filepath.Join(root, "unreadable.go")
			t.Cleanup(func() {
				if err := os.Chmod(path, 0o600); err != nil {
					t.Error(err)
				}
			})
			if err := os.Chmod(path, 0); err != nil {
				t.Fatal(err)
			}
			pass, err := contracts.NewPass(root, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(pass.Unparsed()) != 1 {
				t.Errorf("Unparsed = %v, want unreadable.go", pass.Unparsed())
			}
			report, err := contracts.Run(pass, contracts.RunOptions{Analyzers: []string{"rendering"}})
			if !readable {
				if err == nil || !strings.Contains(err.Error(), "no analysable source files") {
					t.Fatalf("Run = %v, %v; want an incomplete-discovery error", report, err)
				}
				return
			}
			if report != nil || err == nil || !strings.Contains(err.Error(), "unreadable.go") {
				t.Fatalf("Run = %v, %v; want an explicit source-discovery failure", report, err)
			}
		})
	}
}
