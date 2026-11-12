package packcmd

import (
	"maps"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
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
	// The local dev origin written out is a base_url too; only an
	// unset one packs back unset.
	for _, baseURL := range []string{"https://slash.example/", "https://slash.example/prefix/", "http://localhost:8080", ""} {
		t.Run(baseURL, func(t *testing.T) {
			line := ""
			if baseURL != "" {
				line = "  base_url: " + baseURL + "\n"
			}
			spec, err := decodeAppSpecString("app:\n  name: Slash\n  module: example.com/slash\n" + line)
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

// The union spec's invoices seed is a recipe (count 6, status weights)
// over two authored rows. Generated and packed through the real
// generator, the recipe and the authored rows come back as written; the
// fixture-only round trip in pack_seed_recipe_test.go never saw the
// union's refs, states and other seeded entities beside it.
func TestPackUnionKeepsSeedRecipe(t *testing.T) {
	spec := reviewUnionSpec(t)
	packed, err := packAppSpec(materializeAppSpec(t, spec))
	if err != nil {
		t.Fatal(err)
	}
	find := func(seeds []appspec.AppSpecSeedEntity) *appspec.AppSpecSeedEntity {
		for i := range seeds {
			if seeds[i].Entity == "invoices" {
				return &seeds[i]
			}
		}
		return nil
	}
	want, got := find(appgen.ExpandAppSpecSeed(spec).Seed), find(packed.Seed)
	if want == nil || want.Count == 0 {
		t.Fatal("the union spec's invoices seed no longer carries a recipe; the test reads nothing")
	}
	if got == nil {
		t.Fatal("packed spec has no invoices seed")
	}
	if got.Count != want.Count || !reflect.DeepEqual(got.Weights, want.Weights) {
		t.Errorf("recipe = count %d weights %v, want count %d weights %v", got.Count, got.Weights, want.Count, want.Weights)
	}
	// Rendering expands the recipe into Rows (on its own copy, never the
	// caller's spec); Authored counts the leading rows the author wrote,
	// which is all pack keeps beside the recipe.
	authored := want.Rows[:want.Authored]
	if want.Authored == 0 || len(got.Rows) != len(authored) {
		t.Fatalf("packed %d rows, want the %d authored ones", len(got.Rows), want.Authored)
	}
	for i := range authored {
		if got.Rows[i]["number"] != authored[i]["number"] || got.Rows[i]["status"] != authored[i]["status"] {
			t.Errorf("row %d = %v, want %v", i, got.Rows[i], authored[i])
		}
	}
}

// The union spec exercises every generator feature, so generate, pack
// and generate again must write the same files byte for byte. Each loss
// fixed one at a time (the app home, multi-word form fields, the seed
// recipe) showed up here first as one differing file; this keeps the
// next one from landing unseen.
func TestPackUnionRegeneratesSameFiles(t *testing.T) {
	spec := reviewUnionSpec(t)
	packed, err := packAppSpec(materializeAppSpec(t, spec))
	if err != nil {
		t.Fatal(err)
	}
	yml, err := encodeAppSpecYAML(packed)
	if err != nil {
		t.Fatal(err)
	}
	back, err := decodeAppSpecString(yml)
	if err != nil {
		t.Fatal(err)
	}
	before, err := appgen.RenderAppSpecCode(spec)
	if err != nil {
		t.Fatal(err)
	}
	after, err := appgen.RenderAppSpecCode(back)
	if err != nil {
		t.Fatal(err)
	}
	rendered := make(map[string]string, len(after))
	for _, f := range after {
		rendered[f.Name] = f.Content
	}
	for _, f := range before {
		got, ok := rendered[f.Name]
		switch {
		case !ok:
			t.Errorf("%s is missing after generate, pack, generate", f.Name)
		case got != f.Content:
			t.Errorf("%s differs after generate, pack, generate", f.Name)
		}
		delete(rendered, f.Name)
	}
	for _, name := range slices.Sorted(maps.Keys(rendered)) {
		t.Errorf("%s appears only after generate, pack, generate", name)
	}
}
