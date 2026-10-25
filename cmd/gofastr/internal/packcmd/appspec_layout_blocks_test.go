package packcmd

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/framework"
)

func TestAppSpecLayoutBlocksRenderAndPack(t *testing.T) {
	body := []appspec.AppSpecBlock{{
		Kind: "stack", Props: map[string]any{"gap": "xl"}, Children: []appspec.AppSpecBlock{
			{Kind: "cluster", Props: map[string]any{"gap": "sm", "align": "center", "justify": "between", "no_wrap": true}, Children: []appspec.AppSpecBlock{
				{Kind: "link_button", Props: map[string]any{"label": "Docs", "href": "/docs", "variant": "primary"}},
			}},
			{Kind: "grid", Props: map[string]any{"min": "14rem", "gap": "lg"}, Children: []appspec.AppSpecBlock{
				{Kind: "card", Props: map[string]any{"heading": "One"}},
				{Kind: "card", Props: map[string]any{"heading": "Two"}},
			}},
		},
	}}
	bp := appspec.AppSpec{
		App:     appspec.AppSpecApp{Name: "Layouts", Module: "example.com/layouts", DBDriver: "sqlite", DBURL: "layouts.db"},
		Screens: []appspec.AppSpecScreen{{Name: "home", Route: "/", Title: "Layouts", Body: body}},
	}
	if err := appspec.ValidateAppSpec(bp); err != nil {
		t.Fatalf("validate layout blocks: %v", err)
	}
	files, err := appgen.RenderAppSpecFiles(bp)
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

	dir := materializeAppSpec(t, bp)
	packed, err := packReadScreens(dir)
	if err != nil {
		t.Fatalf("pack screens: %v", err)
	}
	if len(packed) != 1 || !reflect.DeepEqual(packed[0].Body, body) {
		t.Fatalf("layout blocks did not round-trip:\nwant=%#v\ngot=%#v", body, packed[0].Body)
	}
}

// A stack with no align prop was emitted as Align: ui.AlignStart, so
// every child shrank to its content: a pricing grid inside one sized
// itself to a single card and the plans stacked down a wide page
// (caught on a generated /pricing). An unset prop leaves the
// component's default (stretch); an explicit start still round-trips.
func TestAppSpecStackAlignDefaults(t *testing.T) {
	body := []appspec.AppSpecBlock{
		{Kind: "stack", Props: map[string]any{"gap": "xl"}, Children: []appspec.AppSpecBlock{
			{Kind: "card", Props: map[string]any{"heading": "Fills"}},
		}},
		{Kind: "stack", Props: map[string]any{"align": "start", "justify": "start"}, Children: []appspec.AppSpecBlock{
			{Kind: "card", Props: map[string]any{"heading": "Shrinks"}},
		}},
	}
	bp := appspec.AppSpec{
		App:     appspec.AppSpecApp{Name: "Layouts", Module: "example.com/layouts", DBDriver: "sqlite", DBURL: "layouts.db"},
		Screens: []appspec.AppSpecScreen{{Name: "home", Route: "/", Title: "Layouts", Body: body}},
	}
	files, err := appgen.RenderAppSpecFiles(bp)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	generated := allScreenContent(files)
	if !strings.Contains(generated, "ui.Stack(ui.StackConfig{Gap: ui.GapXL}") {
		t.Errorf("unset align/justify must leave the stack's defaults:\n%s", generated)
	}
	if !strings.Contains(generated, "Align: ui.AlignStart, Justify: ui.JustifyStart}") {
		t.Errorf("explicit start must be emitted:\n%s", generated)
	}
	packed, err := packReadScreens(materializeAppSpec(t, bp))
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	if len(packed) != 1 || !reflect.DeepEqual(packed[0].Body, body) {
		t.Fatalf("align props did not round-trip:\nwant=%#v\ngot=%#v", body, packed[0].Body)
	}
}

// A screen with actions wraps its stack in an html.Div that carries the
// action wiring. Packed without unwrapping it, the screen's blocks came
// back as one extra stack, and every round trip nested them deeper.
func TestPackUnwrapsActionsWrapper(t *testing.T) {
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{Name: "Acts", Module: "example.com/acts", DBDriver: "sqlite", DBURL: "acts.db"},
		Screens: []appspec.AppSpecScreen{{Name: "home", Route: "/", Title: "Acts", Body: []appspec.AppSpecBlock{
			{Type: "heading", Level: 2, Text: "Posts", Actions: []appspec.AppSpecAction{{Name: "save", Event: "click", ClientJS: "x()"}}},
			{Kind: "card", Props: map[string]any{"heading": "One"}},
		}}},
	}
	dir := materializeAppSpec(t, bp)
	src, err := os.ReadFile(filepath.Join(dir, "screen_home.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "html.Div(") || !strings.Contains(string(src), appgen.AppSpecScreenStackOpen) {
		t.Fatalf("screen does not wrap its stack, nothing to prove:\n%s", src)
	}
	packed, err := packReadScreens(dir)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	if len(packed) != 1 {
		t.Fatalf("packed %d screens, want 1", len(packed))
	}
	kinds := []string{}
	for _, b := range packed[0].Body {
		kinds = append(kinds, b.Kind)
	}
	if !slices.Contains(kinds, "card") || slices.Contains(kinds, "stack") {
		t.Errorf("screen with actions packed to top-level kinds %v; the card belongs at the top, not in a stack", kinds)
	}
}

func TestListFirstKeepsPageHeading(t *testing.T) {
	bp := appspec.AppSpec{
		App:      appspec.AppSpecApp{Name: "Idx", Module: "example.com/idx", DBDriver: "sqlite", DBURL: "idx.db"},
		Entities: []framework.EntityDeclaration{{Name: "posts", Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}}},
		Screens: []appspec.AppSpecScreen{
			{Name: "index", Route: "/index", Title: "Posts", Body: []appspec.AppSpecBlock{{Kind: "entity_list", Entity: "posts", Text: "Index list"}}},
			{Name: "home", Route: "/", Title: "Home", Body: []appspec.AppSpecBlock{
				{Kind: "page_header", Props: map[string]any{"title": "Home"}},
				{Kind: "entity_list", Entity: "posts", Text: "Home list"},
			}},
			// No layout renders an <h1>, so a list under a block that has
			// none (a paragraph, or a stack wrapping the list) is the h1.
			{Name: "para", Route: "/para", Title: "Para", Body: []appspec.AppSpecBlock{
				{Type: "paragraph", Text: "Recent posts."},
				{Kind: "entity_list", Entity: "posts", Text: "Para list"},
			}},
			// An <h1> after the list does not demote it.
			{Name: "nested", Route: "/nested", Title: "Nested", Body: []appspec.AppSpecBlock{
				{Kind: "stack", Children: []appspec.AppSpecBlock{{Kind: "entity_list", Entity: "posts", Text: "Nested list"}}},
				{Kind: "page_header", Props: map[string]any{"title": "Later"}},
			}},
			{Name: "titled", Route: "/titled", Title: "Titled", Body: []appspec.AppSpecBlock{
				{Type: "h1", Text: "Posts"},
				{Kind: "stack", Children: []appspec.AppSpecBlock{{Kind: "entity_list", Entity: "posts", Text: "Titled list"}}},
			}},
		},
	}
	var all strings.Builder
	for _, src := range filesByName(mustRenderAppSpecFiles(t, bp)) {
		all.WriteString(inlinePhrases(src))
	}
	src := all.String()
	// The level is part of the heading call: presence of the exact string
	// is the assertion (it drops only under an earlier <h1>).
	for _, heading := range []string{
		`.Heading("Index list", 1)`,
		`.Heading("Home list", 2)`,
		`.Heading("Para list", 1)`,
		`.Heading("Nested list", 1)`,
		`.Heading("Titled list", 2)`,
	} {
		if !strings.Contains(src, heading) {
			t.Errorf("list heading %s not emitted:\n%s", heading, src)
		}
	}
	// Level is bound to the heading call: no bare level override exists.
	for _, banned := range []string{"HeadingLevel", "WithHeading"} {
		if strings.Contains(src, banned) {
			t.Errorf("emitted screens still use the resource engine's %s", banned)
		}
	}
}

// pack read any ui.Stack a screen returned as the generator's screen
// stack, so a hand-written root with its own Align packed to its children
// and the Align was lost. Only the generator's exact stack is unwrapped.
func TestPackKeepsHandWrittenRootStack(t *testing.T) {
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{Name: "Root", Module: "example.com/root", DBDriver: "sqlite", DBURL: "root.db"},
		Screens: []appspec.AppSpecScreen{{Name: "home", Route: "/", Title: "Root", Body: []appspec.AppSpecBlock{
			{Kind: "card", Props: map[string]any{"heading": "One"}},
		}}},
	}
	dir := materializeAppSpec(t, bp)
	path := filepath.Join(dir, "screen_home.go")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(src), appgen.AppSpecScreenStackOpen, "ui.Stack(ui.StackConfig{Gap: ui.GapXL, Align: ui.AlignStart},", 1)
	if edited == string(src) {
		t.Fatalf("screen stack not found in screen_home.go:\n%s", src)
	}
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	packed, err := packReadScreens(dir)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	for _, s := range packed {
		if len(s.Body) > 0 {
			t.Errorf("screen %q packed to %d blocks; its hand-written root stack would lose Align", s.Name, len(s.Body))
		}
	}
}
