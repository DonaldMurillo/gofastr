package packcmd

import (
	"maps"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

// packRoundTrip generates the app spec, packs the tree, and returns the
// packed screen body of the one screen in the spec. It fails the test when
// regenerating from the packed YAML writes different files: the pack
// round trip must hand back every catalog block it was given.
func packRoundTrip(t *testing.T, body string) []appspec.AppSpecBlock {
	t.Helper()
	spec, err := decodeAppSpecString(`app:
  name: Probe
  module: example.com/probe
screens:
  - name: home
    route: /
    body:
      - kind: page_header
        props:
          title: Home
` + body)
	if err != nil {
		t.Fatal(err)
	}
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
		if got := rendered[f.Name]; got != f.Content {
			t.Errorf("%s differs after generate, pack, generate", f.Name)
		}
		delete(rendered, f.Name)
	}
	for _, name := range slices.Sorted(maps.Keys(rendered)) {
		t.Errorf("%s appears only after generate, pack, generate", name)
	}
	for _, s := range packed.Screens {
		if s.Name == "home" {
			return s.Body
		}
	}
	t.Fatal("packed spec has no home screen")
	return nil
}

// The generator draws a callout as ui.Callout and a divider as
// ui.Divider; pack had no reverse for either and dropped both, so the
// packed spec regenerated the screen without them.
func TestPackKeepsCalloutAndDivider(t *testing.T) {
	body := packRoundTrip(t, `      - kind: callout
        props:
          title: Heads up
        text: Backups run nightly.
      - kind: callout
        text: Untitled note.
      - kind: divider
`)
	if len(body) != 4 {
		t.Fatalf("packed %d blocks, want 4: %#v", len(body), body)
	}
	if b := body[1]; b.Kind != "callout" || b.Props["title"] != "Heads up" || b.Text != "Backups run nightly." {
		t.Fatalf("titled callout packed as %#v", b)
	}
	if b := body[2]; b.Kind != "callout" || b.Props["title"] != nil || b.Text != "Untitled note." {
		t.Fatalf("untitled callout packed as %#v", b)
	}
	if b := body[3]; b.Kind != "divider" {
		t.Fatalf("divider packed as %#v", b)
	}
}

// The generator draws stat_grid as ui.Grid(GridConfig{Min, Gap}) and
// stat_row as ui.Grid(GridConfig{Min: "12rem"}) with no Gap. pack read
// every grid of stat cards as a stat_row and dropped its props, so a
// stat_grid came back as a stat_row with the default minimum. A static
// stat_card's value was dropped too, so the packed card failed validate.
func TestPackKeepsStatGridLayout(t *testing.T) {
	body := packRoundTrip(t, `      - kind: stat_grid
        props:
          min: 20rem
          gap: lg
        children:
          - kind: stat_card
            props:
              label: A
              value: "1"
      - kind: stat_grid
        children:
          - kind: stat_card
            props:
              label: B
              value: "2"
      - kind: stat_row
        children:
          - kind: stat_card
            props:
              label: C
              value: "3"
      - kind: grid
        props:
          min: 24rem
          gap: sm
        children:
          - kind: stat_card
            props:
              label: D
              value: "4"
      - kind: stat_row
        children:
          - kind: card
            props:
              heading: E
`)
	if len(body) != 6 {
		t.Fatalf("packed %d blocks, want 6: %#v", len(body), body)
	}
	if b := body[1]; b.Kind != "stat_grid" || b.Props["min"] != "20rem" || b.Props["gap"] != "lg" {
		t.Fatalf("stat_grid packed as kind=%q props=%v", b.Kind, b.Props)
	}
	if b := body[2]; b.Kind != "stat_grid" || len(b.Props) != 0 {
		t.Fatalf("default stat_grid packed as kind=%q props=%v", b.Kind, b.Props)
	}
	if b := body[3]; b.Kind != "stat_row" || len(b.Props) != 0 {
		t.Fatalf("stat_row packed as kind=%q props=%v", b.Kind, b.Props)
	}
	if v := body[1].Children[0].Props["value"]; v != "1" {
		t.Fatalf("static stat_card value packed as %v", v)
	}
	if b := body[5]; b.Kind != "stat_row" || len(b.Children) != 1 {
		t.Fatalf("stat_row of a card packed as kind=%q children=%d", b.Kind, len(b.Children))
	}
}
