package static

import (
	"context"
	"errors"
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

// The refusal comes before the build writes anything: a filesystem-root
// OutDir must not get pages and assets copied into it first. The
// contributor stops any build that gets past setup, so even a build
// missing the refusal writes no page into the root.
func TestRootOutDirFailsBeforeWriting(t *testing.T) {
	a := coreapp.NewApp("t")
	a.Register("/", &homeScreen{}, nil)
	root := filepath.VolumeName(os.TempDir()) + string(filepath.Separator)
	stop := &stopContributor{}
	b := &Builder{Host: uihost.New(a), OutDir: root, Contributors: []Contributor{stop}}
	_, err := b.Build(context.Background())
	if err == nil || !strings.Contains(err.Error(), "filesystem root") {
		t.Fatalf("Build(OutDir %q) = %v; want the filesystem-root refusal", root, err)
	}
	if stop.started {
		t.Error("Build reached its contributors before refusing the filesystem-root OutDir")
	}
}

type stopContributor struct{ started bool }

func (c *stopContributor) ExportStart(context.Context) error {
	c.started = true
	return errors.New("stop")
}
func (c *stopContributor) ExportPage(string, string) error { return nil }
func (c *stopContributor) ExportFiles(context.Context) (map[string][]byte, error) {
	return nil, nil
}
