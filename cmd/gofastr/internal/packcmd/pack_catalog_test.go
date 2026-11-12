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
	return packRoundTripApp(t, `app:
  name: Probe
  module: example.com/probe
screens:
  - name: home
    route: /
    body:
      - kind: page_header
        props:
          title: Home
`+body)
}

// packRoundTripApp is packRoundTrip over a whole app spec, for blocks
// that need entities; it returns the body of the spec's home screen.
func packRoundTripApp(t *testing.T, yml string) []appspec.AppSpecBlock {
	t.Helper()
	spec, err := decodeAppSpecString(yml)
	if err != nil {
		t.Fatal(err)
	}
	packed, err := packAppSpec(materializeAppSpec(t, spec))
	if err != nil {
		t.Fatal(err)
	}
	out, err := encodeAppSpecYAML(packed)
	if err != nil {
		t.Fatal(err)
	}
	back, err := decodeAppSpecString(out)
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

// The generator gives a hero with an image (or its media alias) a
// Media: html.Image(ImageConfig{Src}), and draws a lone secondary CTA as
// the only, secondary-variant action. pack read neither: the image was
// dropped and a lone secondary CTA came back as the primary one.
func TestPackKeepsHeroImageAndCTAs(t *testing.T) {
	body := packRoundTrip(t, `      - kind: hero
        props:
          title: Ship it
          image: /static/hero.jpg
          cta_text: Start
          cta_href: /start
      - kind: hero
        props:
          title: Media alias
          media: /static/alias.png
      - kind: hero
        props:
          title: Only secondary
          secondary_text: Learn
          secondary_href: /learn
`)
	if len(body) != 4 {
		t.Fatalf("packed %d blocks, want 4: %#v", len(body), body)
	}
	if p := body[1].Props; p["image"] != "/static/hero.jpg" || p["cta_text"] != "Start" {
		t.Fatalf("hero packed props=%v", p)
	}
	if p := body[2].Props; p["image"] != "/static/alias.png" {
		t.Fatalf("media hero packed props=%v", p)
	}
	if p := body[3].Props; p["secondary_text"] != "Learn" || p["secondary_href"] != "/learn" || p["cta_text"] != nil {
		t.Fatalf("secondary-only hero packed props=%v", p)
	}
}

// A titled chart is drawn as ui.Card(CardConfig{Heading}, chart); a card
// whose first child is a chart is ui.Card(CardConfig{Heading,
// Description}, chart, siblings…). Pack read both as the titled chart and
// dropped the card's text and every other child.
func TestPackKeepsCardHoldingChart(t *testing.T) {
	body := packRoundTripApp(t, `app:
  name: Probe
  module: example.com/probe
entities:
  - name: posts
    fields:
      - name: title
        type: string
screens:
  - name: home
    route: /
    body:
      - kind: card
        props:
          heading: Sales
          text: How we did
        children:
          - kind: bar_chart
            props:
              source:
                entity: posts
                group_by: title
          - kind: card
            props:
              heading: Note
              text: Charts update nightly
      - kind: card
        props:
          heading: Lone
        children:
          - kind: pie_chart
            props:
              source:
                entity: posts
                group_by: title
      - kind: bar_chart
        props:
          title: Titled
          source:
            entity: posts
            group_by: title
`)
	if len(body) != 3 {
		t.Fatalf("packed body has %d blocks, want 3: %+v", len(body), body)
	}
	outer := body[0]
	if outer.Kind != "card" || outer.Props["heading"] != "Sales" || outer.Props["text"] != "How we did" {
		t.Fatalf("card with a chart child packed as %+v", outer)
	}
	if len(outer.Children) != 2 || outer.Children[0].Kind != "bar_chart" || outer.Children[1].Kind != "card" {
		t.Fatalf("card children packed as %+v, want bar_chart then card", outer.Children)
	}
	if _, titled := outer.Children[0].Props["title"]; titled {
		t.Errorf("the card's heading leaked into its chart's title: %+v", outer.Children[0].Props)
	}
	lone := body[1]
	if lone.Kind != "card" || lone.Props["heading"] != "Lone" || len(lone.Children) != 1 || lone.Children[0].Kind != "pie_chart" {
		t.Errorf("card holding one chart packed as %+v", lone)
	}
	if titled := body[2]; titled.Kind != "bar_chart" || titled.Props["title"] != "Titled" {
		t.Errorf("titled chart packed as %+v", titled)
	}
}

// A `type: section` text block is drawn as render.Tag("section", …) and
// every other bare tag (div, header, article, …) as html.Div; pack read
// neither back and dropped the block with its text and class.
func TestPackKeepsSectionAndDivText(t *testing.T) {
	body := packRoundTrip(t, `      - type: section
        class: lead
        text: About this site
      - type: div
        text: A plain note
      - type: header
        class: banner
        text: Top of the page
`)
	want := []appspec.AppSpecBlock{
		{Type: "section", Class: "lead", Text: "About this site"},
		{Type: "div", Text: "A plain note"},
		{Type: "div", Class: "banner", Text: "Top of the page"},
	}
	if len(body) != 1+len(want) {
		t.Fatalf("packed body has %d blocks, want %d: %+v", len(body), 1+len(want), body)
	}
	for i, w := range want {
		got := body[i+1]
		if got.Type != w.Type || got.Class != w.Class || got.Text != w.Text || got.Kind != "" {
			t.Errorf("block %d packed as %+v, want type %q class %q text %q", i+1, got, w.Type, w.Class, w.Text)
		}
	}
}
