package packcmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

// writeBP writes an app spec and loads it, returning the error for inspection.
func loadBPWithEntity(t *testing.T, entityYAML string) (appspec.AppSpec, error) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	bp := "app:\n  name: testapp\nentities:\n" + entityYAML
	if err := os.WriteFile(path, []byte(bp), 0o644); err != nil {
		t.Fatal(err)
	}
	return appspec.LoadAppSpec(path)
}
