package static

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// The prune manifest lists the export's pages. It is the builder's own
// state, so it lives beside OutDir: a static host serves every file in
// the export tree, and the list has no business being one of them.
func TestManifestStaysOutOfExportTree(t *testing.T) {
	a := coreapp.NewApp("t")
	a.Register("/", &homeScreen{}, nil)
	parent := t.TempDir()
	out := filepath.Join(parent, "site")
	b := &Builder{Host: uihost.New(a), OutDir: out}
	if _, err := b.Build(context.Background()); err != nil {
		t.Fatalf("Build: %v", err)
	}
	err := filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.Contains(d.Name(), "gofastr-pages") {
			t.Errorf("export tree holds the prune manifest %s", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(parent, ".site.gofastr-pages"))
	if err != nil {
		t.Fatalf("no manifest beside OutDir: %v", err)
	}
	if !strings.Contains(string(got), "index.html") {
		t.Errorf("manifest = %q, want it to list index.html", got)
	}
}

// An OutDir with no parent has nowhere outside it to keep the list, so
// the build refuses rather than write it into the export.
func TestManifestRefusesRootOutDir(t *testing.T) {
	root := filepath.VolumeName(os.TempDir()) + string(filepath.Separator)
	if _, _, err := (&Builder{OutDir: root}).pageManifest(); err == nil {
		t.Fatalf("pageManifest(%q) found a place for the manifest; want an error", root)
	}
	dir, name, err := (&Builder{OutDir: filepath.Join(t.TempDir(), "site")}).pageManifest()
	if err != nil || name != ".site.gofastr-pages" || filepath.Base(filepath.Join(dir, "site")) != "site" {
		t.Fatalf("pageManifest = %q, %q, %v; want the parent dir and .site.gofastr-pages", dir, name, err)
	}
}
