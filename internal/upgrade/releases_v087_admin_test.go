package upgrade_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
	"github.com/DonaldMurillo/gofastr/internal/upgrade/scantest"
)

// adminKit is the v0.86 battery/admin surface the two admin notes match.
var adminKit = map[string]string{"battery/admin/admin.go": `package admin

type Config struct {
	PathPrefix      string
	Theme           any
	FontFaceCSS     string
	EntityListLimit int
	Secret          string
	AllEntities     bool
}

type Battery struct{}

func New(cfg Config) *Battery   { return &Battery{} }
func (b *Battery) RegisterRoutes(any) {}
`}

func adminNote(t *testing.T, change string) *upgrade.Note {
	t.Helper()
	reg, err := upgrade.Load()
	if err != nil {
		t.Fatalf("upgrade.Load: %v", err)
	}
	for _, rel := range reg.Releases {
		if rel.Version != "v0.87.0" {
			continue
		}
		for i := range rel.Notes {
			if strings.HasPrefix(rel.Notes[i].Change, change) {
				return rel.Notes[i]
			}
		}
	}
	t.Fatalf("no v0.87.0 note starting %q", change)
	return nil
}

func TestV087AdminFieldsNoteHitsRemovedFields(t *testing.T) {
	n := adminNote(t, "battery/admin's Config.Theme")
	app := scantest.App(t, map[string]string{"main.go": `package main

import "github.com/DonaldMurillo/gofastr/battery/admin"

func main() {
	_ = admin.New(admin.Config{PathPrefix: "/admin", Theme: nil, EntityListLimit: 50})
}
`}, scantest.Options{Kit: adminKit})
	res := scantest.Run(t, app, []*upgrade.Note{n}, upgrade.MarkerSinks{})
	if got := scantest.Hits(res, n); len(got) != 2 {
		t.Fatalf("hits = %v, want Theme and EntityListLimit", got)
	}
	quiet := scantest.App(t, map[string]string{"main.go": `package main

import "github.com/DonaldMurillo/gofastr/battery/admin"

func main() { _ = admin.New(admin.Config{PathPrefix: "/admin", AllEntities: true}) }
`}, scantest.Options{Kit: adminKit})
	if got := scantest.Hits(scantest.Run(t, quiet, []*upgrade.Note{n}, upgrade.MarkerSinks{}), n); len(got) != 0 {
		t.Fatalf("fires with no removed field: %v", got)
	}
}

func TestV087AdminRoutesNoteHitsOldPaths(t *testing.T) {
	n := adminNote(t, "battery/admin draws entity screens")
	if !n.Review {
		t.Fatal("the routes note must be review-tier: Config.UI is a decision, not an edit")
	}
	app := scantest.App(t, map[string]string{"main.go": `package main

import "github.com/DonaldMurillo/gofastr/battery/admin"

func main() { _, _ = admin.New(admin.Config{AllEntities: true}), "/admin/e/customers" }
`}, scantest.Options{Kit: adminKit})
	got := scantest.Hits(scantest.Run(t, app, []*upgrade.Note{n}, upgrade.MarkerSinks{}), n)
	if len(got) != 2 {
		t.Fatalf("hits = %v, want the admin.New call and the /admin/e/ path", got)
	}
}
