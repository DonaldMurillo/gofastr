package migrate

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestRoutinesAtFSRoot(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "totals.sql"), []byte("SELECT 1"), 0600); err != nil {
		t.Fatal(err)
	}
	for name, fsys := range map[string]fs.FS{
		"map":  fstest.MapFS{"totals.sql": {Data: []byte("SELECT 1")}},
		"disk": os.DirFS(dir),
	} {
		t.Run(name, func(t *testing.T) {
			routines, err := RoutinesFS(fsys, ".")
			if err != nil {
				t.Fatal(err)
			}
			if len(routines) != 1 || routines[0].Name != "totals" || routines[0].Up != "SELECT 1" {
				t.Fatalf("unexpected routines: %v", routines)
			}
		})
	}
}
