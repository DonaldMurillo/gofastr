package packcmd

import (
	"go/parser"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

//gofastr:test-reads /cmd/gofastr/internal/appgen/testdata/union/gofastr.yml

// The generator draws a node block (a plain section, heading, div, and
// any island: or widget: wrapper around one) as a uinode.Node literal.
// pack had no reverse for it and dropped the block, so the recovered
// spec regenerated the screen without it.
func TestPackKeepsNodeBlocks(t *testing.T) {
	union, err := appspec.LoadAppSpec(filepath.Join("..", "appgen", "testdata", "union", "gofastr.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var screens []appspec.AppSpecScreen
	for _, screen := range union.Screens {
		switch screen.Name {
		case "home":
			// The entity list needs the posts entity; the node, island,
			// widget and action blocks around it are what this pins.
			var body []appspec.AppSpecBlock
			for _, b := range screen.Body {
				if !appspec.IsEntityListBlock(b) {
					body = append(body, b)
				}
			}
			screen.Body = body
			screens = append(screens, screen)
		case "type_spelled":
			screens = append(screens, screen)
		}
	}
	if len(screens) != 2 {
		t.Fatalf("union home and type_spelled screens missing: got %d", len(screens))
	}
	// Two actions on one node: the first names its input event in
	// data-action-type, the second rides data-action-blur. A prop an
	// author spelled data-action-* that names no action stays a prop.
	screens = append(screens, appspec.AppSpecScreen{Name: "multi", Route: "/multi", Title: "Multi", Body: []appspec.AppSpecBlock{
		{Kind: "heading", Props: map[string]any{"level": int64(1), "text": "Multi"}},
		{Kind: "input", Props: map[string]any{"name": "q", "data-action-hint": "not an action"}, Actions: []appspec.AppSpecAction{
			{Name: "typed", Event: "input", ClientJS: "G.updateText('#out', 'typed');"},
			{Name: "left", Event: "blur", ClientJS: "G.updateText('#out', 'left');"},
		}},
	}})
	spec := appspec.AppSpec{App: appspec.AppSpecApp{Name: "Nodes", Module: "example.com/nodes", DBDriver: "sqlite", DBURL: "nodes.db"}, Screens: screens}
	original, err := appgen.RenderAppSpecFiles(spec)
	if err != nil {
		t.Fatal(err)
	}
	packed, err := packReadScreens(materializeAppSpec(t, spec))
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range packed {
		if want := len(spec.Screens[i].Body); len(s.Body) != want {
			t.Fatalf("screen %s packed %d blocks, want %d: %#v", s.Name, len(s.Body), want, s.Body)
		}
	}
	input := packed[2].Body[1]
	if len(input.Actions) != 2 || input.Actions[0] != spec.Screens[2].Body[1].Actions[0] || input.Actions[1] != spec.Screens[2].Body[1].Actions[1] {
		t.Fatalf("packed actions = %#v, want typed(input) then left(blur)", input.Actions)
	}
	if _, ok := input.Props["data-action"]; ok || input.Props["data-action-hint"] != "not an action" {
		t.Fatalf("packed input props = %#v: markers kept or author prop lost", input.Props)
	}
	// Through the YAML pack writes and generate reads, as a user runs it.
	spec.Screens = packed
	yaml, err := encodeAppSpecYAML(spec)
	if err != nil {
		t.Fatal(err)
	}
	if spec, err = decodeAppSpecString(yaml); err != nil {
		t.Fatalf("packed spec does not load: %v\n%s", err, yaml)
	}
	regenerated, err := appgen.RenderAppSpecFiles(spec)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := allScreenContent(regenerated), allScreenContent(original); got != want {
		t.Fatalf("packed node blocks regenerate differently:\nGOT:\n%s\nWANT:\n%s", got, want)
	}
}

// The generator draws a line chart as appUI.LineChart(ctx, entity,
// group_by); pack read only the older lineChart helper and dropped it.
func TestPackKeepsLineChart(t *testing.T) {
	for _, src := range []string{
		`appUI.LineChart(ctx, "cards", "order")`,
		`ui.Card(ui.CardConfig{Heading: "Orders"}, appUI.LineChart(ctx, "cards", "order"))`,
	} {
		e, err := parser.ParseExpr(src)
		if err != nil {
			t.Fatal(err)
		}
		b, ok := reverseBlock(e, nil)
		if !ok || b.Kind != "line_chart" {
			t.Fatalf("%s: reverseBlock = %+v, %v; want a line_chart", src, b, ok)
		}
		source, _ := b.Props["source"].(map[string]any)
		if source["entity"] != "cards" || source["group_by"] != "order" {
			t.Fatalf("%s: source = %#v", src, b.Props["source"])
		}
	}
}
