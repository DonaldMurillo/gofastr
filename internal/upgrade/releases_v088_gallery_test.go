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

// v088Note is the v0.88.0 note whose change starts with prefix, read
// through the shipped YAML.
func v088Note(t *testing.T, prefix string) *upgrade.Note {
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
			if strings.HasPrefix(rel.Notes[i].Change, prefix) {
				return rel.Notes[i]
			}
		}
	}
	t.Fatalf("no v0.88.0 note starting %q", prefix)
	return nil
}

// v088Hits runs one note over a one-file app built on kit.
func v088Hits(t *testing.T, n *upgrade.Note, kit map[string]string, mainGo string) []string {
	t.Helper()
	app := scantest.App(t, map[string]string{"main.go": mainGo}, scantest.Options{Kit: kit})
	return scantest.Hits(scantest.Run(t, app, []*upgrade.Note{n}, upgrade.MarkerSinks{}), n)
}

// Both removed functions are found where an app calls them, and an app
// that renders the catalog without them is quiet.
func TestV088GalleryCSSNoteHitsRemovedFuncs(t *testing.T) {
	n := v088Note(t, "gallery.ContributeCSS and gallery.BaseCSS are removed")
	if got := v088Hits(t, n, galleryKit, `package main

import "github.com/DonaldMurillo/gofastr/framework/gallery"

func main() {
	gallery.ContributeCSS(nil)
	_ = gallery.BaseCSS(gallery.Theme{}) + "x"
}
`); len(got) != 2 {
		t.Fatalf("hits = %v, want the ContributeCSS and BaseCSS calls", got)
	}
	if got := v088Hits(t, n, galleryKit, `package main

import "github.com/DonaldMurillo/gofastr/framework/gallery"

func main() { var _ gallery.Theme }
`); len(got) != 0 {
		t.Fatalf("fires with no removed call: %v", got)
	}
}
