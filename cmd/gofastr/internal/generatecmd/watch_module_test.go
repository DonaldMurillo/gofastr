package generatecmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
)

func TestWatchTracksEnclosingModule(t *testing.T) {
	for _, nested := range []bool{false, true} {
		t.Run(map[bool]string{false: "root", true: "nested"}[nested], func(t *testing.T) {
			root := t.TempDir()
			directory := root
			if nested {
				directory = filepath.Join(root, "tools", "project")
				if err := os.MkdirAll(directory, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			config := filepath.Join(directory, "gofastr.settings.yml")
			if err := os.WriteFile(config, []byte("version: 1\ncodegen:\n  generators:\n    - name: cli\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			options := appgen.GenerateOptions{ConfigPath: config}
			previous := hashGenerateInputs(options)
			for _, module := range []string{"example.com/first", "example.org/renamed"} {
				if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module "+module+"\n\ngo 1.27\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				current := hashGenerateInputs(options)
				if current == previous {
					t.Fatalf("module creation/rename to %s did not change the hash", module)
				}
				previous = current
			}
			if err := os.Remove(filepath.Join(root, "go.mod")); err != nil {
				t.Fatal(err)
			}
			if hashGenerateInputs(options) == previous {
				t.Fatal("module removal did not change the hash")
			}
		})
	}
}
