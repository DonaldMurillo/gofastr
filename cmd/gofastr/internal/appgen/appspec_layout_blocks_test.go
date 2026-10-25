package appgen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

// The html import was read off the emitted text, string literals
// included, so a callout whose copy said "html.Div" imported core-ui/html
// into a screen that never called it, and the app did not compile.
func TestHTMLImportIgnoresCopy(t *testing.T) {
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{Name: "Copy", Module: "example.com/copy", DBDriver: "sqlite", DBURL: "copy.db"},
		Screens: []appspec.AppSpecScreen{{Name: "home", Route: "/", Title: "Copy", Body: []appspec.AppSpecBlock{
			{Kind: "callout", Text: "Compose with html.Div and ui.Stack.", Props: map[string]any{"title": "Tip"}},
		}}},
	}
	for _, f := range mustRenderAppSpecFiles(t, bp) {
		if !strings.HasPrefix(f.Name, "screen_") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), f.Name, f.Content, 0)
		if err != nil {
			t.Fatalf("%s: %v", f.Name, err)
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
			t.Errorf("%s: imports core-ui/html=%t but calls it=%t:\n%s", f.Name, imported, used, f.Content)
		}
	}
}

// The app shell's sidebar footer pairs the account action with the icon
// theme toggle in one row, and the login card's submit spans the card.
// Without a dark palette there is no toggle, and the footer is the
// action alone.
func TestAppShellFooterAndAuthSubmit(t *testing.T) {
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{Name: "Shell", Module: "example.com/shell", DBDriver: "sqlite", DBURL: "shell.db",
			Auth: appspec.AppSpecAuth{Enabled: true}, ThemeDark: map[string]string{"primary": "#a5b4fc"}},
		Nav: []appspec.AppSpecNavItem{{Label: "Dash", Href: "/app"}},
		Screens: []appspec.AppSpecScreen{
			{Name: "login", Route: "/login", Body: []appspec.AppSpecBlock{{Kind: "login_form", Props: map[string]any{"action": "/auth/login"}}}},
			{Name: "dash", Route: "/app", Layout: "app", Body: []appspec.AppSpecBlock{{Kind: "card", Props: map[string]any{"heading": "One"}}}},
		},
	}
	all := func(bp appspec.AppSpec) string {
		var sb strings.Builder
		for _, f := range mustRenderAppSpecFiles(t, bp) {
			sb.WriteString(f.Content)
		}
		return sb.String()
	}
	got := all(bp)
	for _, want := range []string{
		"SubmitFullWidth: true}",
		"cfg.Footer = ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter, Justify: ui.JustifyBetween, NoWrap: true}, authAction, ui.ThemeToggle(ui.ThemeToggleConfig{Ctx: ctx, Variant: ui.ThemeToggleIcon}))",
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
