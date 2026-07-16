package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestBlueprintLayoutBlocksRenderAndPack(t *testing.T) {
	body := []BlueprintBlock{{
		Kind: "stack", Props: map[string]any{"gap": "xl"}, Children: []BlueprintBlock{
			{Kind: "cluster", Props: map[string]any{"gap": "sm", "align": "center", "justify": "between", "no_wrap": true}, Children: []BlueprintBlock{
				{Kind: "link_button", Props: map[string]any{"label": "Docs", "href": "/docs", "variant": "primary"}},
			}},
			{Kind: "grid", Props: map[string]any{"min": "14rem", "gap": "lg"}, Children: []BlueprintBlock{
				{Kind: "card", Props: map[string]any{"heading": "One"}},
				{Kind: "card", Props: map[string]any{"heading": "Two"}},
			}},
		},
	}}
	bp := Blueprint{
		App:     BlueprintApp{Name: "Layouts", Module: "example.com/layouts", DBDriver: "sqlite", DBURL: "layouts.db"},
		Screens: []BlueprintScreen{{Name: "home", Route: "/", Title: "Layouts", Body: body}},
	}
	if err := validateBlueprint(bp); err != nil {
		t.Fatalf("validate layout blocks: %v", err)
	}
	files, err := renderBlueprintFiles(bp)
	if err != nil {
		t.Fatalf("render layout blocks: %v", err)
	}
	generated := allScreenContent(files)
	for _, want := range []string{
		"ui.Stack(ui.StackConfig{Gap: ui.GapXL",
		"ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter, Justify: ui.JustifyBetween, NoWrap: true}",
		"ui.Grid(ui.GridConfig{Min: \"14rem\", Gap: ui.GapLG}",
	} {
		if !strings.Contains(generated, want) {
			t.Fatalf("generated screen missing %q:\n%s", want, generated)
		}
	}

	dir := materializeBlueprint(t, bp)
	packed, err := packReadScreens(dir)
	if err != nil {
		t.Fatalf("pack screens: %v", err)
	}
	if len(packed) != 1 || !reflect.DeepEqual(packed[0].Body, body) {
		t.Fatalf("layout blocks did not round-trip:\nwant=%#v\ngot=%#v", body, packed[0].Body)
	}
}

func TestBlueprintLayoutBlocksRejectNonTokenGap(t *testing.T) {
	bp := Blueprint{Screens: []BlueprintScreen{{Name: "home", Route: "/", Body: []BlueprintBlock{{
		Kind: "stack", Props: map[string]any{"gap": "23px"},
	}}}}}
	err := validateBlueprint(bp)
	if err == nil || !strings.Contains(err.Error(), "not a design token") {
		t.Fatalf("bad layout gap error = %v", err)
	}
}
