package packcmd

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/framework"
)

func fileContent(files []appgen.GeneratedFile, name string) string {
	for _, f := range files {
		if f.Name == name {
			return f.Content
		}
	}
	return ""
}

func sortedFileNames(files []appgen.GeneratedFile) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, f.Name)
	}
	sort.Strings(out)
	return out
}

// TestPackRoundTripPerScreenFiles packs a freshly generated per-screen app
// with authored screens in non-lexical (file-name) order and recovers the
// exact authored set in authored order.
func TestPackRoundTripPerScreenFiles(t *testing.T) {
	crud := true
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{Name: "T", Module: "example.com/rt", DBDriver: "sqlite"},
		Entities: []framework.EntityDeclaration{
			{Scope: &framework.ScopeDeclaration{}, Pagination: &framework.PaginationDeclaration{}, Name: "zebra", Exposure: &framework.ExposureDeclaration{CRUD: &crud}, Fields: []framework.FieldDeclaration{{Name: "name", Type: "string"}}},
		},
		Screens: []appspec.AppSpecScreen{
			{Name: "zebra", Route: "/zebra", Title: "Zebras", Body: []appspec.AppSpecBlock{{Kind: "entity_list", Entity: "zebra", Create: true}}},
			{Name: "alpha", Route: "/alpha", Title: "Alpha"},
			{Name: "zebra_detail", Route: "/zebra/{id}", Title: "Zebra", Body: []appspec.AppSpecBlock{{Kind: "entity_detail", Entity: "zebra"}}},
		},
	}
	files, err := appgen.RenderAppSpecFiles(bp)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, f := range files {
		full := filepath.Join(dir, f.Name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(f.Content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := packReadScreens(dir)
	if err != nil {
		t.Fatalf("packReadScreens: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("packed %d screens, want 3 (synthesized new/edit dropped): %+v", len(got), got)
	}
	want := []string{"zebra", "alpha", "zebra_detail"}
	gotNames := make([]string, len(got))
	for i, s := range got {
		gotNames[i] = s.Name
	}
	if !reflect.DeepEqual(want, gotNames) {
		t.Fatalf("screen order not recovered: want %v, got %v", want, gotNames)
	}
}

// TestPackLegacyAggregatedScreens verifies the pack legacy fallback still
// reads a pre-per-screen project: all screen structs in one screens.go, the
// site.Register calls in app.go's RegisterGenerated, no screen_*.go files.
func TestPackLegacyAggregatedScreens(t *testing.T) {
	dir := t.TempDir()
	const legacyScreens = `package main

import (
	"context"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type HomeScreen struct{}

func (s *HomeScreen) ScreenTitle(ctx context.Context) string        { return "Home" }
func (s *HomeScreen) ScreenType() app.ScreenType { return app.ScreenPage }
func (s *HomeScreen) Render() render.HTML {
	return render.Tag("div", nil, nil)
}
`
	const legacyApp = `package main

import "database/sql"

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework"
)

func RegisterGenerated(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.Register("/", &HomeScreen{}, nil)
}
`
	if err := os.WriteFile(filepath.Join(dir, "screens.go"), []byte(legacyScreens), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.go"), []byte(legacyApp), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := packReadScreens(dir)
	if err != nil {
		t.Fatalf("packReadScreens: %v", err)
	}
	if len(got) != 1 || got[0].Name != "home" || got[0].Route != "/" {
		t.Fatalf("legacy fallback did not recover the home screen: %+v", got)
	}
}
