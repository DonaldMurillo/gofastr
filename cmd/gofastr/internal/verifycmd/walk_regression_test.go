package verifycmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/codegen"
)

func TestProvenanceSkipsBuildOutput(t *testing.T) {
	root := t.TempDir()
	stamped, err := codegen.Stamp("app.go", "package app\n", codegen.Marker{Generator: "app", Kind: codegen.KindScaffold})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"dist", "bin", "build", "tmp"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name, "app.go"), []byte(stamped), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := ScanProvenance(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("generated outputs reported: entries=%v err=%v", entries, err)
	}
}
