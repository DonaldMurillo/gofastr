package desktopcmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFailedTypesRemovesStaging(t *testing.T) {
	if os.Getenv("GOFASTR_TEST_FAILED_TYPES") == "1" {
		runDesktopTypes([]string{"--out", os.Getenv("GOFASTR_TEST_TYPES_OUT")})
		return
	}
	for _, test := range []struct{ name, source, message string }{
		{"build", "package main\nfunc main( broken", "Build failed"},
		{"run", "package main\nimport \"os\"\nfunc main(){os.Exit(1)}", "App run failed"},
		{"decode", "package main\nimport \"fmt\"\nfunc main(){fmt.Print(\"not JSON\")}", "Manifest output is not JSON"},
		{"write", "package main\nimport \"fmt\"\nfunc main(){fmt.Print(\"{}\")}", "Write "},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			project := filepath.Join(dir, "project")
			if err := os.Mkdir(project, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte("module test.invalid/types\n\ngo 1.26\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(project, "main.go"), []byte(test.source), 0o600); err != nil {
				t.Fatal(err)
			}
			child := exec.Command(os.Args[0], "-test.run=^TestFailedTypesRemovesStaging$")
			child.Dir = project
			child.Env = append(os.Environ(), "GOFASTR_TEST_FAILED_TYPES=1", "GOFASTR_TEST_TYPES_OUT="+filepath.Join(dir, "missing", "desktop.d.ts"), "TMPDIR="+dir, "TMP="+dir, "TEMP="+dir)
			output, err := child.CombinedOutput()
			if err == nil || !strings.Contains(string(output), test.message) {
				t.Fatalf("expected %s failure: %v\n%s", test.name, err, output)
			}
			leftovers, err := filepath.Glob(filepath.Join(dir, "gofastr-desktop-types-*"))
			if err != nil {
				t.Fatal(err)
			}
			if len(leftovers) != 0 {
				t.Fatalf("failed types left staging directories: %v", leftovers)
			}
		})
	}
}
