package packcmd

import (
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

//gofastr:test-reads /cmd/gofastr/internal/appgen/testdata/union/gofastr.yml

func TestPackKeepsControlScreen(t *testing.T) {
	spec, err := appspec.LoadAppSpec(filepath.Join("..", "appgen", "testdata", "union", "gofastr.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, screen := range spec.Screens {
		if screen.Name == "contact" {
			spec.Screens = []appspec.AppSpecScreen{screen}
			break
		}
	}
	if len(spec.Screens) != 1 || spec.Screens[0].Name != "contact" {
		t.Fatal("union contact screen missing")
	}
	spec = appspec.AppSpec{App: appspec.AppSpecApp{Name: "Controls", Module: "example.com/controls", DBDriver: "sqlite", DBURL: "controls.db"}, Screens: spec.Screens}
	form := &spec.Screens[0].Body[0]
	form.Children = append(form.Children,
		appspec.AppSpecBlock{Kind: "number_field", Props: map[string]any{"name": "count", "label": "Count", "placeholder": "Number", "help": "How many", "required": true, "disabled": true}},
		appspec.AppSpecBlock{Kind: "checkbox", Props: map[string]any{"name": "accept", "label": "Accept", "checked": true, "disabled": true}},
		appspec.AppSpecBlock{Kind: "switch", Props: map[string]any{"name": "alerts", "label": "Alerts", "checked": true}},
	)
	original, err := appgen.RenderAppSpecFiles(spec)
	if err != nil {
		t.Fatal(err)
	}
	packed, err := packReadScreens(materializeAppSpec(t, spec))
	if err != nil {
		t.Fatal(err)
	}
	if len(packed) != 1 || len(packed[0].Body) != len(spec.Screens[0].Body) {
		t.Fatalf("packed control blocks=%#v; want %d blocks", packed, len(spec.Screens[0].Body))
	}
	if len(packed[0].Body[0].Children) != len(form.Children) {
		t.Fatalf("packed form controls=%#v", packed[0].Body[0].Children)
	}
	spec.Screens = packed
	regenerated, err := appgen.RenderAppSpecFiles(spec)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := allScreenContent(regenerated), allScreenContent(original); got != want {
		t.Fatalf("packed controls regenerate differently:\nGOT:\n%s\nWANT:\n%s", got, want)
	}
}
