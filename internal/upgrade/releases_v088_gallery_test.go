package upgrade_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
	"github.com/DonaldMurillo/gofastr/internal/upgrade/scantest"
)

// galleryKit is the v0.87 framework/gallery surface the CSS note matches.
var galleryKit = map[string]string{"framework/gallery/css.go": `package gallery

type StyleSheet struct{}
type Theme struct{}

func ContributeCSS(ss *StyleSheet) {}
func BaseCSS(t Theme) string     { return "" }
`}

func galleryCSSNote(t *testing.T) *upgrade.Note {
	t.Helper()
	reg, err := upgrade.Load()
	if err != nil {
		t.Fatalf("upgrade.Load: %v", err)
	}
	for _, rel := range reg.Releases {
		if rel.Version != "v0.88.0" {
			continue
		}
		for i := range rel.Notes {
			if strings.HasPrefix(rel.Notes[i].Change, "gallery.ContributeCSS and gallery.BaseCSS are removed") {
				return rel.Notes[i]
			}
		}
	}
	t.Fatal("no v0.88.0 note for the removed gallery CSS")
	return nil
}

// Both removed functions are found where an app calls them, and an app
// that renders the catalog without them is quiet.
func TestV088GalleryCSSNoteHitsRemovedFuncs(t *testing.T) {
	n := galleryCSSNote(t)
	app := scantest.App(t, map[string]string{"main.go": `package main

import "github.com/DonaldMurillo/gofastr/framework/gallery"

func main() {
	gallery.ContributeCSS(nil)
	_ = gallery.BaseCSS(gallery.Theme{}) + "x"
}
`}, scantest.Options{Kit: galleryKit})
	if got := scantest.Hits(scantest.Run(t, app, []*upgrade.Note{n}, upgrade.MarkerSinks{}), n); len(got) != 2 {
		t.Fatalf("hits = %v, want the ContributeCSS and BaseCSS calls", got)
	}
	quiet := scantest.App(t, map[string]string{"main.go": `package main

import "github.com/DonaldMurillo/gofastr/framework/gallery"

func main() { var _ gallery.Theme }
`}, scantest.Options{Kit: galleryKit})
	if got := scantest.Hits(scantest.Run(t, quiet, []*upgrade.Note{n}, upgrade.MarkerSinks{}), n); len(got) != 0 {
		t.Fatalf("fires with no removed call: %v", got)
	}
}
