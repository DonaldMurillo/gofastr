package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// The html import was read off the emitted text, string literals
// included, so a callout whose copy said "html.Div" imported core-ui/html
// into a screen that never called it, and the app did not compile.
func TestHTMLImportIgnoresCopy(t *testing.T) {
	bp := Blueprint{
		App: BlueprintApp{Name: "Copy", Module: "example.com/copy", DBDriver: "sqlite", DBURL: "copy.db"},
		Screens: []BlueprintScreen{{Name: "home", Route: "/", Title: "Copy", Body: []BlueprintBlock{
			{Kind: "callout", Text: "Compose with html.Div and ui.Stack.", Props: map[string]any{"title": "Tip"}},
		}}},
	}
	for _, f := range mustRenderBlueprintFiles(t, bp) {
		if !strings.HasPrefix(f.name, "screen_") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), f.name, f.content, 0)
		if err != nil {
			t.Fatalf("%s: %v", f.name, err)
		}
		imported, used := false, false
		for _, imp := range file.Imports {
			imported = imported || imp.Path.Value == `"github.com/DonaldMurillo/gofastr/core-ui/html"`
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "html" {
					used = true
				}
			}
			return true
		})
		if imported != used {
			t.Errorf("%s: imports core-ui/html=%t but calls it=%t:\n%s", f.name, imported, used, f.content)
		}
	}
}

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

// A stack with no align prop was emitted as Align: ui.AlignStart, so
// every child shrank to its content: a pricing grid inside one sized
// itself to a single card and the plans stacked down a wide page
// (caught on a generated /pricing). An unset prop leaves the
// component's default (stretch); an explicit start still round-trips.
func TestBlueprintStackAlignDefaults(t *testing.T) {
	body := []BlueprintBlock{
		{Kind: "stack", Props: map[string]any{"gap": "xl"}, Children: []BlueprintBlock{
			{Kind: "card", Props: map[string]any{"heading": "Fills"}},
		}},
		{Kind: "stack", Props: map[string]any{"align": "start", "justify": "start"}, Children: []BlueprintBlock{
			{Kind: "card", Props: map[string]any{"heading": "Shrinks"}},
		}},
	}
	bp := Blueprint{
		App:     BlueprintApp{Name: "Layouts", Module: "example.com/layouts", DBDriver: "sqlite", DBURL: "layouts.db"},
		Screens: []BlueprintScreen{{Name: "home", Route: "/", Title: "Layouts", Body: body}},
	}
	files, err := renderBlueprintFiles(bp)
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
	packed, err := packReadScreens(materializeBlueprint(t, bp))
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
	bp := Blueprint{
		App: BlueprintApp{Name: "Acts", Module: "example.com/acts", DBDriver: "sqlite", DBURL: "acts.db"},
		Screens: []BlueprintScreen{{Name: "home", Route: "/", Title: "Acts", Body: []BlueprintBlock{
			{Type: "heading", Level: 2, Text: "Posts", Actions: []BlueprintAction{{Name: "save", Event: "click", ClientJS: "x()"}}},
			{Kind: "card", Props: map[string]any{"heading": "One"}},
		}}},
	}
	dir := materializeBlueprint(t, bp)
	src, err := os.ReadFile(filepath.Join(dir, "screen_home.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "html.Div(") || !strings.Contains(string(src), blueprintScreenStackOpen) {
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

// The app shell's sidebar footer pairs the account action with the icon
// theme toggle in one row, and the login card's submit spans the card.
// Without a dark palette there is no toggle, and the footer is the
// action alone.
func TestAppShellFooterAndAuthSubmit(t *testing.T) {
	bp := Blueprint{
		App: BlueprintApp{Name: "Shell", Module: "example.com/shell", DBDriver: "sqlite", DBURL: "shell.db",
			Auth: BlueprintAuth{Enabled: true}, ThemeDark: map[string]string{"primary": "#a5b4fc"}},
		Nav: []BlueprintNavItem{{Label: "Dash", Href: "/app"}},
		Screens: []BlueprintScreen{
			{Name: "login", Route: "/login", Body: []BlueprintBlock{{Kind: "login_form", Props: map[string]any{"action": "/auth/login"}}}},
			{Name: "dash", Route: "/app", Layout: "app", Body: []BlueprintBlock{{Kind: "card", Props: map[string]any{"heading": "One"}}}},
		},
	}
	all := func(bp Blueprint) string {
		var sb strings.Builder
		for _, f := range mustRenderBlueprintFiles(t, bp) {
			sb.WriteString(f.content)
		}
		return sb.String()
	}
	got := all(bp)
	for _, want := range []string{
		"SubmitFullWidth: true}",
		"cfg.Footer = ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter, Justify: ui.JustifyBetween, NoWrap: true}, authAction, ui.ThemeToggle(ui.ThemeToggleConfig{Variant: ui.ThemeToggleIcon}))",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated app missing %q", want)
		}
	}
	bp.App.ThemeDark = nil
	if got := all(bp); !strings.Contains(got, "cfg.Footer = authAction\n") {
		t.Error("without a dark palette the sidebar footer must be the auth action alone")
	}
}

// An index screen whose first block is its entity list has no other
// heading, so the list title stays the page's h1; a list below another
// block drops to a section.
func TestListFirstKeepsPageHeading(t *testing.T) {
	bp := Blueprint{
		App:      BlueprintApp{Name: "Idx", Module: "example.com/idx", DBDriver: "sqlite", DBURL: "idx.db"},
		Entities: []framework.EntityDeclaration{{Name: "posts", Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}}},
		Screens: []BlueprintScreen{
			{Name: "index", Route: "/index", Title: "Posts", Body: []BlueprintBlock{{Kind: "entity_list", Entity: "posts"}}},
			{Name: "home", Route: "/", Title: "Home", Body: []BlueprintBlock{
				{Kind: "page_header", Props: map[string]any{"title": "Home"}},
				{Kind: "entity_list", Entity: "posts"},
			}},
			// No layout renders an <h1>, so a list under a block that has
			// none (a paragraph, or a stack wrapping the list) is the h1.
			{Name: "para", Route: "/para", Title: "Para", Body: []BlueprintBlock{
				{Type: "paragraph", Text: "Recent posts."},
				{Kind: "entity_list", Entity: "posts"},
			}},
			// An <h1> after the list does not demote it.
			{Name: "nested", Route: "/nested", Title: "Nested", Body: []BlueprintBlock{
				{Kind: "stack", Children: []BlueprintBlock{{Kind: "entity_list", Entity: "posts"}}},
				{Kind: "page_header", Props: map[string]any{"title": "Later"}},
			}},
			{Name: "titled", Route: "/titled", Title: "Titled", Body: []BlueprintBlock{
				{Type: "h1", Text: "Posts"},
				{Kind: "stack", Children: []BlueprintBlock{{Kind: "entity_list", Entity: "posts"}}},
			}},
		},
	}
	var all strings.Builder
	for _, src := range filesByName(mustRenderBlueprintFiles(t, bp)) {
		all.WriteString(src)
	}
	src := all.String()
	for screen, level2 := range map[string]bool{"index": false, "home": true, "para": false, "nested": false, "titled": true} {
		island := `.WithIsland("/tables/` + screen + `/posts")`
		h2 := strings.Contains(src, `WithHeadingLevel(2)`+island)
		if !h2 && !strings.Contains(src, island) {
			t.Fatalf("screen %s rendered no list:\n%s", screen, src)
		}
		if h2 != level2 {
			t.Errorf("screen %s: list at level 2 = %v, want %v (it drops only under an earlier <h1>)", screen, h2, level2)
		}
	}
}

// pack read any ui.Stack a screen returned as the generator's screen
// stack, so a hand-written root with its own Align packed to its children
// and the Align was lost. Only the generator's exact stack is unwrapped.
func TestPackKeepsHandWrittenRootStack(t *testing.T) {
	bp := Blueprint{
		App: BlueprintApp{Name: "Root", Module: "example.com/root", DBDriver: "sqlite", DBURL: "root.db"},
		Screens: []BlueprintScreen{{Name: "home", Route: "/", Title: "Root", Body: []BlueprintBlock{
			{Kind: "card", Props: map[string]any{"heading": "One"}},
		}}},
	}
	dir := materializeBlueprint(t, bp)
	path := filepath.Join(dir, "screen_home.go")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(src), blueprintScreenStackOpen, "ui.Stack(ui.StackConfig{Gap: ui.GapXL, Align: ui.AlignStart},", 1)
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
