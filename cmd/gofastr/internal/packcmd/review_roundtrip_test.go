package packcmd

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

//gofastr:test-reads /cmd/gofastr/internal/appgen/testdata/union/gofastr.yml

func reviewUnionSpec(t *testing.T) appspec.AppSpec {
	t.Helper()
	spec, err := appspec.LoadAppSpec(filepath.Join("..", "appgen", "testdata", "union", "gofastr.yml"))
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func TestPackUnionOmitsDerivedDescription(t *testing.T) {
	spec := reviewUnionSpec(t)
	for _, withEntities := range []bool{false, true} {
		t.Run(map[bool]string{false: "without-entities", true: "with-entities"}[withEntities], func(t *testing.T) {
			original := spec
			if !withEntities {
				original = appspec.AppSpec{App: spec.App}
			}
			packed, err := packAppSpec(materializeAppSpec(t, original))
			if err != nil {
				t.Fatal(err)
			}
			if packed.App.Description != original.App.Description {
				t.Fatalf("description=%q, want %q", packed.App.Description, original.App.Description)
			}
		})
	}
}

func TestPackBaseURLCanonicalRoundTrip(t *testing.T) {
	for _, baseURL := range []string{"https://slash.example/", "https://slash.example/prefix/"} {
		t.Run(baseURL, func(t *testing.T) {
			spec, err := decodeAppSpecString("app:\n  name: Slash\n  module: example.com/slash\n  base_url: " + baseURL + "\n")
			if err != nil {
				t.Fatal(err)
			}
			packed, err := packAppSpec(materializeAppSpec(t, spec))
			if err != nil {
				t.Fatal(err)
			}
			if packed.App.BaseURL != spec.App.BaseURL {
				t.Fatalf("base_url=%q, parsed=%q", packed.App.BaseURL, spec.App.BaseURL)
			}
		})
	}
}

func TestPackUnionControlCanonicalRoundTrip(t *testing.T) {
	spec := reviewUnionSpec(t)
	for _, screen := range spec.Screens {
		if screen.Name == "contact" {
			spec.Screens = []appspec.AppSpecScreen{screen}
			break
		}
	}
	if len(spec.Screens) != 1 || spec.Screens[0].Name != "contact" {
		t.Fatal("union contact screen missing")
	}
	spec = appspec.AppSpec{App: appspec.AppSpecApp{Name: "Controls", Module: "example.com/controls"}, Screens: spec.Screens}
	spec.Screens[0].Body = append(spec.Screens[0].Body,
		appspec.AppSpecBlock{Kind: "action_button", Props: map[string]any{"label": "Default"}},
		appspec.AppSpecBlock{Kind: "action_button", Props: map[string]any{"label": "Explicit defaults", "variant": "Primary", "type": "Button"}},
		appspec.AppSpecBlock{Kind: "action_button", Props: map[string]any{"label": "Submit", "variant": "Secondary", "type": "Submit"}},
		appspec.AppSpecBlock{Kind: "nav_links", Props: map[string]any{"label": "Default gap", "gap": "md"}},
		appspec.AppSpecBlock{Kind: "nav_links", Props: map[string]any{"label": "Small gap", "gap": "sm"}},
	)
	yaml, err := encodeAppSpecYAML(spec)
	if err != nil {
		t.Fatal(err)
	}
	spec, err = decodeAppSpecString(yaml)
	if err != nil {
		t.Fatal(err)
	}
	packed, err := packAppSpec(materializeAppSpec(t, spec))
	if err != nil {
		t.Fatal(err)
	}
	if len(packed.Screens) != 1 || len(packed.Screens[0].Body) != len(spec.Screens[0].Body) {
		t.Fatalf("packed contact screen=%+v", packed.Screens)
	}
	for index, original := range spec.Screens[0].Body {
		if original.Kind != "action_button" && original.Kind != "nav_links" {
			continue
		}
		if actual := packed.Screens[0].Body[index]; !reflect.DeepEqual(original, actual) {
			t.Errorf("%s %q: packed props=%#v; parsed props=%#v", original.Kind, original.Props["label"], actual.Props, original.Props)
		}
	}
}
