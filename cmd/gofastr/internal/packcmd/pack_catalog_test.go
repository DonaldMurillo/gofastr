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
